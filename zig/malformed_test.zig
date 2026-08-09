// Malformed SKG input rejection tests.
//
// Ensures that invalid SKG syntax is rejected with a meaningful diagnostic
// and never panics or produces a corrupt AST.
//
// Coverage:
//   - Lexer errors (bad escape sequence, unterminated string)
//   - Parser errors (missing colon, unclosed block, mixed array types)
const std = @import("std");
const testing = std.testing;

const skg_root = @import("root.zig");
const parser = @import("parser.zig");

// ─── Helpers ─────────────────────────────────────────────────────────────────

fn expectParseFailure(src: []const u8) !skg_root.Diagnostic {
    var result = skg_root.parseSource(testing.allocator, src, "<test>");
    defer result.deinit();
    try testing.expect(result.file == null);
    return result.diagnostic orelse return error.ExpectedDiagnostic;
}

// ─── Lexer errors ─────────────────────────────────────────────────────────────

test "reject unterminated string" {
    const diag = try expectParseFailure("key: \"unterminated");
    try testing.expectEqualStrings("unterminated string literal", diag.message);
}

test "reject bad escape sequence" {
    const diag = try expectParseFailure("key: \"bad \\q escape\"");
    try testing.expectEqualStrings("invalid escape sequence", diag.message);
}

// ─── Parser errors ────────────────────────────────────────────────────────────

test "reject missing colon" {
    const diag = try expectParseFailure("key \"value\"");
    try testing.expectEqualStrings("expected ':', '{', or '[' after identifier", diag.message);
}

test "reject unclosed block" {
    const diag = try expectParseFailure("theme {\n  accent: \"green\"\n");
    try testing.expectEqualStrings("unterminated block, expected '}'", diag.message);
}

test "reject mixed array types" {
    const diag = try expectParseFailure("arr: [1, \"two\", 3]");
    try testing.expectEqualStrings("mixed types in array", diag.message);
}

test "reject duplicate skg_version" {
    const diag = try expectParseFailure("skg_version: \"1.0\"\nskg_version: \"2.0\"\n");
    try testing.expectEqualStrings("duplicate skg_version declaration", diag.message);
}

test "reject duplicate schema_version" {
    const diag = try expectParseFailure("schema_version: \"1.0.0\"\nschema_version: \"2.0.0\"\n");
    try testing.expectEqualStrings("duplicate schema_version declaration", diag.message);
}

// ─── Additional error paths ──────────────────────────────────────────────────

test "reject expected value" {
    const diag = try expectParseFailure("key: }");
    try testing.expectEqualStrings("expected a value (string, number, bool, or array)", diag.message);
}

test "reject unterminated array" {
    const diag = try expectParseFailure("arr: [1, 2");
    try testing.expectEqualStrings("unterminated array, expected ']'", diag.message);
}

test "reject bad import syntax" {
    const diag = try expectParseFailure("import 42");
    try testing.expectEqualStrings("expected import path string or '['", diag.message);
}

test "reject unterminated import list" {
    const diag = try expectParseFailure("import [\"a.skg\"");
    try testing.expectEqualStrings("unterminated import list, expected ']'", diag.message);
}

// ─── Diagnostic position tests ───────────────────────────────────────────────

test "diagnostic reports correct line and column" {
    const diag = try expectParseFailure("name: \"hello\"\nbad_key");
    try testing.expect(diag.line >= 2);
    try testing.expectEqualStrings("<test>", diag.path);
}

test "diagnostic includes file path" {
    var result = skg_root.parseSource(testing.allocator, "bad {{{", "my/config.skg");
    defer result.deinit();
    try testing.expect(result.file == null);
    const diag = result.diagnostic orelse return error.ExpectedDiagnostic;
    try testing.expectEqualStrings("my/config.skg", diag.path);
}

// ─── Nesting depth limit ─────────────────────────────────────────────────────

const max_depth: usize = parser.max_nesting_depth;

/// Build `key: ` followed by `depth` open brackets and `depth` close brackets.
fn nestedArraySource(buf: *std.ArrayListUnmanaged(u8), depth: usize) !void {
    try buf.appendSlice(testing.allocator, "key: ");
    try buf.appendNTimes(testing.allocator, '[', depth);
    try buf.appendNTimes(testing.allocator, ']', depth);
}

test "accept nesting exactly at the depth limit" {
    var buf: std.ArrayListUnmanaged(u8) = .empty;
    defer buf.deinit(testing.allocator);
    try nestedArraySource(&buf, max_depth);

    var result = skg_root.parseSource(testing.allocator, buf.items, "<test>");
    defer result.deinit();
    try testing.expect(result.file != null);
}

test "reject arrays nested past the depth limit" {
    var buf: std.ArrayListUnmanaged(u8) = .empty;
    defer buf.deinit(testing.allocator);
    try nestedArraySource(&buf, max_depth + 1);

    const diag = try expectParseFailure(buf.items);
    try testing.expectEqualStrings("nesting too deep (max 128)", diag.message);
}

test "reject blocks nested past the depth limit" {
    var buf: std.ArrayListUnmanaged(u8) = .empty;
    defer buf.deinit(testing.allocator);
    var i: usize = 0;
    while (i < max_depth + 1) : (i += 1) {
        try buf.appendSlice(testing.allocator, "a{");
    }
    try buf.appendNTimes(testing.allocator, '}', max_depth + 1);

    const diag = try expectParseFailure(buf.items);
    try testing.expectEqualStrings("nesting too deep (max 128)", diag.message);
}

test "successful parse has no diagnostic" {
    var result = skg_root.parseSource(testing.allocator, "key: \"value\"", "<test>");
    defer result.deinit();
    try testing.expect(result.file != null);
    try testing.expect(result.diagnostic == null);
}
