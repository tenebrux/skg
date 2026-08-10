/// SKG parser - builds an AST from a token stream.
///
/// All allocations go into the caller-provided allocator (use an arena for easy cleanup).
/// Returns an ast.File with all memory owned by that allocator.
///
/// Import resolution is NOT handled here - see root.zig.
const std = @import("std");
const Allocator = std.mem.Allocator;
const Lexer = @import("lexer.zig").Lexer;
const LexError = @import("lexer.zig").LexError;
const Token = @import("token.zig").Token;
const Tag = @import("token.zig").Tag;
const ast = @import("ast.zig");
const merge = @import("merge.zig");

pub const ParseError = LexError || error{
    UnexpectedToken,
    ExpectedValue,
    ExpectedRbrace,
    ExpectedRbracket,
    MixedArrayTypes,
    DuplicateSKGVersion,
    DuplicateSchemaVersion,
    UnsupportedSKGVersion,
    MalformedSKGVersion,
    NestingTooDeep,
    InvalidInt,
    InvalidFloat,
    AbsoluteImportPath,
    DirectiveAfterBody,
    OutOfMemory,
};

/// The highest skg_version this parser supports.
pub const supported_major: u8 = 1;
pub const supported_minor: u8 = 0;

/// Maximum number of nested blocks, block arrays and arrays.
///
/// The parser descends one native stack frame per nesting level, so without a
/// bound a deeply nested file would overflow the thread stack and crash the
/// process. The Go sibling parser (go/parser.go) uses the same constant, so both
/// implementations accept and reject exactly the same inputs.
pub const max_nesting_depth: u32 = 128;

const nesting_too_deep_message = std.fmt.comptimePrint(
    "nesting too deep (max {d})",
    .{max_nesting_depth},
);

const ast_mod = @import("ast.zig");

const Parser = struct {
    allocator: Allocator,
    lexer: Lexer,
    peeked: ?Token,
    path: []const u8,
    last_diagnostic: ?ast_mod.Diagnostic = null,
    comment_buf: std.ArrayListUnmanaged([]const u8) = .empty,
    /// Current nesting level - one per open '{' or '[' being parsed.
    depth: u32 = 0,

    fn init(allocator: Allocator, src: []const u8, path: []const u8) Parser {
        return .{
            .allocator = allocator,
            .lexer = Lexer.init(src),
            .peeked = null,
            .path = path,
            .last_diagnostic = null,
            .comment_buf = .empty,
            .depth = 0,
        };
    }

    /// Record entry into a nested construct. Returns a parse error (never
    /// crashes) once the input nests deeper than `max_nesting_depth`.
    /// Every successful call must be paired with `exitNesting`.
    fn enterNesting(self: *Parser, line: u32, col: u32) ParseError!void {
        if (self.depth >= max_nesting_depth) {
            self.setDiagnostic(line, col, .NESTING_TOO_DEEP, nesting_too_deep_message);
            return error.NestingTooDeep;
        }
        self.depth += 1;
    }

    fn exitNesting(self: *Parser) void {
        self.depth -= 1;
    }

    fn setDiagnostic(self: *Parser, line: u32, col: u32, code: ast_mod.ErrorCode, message: []const u8) void {
        self.last_diagnostic = .{
            .code = code,
            .path = self.path,
            .line = line,
            .col = col,
            .message = message,
        };
    }

    fn nextToken(self: *Parser) ParseError!Token {
        return self.lexer.next() catch |err| {
            const code: ast_mod.ErrorCode = switch (err) {
                error.UnexpectedChar => .UNEXPECTED_CHAR,
                error.UnterminatedString => .UNTERMINATED_STRING,
                error.InvalidEscape => .INVALID_ESCAPE,
                error.InvalidInt => .INVALID_INT,
                error.InvalidFloat => .INVALID_FLOAT,
            };
            self.setDiagnostic(self.lexer.line, self.lexer.col, code, switch (err) {
                error.UnexpectedChar => "unexpected character",
                error.UnterminatedString => "unterminated string literal",
                error.InvalidEscape => "invalid escape sequence",
                error.InvalidInt => "invalid integer literal, a leading zero is not allowed",
                error.InvalidFloat => "invalid float literal, expected a digit after '.' and no leading zero",
            });
            return err;
        };
    }

    /// Peek at the next non-comment token. Comments are buffered into comment_buf.
    fn peek(self: *Parser) ParseError!Token {
        if (self.peeked == null) {
            var tok = try self.nextToken();
            while (tok.tag == .comment) {
                try self.comment_buf.append(self.allocator, tok.text);
                tok = try self.nextToken();
            }
            self.peeked = tok;
        }
        return self.peeked.?;
    }

    fn consume(self: *Parser) ParseError!Token {
        const t = try self.peek();
        self.peeked = null;
        return t;
    }

    /// Return buffered comments as an owned slice and clear the buffer.
    fn drainComments(self: *Parser) ParseError![]const []const u8 {
        if (self.comment_buf.items.len == 0) return &.{};
        const slice = self.comment_buf.toOwnedSlice(self.allocator) catch return error.OutOfMemory;
        return slice;
    }

    /// Move buffered comments onto the end of `out` and clear the buffer.
    ///
    /// Used for the header region, where a comment sitting between two
    /// directives has no node of its own to hang from. Appending keeps it in
    /// the file's leading trivia; the alternative the parser used to take was to
    /// drop it, which loses a line every time `skg fmt` rewrites the file.
    fn drainCommentsInto(self: *Parser, out: *std.ArrayListUnmanaged([]const u8)) ParseError!void {
        if (self.comment_buf.items.len == 0) return;
        try out.appendSlice(self.allocator, self.comment_buf.items);
        self.comment_buf.clearRetainingCapacity();
    }

    /// Check if the very next raw token (no skipping) is a comment on the given line.
    /// If so, consume it and return its text. Otherwise leave it for normal peek flow.
    fn tryTrailingComment(self: *Parser, line: u32) ParseError!?[]const u8 {
        // If we already have a peeked non-comment token, check its line
        if (self.peeked) |p| {
            _ = p;
            // peeked is a non-comment token; no trailing comment available
            // (comments before this token are already in comment_buf)
            return null;
        }
        // Peek at the raw next token without buffering comments
        const tok = try self.nextToken();
        if (tok.tag == .comment and tok.line == line) {
            return tok.text;
        }
        // Not a trailing comment - save it as peeked (or buffer if comment on different line)
        if (tok.tag == .comment) {
            try self.comment_buf.append(self.allocator, tok.text);
        } else {
            self.peeked = tok;
        }
        return null;
    }

    fn expect(self: *Parser, tag: Tag) ParseError!Token {
        const t = try self.consume();
        if (t.tag != tag) {
            const code: ast_mod.ErrorCode = switch (tag) {
                .colon => .EXPECTED_COLON,
                .rbrace => .EXPECTED_RBRACE,
                .rbracket => .EXPECTED_RBRACKET,
                .string => .EXPECTED_STRING,
                .float => .EXPECTED_VALUE,
                .ident => .EXPECTED_IDENT,
                else => .UNEXPECTED_TOKEN,
            };
            self.setDiagnostic(t.line, t.col, code, switch (tag) {
                .colon => "expected ':'",
                .rbrace => "expected '}'",
                .rbracket => "expected ']'",
                .string => "expected string value",
                .float => "expected float value",
                .ident => "expected identifier",
                else => "unexpected token",
            });
            return error.UnexpectedToken;
        }
        return t;
    }

    fn parseFile(self: *Parser) ParseError!ast.File {
        var skg_version: ?[]const u8 = null;
        var schema_version: ?[]const u8 = null;
        var import_paths: std.ArrayListUnmanaged([]const u8) = .empty;
        var import_positions: std.ArrayListUnmanaged(ast_mod.Position) = .empty;
        var children: std.ArrayListUnmanaged(ast.Node) = .empty;
        var file_leading: std.ArrayListUnmanaged([]const u8) = .empty;
        var captured_file_leading = false;

        while (true) {
            const t = try self.peek();
            if (t.tag == .eof) break;

            if (t.tag == .ident and isDirective(t.text)) {
                // Every directive belongs to the header, and the header comes
                // before the body (docs/spec.md, "File Structure"). Accepting one
                // after a block or field would freeze a second spelling of the
                // same file at V1, and the emitter has no way to reproduce it.
                if (children.items.len > 0) {
                    self.setDiagnostic(t.line, t.col, .DIRECTIVE_AFTER_BODY, "header directives must appear before the first block or field");
                    return error.DirectiveAfterBody;
                }
                try self.drainCommentsInto(&file_leading);
                captured_file_leading = true;
                _ = try self.consume();

                if (std.mem.eql(u8, t.text, "import")) {
                    try self.parseImports(&import_paths, &import_positions);
                    continue;
                }

                _ = try self.expect(.colon);
                const val_tok = try self.expect(.string);
                if (std.mem.eql(u8, t.text, "skg_version")) {
                    if (skg_version != null) {
                        self.setDiagnostic(val_tok.line, val_tok.col, .DUPLICATE_SKG_VERSION, "duplicate skg_version declaration");
                        return error.DuplicateSKGVersion;
                    }
                    skg_version = try self.unescapeString(val_tok.text);
                    switch (checkVersion(skg_version.?)) {
                        .ok => {},
                        .malformed => {
                            self.setDiagnostic(val_tok.line, val_tok.col, .MALFORMED_SKG_VERSION, "skg_version is malformed, expected \"MAJOR.MINOR\"");
                            return error.MalformedSKGVersion;
                        },
                        .too_new => {
                            self.setDiagnostic(val_tok.line, val_tok.col, .UNSUPPORTED_SKG_VERSION, "skg_version is newer than this parser supports");
                            return error.UnsupportedSKGVersion;
                        },
                    }
                } else {
                    if (schema_version != null) {
                        self.setDiagnostic(val_tok.line, val_tok.col, .DUPLICATE_SCHEMA_VERSION, "duplicate schema_version declaration");
                        return error.DuplicateSchemaVersion;
                    }
                    schema_version = try self.unescapeString(val_tok.text);
                }
                continue;
            }

            // First real node captures file-level leading comments if not yet done
            if (!captured_file_leading) {
                try self.drainCommentsInto(&file_leading);
                captured_file_leading = true;
            }

            const node = try self.parseNode();
            try children.append(self.allocator, node);
        }

        // Any comments after the last node are file trailing comments
        const file_trailing = try self.drainComments();

        // If nothing at all was parsed, file_leading captures everything before EOF
        if (!captured_file_leading) {
            try file_leading.appendSlice(self.allocator, file_trailing);
        }

        const raw_children = try children.toOwnedSlice(self.allocator);
        return ast.File{
            .skg_version = skg_version,
            .schema_version = schema_version,
            .import_paths = try import_paths.toOwnedSlice(self.allocator),
            .import_positions = try import_positions.toOwnedSlice(self.allocator),
            .children = try dedup(self.allocator, raw_children),
            .path = self.path,
            .leading_comments = try file_leading.toOwnedSlice(self.allocator),
            .trailing_comments = if (!captured_file_leading) &.{} else file_trailing,
        };
    }

    fn parseImports(
        self: *Parser,
        list: *std.ArrayListUnmanaged([]const u8),
        positions: *std.ArrayListUnmanaged(ast_mod.Position),
    ) ParseError!void {
        const t = try self.peek();
        if (t.tag == .string) {
            _ = try self.consume();
            try self.appendImport(list, positions, t);
        } else if (t.tag == .lbracket) {
            _ = try self.consume();
            while (true) {
                const nt = try self.peek();
                if (nt.tag == .rbracket) {
                    _ = try self.consume();
                    break;
                }
                if (nt.tag == .comma) {
                    _ = try self.consume();
                    continue;
                }
                if (nt.tag == .eof) {
                    self.setDiagnostic(nt.line, nt.col, .UNTERMINATED_IMPORT_LIST, "unterminated import list, expected ']'");
                    return error.ExpectedRbracket;
                }
                const path_tok = try self.expect(.string);
                try self.appendImport(list, positions, path_tok);
            }
        } else {
            self.setDiagnostic(t.line, t.col, .EXPECTED_IMPORT_PATH, "expected import path string or '['");
            return error.UnexpectedToken;
        }
    }

    /// Record one import path and where it was written, rejecting absolute paths.
    fn appendImport(
        self: *Parser,
        list: *std.ArrayListUnmanaged([]const u8),
        positions: *std.ArrayListUnmanaged(ast_mod.Position),
        tok: Token,
    ) ParseError!void {
        const path = try self.unescapeString(tok.text);
        if (isAbsoluteImportPath(path)) {
            self.setDiagnostic(tok.line, tok.col, .ABSOLUTE_IMPORT_PATH, "import paths must be relative to the importing file");
            return error.AbsoluteImportPath;
        }
        try list.append(self.allocator, path);
        try positions.append(self.allocator, .{ .line = tok.line, .col = tok.col });
    }

    /// Parse a single node (field or block). Expects an ident token next.
    /// Leading comments are already buffered by the time we get here -
    /// drain them before consuming the identifier.
    fn parseNode(self: *Parser) ParseError!ast.Node {
        const leading = try self.drainComments();
        const name_tok = try self.expect(.ident);
        const nt = try self.peek();

        if (nt.tag == .colon) {
            _ = try self.consume();
            const value = try self.parseValue();
            // A trailing comment sits on the line the *value* ends on, which is
            // not the line the key starts on for a multiline string or a
            // multi-line array. Anchoring on the key made those comments look
            // like own-line comments and migrate onto the next field.
            const trailing = try self.tryTrailingComment(self.lexer.line);
            return ast.Node{ .field = .{
                .key = name_tok.text,
                .value = value,
                .line = name_tok.line,
                .col = name_tok.col,
                .leading_comments = leading,
                .trailing_comment = trailing,
            } };
        } else if (nt.tag == .lbrace) {
            _ = try self.consume();
            try self.enterNesting(nt.line, nt.col);
            defer self.exitNesting();
            var children: std.ArrayListUnmanaged(ast.Node) = .empty;
            while (true) {
                const ct = try self.peek();
                if (ct.tag == .rbrace) {
                    break;
                }
                if (ct.tag == .eof) {
                    self.setDiagnostic(ct.line, ct.col, .UNTERMINATED_BLOCK, "unterminated block, expected '}'");
                    return error.ExpectedRbrace;
                }
                try children.append(self.allocator, try self.parseNode());
            }
            // Comments before '}' are trailing comments for the block
            const block_trailing = try self.drainComments();
            _ = try self.consume(); // consume '}'
            const raw = try children.toOwnedSlice(self.allocator);
            return ast.Node{ .block = .{
                .name = name_tok.text,
                .children = try dedup(self.allocator, raw),
                .line = name_tok.line,
                .col = name_tok.col,
                .leading_comments = leading,
                .trailing_comments = block_trailing,
            } };
        } else if (nt.tag == .lbracket) {
            _ = try self.consume();
            try self.enterNesting(nt.line, nt.col);
            defer self.exitNesting();
            return try self.parseBlockArray(name_tok, leading);
        } else {
            self.setDiagnostic(nt.line, nt.col, .EXPECTED_NODE_BODY, "expected ':', '{', or '[' after identifier");
            return error.UnexpectedToken;
        }
    }

    /// Parse block array entries. '[' already consumed, and the caller has
    /// already accounted for that bracket's nesting level.
    /// Expects `{ children }` blocks until `]`. If the first token after `[`
    /// is not `{`, falls back to parsing as a scalar array field (colonless shorthand).
    fn parseBlockArray(self: *Parser, name_tok: Token, leading: []const []const u8) ParseError!ast.Node {
        var items: std.ArrayListUnmanaged([]ast.Node) = .empty;

        while (true) {
            // peek() buffers any comments before the next real token
            const t = try self.peek();
            if (t.tag == .rbracket) {
                break;
            }
            if (t.tag == .comma) {
                _ = try self.consume();
                continue;
            }
            if (t.tag == .eof) {
                self.setDiagnostic(t.line, t.col, .UNTERMINATED_BLOCK_ARRAY, "unterminated block array, expected ']'");
                return error.ExpectedRbracket;
            }
            if (t.tag != .lbrace) {
                // `name [` whose first element is not `{` is the colonless
                // scalar-array shorthand (docs/spec.md, "Block Arrays"), so hand
                // the rest of the elements to the scalar-array parser.
                //
                // Once an entry has been parsed this is no longer a choice
                // between two readings: the collection has already committed to
                // being a block array, and a scalar here mixes element kinds.
                // Falling back at that point silently discarded every entry
                // parsed so far, turning `users [ {name: "a"} 99 ]` into
                // `users: [99]` with no diagnostic.
                if (items.items.len > 0) {
                    self.setDiagnostic(t.line, t.col, .MIXED_ARRAY_TYPES, "mixed block and scalar elements in an array");
                    return error.MixedArrayTypes;
                }
                return self.reParseAsFieldArray(name_tok, leading);
            }
            _ = try self.consume(); // consume '{'
            try self.enterNesting(t.line, t.col);
            defer self.exitNesting();
            var children: std.ArrayListUnmanaged(ast.Node) = .empty;
            while (true) {
                const ct = try self.peek();
                if (ct.tag == .rbrace) {
                    _ = try self.consume();
                    break;
                }
                if (ct.tag == .eof) {
                    self.setDiagnostic(ct.line, ct.col, .UNTERMINATED_BLOCK, "unterminated block in block array, expected '}'");
                    return error.ExpectedRbrace;
                }
                try children.append(self.allocator, try self.parseNode());
            }
            const raw = try children.toOwnedSlice(self.allocator);
            try items.append(self.allocator, try dedup(self.allocator, raw));
        }
        // Comments before ']' are trailing comments
        const arr_trailing = try self.drainComments();
        _ = try self.consume(); // consume ']'
        return ast.Node{ .block_array = .{
            .name = name_tok.text,
            .items = try items.toOwnedSlice(self.allocator),
            .line = name_tok.line,
            .col = name_tok.col,
            .leading_comments = leading,
            .trailing_comments = arr_trailing,
        } };
    }

    /// Fallback: `name [` was followed by a non-brace token, so parse
    /// remaining contents as a scalar array and return as a field node.
    fn reParseAsFieldArray(self: *Parser, name_tok: Token, leading: []const []const u8) ParseError!ast.Node {
        var items: std.ArrayListUnmanaged(ast.Value) = .empty;
        var element_type: ?ast.ValueType = null;

        while (true) {
            const t = try self.peek();
            if (t.tag == .rbracket) {
                _ = try self.consume();
                break;
            }
            if (t.tag == .comma) {
                _ = try self.consume();
                continue;
            }
            if (t.tag == .eof) {
                self.setDiagnostic(t.line, t.col, .UNTERMINATED_ARRAY, "unterminated array, expected ']'");
                return error.ExpectedRbracket;
            }
            if (t.tag == .lbrace) {
                // The mirror of the check in parseBlockArray: this collection has
                // committed to holding scalars, so a block entry mixes kinds.
                self.setDiagnostic(t.line, t.col, .MIXED_ARRAY_TYPES, "mixed block and scalar elements in an array");
                return error.MixedArrayTypes;
            }

            const val = try self.parseValue();
            const vtype = std.meta.activeTag(val);
            if (element_type) |et| {
                if (et != vtype) {
                    self.setDiagnostic(t.line, t.col, .MIXED_ARRAY_TYPES, "mixed types in array");
                    return error.MixedArrayTypes;
                }
            } else {
                element_type = vtype;
            }
            try items.append(self.allocator, val);
        }

        return ast.Node{ .field = .{
            .key = name_tok.text,
            .value = .{ .array = .{
                .element_type = element_type orelse .string,
                .items = try items.toOwnedSlice(self.allocator),
            } },
            .line = name_tok.line,
            .col = name_tok.col,
            .leading_comments = leading,
        } };
    }

    fn parseValue(self: *Parser) ParseError!ast.Value {
        const t = try self.consume();
        return switch (t.tag) {
            .int => ast.Value{ .int = std.fmt.parseInt(i64, t.text, 10) catch {
                self.setDiagnostic(t.line, t.col, .INVALID_INT, "invalid integer literal");
                return error.InvalidInt;
            } },
            .float => ast.Value{ .float = try self.parseFloatLiteral(t) },
            .bool_true => ast.Value{ .bool = true },
            .bool_false => ast.Value{ .bool = false },
            .null_lit => ast.Value{ .null = {} },
            .string => ast.Value{ .string = try self.unescapeString(t.text) },
            .lbracket => try self.parseArray(t),
            else => {
                self.setDiagnostic(t.line, t.col, .EXPECTED_VALUE, "expected a value (string, number, bool, or array)");
                return error.ExpectedValue;
            },
        };
    }

    /// Convert a float token to an f64, rejecting magnitudes f64 cannot hold.
    ///
    /// `std.fmt.parseFloat` saturates to +/-inf instead of failing, and SKG has
    /// no literal for infinity - the emitter would write `inf.0`, which does not
    /// re-parse. Go's `strconv.ParseFloat` reports the same input as out of
    /// range, so rejecting it here is what keeps the two parsers agreeing.
    /// Underflow to zero is accepted by both and stays accepted.
    fn parseFloatLiteral(self: *Parser, t: Token) ParseError!f64 {
        const f = std.fmt.parseFloat(f64, t.text) catch {
            self.setDiagnostic(t.line, t.col, .INVALID_FLOAT, "invalid float literal");
            return error.InvalidFloat;
        };
        if (!std.math.isFinite(f)) {
            self.setDiagnostic(t.line, t.col, .INVALID_FLOAT, "float literal is out of range for a 64-bit float");
            return error.InvalidFloat;
        }
        return f;
    }

    /// Parse array elements. `open_tok` is the already-consumed `[`.
    fn parseArray(self: *Parser, open_tok: Token) ParseError!ast.Value {
        try self.enterNesting(open_tok.line, open_tok.col);
        defer self.exitNesting();

        var items: std.ArrayListUnmanaged(ast.Value) = .empty;
        var element_type: ?ast.ValueType = null;

        while (true) {
            const t = try self.peek();
            if (t.tag == .rbracket) {
                _ = try self.consume();
                break;
            }
            if (t.tag == .comma) {
                _ = try self.consume();
                continue;
            }
            if (t.tag == .eof) {
                self.setDiagnostic(t.line, t.col, .UNTERMINATED_ARRAY, "unterminated array, expected ']'");
                return error.ExpectedRbracket;
            }

            const val = try self.parseValue();
            const vtype = std.meta.activeTag(val);
            if (element_type) |et| {
                if (et != vtype) {
                    self.setDiagnostic(t.line, t.col, .MIXED_ARRAY_TYPES, "mixed types in array");
                    return error.MixedArrayTypes;
                }
            } else {
                element_type = vtype;
            }
            try items.append(self.allocator, val);
        }

        return ast.Value{ .array = .{
            .element_type = element_type orelse .string,
            .items = try items.toOwnedSlice(self.allocator),
        } };
    }

    /// Strip surrounding quotes and process escape sequences.
    /// Handles both single-quoted ("...") and triple-quoted ("""...""") strings.
    fn unescapeString(self: *Parser, raw: []const u8) ParseError![]const u8 {
        std.debug.assert(raw.len >= 2 and raw[0] == '"' and raw[raw.len - 1] == '"');

        // Triple-quoted multiline string: return source slice directly
        if (raw.len >= 6 and raw[1] == '"' and raw[2] == '"' and
            raw[raw.len - 2] == '"' and raw[raw.len - 3] == '"')
        {
            return raw[3 .. raw.len - 3];
        }

        const inner = raw[1 .. raw.len - 1];

        // Fast path: no escapes → return source slice directly
        if (std.mem.indexOfScalar(u8, inner, '\\') == null) {
            return inner;
        }

        var buf: std.ArrayListUnmanaged(u8) = .empty;
        var i: usize = 0;
        while (i < inner.len) {
            if (inner[i] == '\\') {
                i += 1;
                switch (inner[i]) {
                    '"' => try buf.append(self.allocator, '"'),
                    '\\' => try buf.append(self.allocator, '\\'),
                    'n' => try buf.append(self.allocator, '\n'),
                    't' => try buf.append(self.allocator, '\t'),
                    else => {
                        self.setDiagnostic(self.lexer.line, self.lexer.col, .INVALID_ESCAPE, "invalid escape sequence");
                        return error.InvalidEscape;
                    },
                }
            } else {
                try buf.append(self.allocator, inner[i]);
            }
            i += 1;
        }
        return buf.toOwnedSlice(self.allocator);
    }
};

fn dedup(allocator: Allocator, nodes: []const ast.Node) ![]ast.Node {
    return merge.mergeNodes(allocator, &.{}, nodes);
}

/// Reports whether `name` is a header directive rather than a node name.
///
/// These three words are reserved at the top level of a file only. Inside a
/// block they are ordinary identifiers, because a block has no header.
pub fn isDirective(name: []const u8) bool {
    return std.mem.eql(u8, name, "skg_version") or
        std.mem.eql(u8, name, "schema_version") or
        std.mem.eql(u8, name, "import");
}

/// Reports whether an import path escapes the relative-path grammar.
///
/// Absolute imports are rejected outright (docs/spec.md, "Imports"): they are
/// not portable between machines, and a config parsed as root - which is how
/// umbra reads its manifests - has no business following a path out of the
/// config tree. The Windows spellings are rejected too so a file cannot mean
/// different things on different hosts. go/parser.go carries the same rule.
pub fn isAbsoluteImportPath(path: []const u8) bool {
    if (path.len == 0) return false;
    if (path[0] == '/' or path[0] == '\\') return true;
    // Drive-relative or drive-absolute Windows path: "C:", "C:\x", "C:x".
    if (path.len >= 2 and path[1] == ':' and std.ascii.isAlphabetic(path[0])) return true;
    return false;
}

/// Outcome of validating a declared skg_version.
pub const VersionCheck = enum {
    /// Well-formed and supported.
    ok,
    /// Not a "MAJOR.MINOR" pair of decimal numbers.
    malformed,
    /// Well-formed, but newer than this parser supports.
    too_new,
};

/// Classify a declared skg_version: malformed values are reported separately
/// from values that are merely newer than `supported_major.supported_minor`.
fn checkVersion(version: []const u8) VersionCheck {
    const dot = std.mem.indexOfScalar(u8, version, '.') orelse return .malformed;
    const major = std.fmt.parseUnsigned(u8, version[0..dot], 10) catch return .malformed;
    const minor = std.fmt.parseUnsigned(u8, version[dot + 1 ..], 10) catch return .malformed;
    if (major > supported_major) return .too_new;
    if (major == supported_major and minor > supported_minor) return .too_new;
    return .ok;
}

/// Parse an SKG source string into an ast.File.
/// All AST memory is allocated from `allocator` - use an arena for easy cleanup.
/// Import paths are recorded but not resolved (see root.zig).
/// Pass a non-null `diagnostic` pointer to capture error location on failure.
pub fn parseSource(
    allocator: Allocator,
    src: []const u8,
    path: []const u8,
    diagnostic: ?*?ast_mod.Diagnostic,
) ParseError!ast.File {
    var p = Parser.init(allocator, src, path);
    return p.parseFile() catch |err| {
        if (diagnostic) |d| {
            d.* = p.last_diagnostic orelse .{
                .code = .UNKNOWN,
                .path = path,
                .line = p.lexer.line,
                .col = p.lexer.col,
                .message = "parse error",
            };
        }
        return err;
    };
}
