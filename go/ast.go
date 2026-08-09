// Package skg implements a parser for the SKG (Static Key Group) configuration language.
//
// SKG is a simple hierarchical key-value format with nested blocks, typed values,
// and import support. It fills the gap between JSON (no comments), YAML (whitespace-sensitive),
// TOML (flat), and CUE (bloated).
//
// Use Unmarshal to decode SKG into Go structs using struct tags:
//
//	type Config struct {
//	    Name  string `skg:"name"`
//	    Theme struct {
//	        Accent string `skg:"accent"`
//	    } `skg:"theme"`
//	}
//
//	var cfg Config
//	err := skg.Unmarshal(data, &cfg)
package skg

// ValueType identifies the kind of value in an SKG field.
type ValueType int

const (
	TypeString ValueType = iota
	TypeInt
	TypeFloat
	TypeBool
	TypeNull
	TypeArray
)

func (t ValueType) String() string {
	switch t {
	case TypeString:
		return "string"
	case TypeInt:
		return "int"
	case TypeFloat:
		return "float"
	case TypeBool:
		return "bool"
	case TypeNull:
		return "null"
	case TypeArray:
		return "array"
	default:
		return "unknown"
	}
}

// Value represents a scalar or array value from a field assignment.
type Value struct {
	Type ValueType

	// Exactly one of these is populated based on Type.
	Str   string  // TypeString
	Int   int64   // TypeInt
	Float float64 // TypeFloat
	Bool  bool    // TypeBool
	Array *Array  // TypeArray
	// TypeNull uses no fields.
}

// Array is a typed array. All elements must be the same type (enforced by parser).
type Array struct {
	ElementType ValueType
	Items       []Value
}

// Field is a key-value pair: `key: value`
type Field struct {
	Key   string
	Value Value
	Line  int
	Col   int
}

// Block is a named scope: `name { children... }`
type Block struct {
	Name     string
	Children []Node
	Line     int
	Col      int
}

// BlockArray is a named list of blocks: `name [ { ... } { ... } ]`
// Each item is a list of child nodes representing one block entry.
type BlockArray struct {
	Name  string
	Items [][]Node
	Line  int
	Col   int
}

// Node is either a field, a block, or a block array.
type Node struct {
	// Exactly one is non-nil.
	Field      *Field
	Block      *Block
	BlockArray *BlockArray
}

// File is the parsed representation of a single .skg file.
type File struct {
	SKGVersion    *string  // skg_version: "1.0" - nil if absent
	SchemaVersion *string  // schema_version: "1.0.0" - nil if absent
	ImportPaths   []string // Raw import path strings
	Children      []Node
}

// ErrorCode is a stable, implementation-independent identifier for a parse
// failure.
//
// Human-readable messages are free to differ between implementations and to be
// reworded at any time; codes are not. Conformance fixtures assert the code, so
// the Go and Zig parsers can word the same failure differently and still be
// held to the same behaviour.
//
// The closed registry lives in testdata/error-codes.json and is documented in
// docs/conformance.md. Every constant below must appear there - the conformance
// suite fails if the two drift.
type ErrorCode string

const (
	// Lexical.
	CodeUnexpectedChar     ErrorCode = "UNEXPECTED_CHAR"
	CodeUnterminatedString ErrorCode = "UNTERMINATED_STRING"
	CodeInvalidEscape      ErrorCode = "INVALID_ESCAPE"

	// Syntax.
	CodeExpectedColon          ErrorCode = "EXPECTED_COLON"
	CodeExpectedRbrace         ErrorCode = "EXPECTED_RBRACE"
	CodeExpectedRbracket       ErrorCode = "EXPECTED_RBRACKET"
	CodeExpectedString         ErrorCode = "EXPECTED_STRING"
	CodeExpectedIdent          ErrorCode = "EXPECTED_IDENT"
	CodeExpectedValue          ErrorCode = "EXPECTED_VALUE"
	CodeExpectedNodeBody       ErrorCode = "EXPECTED_NODE_BODY"
	CodeUnexpectedToken        ErrorCode = "UNEXPECTED_TOKEN"
	CodeUnterminatedBlock      ErrorCode = "UNTERMINATED_BLOCK"
	CodeUnterminatedBlockArray ErrorCode = "UNTERMINATED_BLOCK_ARRAY"
	CodeUnterminatedArray      ErrorCode = "UNTERMINATED_ARRAY"
	CodeMixedArrayTypes        ErrorCode = "MIXED_ARRAY_TYPES"
	CodeInvalidInt             ErrorCode = "INVALID_INT"
	CodeInvalidFloat           ErrorCode = "INVALID_FLOAT"

	// Header directives.
	CodeDuplicateSKGVersion    ErrorCode = "DUPLICATE_SKG_VERSION"
	CodeDuplicateSchemaVersion ErrorCode = "DUPLICATE_SCHEMA_VERSION"
	CodeMalformedSKGVersion    ErrorCode = "MALFORMED_SKG_VERSION"
	CodeUnsupportedSKGVersion  ErrorCode = "UNSUPPORTED_SKG_VERSION"
	CodeUnterminatedImportList ErrorCode = "UNTERMINATED_IMPORT_LIST"
	CodeExpectedImportPath     ErrorCode = "EXPECTED_IMPORT_PATH"

	// Resource limits.
	CodeNestingTooDeep ErrorCode = "NESTING_TOO_DEEP"
	CodeFileTooLarge   ErrorCode = "FILE_TOO_LARGE"

	// Import resolution. Only a parser that resolves imports from disk can
	// produce these; the Go parser records import paths but does not yet
	// resolve them (see go/conformance.json).
	CodeCircularImport     ErrorCode = "CIRCULAR_IMPORT"
	CodeImportNotFound     ErrorCode = "IMPORT_NOT_FOUND"
	CodeImportChainTooDeep ErrorCode = "IMPORT_CHAIN_TOO_DEEP"

	// Fallback. Never expected in a fixture - seeing it means a diagnostic
	// site is missing its code.
	CodeUnknown ErrorCode = "UNKNOWN"
)

// ErrorCodes lists every code this implementation knows about, in registry
// order. The conformance suite checks it against testdata/error-codes.json.
var ErrorCodes = []ErrorCode{
	CodeUnexpectedChar,
	CodeUnterminatedString,
	CodeInvalidEscape,
	CodeExpectedColon,
	CodeExpectedRbrace,
	CodeExpectedRbracket,
	CodeExpectedString,
	CodeExpectedIdent,
	CodeExpectedValue,
	CodeExpectedNodeBody,
	CodeUnexpectedToken,
	CodeUnterminatedBlock,
	CodeUnterminatedBlockArray,
	CodeUnterminatedArray,
	CodeMixedArrayTypes,
	CodeInvalidInt,
	CodeInvalidFloat,
	CodeDuplicateSKGVersion,
	CodeDuplicateSchemaVersion,
	CodeMalformedSKGVersion,
	CodeUnsupportedSKGVersion,
	CodeUnterminatedImportList,
	CodeExpectedImportPath,
	CodeNestingTooDeep,
	CodeFileTooLarge,
	CodeCircularImport,
	CodeImportNotFound,
	CodeImportChainTooDeep,
	CodeUnknown,
}

// Diagnostic contains structured error information from a parse failure.
type Diagnostic struct {
	// Code is the stable machine-readable classification of the failure.
	Code    ErrorCode
	Path    string
	Line    int
	Col     int
	Message string
}

// ParseError is returned when parsing fails. It implements the error interface
// and contains structured diagnostic information.
type ParseError struct {
	Diag Diagnostic

	// Err is the underlying cause, when the failure originated outside the
	// parser (a filesystem error while loading an import, say). It carries
	// the diagnostic's error code while keeping errors.Is/As working against
	// the original, so callers can still test for fs.ErrNotExist.
	Err error
}

func (e *ParseError) Error() string {
	return e.Diag.Path + ":" + itoa(e.Diag.Line) + ":" + itoa(e.Diag.Col) + ": " + e.Diag.Message
}

func (e *ParseError) Unwrap() error { return e.Err }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
