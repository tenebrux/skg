/// SKG parser tests.
///
/// Tests cover: lexer tokens, parser AST shape, merge behavior, and
/// end-to-end parse of the example config.
const std = @import("std");
const testing = std.testing;

const lexer_mod = @import("lexer.zig");
const Lexer = lexer_mod.Lexer;
const Tag = @import("token.zig").Tag;
const parser = @import("parser.zig");
const merge = @import("merge.zig");
const ast = @import("ast.zig");
const root = @import("root.zig");
const emit_mod = @import("emit.zig");

// ─── Lexer ────────────────────────────────────────────────────────────────────

test "lexer: basic tokens" {
    var lex = Lexer.init("key: 42");
    const t1 = try lex.next();
    try testing.expectEqual(Tag.ident, t1.tag);
    try testing.expectEqualStrings("key", t1.text);

    const t2 = try lex.next();
    try testing.expectEqual(Tag.colon, t2.tag);

    const t3 = try lex.next();
    try testing.expectEqual(Tag.int, t3.tag);
    try testing.expectEqualStrings("42", t3.text);

    const t4 = try lex.next();
    try testing.expectEqual(Tag.eof, t4.tag);
}

test "lexer: float" {
    var lex = Lexer.init("0.92");
    const t = try lex.next();
    try testing.expectEqual(Tag.float, t.tag);
    try testing.expectEqualStrings("0.92", t.text);
}

test "lexer: negative number" {
    var lex = Lexer.init("-15.0");
    const t = try lex.next();
    try testing.expectEqual(Tag.float, t.tag);
    try testing.expectEqualStrings("-15.0", t.text);
}

test "lexer: bool tokens" {
    var lex = Lexer.init("true false");
    try testing.expectEqual(Tag.bool_true, (try lex.next()).tag);
    try testing.expectEqual(Tag.bool_false, (try lex.next()).tag);
}

test "lexer: string with escape" {
    var lex = Lexer.init(
        \\"hello \"world\""
    );
    const t = try lex.next();
    try testing.expectEqual(Tag.string, t.tag);
    try testing.expectEqualStrings(
        \\"hello \"world\""
    , t.text);
}

test "lexer: emits comments" {
    var lex = Lexer.init("# comment\nkey: 1");
    const c = try lex.next();
    try testing.expectEqual(Tag.comment, c.tag);
    try testing.expectEqualStrings("# comment", c.text);
    const t = try lex.next();
    try testing.expectEqual(Tag.ident, t.tag);
    try testing.expectEqualStrings("key", t.text);
}

test "lexer: array tokens" {
    var lex = Lexer.init("[1, 2, 3]");
    try testing.expectEqual(Tag.lbracket, (try lex.next()).tag);
    try testing.expectEqual(Tag.int, (try lex.next()).tag);
    try testing.expectEqual(Tag.comma, (try lex.next()).tag);
    try testing.expectEqual(Tag.int, (try lex.next()).tag);
    try testing.expectEqual(Tag.comma, (try lex.next()).tag);
    try testing.expectEqual(Tag.int, (try lex.next()).tag);
    try testing.expectEqual(Tag.rbracket, (try lex.next()).tag);
}

// ─── Parser ───────────────────────────────────────────────────────────────────

test "parser: simple field" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const src = "timeout: 5000";
    const file = try parser.parseSource(a, src, "test.skg", null);

    try testing.expectEqual(1, file.children.len);
    const node = file.children[0];
    try testing.expect(node == .field);
    try testing.expectEqualStrings("timeout", node.field.key);
    try testing.expectEqual(ast.ValueType.int, std.meta.activeTag(node.field.value));
    try testing.expectEqual(5000, node.field.value.int);
}

test "parser: float field" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src = "opacity: 0.92";
    const file = try parser.parseSource(arena.allocator(), src, "test.skg", null);

    try testing.expectEqual(1, file.children.len);
    const val = file.children[0].field.value;
    try testing.expectEqual(ast.ValueType.float, std.meta.activeTag(val));
    try testing.expectApproxEqAbs(0.92, val.float, 0.001);
}

test "parser: bool field" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src = "managed: true";
    const file = try parser.parseSource(arena.allocator(), src, "test.skg", null);

    const val = file.children[0].field.value;
    try testing.expectEqual(true, val.bool);
}

test "parser: string field with escape" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src =
        \\accent: "green"
    ;
    const file = try parser.parseSource(arena.allocator(), src, "test.skg", null);

    const val = file.children[0].field.value;
    try testing.expectEqual(ast.ValueType.string, std.meta.activeTag(val));
    try testing.expectEqualStrings("green", val.string);
}

test "parser: nested block" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src =
        \\theme {
        \\  accent: "green"
        \\}
    ;
    const file = try parser.parseSource(arena.allocator(), src, "test.skg", null);

    try testing.expectEqual(1, file.children.len);
    const block = file.children[0].block;
    try testing.expectEqualStrings("theme", block.name);
    try testing.expectEqual(1, block.children.len);
    try testing.expectEqualStrings("accent", block.children[0].field.key);
}

test "parser: array field" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src =
        \\workspace_go: ["super+1", "super+2", "super+3"]
    ;
    const file = try parser.parseSource(arena.allocator(), src, "test.skg", null);

    const val = file.children[0].field.value;
    try testing.expectEqual(ast.ValueType.array, std.meta.activeTag(val));
    try testing.expectEqual(3, val.array.items.len);
    try testing.expectEqualStrings("super+1", val.array.items[0].string);
}

test "parser: skg_version and schema_version" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src =
        \\skg_version: "1.0"
        \\schema_version: "1.0.0"
    ;
    const file = try parser.parseSource(arena.allocator(), src, "test.skg", null);

    try testing.expect(file.skg_version != null);
    try testing.expectEqualStrings("1.0", file.skg_version.?);
    try testing.expect(file.schema_version != null);
    try testing.expectEqualStrings("1.0.0", file.schema_version.?);
    try testing.expectEqual(0, file.children.len);
}

test "parser: inline import" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src =
        \\import "./theme.skg"
        \\key: "val"
    ;
    const file = try parser.parseSource(arena.allocator(), src, "test.skg", null);

    try testing.expectEqual(1, file.import_paths.len);
    try testing.expectEqualStrings("./theme.skg", file.import_paths[0]);
    try testing.expectEqual(1, file.children.len);
}

test "parser: array import" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src =
        \\import [
        \\  "./theme.skg",
        \\  "./keybinds.skg",
        \\]
    ;
    const file = try parser.parseSource(arena.allocator(), src, "test.skg", null);

    try testing.expectEqual(2, file.import_paths.len);
    try testing.expectEqualStrings("./theme.skg", file.import_paths[0]);
    try testing.expectEqualStrings("./keybinds.skg", file.import_paths[1]);
}

test "parser: mixed int float rejected in array" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src = "sizes: [1, 2.0]";
    const result = parser.parseSource(arena.allocator(), src, "test.skg", null);
    try testing.expectError(error.MixedArrayTypes, result);
}

// ─── Null values ─────────────────────────────────────────────────────────────

test "parser: null value" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const file = try parser.parseSource(arena.allocator(), "key: null", "test.skg", null);
    try testing.expectEqual(1, file.children.len);
    try testing.expectEqual(ast.ValueType.null, std.meta.activeTag(file.children[0].field.value));
}

test "lexer: null token" {
    var lex = Lexer.init("null");
    const t = try lex.next();
    try testing.expectEqual(Tag.null_lit, t.tag);
    try testing.expectEqualStrings("null", t.text);
}

// ─── Multiline strings ──────────────────────────────────────────────────────

test "parser: triple-quoted multiline string" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src = "desc: \"\"\"line one\nline two\"\"\"";
    const file = try parser.parseSource(arena.allocator(), src, "test.skg", null);
    try testing.expectEqual(1, file.children.len);
    try testing.expectEqualStrings("line one\nline two", file.children[0].field.value.string);
}

test "lexer: triple-quoted string token" {
    var lex = Lexer.init("\"\"\"hello\nworld\"\"\"");
    const t = try lex.next();
    try testing.expectEqual(Tag.string, t.tag);
    try testing.expectEqualStrings("\"\"\"hello\nworld\"\"\"", t.text);
}

// ─── Nested arrays ──────────────────────────────────────────────────────────

test "parser: nested array" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src = "grid: [[1, 2], [3, 4]]";
    const file = try parser.parseSource(arena.allocator(), src, "test.skg", null);
    try testing.expectEqual(1, file.children.len);
    const outer = file.children[0].field.value.array;
    try testing.expectEqual(2, outer.items.len);
    try testing.expectEqual(ast.ValueType.array, outer.element_type);
    try testing.expectEqual(2, outer.items[0].array.items.len);
    try testing.expectEqual(@as(i64, 1), outer.items[0].array.items[0].int);
}

// ─── Duplicate field last-wins ───────────────────────────────────────────────

test "parser: duplicate field last-wins" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src = "key: \"first\"\nkey: \"second\"";
    const file = try parser.parseSource(arena.allocator(), src, "test.skg", null);
    try testing.expectEqual(1, file.children.len);
    try testing.expectEqualStrings("second", file.children[0].field.value.string);
}

test "parser: duplicate block merges children" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();

    const src =
        \\theme {
        \\  a: "one"
        \\}
        \\theme {
        \\  b: "two"
        \\}
    ;
    const file = try parser.parseSource(arena.allocator(), src, "test.skg", null);
    try testing.expectEqual(1, file.children.len);
    try testing.expectEqual(2, file.children[0].block.children.len);
}

// ─── Merge ────────────────────────────────────────────────────────────────────

test "merge: overlay field overwrites base" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const base = try parser.parseSource(a, "accent: \"green\"", "base.skg", null);
    const overlay = try parser.parseSource(a, "accent: \"purple\"", "overlay.skg", null);

    const merged = try merge.mergeNodes(a, base.children, overlay.children);
    try testing.expectEqual(1, merged.len);
    try testing.expectEqualStrings("purple", merged[0].field.value.string);
}

test "merge: overlay block merges children" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const base = try parser.parseSource(a,
        \\theme {
        \\  accent: "green"
        \\  size: 13.0
        \\}
    , "base.skg", null);
    const overlay = try parser.parseSource(a,
        \\theme {
        \\  accent: "purple"
        \\}
    , "overlay.skg", null);

    const merged = try merge.mergeNodes(a, base.children, overlay.children);
    try testing.expectEqual(1, merged.len);
    const block = merged[0].block;
    try testing.expectEqual(2, block.children.len);
    // accent was overwritten
    var found_accent = false;
    var found_size = false;
    for (block.children) |c| {
        if (std.mem.eql(u8, c.field.key, "accent")) {
            try testing.expectEqualStrings("purple", c.field.value.string);
            found_accent = true;
        }
        if (std.mem.eql(u8, c.field.key, "size")) {
            found_size = true;
        }
    }
    try testing.expect(found_accent);
    try testing.expect(found_size);
}

// ─── Emit ────────────────────────────────────────────────────────────────────

test "emit: round-trip simple fields" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const src = "name: \"hello\"\ncount: 42\nenabled: true\n";
    const file = try parser.parseSource(a, src, "test.skg", null);
    const output = try emit_mod.emitFile(a, file);
    try testing.expectEqualStrings(src, output);
}

test "emit: round-trip block" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const src = "theme {\n  accent: \"green\"\n  size: 13.0\n}\n";
    const file = try parser.parseSource(a, src, "test.skg", null);
    const output = try emit_mod.emitFile(a, file);
    try testing.expectEqualStrings(src, output);
}

test "emit: round-trip with versions" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const src = "skg_version: \"1.0\"\nschema_version: \"1.0.0\"\n\nkey: \"val\"\n";
    const file = try parser.parseSource(a, src, "test.skg", null);
    const output = try emit_mod.emitFile(a, file);
    try testing.expectEqualStrings(src, output);
}

test "emit: null value" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const src = "key: null\n";
    const file = try parser.parseSource(a, src, "test.skg", null);
    const output = try emit_mod.emitFile(a, file);
    try testing.expectEqualStrings(src, output);
}

test "emit: array" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const src = "tags: [\"a\", \"b\", \"c\"]\n";
    const file = try parser.parseSource(a, src, "test.skg", null);
    const output = try emit_mod.emitFile(a, file);
    try testing.expectEqualStrings(src, output);
}

test "emit: multiline string uses triple quotes" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const children = try a.alloc(ast.Node, 1);
    children[0] = ast.Node{ .field = .{
        .key = "desc",
        .value = ast.Value{ .string = "line one\nline two" },
        .line = 1,
        .col = 1,
    } };
    const imports: [][]const u8 = &.{};
    const file = ast.File{
        .skg_version = null,
        .schema_version = null,
        .import_paths = imports,
        .children = children,
        .path = "test.skg",
    };
    const output = try emit_mod.emitFile(a, file);
    try testing.expectEqualStrings("desc: \"\"\"line one\nline two\"\"\"\n", output);
}

test "emit: escaped string" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const src = "msg: \"say \\\"hello\\\"\"\n";
    const file = try parser.parseSource(a, src, "test.skg", null);
    const output = try emit_mod.emitFile(a, file);
    try testing.expectEqualStrings(src, output);
}

// ─── Import resolution ───────────────────────────────────────────────────────

// A diamond graph where every level imports the level below it twice is
// 2^depth files to load without memoisation, so 30 levels - still inside
// `max_import_depth` - never finishes. The assertion is the result, not a
// wall-clock budget: the test simply cannot complete unless the cache works.
test "imports: a deep diamond does not blow up exponentially" {
    var tmp = testing.tmpDir(.{});
    defer tmp.cleanup();

    const depth = 30;
    var name_buf: [32]u8 = undefined;
    var body_buf: [128]u8 = undefined;

    try tmp.dir.writeFile(.{
        .sub_path = try std.fmt.bufPrint(&name_buf, "f{d:0>2}.skg", .{depth}),
        .data = "leaf: \"bottom\"\nblock {\n# same text, different source\n}\n",
    });
    var i: usize = 0;
    while (i < depth) : (i += 1) {
        const name = try std.fmt.bufPrint(&name_buf, "f{d:0>2}.skg", .{i});
        const body = try std.fmt.bufPrint(&body_buf, "import [\"./f{d:0>2}.skg\", \"./f{d:0>2}.skg\"]\n\nlevel{d:0>2}: {d}\nblock {{\n# same text, different source\n}}\n", .{ i + 1, i + 1, i, i });
        try tmp.dir.writeFile(.{ .sub_path = name, .data = body });
    }

    const entry = try tmp.dir.realpathAlloc(testing.allocator, "f00.skg");
    defer testing.allocator.free(entry);

    var result = root.parse(testing.allocator, entry);
    defer result.deinit();
    try testing.expect(result.file != null);
    try testing.expectEqual(@as(usize, depth + 2), result.file.?.children.len);
    try testing.expectEqual(@as(usize, depth + 1), result.file.?.children[1].block.trailing_comments.len);
}

// The cycle guard compares canonical paths, so a cycle spelled `./b.skg` - the
// spelling docs/spec.md's own examples use - is caught on the second visit
// rather than after ~2000 re-reads that grow the path to PATH_MAX.
test "imports: a ./-spelled cycle is detected immediately" {
    var tmp = testing.tmpDir(.{});
    defer tmp.cleanup();

    try tmp.dir.writeFile(.{ .sub_path = "main.skg", .data = "import \"./b.skg\"\nname: \"main\"\n" });
    try tmp.dir.writeFile(.{ .sub_path = "b.skg", .data = "import \"./main.skg\"\nname: \"b\"\n" });

    const entry = try tmp.dir.realpathAlloc(testing.allocator, "main.skg");
    defer testing.allocator.free(entry);

    var result = root.parse(testing.allocator, entry);
    defer result.deinit();
    try testing.expect(result.file == null);
    try testing.expectEqual(ast.ErrorCode.CIRCULAR_IMPORT, result.diagnostic.?.code);
    // Reported where the import was written, not at 0:0.
    try testing.expectEqual(@as(u32, 1), result.diagnostic.?.line);
    try testing.expectEqual(@as(u32, 8), result.diagnostic.?.col);
}

test "imports: aggregate resolution limits have stable diagnostics" {
    var tmp = testing.tmpDir(.{});
    defer tmp.cleanup();
    const main = "import \"child.skg\"\nroot: 1\n";
    const child = "child: 2\n";
    try tmp.dir.writeFile(.{ .sub_path = "main.skg", .data = main });
    try tmp.dir.writeFile(.{ .sub_path = "child.skg", .data = child });
    const entry = try tmp.dir.realpathAlloc(testing.allocator, "main.skg");
    defer testing.allocator.free(entry);

    const Case = struct { options: root.ResolveOptions, code: ast.ErrorCode };
    for ([_]Case{
        .{ .options = .{ .max_files = 1 }, .code = .RESOLUTION_FILE_LIMIT },
        .{ .options = .{ .max_bytes = main.len + child.len - 1 }, .code = .RESOLUTION_BYTE_LIMIT },
        .{ .options = .{ .max_merge_work = 2 }, .code = .RESOLUTION_WORK_LIMIT },
    }) |case| {
        var result = root.parseWithOptions(testing.allocator, entry, case.options);
        defer result.deinit();
        try testing.expect(result.file == null);
        try testing.expectEqual(case.code, result.diagnostic.?.code);
    }

    try tmp.dir.writeFile(.{ .sub_path = "nodes.skg", .data = "items: [1, 2]\n" });
    const nodes = try tmp.dir.realpathAlloc(testing.allocator, "nodes.skg");
    defer testing.allocator.free(nodes);
    var result = root.parseWithOptions(testing.allocator, nodes, .{ .max_nodes = 3 });
    defer result.deinit();
    try testing.expect(result.file == null);
    try testing.expectEqual(ast.ErrorCode.RESOLUTION_NODE_LIMIT, result.diagnostic.?.code);
}

test "imports: rooted policy accepts contained parent paths and rejects escapes" {
    var tmp = testing.tmpDir(.{});
    defer tmp.cleanup();
    try tmp.dir.makePath("root/cfg");
    try tmp.dir.makePath("outside");
    try tmp.dir.writeFile(.{ .sub_path = "root/shared.skg", .data = "shared: 2\n" });
    try tmp.dir.writeFile(.{ .sub_path = "root/cfg/main.skg", .data = "import \"../shared.skg\"\nroot: 1\n" });
    try tmp.dir.writeFile(.{ .sub_path = "outside/value.skg", .data = "outside: 3\n" });
    try tmp.dir.writeFile(.{ .sub_path = "root/cfg/escape.skg", .data = "import \"../../outside/value.skg\"\n" });
    const root_path = try tmp.dir.realpathAlloc(testing.allocator, "root");
    defer testing.allocator.free(root_path);
    const entry = try tmp.dir.realpathAlloc(testing.allocator, "root/cfg/main.skg");
    defer testing.allocator.free(entry);
    var accepted = root.parseWithOptions(testing.allocator, entry, .{ .root = root_path });
    defer accepted.deinit();
    try testing.expect(accepted.file != null);

    const escape = try tmp.dir.realpathAlloc(testing.allocator, "root/cfg/escape.skg");
    defer testing.allocator.free(escape);
    var rejected = root.parseWithOptions(testing.allocator, escape, .{ .root = root_path });
    defer rejected.deinit();
    try testing.expect(rejected.file == null);
    try testing.expectEqual(ast.ErrorCode.PATH_OUTSIDE_ROOT, rejected.diagnostic.?.code);

    const outside = try tmp.dir.realpathAlloc(testing.allocator, "outside/value.skg");
    defer testing.allocator.free(outside);
    var outside_entry = root.parseWithOptions(testing.allocator, outside, .{ .root = root_path });
    defer outside_entry.deinit();
    try testing.expect(outside_entry.file == null);
    try testing.expectEqual(ast.ErrorCode.PATH_OUTSIDE_ROOT, outside_entry.diagnostic.?.code);
}

test "imports: canonical symlink identity is cached and rooted" {
    if (@import("builtin").os.tag == .windows) return error.SkipZigTest;
    var tmp = testing.tmpDir(.{});
    defer tmp.cleanup();
    try tmp.dir.makePath("root");
    try tmp.dir.makePath("outside");
    try tmp.dir.writeFile(.{ .sub_path = "root/main.skg", .data = "import [\"alias-a.skg\", \"alias-b.skg\"]\n" });
    try tmp.dir.writeFile(.{ .sub_path = "root/target.skg", .data = "value: 1\n" });
    try tmp.dir.symLink("target.skg", "root/alias-a.skg", .{});
    try tmp.dir.symLink("target.skg", "root/alias-b.skg", .{});
    const root_path = try tmp.dir.realpathAlloc(testing.allocator, "root");
    defer testing.allocator.free(root_path);
    const entry = try tmp.dir.realpathAlloc(testing.allocator, "root/main.skg");
    defer testing.allocator.free(entry);
    var cached = root.parseWithOptions(testing.allocator, entry, .{ .root = root_path, .max_files = 2 });
    defer cached.deinit();
    try testing.expect(cached.file != null);

    try tmp.dir.writeFile(.{ .sub_path = "outside/target.skg", .data = "outside: 1\n" });
    try tmp.dir.symLink("../outside/target.skg", "root/outside-link.skg", .{});
    try tmp.dir.writeFile(.{ .sub_path = "root/escape.skg", .data = "import \"outside-link.skg\"\n" });
    const escape = try tmp.dir.realpathAlloc(testing.allocator, "root/escape.skg");
    defer testing.allocator.free(escape);
    var rejected = root.parseWithOptions(testing.allocator, escape, .{ .root = root_path });
    defer rejected.deinit();
    try testing.expect(rejected.file == null);
    try testing.expectEqual(ast.ErrorCode.PATH_OUTSIDE_ROOT, rejected.diagnostic.?.code);
}

test "source API owns input and path and enforces the file size limit" {
    var src = "value: \"hello\"\n".*;
    var path = "config.skg".*;
    var result = root.parseSource(testing.allocator, &src, &path);
    defer result.deinit();
    @memset(&src, 'x');
    @memset(&path, 'y');
    try testing.expectEqualStrings("value", result.file.?.children[0].field.key);
    try testing.expectEqualStrings("hello", result.file.?.children[0].field.value.string);
    try testing.expectEqualStrings("config.skg", result.file.?.path);
    const large = try testing.allocator.alloc(u8, 10 * 1024 * 1024 + 1);
    defer testing.allocator.free(large);
    @memset(large, ' ');
    var rejected = root.parseSource(testing.allocator, large, "large.skg");
    defer rejected.deinit();
    try testing.expect(rejected.file == null);
    try testing.expectEqual(ast.ErrorCode.FILE_TOO_LARGE, rejected.diagnostic.?.code);
}

test "header strings round-trip using SKG escapes" {
    for ([_][]const u8{ "a\"b", "a\\b", "a\nb", "a\rb", "\x00", "é" }) |value| {
        var arena = std.heap.ArenaAllocator.init(testing.allocator);
        defer arena.deinit();
        const a = arena.allocator();
        var imports = [_][]const u8{value};
        const file = ast.File{ .skg_version = null, .schema_version = value, .import_paths = &imports, .children = &.{}, .path = "test" };
        const output = try emit_mod.emitFile(a, file);
        var result = root.parseSource(testing.allocator, output, "test");
        defer result.deinit();
        try testing.expect(result.file != null);
        try testing.expectEqualStrings(value, result.file.?.schema_version.?);
        try testing.expectEqualStrings(value, result.file.?.import_paths[0]);
    }
}

test "cached suffix depth is checked independent of import order" {
    for ([_]usize{ 31, 32 }) |length| {
        for ([_][]const u8{ "import \"f1.skg\"", "import [\"f2.skg\", \"f1.skg\"]", "import [\"f1.skg\", \"f2.skg\"]" }) |imports| {
            var tmp = testing.tmpDir(.{});
            defer tmp.cleanup();
            try tmp.dir.writeFile(.{ .sub_path = "main.skg", .data = imports });
            try tmp.dir.writeFile(.{ .sub_path = "leaf.skg", .data = "value: 1" });
            var name_buf: [32]u8 = undefined;
            var body_buf: [64]u8 = undefined;
            for (1..length + 1) |i| {
                const name = try std.fmt.bufPrint(&name_buf, "f{d}.skg", .{i});
                const body = if (i == length) "import \"leaf.skg\"" else try std.fmt.bufPrint(&body_buf, "import \"f{d}.skg\"", .{i + 1});
                try tmp.dir.writeFile(.{ .sub_path = name, .data = body });
            }
            const entry = try tmp.dir.realpathAlloc(testing.allocator, "main.skg");
            defer testing.allocator.free(entry);
            var result = root.parse(testing.allocator, entry);
            defer result.deinit();
            if (length == 31) {
                try testing.expect(result.file != null);
            } else {
                try testing.expect(result.file == null);
                try testing.expectEqual(ast.ErrorCode.IMPORT_CHAIN_TOO_DEEP, result.diagnostic.?.code);
                try testing.expect(result.diagnostic.?.line > 0);
                try testing.expect(result.diagnostic.?.col > 0);
            }
        }
    }
}

test "structured values retain object and array closing comments" {
    const src =
        \\matrix: [[{
        \\  id: 1
        \\  # object end
        \\}, null
        \\# array end
        \\]]
    ;
    var result = root.parseSource(testing.allocator, src, "objects.skg");
    defer result.deinit();
    const file = result.file orelse return error.UnexpectedParseFailure;
    const inner = file.children[0].field.value.array.items[0].array;
    try testing.expectEqual(ast.ValueType.object, inner.element_type);
    try testing.expectEqualStrings("# object end", inner.items[0].object.trailing_comments[0]);
    try testing.expectEqualStrings("# array end", inner.trailing_comments[0]);
    const text = try emit_mod.emitFile(testing.allocator, file);
    defer testing.allocator.free(text);
    var again = root.parseSource(testing.allocator, text, "objects.skg");
    defer again.deinit();
    const text2 = try emit_mod.emitFile(testing.allocator, again.file orelse return error.UnexpectedParseFailure);
    defer testing.allocator.free(text2);
    try testing.expectEqualStrings(text, text2);
}

test "emit preserves every comment exactly once across relocatable trivia" {
    const src =
        \\# file top
        \\skg_version: "1.0"
        \\# before import
        \\import "base.skg"
        \\# before schema
        \\schema_version: "4"
        \\
        \\# field leading
        \\values: [1, # after first item
        \\# before second item
        \\2,
        \\# array end
        \\]
        \\
        \\block {
        \\  value: true # inline field
        \\  # block end
        \\}
        \\# file end
    ;
    var parsed = root.parseSource(testing.allocator, src, "comments.skg");
    defer parsed.deinit();
    const output = try emit_mod.emitFile(testing.allocator, parsed.file orelse return error.UnexpectedParseFailure);
    defer testing.allocator.free(output);

    var before = try commentCounts(testing.allocator, src);
    defer before.deinit();
    var after = try commentCounts(testing.allocator, output);
    defer after.deinit();
    try testing.expectEqual(before.count(), after.count());
    var iterator = before.iterator();
    while (iterator.next()) |entry| {
        try testing.expectEqual(entry.value_ptr.*, after.get(entry.key_ptr.*) orelse 0);
    }
}

fn commentCounts(allocator: std.mem.Allocator, source: []const u8) !std.StringHashMap(usize) {
    var counts = std.StringHashMap(usize).init(allocator);
    var lexer = Lexer.init(source);
    while (true) {
        const token = try lexer.next();
        if (token.tag == .eof) break;
        if (token.tag != .comment) continue;
        const entry = try counts.getOrPut(token.text);
        if (!entry.found_existing) entry.value_ptr.* = 0;
        entry.value_ptr.* += 1;
    }
    return counts;
}

test "overlay materialization preserves source instructions and clears nested markers" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();
    const base = try parser.parseSource(a, "x { inherited: 1 }", "base.skg", null);
    const ops = try parser.parseSource(a, "@delete x x { @delete gone fresh: 2 } @delete absent", "ops.skg", null);
    const before = try emit_mod.emitFile(a, ops);
    const composed = try merge.mergeNodes(a, base.children, ops.children);
    const final = try merge.materializeNodes(a, composed);
    try testing.expectEqual(@as(usize, 1), final.len);
    try testing.expect(!final[0].block.replace);
    try testing.expectEqual(@as(usize, 1), final[0].block.children.len);
    try testing.expectEqualStrings("fresh", final[0].block.children[0].field.key);
    try testing.expectEqualStrings(before, try emit_mod.emitFile(a, ops));
    try testing.expect(composed[0].block.replace);
    try testing.expectEqual(@as(usize, 2), composed[0].block.children.len);
}
