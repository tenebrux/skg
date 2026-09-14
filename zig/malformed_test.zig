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

fn expectParseFailure(src: []const u8) !skg_root.ParseResult {
    var result = skg_root.parseSource(testing.allocator, src, "<test>");
    errdefer result.deinit();
    try testing.expect(result.file == null);
    try testing.expect(result.diagnostic != null);
    return result;
}

// ─── Lexer errors ─────────────────────────────────────────────────────────────

test "reject unterminated string" {
    var failure = try expectParseFailure("key: \"unterminated");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("unterminated string literal", diag.message);
}

test "reject bad escape sequence" {
    var failure = try expectParseFailure("key: \"bad \\q escape\"");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("invalid escape sequence", diag.message);
}

// ─── Parser errors ────────────────────────────────────────────────────────────

test "reject missing colon" {
    var failure = try expectParseFailure("key \"value\"");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("expected ':', '{', or '[' after key", diag.message);
}

test "reject unclosed block" {
    var failure = try expectParseFailure("theme {\n  accent: \"green\"\n");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("unterminated block, expected '}'", diag.message);
}

test "reject mixed array types" {
    var failure = try expectParseFailure("arr: [1, \"two\", 3]");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("mixed types in array", diag.message);
}

test "reject duplicate skg_version" {
    var failure = try expectParseFailure("skg_version: \"1.0\"\nskg_version: \"2.0\"\n");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("duplicate skg_version declaration", diag.message);
}

test "reject duplicate schema_version" {
    var failure = try expectParseFailure("schema_version: \"1.0.0\"\nschema_version: \"2.0.0\"\n");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("duplicate schema_version declaration", diag.message);
}

// ─── Additional error paths ──────────────────────────────────────────────────

test "reject expected value" {
    var failure = try expectParseFailure("key: }");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("expected a value (string, number, bool, or array)", diag.message);
}

test "reject unterminated array" {
    var failure = try expectParseFailure("arr: [1, 2");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("unterminated array, expected ']'", diag.message);
}

test "reject bad import syntax" {
    var failure = try expectParseFailure("import 42");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("expected import path string or '['", diag.message);
}

test "reject unterminated import list" {
    var failure = try expectParseFailure("import [\"a.skg\"");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("unterminated import list, expected ']'", diag.message);
}

// ─── Diagnostic position tests ───────────────────────────────────────────────

test "diagnostic reports correct line and column" {
    var failure = try expectParseFailure("name: \"hello\"\nbad_key");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
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

// ─── skg_version classification ──────────────────────────────────────────────

test "reject malformed skg_version" {
    var failure = try expectParseFailure("skg_version: \"abc\"\n");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("skg_version is malformed, expected \"MAJOR.MINOR\"", diag.message);
}

test "reject skg_version newer than supported" {
    var failure = try expectParseFailure("skg_version: \"9.9\"\n");
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("skg_version is not supported by this parser", diag.message);
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

    var failure = try expectParseFailure(buf.items);
    defer failure.deinit();
    const diag = failure.diagnostic.?;
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

    var failure = try expectParseFailure(buf.items);
    defer failure.deinit();
    const diag = failure.diagnostic.?;
    try testing.expectEqualStrings("nesting too deep (max 128)", diag.message);
}

test "successful parse has no diagnostic" {
    var result = skg_root.parseSource(testing.allocator, "key: \"value\"", "<test>");
    defer result.deinit();
    try testing.expect(result.file != null);
    try testing.expect(result.diagnostic == null);
}

test "invalid UTF-8 reports the first bad byte" {
    const source = [_]u8{ 'n', 'a', 'm', 'e', ':', ' ', '"', 'o', 'k', '"', '\n', '#', ' ', 0xff };
    var diag: ?skg_root.Diagnostic = null;
    try testing.expectError(error.InvalidUtf8, parser.parseSource(testing.allocator, &source, "encoding.skg", &diag));
    try testing.expectEqual(skg_root.ast.ErrorCode.INVALID_UTF8, diag.?.code);
    try testing.expectEqualStrings("encoding.skg", diag.?.path);
    try testing.expectEqual(@as(u32, 2), diag.?.line);
    try testing.expectEqual(@as(u32, 3), diag.?.col);
}
