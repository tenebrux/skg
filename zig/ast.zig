/// SKG AST node types.
///
/// All string slices in the AST are allocated from the arena passed to the parser.
/// Free everything by deiniting that arena - do not free individual slices.
/// The type tag of a Value.
pub const ValueType = enum { int, float, bool, string, array, null };

/// Stable, implementation-independent classification of a parse failure.
///
/// Human-readable messages differ between implementations and may be reworded
/// at any time; these tags may not. Conformance fixtures assert the tag name,
/// so `@tagName` has to keep producing the exact registry spelling - that is
/// why the fields are SCREAMING_SNAKE instead of the usual Zig snake_case.
/// Deriving the wire string from the tag means the two can never drift.
///
/// The closed registry lives in testdata/error-codes.json and is documented in
/// docs/conformance.md. Adding a tag here without adding it there fails the
/// conformance suite.
pub const ErrorCode = enum {
    // Lexical.
    UNEXPECTED_CHAR,
    UNTERMINATED_STRING,
    INVALID_ESCAPE,

    // Syntax.
    EXPECTED_COLON,
    EXPECTED_RBRACE,
    EXPECTED_RBRACKET,
    EXPECTED_STRING,
    EXPECTED_IDENT,
    EXPECTED_VALUE,
    EXPECTED_NODE_BODY,
    UNEXPECTED_TOKEN,
    UNTERMINATED_BLOCK,
    UNTERMINATED_BLOCK_ARRAY,
    UNTERMINATED_ARRAY,
    MIXED_ARRAY_TYPES,
    INVALID_INT,
    INVALID_FLOAT,

    // Header directives.
    DUPLICATE_SKG_VERSION,
    DUPLICATE_SCHEMA_VERSION,
    MALFORMED_SKG_VERSION,
    UNSUPPORTED_SKG_VERSION,
    UNTERMINATED_IMPORT_LIST,
    EXPECTED_IMPORT_PATH,

    // Resource limits.
    NESTING_TOO_DEEP,
    FILE_TOO_LARGE,

    // Import resolution (file API only).
    CIRCULAR_IMPORT,
    IMPORT_NOT_FOUND,
    IMPORT_CHAIN_TOO_DEEP,

    // Fallback. Never expected in a fixture - seeing it means a diagnostic
    // site is missing its code.
    UNKNOWN,
};

/// Structured error context for parse failures.
pub const Diagnostic = struct {
    /// Stable machine-readable classification. `message` is for humans and
    /// carries no compatibility promise; this does.
    code: ErrorCode = .UNKNOWN,
    path: []const u8,
    line: u32,
    col: u32,
    message: []const u8,
};

/// A typed array. All elements must be the same type (enforced by parser).
pub const Array = struct {
    element_type: ValueType,
    items: []Value,
};

/// A scalar or array value from a field assignment.
pub const Value = union(ValueType) {
    int: i64,
    float: f64,
    bool: bool,
    /// Unescaped string content, no surrounding quotes. Allocated from parse arena.
    string: []const u8,
    array: Array,
    null: void,
};

/// A key-value pair: `key: value`
pub const Field = struct {
    key: []const u8, // slice into source (idents are never escaped)
    value: Value,
    line: u32,
    col: u32,
    leading_comments: []const []const u8 = &.{},
    trailing_comment: ?[]const u8 = null,
};

/// A named scope: `name { children... }`
pub const Block = struct {
    name: []const u8, // slice into source
    children: []Node,
    line: u32,
    col: u32,
    leading_comments: []const []const u8 = &.{},
    trailing_comments: []const []const u8 = &.{},
};

/// A named list of blocks: `name [ { ... } { ... } ]`
/// Each item is a slice of child nodes representing one block entry.
pub const BlockArray = struct {
    name: []const u8, // slice into source
    items: [][]Node,
    line: u32,
    col: u32,
    leading_comments: []const []const u8 = &.{},
    trailing_comments: []const []const u8 = &.{},
};

pub const Node = union(enum) {
    field: Field,
    block: Block,
    block_array: BlockArray,
};

/// The parsed representation of a single .skg file.
/// Does not include resolved imports - see root.zig for that.
pub const File = struct {
    /// `skg_version: "1.0"` - unescaped, null if absent. Allocated from parse arena.
    skg_version: ?[]const u8,
    /// `schema_version: "1.0.0"` - unescaped, null if absent. Allocated from parse arena.
    schema_version: ?[]const u8,
    /// Raw import path strings (unescaped, no quotes). Allocated from parse arena.
    import_paths: [][]const u8,
    /// Top-level blocks and fields (after skg_version, imports, schema_version).
    children: []Node,
    /// File path this was parsed from (for error messages and import resolution).
    path: []const u8,
    /// Comments before the first node (or before skg_version/imports).
    leading_comments: []const []const u8 = &.{},
    /// Comments after the last node.
    trailing_comments: []const []const u8 = &.{},
};
