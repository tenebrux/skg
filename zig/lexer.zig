/// SKG lexer - tokenizes source into a stream of Tokens.
///
/// Returns raw source slices. No allocation. Strings include surrounding quotes.
/// The parser is responsible for unescaping string content.
///
/// Error position: after a LexError, check `lexer.line` and `lexer.col`.
const std = @import("std");
const token = @import("token.zig");

pub const Token = token.Token;
pub const Tag = token.Tag;

pub const LexError = error{
    UnexpectedChar,
    UnterminatedString,
    InvalidEscape,
    /// An integer literal was written in a form the grammar does not allow
    /// (a redundant leading zero). Values that do not fit i64 are the parser's
    /// business, not the lexer's, but they share the code.
    InvalidInt,
    /// A float literal was written in a form the grammar does not allow
    /// (no digit after the decimal point, or a redundant leading zero).
    InvalidFloat,
};

pub const Lexer = struct {
    src: []const u8,
    pos: usize,
    line: u32,
    col: u32,

    pub fn init(src: []const u8) Lexer {
        return .{ .src = src, .pos = 0, .line = 1, .col = 1 };
    }

    fn peek(self: *const Lexer) ?u8 {
        if (self.pos >= self.src.len) return null;
        return self.src[self.pos];
    }

    fn peekAhead(self: *const Lexer, offset: usize) ?u8 {
        const idx = self.pos + offset;
        if (idx >= self.src.len) return null;
        return self.src[idx];
    }

    fn advance(self: *Lexer) u8 {
        const c = self.src[self.pos];
        self.pos += 1;
        if (c == '\n') {
            self.line += 1;
            self.col = 1;
        } else {
            self.col += 1;
        }
        return c;
    }

    fn skipWhitespace(self: *Lexer) void {
        while (self.pos < self.src.len) {
            const c = self.src[self.pos];
            if (c == ' ' or c == '\t' or c == '\r' or c == '\n') {
                _ = self.advance();
            } else {
                break;
            }
        }
    }

    /// Return the next token. Returns `.eof` at end of input.
    /// Comment tokens are emitted for `#` lines (text includes `#` prefix, no trailing newline).
    pub fn next(self: *Lexer) LexError!Token {
        self.skipWhitespace();

        if (self.pos >= self.src.len) {
            return Token{ .tag = .eof, .text = "", .line = self.line, .col = self.col };
        }

        const tok_line = self.line;
        const tok_col = self.col;
        const c = self.src[self.pos];

        switch (c) {
            ':' => {
                const start = self.pos;
                _ = self.advance();
                return Token{ .tag = .colon, .text = self.src[start..self.pos], .line = tok_line, .col = tok_col };
            },
            '{' => {
                const start = self.pos;
                _ = self.advance();
                return Token{ .tag = .lbrace, .text = self.src[start..self.pos], .line = tok_line, .col = tok_col };
            },
            '}' => {
                const start = self.pos;
                _ = self.advance();
                return Token{ .tag = .rbrace, .text = self.src[start..self.pos], .line = tok_line, .col = tok_col };
            },
            '[' => {
                const start = self.pos;
                _ = self.advance();
                return Token{ .tag = .lbracket, .text = self.src[start..self.pos], .line = tok_line, .col = tok_col };
            },
            ']' => {
                const start = self.pos;
                _ = self.advance();
                return Token{ .tag = .rbracket, .text = self.src[start..self.pos], .line = tok_line, .col = tok_col };
            },
            ',' => {
                const start = self.pos;
                _ = self.advance();
                return Token{ .tag = .comma, .text = self.src[start..self.pos], .line = tok_line, .col = tok_col };
            },
            '#' => {
                const start = self.pos;
                while (self.pos < self.src.len and self.src[self.pos] != '\n') {
                    self.pos += 1;
                    self.col += 1;
                }
                return Token{ .tag = .comment, .text = self.src[start..self.pos], .line = tok_line, .col = tok_col };
            },
            '"' => return self.lexString(tok_line, tok_col),
            '-' => return self.lexNegativeNumber(tok_line, tok_col),
            '0'...'9' => return self.lexNumber(tok_line, tok_col),
            'a'...'z', 'A'...'Z', '_' => return self.lexIdent(tok_line, tok_col),
            else => return error.UnexpectedChar,
        }
    }

    fn lexString(self: *Lexer, line: u32, col: u32) LexError!Token {
        const start = self.pos;
        _ = self.advance(); // consume opening "

        // Triple-quote multiline string: """..."""
        if (self.peek() == '"' and self.peekAhead(1) == '"') {
            _ = self.advance(); // second "
            _ = self.advance(); // third "
            return self.lexMultilineString(start, line, col);
        }

        while (self.pos < self.src.len) {
            const c = self.src[self.pos];
            if (c == '\\') {
                // validate escape sequence
                if (self.pos + 1 >= self.src.len) return error.UnterminatedString;
                switch (self.src[self.pos + 1]) {
                    '"', '\\', 'n', 't' => {},
                    else => return error.InvalidEscape,
                }
                self.pos += 2;
                self.col += 2;
            } else if (c == '"') {
                _ = self.advance(); // consume closing "
                return Token{ .tag = .string, .text = self.src[start..self.pos], .line = line, .col = col };
            } else if (c == '\n') {
                return error.UnterminatedString;
            } else {
                _ = self.advance();
            }
        }
        return error.UnterminatedString;
    }

    fn lexMultilineString(self: *Lexer, start: usize, line: u32, col: u32) LexError!Token {
        while (self.pos < self.src.len) {
            const c = self.src[self.pos];
            if (c == '"' and self.peekAhead(1) == '"' and self.peekAhead(2) == '"') {
                _ = self.advance();
                _ = self.advance();
                _ = self.advance();
                return Token{ .tag = .string, .text = self.src[start..self.pos], .line = line, .col = col };
            }
            _ = self.advance();
        }
        return error.UnterminatedString;
    }

    fn lexNegativeNumber(self: *Lexer, line: u32, col: u32) LexError!Token {
        // '-' followed by a digit → number. Anything else is an error.
        if (self.peekAhead(1)) |next_c| {
            if (next_c >= '0' and next_c <= '9') {
                return self.lexNumber(line, col);
            }
        }
        return error.UnexpectedChar;
    }

    /// Rewind the reported position to `line`:`col` and raise `err`.
    ///
    /// The parser reads `lexer.line`/`lexer.col` when turning a LexError into a
    /// diagnostic, and by the time a malformed number is recognised the cursor
    /// sits past it. Pointing at the first byte of the literal is what a reader
    /// wants; the cursor is discarded anyway because the parse is over.
    fn failAt(self: *Lexer, line: u32, col: u32, err: LexError) LexError {
        self.line = line;
        self.col = col;
        return err;
    }

    /// Lex an int or float literal.
    ///
    /// The grammar admits exactly one spelling per value (docs/spec.md, "one way
    /// to write each construct"), so two shapes a permissive scanner would wave
    /// through are rejected here instead of silently normalised:
    ///
    ///   - a redundant leading zero (`007`, `00.5`): the integer part is `0` or
    ///     starts with a non-zero digit, nothing else;
    ///   - a decimal point with no digit after it (`5.`): spec.md requires the
    ///     trailing zero, so `5.0` is the only spelling of that value.
    fn lexNumber(self: *Lexer, line: u32, col: u32) LexError!Token {
        const start = self.pos;
        // optional leading minus
        if (self.peek() == '-') _ = self.advance();
        // integer part: one or more digits
        const int_start = self.pos;
        while (self.peek()) |c| {
            if (c >= '0' and c <= '9') _ = self.advance() else break;
        }
        const int_digits = self.src[int_start..self.pos];
        const leading_zero = int_digits.len > 1 and int_digits[0] == '0';
        // decimal point → float
        if (self.peek() == '.') {
            _ = self.advance();
            // fractional digits: at least one is required
            const frac_start = self.pos;
            while (self.peek()) |c| {
                if (c >= '0' and c <= '9') _ = self.advance() else break;
            }
            if (self.pos == frac_start or leading_zero) return self.failAt(line, col, error.InvalidFloat);
            return Token{ .tag = .float, .text = self.src[start..self.pos], .line = line, .col = col };
        }
        if (leading_zero) return self.failAt(line, col, error.InvalidInt);
        return Token{ .tag = .int, .text = self.src[start..self.pos], .line = line, .col = col };
    }

    fn lexIdent(self: *Lexer, line: u32, col: u32) LexError!Token {
        const start = self.pos;
        while (self.peek()) |c| {
            if ((c >= 'a' and c <= 'z') or
                (c >= 'A' and c <= 'Z') or
                (c >= '0' and c <= '9') or
                c == '_')
            {
                _ = self.advance();
            } else break;
        }
        const text = self.src[start..self.pos];
        const tag: Tag = if (std.mem.eql(u8, text, "true"))
            .bool_true
        else if (std.mem.eql(u8, text, "false"))
            .bool_false
        else if (std.mem.eql(u8, text, "null"))
            .null_lit
        else
            .ident;
        return Token{ .tag = tag, .text = text, .line = line, .col = col };
    }
};
