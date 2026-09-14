const std = @import("std");
const testing = std.testing;
const skg = @import("root.zig");
const native = skg.native;

test "native structs maps lists defaults enums and nullable records" {
    const Record = struct { id: u16, note: ?[]const u8 };
    const Config = struct {
        name: []const u8,
        enabled: bool = true,
        mode: enum { local, remote } = .local,
        headers: std.StringHashMap([]const u8),
        records: []?Record,
        matrix: [2][]const i32,
        pub const skg_fields = .{ .name = "service.name" };
    };
    var result = skg.decodeSource(Config, testing.allocator,
        \\"service.name": "demo"
        \\mode: "remote"
        \\headers { "Content-Type": "json" }
        \\records [null { id: 3 note: null }]
        \\matrix: [[1, 2], [3]]
        \\unknown { ignored: true }
    , "config.skg", .{});
    defer result.deinit();
    const value = result.value orelse return error.DecodeFailed;
    try testing.expectEqualStrings("demo", value.name);
    try testing.expect(value.enabled);
    try testing.expectEqual(.remote, value.mode);
    try testing.expectEqualStrings("json", value.headers.get("Content-Type").?);
    try testing.expect(value.records[0] == null);
    try testing.expectEqual(@as(u16, 3), value.records[1].?.id);
    try testing.expect(value.records[1].?.note == null);
    try testing.expectEqualSlices(i32, &.{ 1, 2 }, value.matrix[0]);
}

test "native managed map allocator survives result movement and source release" {
    const Config = struct { map: std.StringHashMap(i64) };
    const src = try testing.allocator.dupe(u8, "map { a: 1 }");
    var result = skg.decodeSource(Config, testing.allocator, src, "map.skg", .{});
    testing.allocator.free(src);
    defer result.deinit();
    try testing.expect(result.value != null);
    for (0..100) |i| {
        const key = try std.fmt.allocPrint(result.arena.?.allocator(), "key{d}", .{i});
        try result.value.?.map.put(key, @intCast(i));
    }
    try testing.expectEqual(@as(i64, 99), result.value.?.map.get("key99").?);
}

test "native defaults are copied optional absent and ignored native fields" {
    const Config = struct {
        name: []const u8 = "default",
        optional: ?i32,
        local_only: u8 = 7,
        pub const skg_fields = .{ .local_only = null };
    };
    var result = skg.decodeSource(Config, testing.allocator, "local_only: 9", "defaults.skg", .{});
    defer result.deinit();
    const value = result.value orelse return error.DecodeFailed;
    try testing.expectEqualStrings("default", value.name);
    try testing.expect(value.optional == null);
    try testing.expectEqual(@as(u8, 7), value.local_only);
}

test "native errors include escaped field paths and source positions" {
    const Config = struct {
        values: []u8,
        pub const skg_fields = .{ .values = "a/b~c" };
    };
    var result = skg.decodeSource(Config, testing.allocator, "\"a/b~c\": [1, 256]", "range.skg", .{});
    defer result.deinit();
    try testing.expect(result.value == null);
    const d = result.diagnostic orelse return error.NoDiagnostic;
    try testing.expectEqual(native.Code.number_out_of_range, d.code);
    try testing.expectEqualStrings("/a~1b~0c/1", d.field_path);
    try testing.expectEqualStrings("range.skg", d.source.path);
    try testing.expectEqual(@as(u32, 1), d.source.line);
    try testing.expectEqual(@as(u32, 1), d.source.col);
}

test "native missing null wrong type unknown enum and strict unknowns" {
    const Required = struct { count: u8 };
    const cases = [_]struct { src: []const u8, code: native.Code }{
        .{ .src = "", .code = .missing_field },
        .{ .src = "count: null", .code = .type_mismatch },
        .{ .src = "count: -1", .code = .number_out_of_range },
        .{ .src = "count: \"3\"", .code = .type_mismatch },
    };
    for (cases) |case| {
        var result = skg.decodeSource(Required, testing.allocator, case.src, "bad.skg", .{});
        defer result.deinit();
        try testing.expect(result.value == null);
        try testing.expectEqual(case.code, result.diagnostic.?.code);
    }
    const Config = struct { mode: enum { local } };
    var bad_enum = skg.decodeSource(Config, testing.allocator, "mode: \"remote\"", "", .{});
    defer bad_enum.deinit();
    try testing.expectEqual(native.Code.invalid_enum, bad_enum.diagnostic.?.code);
    var unknown = skg.decodeSource(Required, testing.allocator, "count: 1 extra: true", "", .{ .unknown_fields = .reject });
    defer unknown.deinit();
    try testing.expectEqual(native.Code.unknown_field, unknown.diagnostic.?.code);
    try testing.expectEqualStrings("/extra", unknown.diagnostic.?.field_path);
}

test "native exact numeric conversion with explicit lossy opt in" {
    const Float32 = struct { value: f32 };
    var exact = skg.decodeSource(Float32, testing.allocator, "value: 0.5", "", .{});
    defer exact.deinit();
    try testing.expectEqual(@as(f32, 0.5), exact.value.?.value);
    var inexact = skg.decodeSource(Float32, testing.allocator, "value: 0.1", "", .{});
    defer inexact.deinit();
    try testing.expectEqual(native.Code.inexact_number, inexact.diagnostic.?.code);
    var lossy = skg.decodeSource(Float32, testing.allocator, "value: 0.1", "", .{ .allow_lossy_numbers = true });
    defer lossy.deinit();
    try testing.expectEqual(@as(f32, 0.1), lossy.value.?.value);
    const Float64 = struct { value: f64 };
    var max_int = skg.decodeSource(Float64, testing.allocator, "value: 9223372036854775807", "", .{});
    defer max_int.deinit();
    try testing.expectEqual(native.Code.inexact_number, max_int.diagnostic.?.code);
    var min_int = skg.decodeSource(Float64, testing.allocator, "value: -9223372036854775808", "", .{});
    defer min_int.deinit();
    try testing.expect(min_int.value != null);
}

test "native pointers sentinel strings unmanaged maps dynamic values" {
    const Config = struct {
        text: [:0]const u8,
        record: *const struct { id: i64 },
        map: std.StringHashMapUnmanaged(?i32),
        dynamic: skg.Value,
    };
    var result = skg.decodeSource(Config, testing.allocator, "text: \"hello\" record { id: 2 } map { a: null b: 3 } dynamic: [[{x: 1}, null]]", "", .{});
    defer result.deinit();
    const value = result.value orelse return error.DecodeFailed;
    try testing.expectEqual(@as(u8, 0), value.text[value.text.len]);
    try testing.expectEqual(@as(i64, 2), value.record.id);
    try testing.expect(value.map.get("a").? == null);
    try testing.expectEqual(@as(i32, 3), value.map.get("b").?.?);
    try testing.expect(value.dynamic == .array);
}

test "native hooks decode and validate with local diagnostics" {
    const Port = struct {
        value: u16,
        pub fn skgDecode(ctx: *native.Context, input: skg.Value) native.Error!@This() {
            return .{ .value = try ctx.decode(u16, input) };
        }
        pub fn skgValidate(self: @This(), ctx: *native.Context) native.Error!void {
            if (self.value == 0) return ctx.fail(.custom_error, "port must be nonzero");
        }
    };
    const Config = struct { port: Port };
    var good = skg.decodeSource(Config, testing.allocator, "port: 8080", "", .{});
    defer good.deinit();
    try testing.expectEqual(@as(u16, 8080), good.value.?.port.value);
    var bad = skg.decodeSource(Config, testing.allocator, "port: 0", "", .{});
    defer bad.deinit();
    try testing.expectEqualStrings("/port", bad.diagnostic.?.field_path);
    try testing.expectEqualStrings("port must be nonzero", bad.diagnostic.?.message);
}

test "native import source provenance and overlay finalization" {
    var dir = testing.tmpDir(.{});
    defer dir.cleanup();
    try dir.dir.writeFile(.{ .sub_path = "base.skg", .data = "count: 256\nremoved: 1" });
    try dir.dir.writeFile(.{ .sub_path = "main.skg", .data = "import \"base.skg\"\n@delete removed" });
    const path = try dir.dir.realpathAlloc(testing.allocator, "main.skg");
    defer testing.allocator.free(path);
    const Config = struct { count: u8 };
    var result = skg.decodeFile(Config, testing.allocator, path, .{});
    defer result.deinit();
    try testing.expectEqual(native.Code.number_out_of_range, result.diagnostic.?.code);
    try testing.expectEqualStrings("base.skg", std.fs.path.basename(result.diagnostic.?.source.path));
    const Wide = struct { count: u16 };
    var wide = skg.decodeFile(Wide, testing.allocator, path, .{ .unknown_fields = .reject });
    defer wide.deinit();
    try testing.expectEqual(@as(u16, 256), wide.value.?.count);
}

fn allocationCase(allocator: std.mem.Allocator) !void {
    const Config = struct { name: []const u8, map: std.StringHashMap([]?u16) };
    var result = skg.decodeSource(Config, allocator, "name: \"a\" map { items: [1, null, 3] }", "memory.skg", .{});
    defer result.deinit();
    if (result.value == null) {
        if (result.diagnostic) |d| {
            if (d.code == .out_of_memory) return error.OutOfMemory;
        }
        return error.UnexpectedDecodeFailure;
    }
}

test "native allocation failures clean up every owned allocation" {
    try testing.checkAllAllocationFailures(testing.allocator, allocationCase, .{});
}

test "native parse failures keep parser diagnostics and no partial result" {
    const Config = struct { value: i32 };
    var result = skg.decodeSource(Config, testing.allocator, "value: [", "broken.skg", .{});
    defer result.deinit();
    try testing.expect(result.value == null);
    const d = result.diagnostic.?;
    try testing.expectEqual(native.Code.parse_error, d.code);
    try testing.expectEqual(skg.ast.ErrorCode.UNTERMINATED_ARRAY, d.parse_diagnostic.?.code);
    try testing.expectEqualStrings("broken.skg", d.source.path);
}

test "native recursive hooks are bounded" {
    const Recursive = struct {
        pub fn skgDecode(ctx: *native.Context, value: skg.Value) native.Error!@This() {
            return ctx.decode(@This(), value);
        }
    };
    var result = skg.decodeSource(Recursive, testing.allocator, "", "", .{});
    defer result.deinit();
    try testing.expect(result.value == null);
    try testing.expectEqual(native.Code.nesting_too_deep, result.diagnostic.?.code);
}

test "native mapping errors and fixed array mismatch" {
    const Duplicate = struct {
        a: i32 = 0,
        b: i32 = 0,
        pub const skg_fields = .{ .b = "a" };
    };
    var duplicate = skg.decodeSource(Duplicate, testing.allocator, "", "", .{});
    defer duplicate.deinit();
    try testing.expectEqual(native.Code.unsupported_type, duplicate.diagnostic.?.code);
    const Typo = struct {
        a: i32 = 0,
        pub const skg_fields = .{ .typo = "name" };
    };
    var typo = skg.decodeSource(Typo, testing.allocator, "", "", .{});
    defer typo.deinit();
    try testing.expectEqual(native.Code.unsupported_type, typo.diagnostic.?.code);
    const Fixed = struct { items: [2]u8 };
    var fixed = skg.decodeSource(Fixed, testing.allocator, "items: [1]", "", .{});
    defer fixed.deinit();
    try testing.expectEqual(native.Code.type_mismatch, fixed.diagnostic.?.code);
}

test "native byte loading ignores filesystem imports" {
    const Config = struct { name: []const u8 };
    var result = skg.decodeSource(Config, testing.allocator, "import \"does-not-exist.skg\" name: \"local\"", "local.skg", .{});
    defer result.deinit();
    try testing.expectEqualStrings("local", result.value.?.name);
}

test "native float overflow remains an error with lossy conversion enabled" {
    const Config = struct { value: f32 };
    var result = skg.decodeSource(Config, testing.allocator, "value: 10000000000000000000000000000000000000000.0", "", .{ .allow_lossy_numbers = true });
    defer result.deinit();
    try testing.expect(result.value == null);
    try testing.expectEqual(native.Code.number_out_of_range, result.diagnostic.?.code);
}

test "native mutable defaults are independently owned" {
    const Config = struct { text: []const u8 = "abc", numbers: []const u8 = &.{ 1, 2 } };
    var a = skg.decodeSource(Config, testing.allocator, "", "", .{});
    defer a.deinit();
    var b = skg.decodeSource(Config, testing.allocator, "", "", .{});
    defer b.deinit();
    try testing.expect(a.value.?.text.ptr != b.value.?.text.ptr);
    try testing.expect(a.value.?.numbers.ptr != b.value.?.numbers.ptr);
}

test "native byte arrays are range checked and no failed value escapes" {
    const Config = struct { text: []const u8, later: u8 };
    var result = skg.decodeSource(Config, testing.allocator, "text: [65, 66] later: 256", "", .{});
    defer result.deinit();
    try testing.expect(result.value == null);
    try testing.expectEqualStrings("/later", result.diagnostic.?.field_path);
}
