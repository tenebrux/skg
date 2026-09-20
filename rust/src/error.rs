//! Error codes, diagnostics and typed-error types.
//!
//! The [`ErrorCode`] set is the closed registry in `testdata/error-codes.json`
//! documented in `docs/conformance.md`. Human-readable messages carry no
//! compatibility promise; the codes do.

use std::fmt;

/// Stable, implementation-independent classification of a parse failure.
///
/// The registry lives in `testdata/error-codes.json`; [`ErrorCode::as_str`]
/// spells each tag exactly as the registry does, so fixtures can assert the
/// failure precisely.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum ErrorCode {
    // Lexical.
    UnexpectedChar,
    UnterminatedString,
    InvalidEscape,
    InvalidUtf8,

    // Syntax.
    ExpectedColon,
    ExpectedRbrace,
    ExpectedRbracket,
    ExpectedString,
    ExpectedIdent,
    ExpectedValue,
    ExpectedComma,
    ExpectedNodeBody,
    UnexpectedToken,
    UnterminatedBlock,
    UnterminatedBlockArray,
    UnterminatedArray,
    MixedArrayTypes,
    InvalidInt,
    InvalidFloat,
    UnknownOverlayOperation,
    ExpectedReplacementBlock,

    // Header directives.
    DuplicateSkgVersion,
    DuplicateSchemaVersion,
    MalformedSkgVersion,
    UnsupportedSkgVersion,
    UnterminatedImportList,
    ExpectedImportPath,
    AbsoluteImportPath,
    DirectiveAfterBody,

    // Resource limits.
    NestingTooDeep,
    FileTooLarge,

    // Import resolution. Only file APIs can produce these; byte APIs record
    // import paths without opening them.
    CircularImport,
    ImportNotFound,
    ImportChainTooDeep,
    PathOutsideRoot,
    ResolutionByteLimit,
    ResolutionFileLimit,
    ResolutionNodeLimit,
    ResolutionWorkLimit,

    // Fallback. Never expected in a fixture - seeing it means a diagnostic
    // site is missing its code.
    Unknown,
}

impl ErrorCode {
    /// The registry spelling of this code, as asserted by shared fixtures.
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            ErrorCode::UnexpectedChar => "UNEXPECTED_CHAR",
            ErrorCode::UnterminatedString => "UNTERMINATED_STRING",
            ErrorCode::InvalidEscape => "INVALID_ESCAPE",
            ErrorCode::InvalidUtf8 => "INVALID_UTF8",
            ErrorCode::ExpectedColon => "EXPECTED_COLON",
            ErrorCode::ExpectedRbrace => "EXPECTED_RBRACE",
            ErrorCode::ExpectedRbracket => "EXPECTED_RBRACKET",
            ErrorCode::ExpectedString => "EXPECTED_STRING",
            ErrorCode::ExpectedIdent => "EXPECTED_IDENT",
            ErrorCode::ExpectedValue => "EXPECTED_VALUE",
            ErrorCode::ExpectedComma => "EXPECTED_COMMA",
            ErrorCode::ExpectedNodeBody => "EXPECTED_NODE_BODY",
            ErrorCode::UnexpectedToken => "UNEXPECTED_TOKEN",
            ErrorCode::UnterminatedBlock => "UNTERMINATED_BLOCK",
            ErrorCode::UnterminatedBlockArray => "UNTERMINATED_BLOCK_ARRAY",
            ErrorCode::UnterminatedArray => "UNTERMINATED_ARRAY",
            ErrorCode::MixedArrayTypes => "MIXED_ARRAY_TYPES",
            ErrorCode::InvalidInt => "INVALID_INT",
            ErrorCode::InvalidFloat => "INVALID_FLOAT",
            ErrorCode::UnknownOverlayOperation => "UNKNOWN_OVERLAY_OPERATION",
            ErrorCode::ExpectedReplacementBlock => "EXPECTED_REPLACEMENT_BLOCK",
            ErrorCode::DuplicateSkgVersion => "DUPLICATE_SKG_VERSION",
            ErrorCode::DuplicateSchemaVersion => "DUPLICATE_SCHEMA_VERSION",
            ErrorCode::MalformedSkgVersion => "MALFORMED_SKG_VERSION",
            ErrorCode::UnsupportedSkgVersion => "UNSUPPORTED_SKG_VERSION",
            ErrorCode::UnterminatedImportList => "UNTERMINATED_IMPORT_LIST",
            ErrorCode::ExpectedImportPath => "EXPECTED_IMPORT_PATH",
            ErrorCode::AbsoluteImportPath => "ABSOLUTE_IMPORT_PATH",
            ErrorCode::DirectiveAfterBody => "DIRECTIVE_AFTER_BODY",
            ErrorCode::NestingTooDeep => "NESTING_TOO_DEEP",
            ErrorCode::FileTooLarge => "FILE_TOO_LARGE",
            ErrorCode::CircularImport => "CIRCULAR_IMPORT",
            ErrorCode::ImportNotFound => "IMPORT_NOT_FOUND",
            ErrorCode::ImportChainTooDeep => "IMPORT_CHAIN_TOO_DEEP",
            ErrorCode::PathOutsideRoot => "PATH_OUTSIDE_ROOT",
            ErrorCode::ResolutionByteLimit => "RESOLUTION_BYTE_LIMIT",
            ErrorCode::ResolutionFileLimit => "RESOLUTION_FILE_LIMIT",
            ErrorCode::ResolutionNodeLimit => "RESOLUTION_NODE_LIMIT",
            ErrorCode::ResolutionWorkLimit => "RESOLUTION_WORK_LIMIT",
            ErrorCode::Unknown => "UNKNOWN",
        }
    }

    /// Every code this implementation knows about, in registry order. The
    /// conformance suite checks the set against `testdata/error-codes.json`.
    #[must_use]
    pub fn all() -> &'static [ErrorCode] {
        &[
            ErrorCode::UnexpectedChar,
            ErrorCode::UnterminatedString,
            ErrorCode::InvalidEscape,
            ErrorCode::InvalidUtf8,
            ErrorCode::ExpectedColon,
            ErrorCode::ExpectedRbrace,
            ErrorCode::ExpectedRbracket,
            ErrorCode::ExpectedString,
            ErrorCode::ExpectedIdent,
            ErrorCode::ExpectedValue,
            ErrorCode::ExpectedComma,
            ErrorCode::ExpectedNodeBody,
            ErrorCode::UnexpectedToken,
            ErrorCode::UnterminatedBlock,
            ErrorCode::UnterminatedBlockArray,
            ErrorCode::UnterminatedArray,
            ErrorCode::MixedArrayTypes,
            ErrorCode::InvalidInt,
            ErrorCode::InvalidFloat,
            ErrorCode::UnknownOverlayOperation,
            ErrorCode::ExpectedReplacementBlock,
            ErrorCode::DuplicateSkgVersion,
            ErrorCode::DuplicateSchemaVersion,
            ErrorCode::MalformedSkgVersion,
            ErrorCode::UnsupportedSkgVersion,
            ErrorCode::UnterminatedImportList,
            ErrorCode::ExpectedImportPath,
            ErrorCode::AbsoluteImportPath,
            ErrorCode::DirectiveAfterBody,
            ErrorCode::NestingTooDeep,
            ErrorCode::FileTooLarge,
            ErrorCode::CircularImport,
            ErrorCode::ImportNotFound,
            ErrorCode::ImportChainTooDeep,
            ErrorCode::PathOutsideRoot,
            ErrorCode::ResolutionByteLimit,
            ErrorCode::ResolutionFileLimit,
            ErrorCode::ResolutionNodeLimit,
            ErrorCode::ResolutionWorkLimit,
            ErrorCode::Unknown,
        ]
    }
}

impl fmt::Display for ErrorCode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

/// A 1-based source position. Columns count bytes, not code points.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Position {
    pub line: u32,
    pub col: u32,
}

impl Position {
    #[must_use]
    pub fn new(line: u32, col: u32) -> Self {
        Position { line, col }
    }
}

/// Structured error information from a parse or resolution failure.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Diagnostic {
    /// Stable machine-readable classification. `message` is for humans and
    /// carries no compatibility promise; this does.
    pub code: ErrorCode,
    /// The file path the failure came from.
    pub path: String,
    /// 1-based line; 0 when the failure has no source position (an entry file
    /// that no import statement named).
    pub line: u32,
    /// 1-based byte column; 0 like `line` when there is no position.
    pub col: u32,
    /// Human-readable description. Never empty on a real failure.
    pub message: String,
}

impl Diagnostic {
    #[must_use]
    pub fn new(
        code: ErrorCode,
        path: impl Into<String>,
        line: u32,
        col: u32,
        message: impl Into<String>,
    ) -> Self {
        Diagnostic {
            code,
            path: path.into(),
            line,
            col,
            message: message.into(),
        }
    }

    /// A diagnostic for a failure with no source position, such as an entry
    /// file that no import statement named.
    #[must_use]
    pub fn without_position(
        code: ErrorCode,
        path: impl Into<String>,
        message: impl Into<String>,
    ) -> Self {
        Diagnostic::new(code, path, 0, 0, message)
    }

    #[must_use]
    pub fn position(&self) -> Position {
        Position {
            line: self.line,
            col: self.col,
        }
    }
}

impl fmt::Display for Diagnostic {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "{}:{}:{}: {}",
            self.path, self.line, self.col, self.message
        )
    }
}

impl std::error::Error for Diagnostic {}

/// Returned when parsing fails. `Diagnostic` carries the stable code and the
/// source position.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ParseError {
    pub diagnostic: Diagnostic,
}

impl fmt::Display for ParseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        self.diagnostic.fmt(f)
    }
}

impl std::error::Error for ParseError {}

impl From<Diagnostic> for ParseError {
    fn from(diagnostic: Diagnostic) -> Self {
        ParseError { diagnostic }
    }
}

/// Stable classification of typed (native) loading failures.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum NativeCode {
    /// A parse failure occurred before typed conversion.
    ParseError,
    TypeMismatch,
    MissingField,
    UnknownField,
    InvalidEnum,
    NumberOutOfRange,
    InexactNumber,
    UnsupportedType,
    NestingTooDeep,
    /// A custom `Deserialize` implementation rejected the value.
    CustomError,
}

impl NativeCode {
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            NativeCode::ParseError => "parse_error",
            NativeCode::TypeMismatch => "type_mismatch",
            NativeCode::MissingField => "missing_field",
            NativeCode::UnknownField => "unknown_field",
            NativeCode::InvalidEnum => "invalid_enum",
            NativeCode::NumberOutOfRange => "number_out_of_range",
            NativeCode::InexactNumber => "inexact_number",
            NativeCode::UnsupportedType => "unsupported_type",
            NativeCode::NestingTooDeep => "nesting_too_deep",
            NativeCode::CustomError => "custom_error",
        }
    }
}

impl fmt::Display for NativeCode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

/// Where a typed conversion failure came from.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct SourceLocation {
    pub path: String,
    pub line: u32,
    pub col: u32,
}

impl SourceLocation {
    #[must_use]
    pub fn new(path: impl Into<String>, line: u32, col: u32) -> Self {
        SourceLocation {
            path: path.into(),
            line,
            col,
        }
    }
}

/// Returned when typed loading fails.
///
/// `field_path` is a JSON Pointer (`/items/2/name`, with `~` escaped as `~0`
/// and `/` as `~1`); the empty pointer denotes the document root. Source
/// locations survive imports and merge operations.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DecodeError {
    pub code: NativeCode,
    pub field_path: String,
    pub source: SourceLocation,
    pub message: String,
}

impl DecodeError {
    #[must_use]
    pub fn new(code: NativeCode, message: impl Into<String>) -> Self {
        DecodeError {
            code,
            field_path: String::new(),
            source: SourceLocation::default(),
            message: message.into(),
        }
    }
}

impl fmt::Display for DecodeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "field {}: {}",
            if self.field_path.is_empty() {
                "<root>"
            } else {
                &self.field_path
            },
            self.message
        )
    }
}

impl std::error::Error for DecodeError {}

/// Returned when typed encoding fails.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum EncodeError {
    /// A value cannot be represented in the SKG grammar.
    InvalidValue(String),
    /// A native value maps to nothing SKG V1 can express.
    Unsupported(String),
}

impl fmt::Display for EncodeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            EncodeError::InvalidValue(message) => write!(f, "invalid value: {message}"),
            EncodeError::Unsupported(message) => write!(f, "unsupported: {message}"),
        }
    }
}

impl std::error::Error for EncodeError {}

impl serde::ser::Error for EncodeError {
    fn custom<T: fmt::Display>(message: T) -> Self {
        EncodeError::InvalidValue(message.to_string())
    }
}

/// Returned when file resolution fails.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ResolveError {
    /// A file in the graph failed to parse. The diagnostic names the failing
    /// file and the position inside it.
    Parse(Diagnostic),
    /// Import resolution refused the graph: a cycle, a missing file, a chain
    /// past the depth cap, a path outside the root, or an aggregate budget
    /// that ran out. Failures on an imported file are reported at the import
    /// statement that named it.
    Resolution(Diagnostic),
}

impl ResolveError {
    #[must_use]
    pub fn diagnostic(&self) -> &Diagnostic {
        match self {
            ResolveError::Parse(diagnostic) | ResolveError::Resolution(diagnostic) => diagnostic,
        }
    }

    #[must_use]
    pub fn code(&self) -> ErrorCode {
        self.diagnostic().code
    }
}

impl fmt::Display for ResolveError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        self.diagnostic().fmt(f)
    }
}

impl std::error::Error for ResolveError {}

impl From<ParseError> for ResolveError {
    fn from(error: ParseError) -> Self {
        ResolveError::Parse(error.diagnostic)
    }
}
