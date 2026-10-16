//! The Zig implementation's parity harness. It speaks the protocol described
//! in tools/parity/README.md: read a JSON case file, run every case through the
//! public Zig API, and write one JSON result per line.
const std = @import("std");
const skg = @import("skg");

const Case = struct {
    id: []const u8,
    path: []const u8 = "",
    entry: []const u8 = "",
    root: []const u8 = "",
    max_bytes: usize = 0,
    max_files: usize = 0,
    max_nodes: usize = 0,
    max_merge_work: usize = 0,
    profile: []const u8 = "",
    source: []const u8 = "",
    reject_unknown_fields: bool = false,
    allow_lossy_numbers: bool = false,
};
const CaseFile = struct { mode: []const u8, cases: []Case };

pub fn main() !void {
    var gpa: std.heap.DebugAllocator(.{}) = .init;
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();
    const args = try std.process.argsAlloc(allocator);
    defer std.process.argsFree(allocator, args);
    if (args.len != 3) {
        std.debug.print("usage: skg-parity <cases.json> <results.jsonl>\n", .{});
        std.process.exit(2);
    }
    const data = try std.fs.cwd().readFileAlloc(allocator, args[1], 1 << 30);
    defer allocator.free(data);
    const parsed = try std.json.parseFromSlice(CaseFile, allocator, data, .{});
    defer parsed.deinit();

    const out_file = try std.fs.cwd().createFile(args[2], .{});
    defer out_file.close();
    var buffer: [64 * 1024]u8 = undefined;
    var file_writer = out_file.writer(&buffer);
    const w = &file_writer.interface;

    for (parsed.value.cases) |case| {
        try w.writeAll("{\"id\":");
        try std.json.Stringify.value(case.id, .{}, w);
        if (std.mem.eql(u8, parsed.value.mode, "parse")) {
            const src = try std.fs.cwd().readFileAlloc(allocator, case.path, 1 << 30);
            defer allocator.free(src);
            var result = skg.parseSource(allocator, src, std.fs.path.basename(case.path));
            defer result.deinit();
            try writeParse(allocator, w, result);
        } else if (std.mem.eql(u8, parsed.value.mode, "resolve")) {
            var options: skg.ResolveOptions = .{ .root = if (case.root.len > 0) case.root else null };
            if (case.max_bytes != 0) options.max_bytes = case.max_bytes;
            if (case.max_files != 0) options.max_files = case.max_files;
            if (case.max_nodes != 0) options.max_nodes = case.max_nodes;
            if (case.max_merge_work != 0) options.max_merge_work = case.max_merge_work;
            var result = skg.parseWithOptions(allocator, case.entry, options);
            defer result.deinit();
            try writeParse(allocator, w, result);
        } else if (std.mem.eql(u8, parsed.value.mode, "native")) {
            try native(allocator, w, case);
        } else return error.UnknownMode;
        try w.writeAll("}\n");
    }
    try w.flush();
}

fn writeParse(allocator: std.mem.Allocator, w: *std.Io.Writer, result: skg.ParseResult) !void {
    if (result.file) |file| {
        const text = try skg.emit.emitFile(allocator, file);
        defer allocator.free(text);
        try w.writeAll(",\"ok\":true,\"emit\":");
        try std.json.Stringify.value(text, .{}, w);
        return;
    }
    const d = result.diagnostic orelse return error.OutOfMemory;
    try w.writeAll(",\"ok\":false,\"code\":");
    try std.json.Stringify.value(@tagName(d.code), .{}, w);
    try w.writeAll(",\"file\":");
    try std.json.Stringify.value(std.fs.path.basename(d.path), .{}, w);
    try w.print(",\"line\":{d},\"col\":{d}", .{ d.line, d.col });
}

// The shared native profiles, bound exactly as zig/native_conformance_test.zig
// binds them.
const Record = struct { id: u16, note: ?[]const u8 };
const Port = struct {
    value: u16,
    pub fn skgDecode(ctx: *skg.native.Context, value: skg.Value) skg.native.Error!@This() {
        return .{ .value = try ctx.decode(u16, value) };
    }
    pub fn skgValidate(self: @This(), ctx: *skg.native.Context) skg.native.Error!void {
        if (self.value == 0) return ctx.fail(.custom_error, "port must be nonzero");
    }
};
const Mode = enum { local, remote };

fn native(allocator: std.mem.Allocator, w: *std.Io.Writer, case: Case) !void {
    const options: skg.native.Options = .{
        .unknown_fields = if (case.reject_unknown_fields) .reject else .ignore,
        .allow_lossy_numbers = case.allow_lossy_numbers,
    };
    const profiles = .{
        .{ "bool", struct { value: bool } },
        .{ "u8", struct { value: u8 } },
        .{ "optional_u8", struct { value: ?u8 } },
        .{ "default_u8", struct { value: u8 = 7 } },
        .{ "i64", struct { value: i64 } },
        .{ "f32", struct { value: f32 } },
        .{ "f64", struct { value: f64 } },
        .{ "string", struct { value: []const u8 } },
        .{ "bytes", struct { value: ?[]const u8 } },
        .{ "array2_u8", struct { value: [2]u8 } },
        .{ "record", struct { value: Record } },
        .{ "list_optional_records", struct { value: ?[]?Record } },
        .{ "map_optional_u8", struct { value: ?std.StringHashMap(?u8) } },
        .{ "port", struct { value: Port } },
        .{ "enum", struct { value: Mode } },
    };
    inline for (profiles) |profile| {
        if (std.mem.eql(u8, case.profile, profile[0])) {
            var result = skg.decodeSource(profile[1], allocator, case.source, "case.skg", options);
            defer result.deinit();
            if (result.value) |value| {
                try w.writeAll(",\"ok\":true,\"value\":");
                try writeNative(w, value.value, comptime std.mem.eql(u8, profile[0], "bytes"));
            } else {
                const d = result.diagnostic orelse return error.OutOfMemory;
                try w.writeAll(",\"ok\":false,\"code\":");
                try std.json.Stringify.value(@tagName(d.code), .{}, w);
                try w.writeAll(",\"field_path\":");
                try std.json.Stringify.value(d.field_path, .{}, w);
            }
            return;
        }
    }
    return error.UnknownProfile;
}

/// Render a decoded value in the shared structural form: the bytes profile as
/// an integer array, other byte slices as strings, floats widened to binary64.
fn writeNative(w: *std.Io.Writer, value: anytype, comptime bytes: bool) !void {
    const T = @TypeOf(value);
    if (T == Port) return w.print("{d}", .{value.value});
    if (T == Record) {
        try w.print("{{\"id\":{d},\"note\":", .{value.id});
        try writeNative(w, value.note, false);
        return w.writeAll("}");
    }
    if (T == std.StringHashMap(?u8)) {
        try w.writeAll("{");
        var it = value.iterator();
        var first = true;
        while (it.next()) |entry| {
            if (!first) try w.writeAll(",");
            first = false;
            try std.json.Stringify.value(entry.key_ptr.*, .{}, w);
            try w.writeAll(":");
            try writeNative(w, entry.value_ptr.*, false);
        }
        return w.writeAll("}");
    }
    switch (@typeInfo(T)) {
        .bool => try w.writeAll(if (value) "true" else "false"),
        .int => try w.print("{d}", .{value}),
        .float => try w.print("{e}", .{@as(f64, @floatCast(value))}),
        .@"enum" => try std.json.Stringify.value(@tagName(value), .{}, w),
        .optional => if (value) |inner| try writeNative(w, inner, bytes) else try w.writeAll("null"),
        .pointer, .array => {
            if (comptime !bytes and @typeInfo(T) == .pointer and @typeInfo(T).pointer.child == u8)
                return std.json.Stringify.value(value, .{}, w);
            try w.writeAll("[");
            for (value, 0..) |item, i| {
                if (i > 0) try w.writeAll(",");
                try writeNative(w, item, false);
            }
            try w.writeAll("]");
        },
        else => @compileError("unsupported native value " ++ @typeName(T)),
    }
}
