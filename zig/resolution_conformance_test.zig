//! Shared import-resolution, containment, and aggregate-budget contract.
const std = @import("std");
const testing = std.testing;
const skg = @import("root.zig");

const Limits = struct {
    bytes: usize,
    files: usize,
    nodes: usize,
    merge_work: usize,
};

const ContractFile = struct {
    path: []const u8,
    source: []const u8,
};

const Fixture = struct {
    name: []const u8,
    entry: []const u8,
    root: ?[]const u8 = null,
    limits: Limits,
    files: []ContractFile,
    expected_formatted: ?[]const u8 = null,
    expected_code: ?skg.ast.ErrorCode = null,
};

const Suite = struct {
    contract_version: u32,
    language_version: []const u8,
    cases: []Fixture,
};

test "shared resolution conformance" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    const data = try std.fs.cwd().readFileAlloc(allocator, "testdata/resolution/cases.json", 1024 * 1024);
    const raw = try std.json.parseFromSlice(std.json.Value, allocator, data, .{});
    try validateContractShape(raw.value);
    const suite = (try std.json.parseFromValue(Suite, allocator, raw.value, .{})).value;

    try testing.expectEqual(@as(u32, 1), suite.contract_version);
    try testing.expectEqualStrings("1.0", suite.language_version);
    try testing.expect(suite.cases.len > 0);
    var names = std.StringHashMap(void).init(allocator);
    for (suite.cases) |fixture| {
        try testing.expect(fixture.name.len > 0);
        try testing.expect(!names.contains(fixture.name));
        try names.put(fixture.name, {});
        runFixture(fixture) catch |err| {
            std.debug.print("resolution fixture {s} failed: {}\n", .{ fixture.name, err });
            return err;
        };
    }
    std.debug.print("resolution contract v{d}: {d} cases, none skipped\n", .{ suite.contract_version, suite.cases.len });
}

fn validateContractShape(value: std.json.Value) !void {
    const root = try object(value);
    try fields(root, &.{ "contract_version", "language_version", "cases" }, &.{ "contract_version", "language_version", "cases" });
    const cases = switch (root.get("cases").?) {
        .array => |array| array.items,
        else => return error.BadResolutionContract,
    };
    for (cases) |case_value| {
        const case = try object(case_value);
        try fields(
            case,
            &.{ "name", "entry", "root", "limits", "files", "expected_formatted", "expected_code" },
            &.{ "name", "entry", "limits", "files" },
        );
        if (case.contains("expected_formatted") == case.contains("expected_code")) return error.BadResolutionContract;
        if (case.get("expected_formatted")) |expected| if (expected == .null) return error.BadResolutionContract;
        if (case.get("expected_code")) |expected| if (expected == .null) return error.BadResolutionContract;
        const limits = try object(case.get("limits").?);
        try fields(limits, &.{ "bytes", "files", "nodes", "merge_work" }, &.{ "bytes", "files", "nodes", "merge_work" });
        const files_value = case.get("files").?;
        const file_values = switch (files_value) {
            .array => |array| array.items,
            else => return error.BadResolutionContract,
        };
        for (file_values) |file_value| {
            const file = try object(file_value);
            try fields(file, &.{ "path", "source" }, &.{ "path", "source" });
        }
    }
}

fn object(value: std.json.Value) !std.json.ObjectMap {
    return switch (value) {
        .object => |map| map,
        else => error.BadResolutionContract,
    };
}

fn fields(map: std.json.ObjectMap, allowed: []const []const u8, required: []const []const u8) !void {
    var it = map.iterator();
    while (it.next()) |entry| {
        var known = false;
        for (allowed) |name| known = known or std.mem.eql(u8, name, entry.key_ptr.*);
        if (!known) return error.BadResolutionContract;
    }
    for (required) |name| if (!map.contains(name)) return error.BadResolutionContract;
}

fn runFixture(fixture: Fixture) !void {
    if (fixture.entry.len == 0 or fixture.files.len == 0 or fixture.limits.bytes == 0 or fixture.limits.files == 0 or fixture.limits.nodes == 0 or fixture.limits.merge_work == 0) {
        return error.BadResolutionContract;
    }
    var tmp = testing.tmpDir(.{});
    defer tmp.cleanup();
    var paths = std.StringHashMap(void).init(testing.allocator);
    defer paths.deinit();
    for (fixture.files) |file| {
        try validateRelativePath(file.path);
        if (paths.contains(file.path)) return error.DuplicateContractPath;
        try paths.put(file.path, {});
        if (std.fs.path.dirname(file.path)) |dir| try tmp.dir.makePath(dir);
        try tmp.dir.writeFile(.{ .sub_path = file.path, .data = file.source });
    }

    try validateRelativePath(fixture.entry);
    const entry = try tmp.dir.realpathAlloc(testing.allocator, fixture.entry);
    defer testing.allocator.free(entry);
    var options = skg.ResolveOptions{
        .max_bytes = fixture.limits.bytes,
        .max_files = fixture.limits.files,
        .max_nodes = fixture.limits.nodes,
        .max_merge_work = fixture.limits.merge_work,
    };
    var root_path: ?[]u8 = null;
    defer if (root_path) |path| testing.allocator.free(path);
    if (fixture.root) |root| {
        try validateRelativePath(root);
        root_path = try tmp.dir.realpathAlloc(testing.allocator, root);
        options.root = root_path.?;
    }

    var result = skg.parseWithOptions(testing.allocator, entry, options);
    defer result.deinit();
    if (fixture.expected_code) |code| {
        try testing.expect(result.file == null);
        const diagnostic = result.diagnostic orelse return error.MissingDiagnostic;
        try testing.expectEqual(code, diagnostic.code);
        return;
    }
    const file = result.file orelse {
        if (result.diagnostic) |diagnostic| {
            std.debug.print("unexpected {s}: {s}\n", .{ @tagName(diagnostic.code), diagnostic.message });
        }
        return error.UnexpectedResolutionFailure;
    };
    const emitted = try skg.emit.emitFile(testing.allocator, file);
    defer testing.allocator.free(emitted);
    try testing.expectEqualStrings(fixture.expected_formatted.?, emitted);
}

fn validateRelativePath(path: []const u8) !void {
    if (path.len == 0 or std.fs.path.isAbsolute(path) or std.mem.eql(u8, path, "..") or
        std.mem.startsWith(u8, path, "../") or std.mem.startsWith(u8, path, "..\\"))
    {
        return error.UnsafeContractPath;
    }
}
