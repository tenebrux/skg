//! A downstream-style compile and runtime check for the public V1 Zig surface.
//! Removing or reshaping a referenced declaration is a 1.x gate failure.
const std = @import("std");
const testing = std.testing;
const skg = @import("root.zig");

const Config = struct { value: ?u16 = null };

test "V1 public API remains source compatible" {
    var scratch_arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer scratch_arena.deinit();
    const scratch = scratch_arena.allocator();
    comptime {
        if (skg.File != skg.ast.File or skg.Node != skg.ast.Node or
            skg.Block != skg.ast.Block or skg.Field != skg.ast.Field or
            skg.Delete != skg.ast.Delete or skg.Value != skg.ast.Value or
            skg.ValueType != skg.ast.ValueType or skg.Array != skg.ast.Array or
            skg.Object != skg.ast.Object or skg.BlockArray != skg.ast.BlockArray or
            skg.Diagnostic != skg.ast.Diagnostic)
        {
            @compileError("root AST aliases are part of the V1 API");
        }
    }

    const parse_fn: *const fn (std.mem.Allocator, []const u8) skg.ParseResult = &skg.parse;
    const parse_options_fn: *const fn (std.mem.Allocator, []const u8, skg.ResolveOptions) skg.ParseResult = &skg.parseWithOptions;
    const parse_source_fn: *const fn (std.mem.Allocator, []const u8, []const u8) skg.ParseResult = &skg.parseSource;
    _ = .{ parse_fn, parse_options_fn, parse_source_fn };

    const language: []const u8 = skg.language_version;
    const major: u8 = skg.supported_major_version;
    const minor: u8 = skg.supported_minor_version;
    const syntax_depth: u32 = skg.max_nesting_depth;
    const file_size: usize = skg.max_file_size;
    const import_depth: usize = skg.max_import_depth;
    const native_depth: usize = skg.max_native_nesting_depth;
    const default_bytes: usize = skg.default_max_resolve_bytes;
    const default_files: usize = skg.default_max_resolve_files;
    const default_nodes: usize = skg.default_max_resolve_nodes;
    const default_work: usize = skg.default_max_resolve_merge_work;
    try testing.expectEqualStrings("1.0", language);
    try testing.expectEqual(@as(u8, 1), major);
    try testing.expectEqual(@as(u8, 0), minor);
    try testing.expectEqual(@as(u32, 128), syntax_depth);
    try testing.expectEqual(@as(usize, 10 * 1024 * 1024), file_size);
    try testing.expectEqual(@as(usize, 32), import_depth);
    try testing.expectEqual(@as(usize, 256), native_depth);
    _ = .{ default_bytes, default_files, default_nodes, default_work };

    const options = skg.ResolveOptions{
        .root = null,
        .max_bytes = 1,
        .max_files = 1,
        .max_nodes = 1,
        .max_merge_work = 1,
    };
    _ = options;
    const native_options = skg.native.Options{ .unknown_fields = .reject, .allow_lossy_numbers = true };
    _ = native_options;

    const position = skg.ast.Position{ .line = 1, .col = 1 };
    const location = skg.native.Location{ .path = "v1.skg", .line = 1, .col = 1 };
    const diagnostic = skg.ast.Diagnostic{ .code = .UNKNOWN, .path = "v1.skg", .line = 1, .col = 1, .message = "message" };
    const native_diagnostic = skg.native.Diagnostic{
        .code = .custom_error,
        .field_path = "/value",
        .source = location,
        .message = "message",
        .parse_diagnostic = diagnostic,
    };
    _ = .{ position, native_diagnostic };

    const scalar = skg.Value{ .int = 1 };
    var array_items = [_]skg.Value{scalar};
    var empty_nodes = [_]skg.Node{};
    const array = skg.Array{ .element_type = .int, .items = &array_items, .trailing_comments = &.{} };
    const object = skg.Object{ .children = &empty_nodes, .trailing_comments = &.{} };
    const field = skg.Field{
        .path = "v1.skg",
        .key = "value",
        .value = scalar,
        .line = 1,
        .col = 1,
        .leading_comments = &.{},
        .trailing_comment = null,
    };
    const block = skg.Block{
        .path = "v1.skg",
        .replace = false,
        .name = "block",
        .children = &empty_nodes,
        .line = 1,
        .col = 1,
        .leading_comments = &.{},
        .trailing_comments = &.{},
    };
    var block_items = [_]skg.Value{ .{ .object = object }, .{ .null = {} } };
    const block_array = skg.BlockArray{
        .path = "v1.skg",
        .name = "items",
        .items = &block_items,
        .line = 1,
        .col = 1,
        .leading_comments = &.{},
        .trailing_comments = &.{},
    };
    const deletion = skg.Delete{
        .path = "v1.skg",
        .key = "gone",
        .line = 1,
        .col = 1,
        .leading_comments = &.{},
        .trailing_comment = null,
    };
    var nodes = [_]skg.Node{ .{ .field = field }, .{ .block = block }, .{ .block_array = block_array }, .{ .delete = deletion } };
    var imports = [_][]const u8{};
    const file = skg.File{
        .imports_resolved = false,
        .skg_version = null,
        .schema_version = null,
        .import_paths = &imports,
        .import_positions = &.{},
        .children = &nodes,
        .path = "v1.skg",
        .leading_comments = &.{},
        .trailing_comments = &.{},
    };
    _ = array;
    const emitted = try skg.emit.emitFile(scratch, file);

    const merged = try skg.merge.mergeNodes(scratch, &.{}, file.children);
    var budget: usize = 100;
    const budgeted = try skg.merge.mergeNodesWithBudget(scratch, &.{}, file.children, &budget);
    const materialized = try skg.merge.materializeNodes(scratch, file.children);
    _ = .{ emitted, merged, budgeted, materialized };

    var parsed: skg.ParseResult = skg.parseSource(testing.allocator, "value: 1", "v1.skg");
    defer parsed.deinit();
    try testing.expect(parsed.file != null);
    var decoded: skg.native.Result(Config) = skg.decodeSource(Config, testing.allocator, "value: 1", "v1.skg", .{});
    defer decoded.deinit();
    try testing.expectEqual(@as(?u16, 1), decoded.value.?.value);

    var from_parsed: skg.native.Result(Config) = skg.native.fromParsed(
        Config,
        testing.allocator,
        skg.parseSource(testing.allocator, "value: 2", "v1.skg"),
        .{},
    );
    defer from_parsed.deinit();
    try testing.expectEqual(@as(?u16, 2), from_parsed.value.?.value);

    var context = skg.native.Context{ .allocator = testing.allocator };
    try testing.expectEqual(@as(u8, 3), try context.decode(u8, .{ .int = 3 }));
    const parse_error: skg.ParseError = error.OutOfMemory;
    const native_error: skg.native.Error = error.DecodeFailed;
    try testing.expect(parse_error == error.OutOfMemory);
    try testing.expect(native_error == error.DecodeFailed);
    _ = &skg.native.Context.fail;
}
