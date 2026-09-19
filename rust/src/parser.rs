//! Recursive-descent parser: token stream → [`Document`].
//!
//! The produced document is a composed overlay: `@delete` markers and block
//! replacement flags survive until [`crate::merge::materialize_nodes`] or
//! file loading. Parsing never touches the filesystem; imports are recorded
//! with the position of their path token.

use crate::error::{Diagnostic, ErrorCode, ParseError, Position};
use crate::lexer::{is_identifier, Lexer, Tag, Token};
use crate::merge::merge_nodes;
use crate::model::{
    Array, Block, BlockArray, Delete, Document, Field, Node, ObjectBody, Value, ValueType,
};

/// Bounds how deeply blocks, block arrays, and arrays may nest.
///
/// The parser uses one stack frame per nesting level, so the cap must be
/// enforced here rather than left to the runtime. Mirrors the Go and Zig
/// implementations so all three accept and reject the same inputs.
pub const MAX_NESTING_DEPTH: usize = 128;

/// The largest input the parser accepts, per file.
pub const MAX_FILE_SIZE: usize = 10 * 1024 * 1024;

/// The highest SKG language contract this package accepts.
pub const LANGUAGE_VERSION: &str = "1.0";
pub(crate) const SUPPORTED_MAJOR_VERSION: u64 = 1;
pub(crate) const SUPPORTED_MINOR_VERSION: u64 = 0;

struct Parser<'src> {
    lex: Lexer<'src>,
    peeked: Option<Token>,
    path: String,
    depth: usize,
    comment_buf: Vec<String>,
}

/// Parse SKG source bytes into a composed overlay [`Document`].
///
/// The path labels diagnostics; it is never opened. Imports are recorded in
/// [`Document::import_paths`] and nothing is loaded.
pub(crate) fn parse_bytes(src: &[u8], path: impl Into<String>) -> Result<Document, ParseError> {
    let path = path.into();
    if src.len() > MAX_FILE_SIZE {
        return Err(ParseError {
            diagnostic: Diagnostic::without_position(
                ErrorCode::FileTooLarge,
                path,
                format!("file too large (max {MAX_FILE_SIZE} bytes)"),
            ),
        });
    }
    if let Some(position) = first_invalid_utf8(src) {
        return Err(ParseError {
            diagnostic: Diagnostic::new(
                ErrorCode::InvalidUtf8,
                path,
                position.line,
                position.col,
                "source is not valid UTF-8",
            ),
        });
    }
    let mut parser = Parser {
        lex: Lexer::new(src),
        peeked: None,
        path,
        depth: 0,
        comment_buf: Vec::new(),
    };
    parser.parse_file()
}

impl<'src> Parser<'src> {
    fn error(&self, position: Position, code: ErrorCode, message: impl Into<String>) -> ParseError {
        ParseError {
            diagnostic: Diagnostic::new(
                code,
                self.path.clone(),
                position.line,
                position.col,
                message,
            ),
        }
    }

    /// Fetch the next raw token, turning lexer failures into diagnostics.
    fn next_token(&mut self) -> Result<Token, ParseError> {
        match self.lex.next() {
            Ok(token) => Ok(token),
            Err(mut error) => {
                error.diagnostic.path = self.path.clone();
                Err(error)
            }
        }
    }

    /// Peek at the next non-comment token. Comments buffer into trivia.
    fn peek(&mut self) -> Result<&Token, ParseError> {
        if self.peeked.is_none() {
            let mut token = self.next_token()?;
            while token.tag == Tag::Comment {
                self.comment_buf.push(token.text);
                token = self.next_token()?;
            }
            self.peeked = Some(token);
        }
        Ok(self.peeked.as_ref().expect("peeked is none"))
    }

    fn consume(&mut self) -> Result<Token, ParseError> {
        match self.peeked.take() {
            Some(token) => Ok(token),
            None => self.next_token(),
        }
    }

    /// Return buffered comments as leading trivia and clear the buffer.
    fn drain_comments(&mut self) -> Vec<String> {
        std::mem::take(&mut self.comment_buf)
    }

    /// Move buffered comments onto `out` and clear the buffer.
    fn drain_comments_into(&mut self, out: &mut Vec<String>) {
        out.append(&mut self.comment_buf);
    }

    /// Capture a same-line trailing comment after a field or delete node, if
    /// the next raw token is one. A comment on a later line stays buffered as
    /// leading trivia for whatever follows.
    fn try_trailing_comment(&mut self, line: u32) -> Result<Option<String>, ParseError> {
        if self.peeked.is_some() {
            // A token is already buffered; comments before it are already in
            // comment_buf, so no same-line comment can follow the value.
            return Ok(None);
        }
        let token = self.next_token()?;
        if token.tag == Tag::Comment && token.line == line {
            return Ok(Some(token.text));
        }
        if token.tag == Tag::Comment {
            self.comment_buf.push(token.text);
        } else {
            self.peeked = Some(token);
        }
        Ok(None)
    }

    fn expect(&mut self, tag: Tag) -> Result<Token, ParseError> {
        let token = self.consume()?;
        if token.tag != tag {
            let (code, message) = match tag {
                Tag::Colon => (ErrorCode::ExpectedColon, "expected ':'"),
                Tag::RBrace => (ErrorCode::ExpectedRbrace, "expected '}'"),
                Tag::RBracket => (ErrorCode::ExpectedRbracket, "expected ']'"),
                Tag::String => (ErrorCode::ExpectedString, "expected string value"),
                Tag::Ident => (ErrorCode::ExpectedIdent, "expected identifier"),
                _ => (ErrorCode::UnexpectedToken, "unexpected token"),
            };
            return Err(self.error(Position::new(token.line, token.col), code, message));
        }
        Ok(token)
    }

    /// Record descent into a construct opened at `position`. On failure the
    /// parse aborts, so the counter is not unwound.
    fn enter(&mut self, position: Position) -> Result<(), ParseError> {
        self.depth += 1;
        if self.depth > MAX_NESTING_DEPTH {
            return Err(self.error(
                position,
                ErrorCode::NestingTooDeep,
                format!("nesting too deep (max {MAX_NESTING_DEPTH})"),
            ));
        }
        Ok(())
    }

    fn leave(&mut self) {
        self.depth -= 1;
    }

    fn parse_file(&mut self) -> Result<Document, ParseError> {
        let mut skg_version: Option<String> = None;
        let mut schema_version: Option<String> = None;
        let mut import_paths: Vec<String> = Vec::new();
        let mut import_positions: Vec<Position> = Vec::new();
        let mut children: Vec<Node> = Vec::new();
        let mut file_leading: Vec<String> = Vec::new();
        let mut captured_file_leading = false;

        loop {
            let (line, col, tag, text) = {
                let token = self.peek()?;
                (token.line, token.col, token.tag, token.text.clone())
            };
            if tag == Tag::Eof {
                break;
            }

            if tag == Tag::Ident && is_directive(&text) {
                // Every directive belongs to the header, and the header comes
                // before the body. Accepting one after a block or field would
                // freeze a second spelling of the same file that the emitter
                // cannot reproduce.
                if !children.is_empty() {
                    return Err(self.error(
                        Position::new(line, col),
                        ErrorCode::DirectiveAfterBody,
                        "header directives must appear before the first block or field",
                    ));
                }
                self.drain_comments_into(&mut file_leading);
                captured_file_leading = true;
                let directive = self.consume()?.text;

                if directive == "import" {
                    self.parse_imports(&mut import_paths, &mut import_positions)?;
                    continue;
                }

                self.expect(Tag::Colon)?;
                let value_token = self.expect(Tag::String)?;
                let value = unescape_string(&value_token.text).map_err(|message| {
                    self.error(
                        Position::new(value_token.line, value_token.col),
                        ErrorCode::InvalidEscape,
                        message,
                    )
                })?;
                let value_position = Position::new(value_token.line, value_token.col);

                if directive == "skg_version" {
                    if skg_version.is_some() {
                        return Err(self.error(
                            value_position,
                            ErrorCode::DuplicateSkgVersion,
                            "duplicate skg_version declaration",
                        ));
                    }
                    match check_version(&value) {
                        VersionCheck::Ok => skg_version = Some(value),
                        VersionCheck::Malformed => {
                            return Err(self.error(
                                value_position,
                                ErrorCode::MalformedSkgVersion,
                                "malformed skg_version, expected \"major.minor\" (e.g. \"1.0\")",
                            ));
                        }
                        VersionCheck::Unsupported => {
                            return Err(self.error(
                                value_position,
                                ErrorCode::UnsupportedSkgVersion,
                                "skg_version is not supported by this parser",
                            ));
                        }
                    }
                    continue;
                }

                if schema_version.is_some() {
                    return Err(self.error(
                        value_position,
                        ErrorCode::DuplicateSchemaVersion,
                        "duplicate schema_version declaration",
                    ));
                }
                schema_version = Some(value);
                continue;
            }

            // The first node captures file-level leading comments if the
            // header did not; comments between the header and the body attach
            // to the node they precede.
            if !captured_file_leading {
                self.drain_comments_into(&mut file_leading);
                captured_file_leading = true;
            }

            let node = self.parse_node()?;
            children.push(node);
        }

        let mut file_trailing = self.drain_comments();
        let children = merge_nodes(std::mem::take(&mut children));

        let mut leading = file_leading;

        let trailing = if captured_file_leading {
            std::mem::take(&mut file_trailing)
        } else {
            // Nothing parsed at all: every comment belongs at the top.
            leading.append(&mut file_trailing);
            Vec::new()
        };

        Ok(Document {
            path: self.path.clone(),
            skg_version,
            schema_version,
            import_paths,
            import_positions,
            children,
            imports_resolved: false,
            leading_comments: leading,
            trailing_comments: trailing,
        })
    }

    fn parse_imports(
        &mut self,
        paths: &mut Vec<String>,
        positions: &mut Vec<Position>,
    ) -> Result<(), ParseError> {
        let (tag, first_position) = {
            let token = self.peek()?;
            (token.tag, Position::new(token.line, token.col))
        };
        if tag == Tag::String {
            let token = self.consume()?;
            return self.append_import(paths, positions, token);
        }
        if tag == Tag::LBracket {
            self.consume()?;
            let mut need_path = true;
            loop {
                let (tag, position) = {
                    let token = self.peek()?;
                    (token.tag, Position::new(token.line, token.col))
                };
                if tag == Tag::RBracket {
                    self.consume()?;
                    return Ok(());
                }
                if tag == Tag::Eof {
                    return Err(self.error(
                        position,
                        ErrorCode::UnterminatedImportList,
                        "unterminated import list, expected ']'",
                    ));
                }
                if !need_path {
                    if tag != Tag::Comma {
                        return Err(self.error(
                            position,
                            ErrorCode::ExpectedComma,
                            "expected ',' or ']' in import list",
                        ));
                    }
                    self.consume()?;
                    need_path = true;
                    continue;
                }
                let token = self.expect(Tag::String)?;
                self.append_import(paths, positions, token)?;
                need_path = false;
            }
        }
        Err(self.error(
            first_position,
            ErrorCode::ExpectedImportPath,
            "expected import path string or '['",
        ))
    }

    /// Record one import path and where it was written, rejecting absolute
    /// paths at parse time.
    fn append_import(
        &mut self,
        paths: &mut Vec<String>,
        positions: &mut Vec<Position>,
        token: Token,
    ) -> Result<(), ParseError> {
        let token_position = Position::new(token.line, token.col);
        let path = unescape_string(&token.text)
            .map_err(|message| self.error(token_position, ErrorCode::InvalidEscape, message))?;
        if is_absolute_import_path(&path) {
            return Err(self.error(
                token_position,
                ErrorCode::AbsoluteImportPath,
                "import paths must be relative to the importing file",
            ));
        }
        positions.push(token_position);
        paths.push(path);
        Ok(())
    }

    /// Keys use bare identifiers or ordinary double-quoted strings. Quoting a
    /// header name makes it data, even at the document root. Triple-quoted
    /// keys are not supported.
    fn parse_key(&mut self) -> Result<Token, ParseError> {
        let plain_string = {
            let token = self.peek()?;
            token.tag == Tag::String && !token.text.starts_with("\"\"\"")
        };
        if !plain_string {
            return self.expect(Tag::Ident);
        }
        let token = self.consume()?;
        let token_position = Position::new(token.line, token.col);
        let decoded = unescape_string(&token.text)
            .map_err(|message| self.error(token_position, ErrorCode::InvalidEscape, message))?;
        Ok(Token {
            text: decoded,
            ..token
        })
    }

    fn parse_node(&mut self) -> Result<Node, ParseError> {
        let mut node = self.parse_node_body()?;
        match &mut node {
            Node::Delete(delete) => delete.path = self.path.clone(),
            Node::Field(field) => field.path = self.path.clone(),
            Node::Block(block) => block.path = self.path.clone(),
            Node::BlockArray(block_array) => block_array.path = self.path.clone(),
            Node::None => {}
        }
        Ok(node)
    }

    fn parse_node_body(&mut self) -> Result<Node, ParseError> {
        let leading = self.drain_comments();
        if self.peek()?.tag == Tag::At {
            return self.parse_operation(leading);
        }
        let name = self.parse_key()?;
        let name_position = Position::new(name.line, name.col);
        let (next_tag, next_position) = {
            let token = self.peek()?;
            (token.tag, Position::new(token.line, token.col))
        };

        let (value, colonless) = match next_tag {
            Tag::Colon => {
                self.consume()?;
                (self.parse_value()?, false)
            }
            Tag::LBrace => (self.parse_value()?, false),
            Tag::LBracket => {
                self.consume()?;
                self.enter(next_position)?;
                let value = self.parse_array(true)?;
                self.leave();
                (value, true)
            }
            _ => {
                return Err(self.error(
                    next_position,
                    ErrorCode::ExpectedNodeBody,
                    "expected ':', '{', or '[' after key",
                ));
            }
        };
        let _ = name_position;

        if let Value::Object(object) = &value {
            return Ok(Node::Block(Block {
                path: String::new(),
                replace: false,
                name: name.text.clone(),
                children: object.children.clone(),
                line: name.line,
                col: name.col,
                leading_comments: leading,
                trailing_comments: object.trailing_comments.clone(),
            }));
        }
        if let Value::Array(array) = &value {
            if array.element_type == ValueType::Object || (colonless && array.items.is_empty()) {
                return Ok(Node::BlockArray(BlockArray {
                    path: String::new(),
                    name: name.text.clone(),
                    items: array.items.clone(),
                    line: name.line,
                    col: name.col,
                    leading_comments: leading,
                    trailing_comments: array.trailing_comments.clone(),
                }));
            }
        }
        let trailing = self.try_trailing_comment(self.lex.position().line)?;
        Ok(Node::Field(Field {
            path: String::new(),
            key: name.text,
            value,
            line: name.line,
            col: name.col,
            leading_comments: leading,
            trailing_comment: trailing,
        }))
    }

    fn parse_operation(&mut self, leading: Vec<String>) -> Result<Node, ParseError> {
        self.consume()?; // @
        let operation = self.expect(Tag::Ident)?;
        let operation_position = Position::new(operation.line, operation.col);
        if operation.text != "delete" && operation.text != "replace" {
            return Err(self.error(
                operation_position,
                ErrorCode::UnknownOverlayOperation,
                "unknown overlay operation",
            ));
        }
        let key = self.parse_key()?;
        if operation.text == "delete" {
            let trailing = self.try_trailing_comment(self.lex.position().line)?;
            return Ok(Node::Delete(Delete {
                path: String::new(),
                key: key.text.clone(),
                line: key.line,
                col: key.col,
                leading_comments: leading,
                trailing_comment: trailing,
            }));
        }
        let (open_tag, open_position) = {
            let token = self.peek()?;
            (token.tag, Position::new(token.line, token.col))
        };
        if open_tag != Tag::LBrace {
            return Err(self.error(
                open_position,
                ErrorCode::ExpectedReplacementBlock,
                "@replace requires an object body in braces",
            ));
        }
        self.consume()?;
        let value = self.parse_object(open_position)?;
        let Value::Object(object) = value else {
            unreachable!("parse_object returns an object value");
        };
        Ok(Node::Block(Block {
            path: String::new(),
            replace: true,
            name: key.text,
            children: object.children,
            line: key.line,
            col: key.col,
            leading_comments: leading,
            trailing_comments: object.trailing_comments,
        }))
    }

    /// The opening brace has already been consumed.
    fn parse_object(&mut self, open: Position) -> Result<Value, ParseError> {
        self.enter(open)?;
        let mut children: Vec<Node> = Vec::new();
        loop {
            let tag = self.peek()?.tag;
            if tag == Tag::RBrace {
                let trailing = self.drain_comments();
                self.consume()?;
                self.leave();
                return Ok(Value::Object(ObjectBody {
                    children: merge_nodes(children),
                    trailing_comments: trailing,
                }));
            }
            if tag == Tag::Eof {
                let position = {
                    let token = self.peek()?;
                    Position::new(token.line, token.col)
                };
                return Err(self.error(
                    position,
                    ErrorCode::UnterminatedBlock,
                    "unterminated block, expected '}'",
                ));
            }
            let child = self.parse_node()?;
            children.push(child);
        }
    }

    fn parse_value(&mut self) -> Result<Value, ParseError> {
        let token = self.consume()?;
        let position = Position::new(token.line, token.col);
        match token.tag {
            Tag::Int => {
                let n: i64 = token.text.parse().map_err(|_| {
                    self.error(position, ErrorCode::InvalidInt, "invalid integer literal")
                })?;
                Ok(Value::Int(n))
            }
            Tag::Float => {
                let f: f64 = token.text.parse().map_err(|_| {
                    self.error(position, ErrorCode::InvalidFloat, "invalid float literal")
                })?;
                if !f.is_finite() {
                    return Err(self.error(
                        position,
                        ErrorCode::InvalidFloat,
                        "float literal is out of range for a 64-bit float",
                    ));
                }
                Ok(Value::Float(f))
            }
            Tag::BoolTrue => Ok(Value::Bool(true)),
            Tag::BoolFalse => Ok(Value::Bool(false)),
            Tag::NullLiteral => Ok(Value::Null),
            Tag::String => {
                let s = unescape_string(&token.text)
                    .map_err(|message| self.error(position, ErrorCode::InvalidEscape, message))?;
                Ok(Value::String(s))
            }
            Tag::LBrace => self.parse_object(position),
            Tag::LBracket => {
                self.enter(position)?;
                let value = self.parse_array(false)?;
                self.leave();
                Ok(value)
            }
            _ => Err(self.error(
                position,
                ErrorCode::ExpectedValue,
                "expected a value (string, number, bool, null, array, or object)",
            )),
        }
    }

    /// Parse array elements. Callers have already consumed the `[` and entered
    /// the nesting level.
    fn parse_array(&mut self, colonless: bool) -> Result<Value, ParseError> {
        let mut items: Vec<Value> = Vec::new();
        let mut element_type: Option<ValueType> = None;
        let mut need_value = true;
        let mut first_missing_separator: Option<Position> = None;

        loop {
            let (tag, position) = {
                let token = self.peek()?;
                (token.tag, Position::new(token.line, token.col))
            };
            if tag == Tag::RBracket {
                break;
            }
            if !need_value && tag == Tag::Comma {
                self.consume()?;
                need_value = true;
                continue;
            }
            if tag == Tag::Eof {
                let code = if colonless && element_type.map_or(true, |t| t == ValueType::Object) {
                    ErrorCode::UnterminatedBlockArray
                } else {
                    ErrorCode::UnterminatedArray
                };
                return Err(self.error(position, code, "unterminated array, expected ']'"));
            }
            if need_value && tag == Tag::Comma {
                return Err(self.error(
                    position,
                    ErrorCode::ExpectedValue,
                    "expected an array value after ','",
                ));
            }
            if !need_value {
                if element_type.is_some_and(|t| t != ValueType::Null && t != ValueType::Object) {
                    return Err(self.error(
                        position,
                        ErrorCode::ExpectedComma,
                        "expected ',' or ']' in value array",
                    ));
                }
                if first_missing_separator.is_none() {
                    first_missing_separator = Some(position);
                }
            }
            let value = self.parse_value()?;
            let value_type = value.value_type();
            match element_type {
                Some(et) if et != ValueType::Null => {
                    if value_type != ValueType::Null && et != value_type {
                        return Err(self.error(
                            position,
                            ErrorCode::MixedArrayTypes,
                            "mixed types in array",
                        ));
                    }
                }
                // Null does not fix the array's element type.
                _ => element_type = Some(value_type),
            }
            items.push(value);
            need_value = false;
            match element_type {
                Some(ValueType::Object) => first_missing_separator = None,
                Some(et) if et != ValueType::Null => {
                    if let Some(missing) = first_missing_separator {
                        return Err(self.error(
                            missing,
                            ErrorCode::ExpectedComma,
                            "expected ',' or ']' in value array",
                        ));
                    }
                }
                _ => {}
            }
        }

        if let Some(missing) = first_missing_separator {
            return Err(self.error(
                missing,
                ErrorCode::ExpectedComma,
                "expected ',' or ']' in value array",
            ));
        }

        let trailing = self.drain_comments();
        self.consume()?;
        Ok(Value::Array(Array {
            element_type: element_type.unwrap_or(ValueType::String),
            items,
            trailing_comments: trailing,
        }))
    }
}

/// Whether `name` is a header directive rather than a node name. These three
/// words are reserved at the top level of a file only; inside a block they are
/// ordinary identifiers, because a block has no header.
#[must_use]
pub fn is_directive(name: &str) -> bool {
    matches!(name, "skg_version" | "schema_version" | "import")
}

/// Whether an import path escapes the relative-path grammar.
///
/// Absolute imports are rejected outright: they are not portable between
/// machines, and an import that escapes the config tree is a hazard for a
/// parser running as root. The Windows spellings are rejected too so a file
/// cannot mean different things on different hosts. This is deliberately not
/// the host's own absolute-path test, which is platform-dependent.
#[must_use]
pub fn is_absolute_import_path(path: &str) -> bool {
    let bytes = path.as_bytes();
    if bytes.is_empty() {
        return false;
    }
    if bytes[0] == b'/' || bytes[0] == b'\\' {
        return true;
    }
    // Drive-relative or drive-absolute Windows path: "C:", "C:\x", "C:x".
    bytes.len() >= 2 && bytes[1] == b':' && bytes[0].is_ascii_alphabetic()
}

/// Outcome of validating a declared `skg_version`.
enum VersionCheck {
    /// Well-formed and supported.
    Ok,
    /// Not a "major.minor" pair of decimal integers.
    Malformed,
    /// Well-formed, but outside the versions this parser implements.
    Unsupported,
}

/// Classify a declared `skg_version`. Components are parsed as wide unsigned
/// integers, so "300.0" is well formed and unsupported rather than malformed.
fn check_version(version: &str) -> VersionCheck {
    let Some((major, minor)) = version.split_once('.') else {
        return VersionCheck::Malformed;
    };
    let (Ok(major), Ok(minor)) = (major.parse::<u64>(), minor.parse::<u64>()) else {
        return VersionCheck::Malformed;
    };
    if major != SUPPORTED_MAJOR_VERSION || minor > SUPPORTED_MINOR_VERSION {
        return VersionCheck::Unsupported;
    }
    VersionCheck::Ok
}

/// Strip surrounding quotes and process escape sequences. Handles both
/// ordinary `"..."` and triple-quoted `"""..."""` strings. The lexer has
/// already validated every escape, so the error paths here are unreachable
/// for tokens this lexer produced.
pub(crate) fn unescape_string(raw: &str) -> Result<String, &'static str> {
    let bytes = raw.as_bytes();
    if bytes.len() < 2 || bytes[0] != b'"' || bytes[bytes.len() - 1] != b'"' {
        return Ok(raw.to_string());
    }

    // Triple-quoted multiline string: content is verbatim, no escapes.
    if bytes.len() >= 6
        && bytes[1] == b'"'
        && bytes[2] == b'"'
        && bytes[bytes.len() - 2] == b'"'
        && bytes[bytes.len() - 3] == b'"'
    {
        return Ok(raw[3..raw.len() - 3].to_string());
    }

    let inner = &raw[1..raw.len() - 1];
    if !inner.contains('\\') {
        return Ok(inner.to_string());
    }

    let mut out = String::with_capacity(inner.len());
    let mut chars = inner.chars();
    while let Some(c) = chars.next() {
        if c != '\\' {
            out.push(c);
            continue;
        }
        match chars.next() {
            Some('"') => out.push('"'),
            Some('\\') => out.push('\\'),
            Some('n') => out.push('\n'),
            Some('t') => out.push('\t'),
            _ => return Err("invalid escape sequence"),
        }
    }
    Ok(out)
}

/// The position of the first byte that cannot participate in a well-formed
/// UTF-8 source, computed in the same byte-column space as diagnostics.
fn first_invalid_utf8(src: &[u8]) -> Option<Position> {
    let mut line: u32 = 1;
    let mut col: u32 = 1;
    let mut index = 0;
    while index < src.len() {
        let byte = src[index];
        let width = match byte {
            0x00..=0x7F => 1,
            0xC2..=0xDF => 2,
            0xE0..=0xEF => 3,
            0xF0..=0xF4 => 4,
            _ => return Some(Position::new(line, col)),
        };
        if index + width > src.len() {
            return Some(Position::new(line, col));
        }
        if std::str::from_utf8(&src[index..index + width]).is_err() {
            return Some(Position::new(line, col));
        }
        if byte == b'\n' {
            line += 1;
            col = 1;
        } else {
            col += width as u32;
        }
        index += width;
    }
    None
}

// Retained for the public API surface; the emitter decides key spelling with
// the same predicate plus the header-directive rule.
#[allow(dead_code)]
fn _assert_is_identifier_public() {
    let _ = is_identifier("name");
}
