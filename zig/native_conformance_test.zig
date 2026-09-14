//! Shared native mappings. Each profile is an ordinary native type, not a
//! schema interpreter. New ports bind the same profiles and run every case.
const std = @import("std");
const testing = std.testing;
const skg = @import("root.zig");

const Profile = enum { bool, u8, optional_u8, default_u8, i64, f32, f64, string, bytes, array2_u8, record, list_optional_records, map_optional_u8, port, @"enum" };
const Fixture = struct {
    name: []const u8,
    profile: Profile,
    source: []const u8,
    options: struct { reject_unknown_fields: bool = false, allow_lossy_numbers: bool = false } = .{},
    expected: std.json.Value = .null,
    @"error": ?struct { code: skg.native.Code, field_path: []const u8 } = null,
};
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

test "shared native conformance profiles" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    const data = try std.fs.cwd().readFileAlloc(allocator, "testdata/native/cases.json", 1024 * 1024);
    const raw = try std.json.parseFromSlice(std.json.Value, allocator, data, .{});
    const Suite = struct { contract_version: u32, language_version: []const u8, cases: []Fixture };
    // Closed typed structures reject misspelled properties and profile names.
    const suite = (try std.json.parseFromValue(Suite, allocator, raw.value, .{})).value;
    try testing.expectEqual(@as(u32, 1), suite.contract_version);
    try testing.expectEqualStrings("1.0", suite.language_version);
    try testing.expect(suite.cases.len > 0);
    var names = std.StringHashMap(void).init(allocator);
    for (suite.cases, raw.value.object.get("cases").?.array.items) |fixture, original| {
        try testing.expect(fixture.name.len > 0);
        try testing.expect(!names.contains(fixture.name));
        try names.put(fixture.name, {});
        try testing.expect(original.object.contains("expected") != original.object.contains("error"));
        if (original.object.contains("error")) try testing.expect(fixture.@"error" != null);
        runFixture(fixture) catch |err| {
            std.debug.print("native fixture {s} failed: {}\n", .{ fixture.name, err });
            return err;
        };
    }
    std.debug.print("native contract v{d} for SKG {s}: {d} cases, none skipped\n", .{ suite.contract_version, suite.language_version, suite.cases.len });
}

fn runFixture(fixture: Fixture) !void {
    switch (fixture.profile) {
        .bool => try check(bool, fixture),
        .u8 => try check(u8, fixture),
        .optional_u8 => try check(?u8, fixture),
        .default_u8 => try checkTarget(struct { value: u8 = 7 }, fixture),
        .i64 => try check(i64, fixture),
        .f32 => try check(f32, fixture),
        .f64 => try check(f64, fixture),
        .string => try check([]const u8, fixture),
        .bytes => try check(?[]const u8, fixture),
        .array2_u8 => try check([2]u8, fixture),
        .record => try check(Record, fixture),
        .list_optional_records => try check(?[]?Record, fixture),
        .map_optional_u8 => try check(?std.StringHashMap(?u8), fixture),
        .port => try check(Port, fixture),
        .@"enum" => try check(enum { local, remote }, fixture),
    }
}
fn check(comptime T: type, fixture: Fixture) !void {
    try checkTarget(struct { value: T }, fixture);
}
fn checkTarget(comptime T: type, fixture: Fixture) !void {
    var result = skg.decodeSource(T, testing.allocator, fixture.source, fixture.name, .{
        .unknown_fields = if (fixture.options.reject_unknown_fields) .reject else .ignore,
        .allow_lossy_numbers = fixture.options.allow_lossy_numbers,
    });
    defer result.deinit();
    if (fixture.@"error") |expected| {
        try testing.expect(result.value == null);
        const diagnostic = result.diagnostic orelse return error.MissingDiagnostic;
        try testing.expectEqual(expected.code, diagnostic.code);
        try testing.expectEqualStrings(expected.field_path, diagnostic.field_path);
        return;
    }
    if (result.diagnostic) |d| {
        std.debug.print("unexpected {s}: {s} at {s}\n", .{ @tagName(d.code), d.message, d.field_path });
        return error.UnexpectedDiagnostic;
    }
    try compareJSON(result.value.?.value, fixture.expected);
}

fn compareJSON(actual: anytype, expected: std.json.Value) anyerror!void {
    const T = @TypeOf(actual);
    if (T == Port) return compareJSON(actual.value, expected);
    if (T == std.StringHashMap(?u8)) {
        try testing.expect(expected == .object);
        try testing.expectEqual(expected.object.count(), actual.count());
        var it = expected.object.iterator();
        while (it.next()) |entry| {
            const value = actual.get(entry.key_ptr.*) orelse return error.MissingMapKey;
            try compareJSON(value, entry.value_ptr.*);
        }
        return;
    }
    switch (@typeInfo(T)) {
        .bool => {
            try testing.expect(expected == .bool);
            try testing.expectEqual(expected.bool, actual);
        },
        .optional => {
            if (actual) |value| return compareJSON(value, expected);
            try testing.expect(expected == .null);
        },
        .int => {
            try testing.expect(expected == .integer);
            try testing.expectEqual(@as(i64, @intCast(actual)), expected.integer);
        },
        .float => {
            const value: f64 = switch (expected) {
                .float => |f| f,
                .integer => |i| @floatFromInt(i),
                else => return error.ExpectedNumber,
            };
            try testing.expectEqual(value, @as(f64, @floatCast(actual)));
        },
        .@"enum" => {
            try testing.expect(expected == .string);
            try testing.expectEqualStrings(expected.string, @tagName(actual));
        },
        .pointer, .array => {
            if (comptime @typeInfo(T) == .pointer and @typeInfo(T).pointer.child == u8) {
                if (expected == .string) return testing.expectEqualStrings(expected.string, actual);
            }
            try testing.expect(expected == .array);
            try testing.expectEqual(expected.array.items.len, actual.len);
            for (actual, expected.array.items) |item, value| try compareJSON(item, value);
        },
        .@"struct" => |info| {
            try testing.expect(expected == .object);
            try testing.expectEqual(info.fields.len, expected.object.count());
            inline for (info.fields) |field| {
                const value = expected.object.get(field.name) orelse return error.MissingField;
                try compareJSON(@field(actual, field.name), value);
            }
        },
        else => @compileError("unimplemented native fixture comparison"),
    }
}
