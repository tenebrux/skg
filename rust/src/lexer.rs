//! Lexer: turns source bytes into tokens.
//!
//! Comments are emitted as tokens (the `comments` capability) and buffered by
//! the parser into trivia. Column numbers count bytes. Strings keep their
//! surrounding quotes; the parser decodes the escapes.

use crate::error::{Diagnostic, ErrorCode, ParseError, Position};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum Tag {
    Int,
    Float,
    BoolTrue,
    BoolFalse,
    NullLiteral,
    String,
    Ident,
    Colon,
    LBrace,
    RBrace,
    LBracket,
    RBracket,
    Comma,
    At,
    Comment,
    Eof,
}

#[derive(Debug, Clone)]
pub(crate) struct Token {
    pub tag: Tag,
    /// Token text. Strings include their surrounding quotes; comments include
    /// the leading `#` and exclude the trailing newline.
    pub text: String,
    pub line: u32,
    pub col: u32,
}

pub(crate) struct Lexer<'src> {
    src: &'src [u8],
    pos: usize,
    line: u32,
    col: u32,
}

impl<'src> Lexer<'src> {
    pub(crate) fn new(src: &'src [u8]) -> Self {
        Lexer {
            src,
            pos: 0,
            line: 1,
            col: 1,
        }
    }

    /// The cursor position, used to anchor lexer failures and to decide
    /// whether a following comment is a same-line trailing comment.
    pub(crate) fn position(&self) -> Position {
        Position {
            line: self.line,
            col: self.col,
        }
    }

    fn peek(&self) -> Option<u8> {
        self.src.get(self.pos).copied()
    }

    fn peek_ahead(&self, offset: usize) -> Option<u8> {
        self.src.get(self.pos + offset).copied()
    }

    fn advance(&mut self) -> u8 {
        let c = self.src[self.pos];
        self.pos += 1;
        if c == b'\n' {
            self.line += 1;
            self.col = 1;
        } else {
            self.col += 1;
        }
        c
    }

    fn skip_whitespace_and_comments(&mut self) {
        while let Some(c) = self.peek() {
            match c {
                b' ' | b'\t' | b'\r' | b'\n' => {
                    self.advance();
                }
                _ => break,
            }
        }
    }

    pub(crate) fn next(&mut self) -> Result<Token, ParseError> {
        self.skip_whitespace_and_comments();

        let Some(c) = self.peek() else {
            let position = self.position();
            return Ok(Token {
                tag: Tag::Eof,
                text: String::new(),
                line: position.line,
                col: position.col,
            });
        };

        let line = self.line;
        let col = self.col;

        match c {
            b'@' => {
                self.advance();
                Ok(Token {
                    tag: Tag::At,
                    text: "@".to_string(),
                    line,
                    col,
                })
            }
            b':' | b'{' | b'}' | b'[' | b']' | b',' => {
                let tag = match c {
                    b':' => Tag::Colon,
                    b'{' => Tag::LBrace,
                    b'}' => Tag::RBrace,
                    b'[' => Tag::LBracket,
                    b']' => Tag::RBracket,
                    _ => Tag::Comma,
                };
                self.advance();
                Ok(Token {
                    tag,
                    text: (c as char).to_string(),
                    line,
                    col,
                })
            }
            b'#' => self.lex_comment(line, col),
            b'"' => self.lex_string(line, col),
            b'-' => self.lex_negative_number(line, col),
            b'.' => {
                if self.peek_ahead(1).is_some_and(|next| next.is_ascii_digit()) {
                    Err(ParseError {
                        diagnostic: Diagnostic::new(
                            ErrorCode::InvalidFloat,
                            String::new(),
                            line,
                            col,
                            "float literals require a digit before '.'",
                        ),
                    })
                } else {
                    Err(ParseError {
                        diagnostic: Diagnostic::new(
                            ErrorCode::UnexpectedChar,
                            String::new(),
                            line,
                            col,
                            "unexpected character",
                        ),
                    })
                }
            }
            _ if c.is_ascii_digit() => self.lex_number(line, col),
            _ if is_ident_start(c) => self.lex_ident(line, col),
            _ => Err(ParseError {
                diagnostic: Diagnostic::new(
                    ErrorCode::UnexpectedChar,
                    String::new(),
                    line,
                    col,
                    "unexpected character",
                ),
            }),
        }
    }

    fn lex_comment(&mut self, line: u32, col: u32) -> Result<Token, ParseError> {
        let start = self.pos;
        while let Some(c) = self.peek() {
            if c == b'\n' {
                break;
            }
            self.pos += 1;
            self.col += 1;
        }
        let mut end = self.pos;
        // CRLF input: the carriage return belongs to the line ending, not the
        // comment, so canonical emission stays LF-normalized.
        if end > start && self.src[end - 1] == b'\r' {
            end -= 1;
        }
        let text = String::from_utf8_lossy(&self.src[start..end]).into_owned();
        Ok(Token {
            tag: Tag::Comment,
            text,
            line,
            col,
        })
    }

    fn lex_string(&mut self, line: u32, col: u32) -> Result<Token, ParseError> {
        let start = self.pos;
        self.advance(); // consume the opening quote

        if self.peek() == Some(b'"') && self.peek_ahead(1) == Some(b'"') {
            self.advance(); // second quote
            self.advance(); // third quote
            return self.lex_multiline_string(start, line, col);
        }

        while let Some(c) = self.peek() {
            if c == b'\\' {
                let Some(escaped) = self.peek_ahead(1) else {
                    return Err(self
                        .at_cursor(ErrorCode::UnterminatedString, "unterminated string literal"));
                };
                match escaped {
                    b'"' | b'\\' | b'n' | b't' => {}
                    _ => {
                        return Err(
                            self.at_cursor(ErrorCode::InvalidEscape, "invalid escape sequence")
                        )
                    }
                }
                self.pos += 2;
                self.col += 2;
            } else if c == b'"' {
                self.advance(); // consume the closing quote
                let text = String::from_utf8_lossy(&self.src[start..self.pos]).into_owned();
                return Ok(Token {
                    tag: Tag::String,
                    text,
                    line,
                    col,
                });
            } else if c == b'\n' {
                return Err(
                    self.at_cursor(ErrorCode::UnterminatedString, "unterminated string literal")
                );
            } else {
                self.advance();
            }
        }
        Err(self.at_cursor(ErrorCode::UnterminatedString, "unterminated string literal"))
    }

    fn lex_multiline_string(
        &mut self,
        start: usize,
        line: u32,
        col: u32,
    ) -> Result<Token, ParseError> {
        while let Some(c) = self.peek() {
            if c == b'"' && self.peek_ahead(1) == Some(b'"') && self.peek_ahead(2) == Some(b'"') {
                self.advance();
                self.advance();
                self.advance();
                let text = String::from_utf8_lossy(&self.src[start..self.pos]).into_owned();
                return Ok(Token {
                    tag: Tag::String,
                    text,
                    line,
                    col,
                });
            }
            self.advance();
        }
        Err(self.at_cursor(ErrorCode::UnterminatedString, "unterminated string literal"))
    }

    fn lex_negative_number(&mut self, line: u32, col: u32) -> Result<Token, ParseError> {
        match self.peek_ahead(1) {
            Some(next) if next.is_ascii_digit() => self.lex_number(line, col),
            _ => Err(self.at(line, col, ErrorCode::UnexpectedChar, "unexpected character")),
        }
    }

    /// Lex an int or float literal.
    ///
    /// The grammar admits exactly one spelling per value, so two shapes a
    /// permissive scanner would wave through are rejected here instead of
    /// silently normalized: a redundant leading zero (`007`, `00.5`), and a
    /// decimal point with no digit after it (`5.`). Both are reported at the
    /// first byte of the literal, not at the cursor, which by then sits past
    /// it.
    fn lex_number(&mut self, line: u32, col: u32) -> Result<Token, ParseError> {
        let start = self.pos;
        if self.peek() == Some(b'-') {
            self.advance();
        }
        let int_start = self.pos;
        while let Some(c) = self.peek() {
            if c.is_ascii_digit() {
                self.advance();
            } else {
                break;
            }
        }
        let leading_zero = self.pos - int_start > 1 && self.src[int_start] == b'0';

        if self.peek() == Some(b'.') {
            self.advance();
            let frac_start = self.pos;
            while let Some(c) = self.peek() {
                if c.is_ascii_digit() {
                    self.advance();
                } else {
                    break;
                }
            }
            if self.pos == frac_start || leading_zero {
                return Err(self.at(
                    line,
                    col,
                    ErrorCode::InvalidFloat,
                    "invalid float literal, expected a digit after '.' and no leading zero",
                ));
            }
            if let Some(suffix) = self.peek() {
                if is_ident_char(suffix) || suffix == b'.' {
                    return Err(self.at(
                        line,
                        col,
                        ErrorCode::InvalidFloat,
                        "invalid float literal suffix",
                    ));
                }
            }
            let text = String::from_utf8_lossy(&self.src[start..self.pos]).into_owned();
            return Ok(Token {
                tag: Tag::Float,
                text,
                line,
                col,
            });
        }
        if leading_zero {
            return Err(self.at(
                line,
                col,
                ErrorCode::InvalidInt,
                "invalid integer literal, a leading zero is not allowed",
            ));
        }
        if let Some(suffix) = self.peek() {
            if is_ident_char(suffix) {
                let code = if suffix == b'e' || suffix == b'E' {
                    ErrorCode::InvalidFloat
                } else {
                    ErrorCode::InvalidInt
                };
                return Err(self.at(line, col, code, "invalid numeric literal suffix"));
            }
        }
        let text = String::from_utf8_lossy(&self.src[start..self.pos]).into_owned();
        Ok(Token {
            tag: Tag::Int,
            text,
            line,
            col,
        })
    }

    fn lex_ident(&mut self, line: u32, col: u32) -> Result<Token, ParseError> {
        let start = self.pos;
        while let Some(c) = self.peek() {
            if is_ident_char(c) {
                self.advance();
            } else {
                break;
            }
        }
        let text = String::from_utf8_lossy(&self.src[start..self.pos]).into_owned();
        let tag = match text.as_str() {
            "true" => Tag::BoolTrue,
            "false" => Tag::BoolFalse,
            "null" => Tag::NullLiteral,
            _ => Tag::Ident,
        };
        Ok(Token {
            tag,
            text,
            line,
            col,
        })
    }

    /// A failure reported at the current cursor, which still points at the
    /// offending byte.
    fn at_cursor(&self, code: ErrorCode, message: &'static str) -> ParseError {
        let position = self.position();
        ParseError {
            diagnostic: Diagnostic::new(code, String::new(), position.line, position.col, message),
        }
    }

    /// A failure reported at an explicit position, such as the first byte of a
    /// literal the scanner has already moved past.
    fn at(&self, line: u32, col: u32, code: ErrorCode, message: &'static str) -> ParseError {
        ParseError {
            diagnostic: Diagnostic::new(code, String::new(), line, col, message),
        }
    }
}

pub(crate) fn is_ident_start(c: u8) -> bool {
    c.is_ascii_alphabetic() || c == b'_'
}

pub(crate) fn is_ident_char(c: u8) -> bool {
    is_ident_start(c) || c.is_ascii_digit()
}

/// Whether `key` can be written as a bare SKG identifier. Reserved value
/// literals need quotes; the caller adds the header-directive rule.
pub(crate) fn is_identifier(key: &str) -> bool {
    let bytes = key.as_bytes();
    if bytes.is_empty() || !is_ident_start(bytes[0]) {
        return false;
    }
    if !bytes[1..].iter().all(|&b| is_ident_char(b)) {
        return false;
    }
    !matches!(key, "true" | "false" | "null")
}
