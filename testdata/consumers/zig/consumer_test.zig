const std = @import("std");
const testing = std.testing;
const skg = @import("skg");

const Port = struct {
    value: u16,

    pub fn skgDecode(ctx: *skg.native.Context, input: skg.Value) skg.native.Error!@This() {
        return .{ .value = try ctx.decode(u16, input) };
    }

    pub fn skgValidate(self: @This(), ctx: *skg.native.Context) skg.native.Error!void {
        if (self.value == 0) return ctx.fail(.custom_error, "port must be nonzero");
    }
};

const Worker = struct { name: []const u8, weight: u8 };
const Service = struct { host: []const u8, port: Port, mode: []const u8 };
const Nested = struct { values: []const []const i64 };
const Config = struct {
    service: Service,
    features: std.StringHashMap(bool),
    workers: []const ?Worker,
    thresholds: []const ?i16,
    labels: std.StringHashMap(u8),
    remove_me: ?[]const u8,
    nested: Nested,
    local_default: u8 = 9,
};

const base_source =
    \\service { host: "base" port: 80 mode: "safe" }
    \\features { old: true keep: true }
    \\workers [ { name: "alpha" weight: 1 } null ]
    \\thresholds: [1, null, 3]
    \\labels { "a/b~c": 7 }
    \\remove_me: "gone"
    \\extra: true
;

const main_source =
    \\skg_version: "1.0"
    \\import "base.skg"
    \\service { host: "main" port: 8080 }
    \\@replace features { new: true }
    \\@delete remove_me
    \\nested: { values: [[1, 2], [3]] }
;

const Graph = struct {
    dir: testing.TmpDir,
    root: []u8,
    main_path: []u8,

    fn deinit(self: *@This()) void {
        testing.allocator.free(self.root);
        testing.allocator.free(self.main_path);
        self.dir.cleanup();
    }
};

fn writeGraph(base: []const u8, main: []const u8) !Graph {
    var dir = testing.tmpDir(.{});
    errdefer dir.cleanup();
    try dir.dir.writeFile(.{ .sub_path = "base.skg", .data = base });
    try dir.dir.writeFile(.{ .sub_path = "main.skg", .data = main });
    const root = try dir.dir.realpathAlloc(testing.allocator, ".");
    errdefer testing.allocator.free(root);
    const main_path = try dir.dir.realpathAlloc(testing.allocator, "main.skg");
    return .{ .dir = dir, .root = root, .main_path = main_path };
}

test "public consumer workflow" {
    var source = skg.parseSource(testing.allocator, "import \"ghost.skg\" value: 1", "memory.skg");
    defer source.deinit();
    const source_file = source.file orelse return error.UnexpectedParseFailure;
    try testing.expectEqual(@as(usize, 1), source_file.import_paths.len);
    try testing.expectEqualStrings("ghost.skg", source_file.import_paths[0]);
    try testing.expect(!source_file.imports_resolved);

    var graph = try writeGraph(base_source, main_source);
    defer graph.deinit();

    var parsed = skg.parseWithOptions(testing.allocator, graph.main_path, .{ .root = graph.root });
    defer parsed.deinit();
    const parsed_file = parsed.file orelse return error.UnexpectedParseFailure;
    try testing.expect(parsed_file.imports_resolved);
    const canonical = try skg.emit.emitFile(testing.allocator, parsed_file);
    defer testing.allocator.free(canonical);
    try testing.expect(std.mem.indexOf(u8, canonical, "import ") == null);
    try testing.expect(std.mem.indexOf(u8, canonical, "@delete") == null);
    try testing.expect(std.mem.indexOf(u8, canonical, "@replace") == null);
    var reparsed = skg.parseSource(testing.allocator, canonical, "canonical.skg");
    defer reparsed.deinit();
    try testing.expect(reparsed.file != null);

    var decoded = skg.decodeFileWithOptions(Config, testing.allocator, graph.main_path, .{}, .{ .root = graph.root });
    defer decoded.deinit();
    const value = decoded.value orelse return error.UnexpectedDecodeFailure;
    try expectConfig(value);
}

fn expectConfig(value: Config) !void {
    try testing.expectEqualStrings("main", value.service.host);
    try testing.expectEqual(@as(u16, 8080), value.service.port.value);
    try testing.expectEqualStrings("safe", value.service.mode);
    try testing.expectEqual(@as(usize, 1), value.features.count());
    try testing.expect(value.features.get("new").?);
    try testing.expect(value.remove_me == null);
    try testing.expectEqual(@as(usize, 2), value.workers.len);
    try testing.expectEqualStrings("alpha", value.workers[0].?.name);
    try testing.expect(value.workers[1] == null);
    try testing.expectEqual(@as(i16, 1), value.thresholds[0].?);
    try testing.expect(value.thresholds[1] == null);
    try testing.expectEqual(@as(i16, 3), value.thresholds[2].?);
    try testing.expectEqual(@as(u8, 7), value.labels.get("a/b~c").?);
    try testing.expectEqualSlices(i64, &.{ 1, 2 }, value.nested.values[0]);
    try testing.expectEqual(@as(i64, 3), value.nested.values[1][0]);
    try testing.expectEqual(@as(u8, 9), value.local_default);
}

test "strict diagnostics hooks and owned failure" {
    const Known = struct { value: u8 };
    var strict = skg.decodeSource(Known, testing.allocator, "value: 1 extra: true", "strict.skg", .{ .unknown_fields = .reject });
    defer strict.deinit();
    try expectDiagnostic(strict.diagnostic, .unknown_field, "/extra", "strict.skg");

    const HookConfig = struct { port: Port };
    var hook = skg.decodeSource(HookConfig, testing.allocator, "port: 0", "hook.skg", .{});
    defer hook.deinit();
    try testing.expect(hook.value == null);
    try expectDiagnostic(hook.diagnostic, .custom_error, "/port", "hook.skg");

    var invalid = skg.decodeSource(Known, testing.allocator, "value: 256", "range.skg", .{});
    defer invalid.deinit();
    try testing.expect(invalid.value == null);
    try expectDiagnostic(invalid.diagnostic, .number_out_of_range, "/value", "range.skg");
}

test "imported failure retains provenance" {
    var graph = try writeGraph("service { port: 70000 }\n", "import \"base.skg\"\n");
    defer graph.deinit();
    const BadConfig = struct { service: struct { port: Port } };
    var decoded = skg.decodeFileWithOptions(BadConfig, testing.allocator, graph.main_path, .{}, .{ .root = graph.root });
    defer decoded.deinit();
    try testing.expect(decoded.value == null);
    try expectDiagnostic(decoded.diagnostic, .number_out_of_range, "/service/port", "base.skg");
}

fn expectDiagnostic(actual: ?skg.native.Diagnostic, code: skg.native.Code, field_path: []const u8, source_base: []const u8) !void {
    const diagnostic = actual orelse return error.NoDiagnostic;
    try testing.expectEqual(code, diagnostic.code);
    try testing.expectEqualStrings(field_path, diagnostic.field_path);
    try testing.expectEqualStrings(source_base, std.fs.path.basename(diagnostic.source.path));
    try testing.expect(diagnostic.source.line > 0);
    try testing.expect(diagnostic.source.col > 0);
}
