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
        .data = "leaf: \"bottom\"\n",
    });
    var i: usize = 0;
    while (i < depth) : (i += 1) {
        const name = try std.fmt.bufPrint(&name_buf, "f{d:0>2}.skg", .{i});
        const body = try std.fmt.bufPrint(&body_buf, "import [\"./f{d:0>2}.skg\", \"./f{d:0>2}.skg\"]\n\nlevel{d:0>2}: {d}\n", .{ i + 1, i + 1, i, i });
        try tmp.dir.writeFile(.{ .sub_path = name, .data = body });
    }

    const entry = try tmp.dir.realpathAlloc(testing.allocator, "f00.skg");
    defer testing.allocator.free(entry);

    var result = root.parse(testing.allocator, entry);
    defer result.deinit();
    try testing.expect(result.file != null);
    try testing.expectEqual(@as(usize, depth + 1), result.file.?.children.len);
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
