package skg_test

// This file is a compile-time V1 consumer. It intentionally lives outside the
// skg package so every referenced name, field, method, and signature must remain
// available to downstream code throughout 1.x.

import skg "github.com/tenebrux/skg/go"

type v1Config struct {
	Value uint16 `skg:"value"`
}

type v1Hook uint16

func (*v1Hook) DecodeSKG(*skg.DecodeContext, skg.Value) error { return nil }
func (v1Hook) ValidateSKG(*skg.DecodeContext) error           { return nil }

var (
	_ func([]byte) (*skg.File, error)                                       = skg.Parse
	_ func([]byte, string) (*skg.File, error)                               = skg.ParseSource
	_ func(string) (*skg.File, error)                                       = skg.ParseFile
	_ func(string, skg.ResolveOptions) (*skg.File, error)                   = skg.ParseFileWithOptions
	_ func(*skg.File) []byte                                                = skg.Emit
	_ func([]skg.Node, []skg.Node) []skg.Node                               = skg.MergeNodes
	_ func([]skg.Node) []skg.Node                                           = skg.MaterializeNodes
	_ func(any) ([]byte, error)                                             = skg.Marshal
	_ func([]byte, any) error                                               = skg.Unmarshal
	_ func(string, any) error                                               = skg.UnmarshalFile
	_ func(string, any, skg.ResolveOptions) error                           = skg.UnmarshalFileWithOptions
	_ func([]byte, string, skg.DecodeOptions) (v1Config, error)             = skg.DecodeSource[v1Config]
	_ func(string, skg.DecodeOptions) (v1Config, error)                     = skg.DecodeFile[v1Config]
	_ func(string, skg.DecodeOptions, skg.ResolveOptions) (v1Config, error) = skg.DecodeFileWithOptions[v1Config]
	_ func([]byte, string, any, skg.DecodeOptions) error                    = skg.DecodeSourceInto
	_ func(string, any, skg.DecodeOptions) error                            = skg.DecodeFileInto
	_ func(string, any, skg.DecodeOptions, skg.ResolveOptions) error        = skg.DecodeFileIntoWithOptions

	_ func(skg.ValueType) string                                                 = skg.ValueType.String
	_ func(*skg.ParseError) string                                               = (*skg.ParseError).Error
	_ func(*skg.ParseError) error                                                = (*skg.ParseError).Unwrap
	_ func(*skg.DecodeError) string                                              = (*skg.DecodeError).Error
	_ func(*skg.DecodeError) error                                               = (*skg.DecodeError).Unwrap
	_ func(*skg.InvalidUnmarshalError) string                                    = (*skg.InvalidUnmarshalError).Error
	_ func(*skg.DecodeContext, skg.Value, any) error                             = (*skg.DecodeContext).Decode
	_ func(*skg.DecodeContext, skg.Value, string, skg.SourceLocation, any) error = (*skg.DecodeContext).DecodeChild
	_ func(*skg.DecodeContext, skg.NativeCode, string) error                     = (*skg.DecodeContext).Fail

	_ skg.NativeDecoder   = (*v1Hook)(nil)
	_ skg.NativeValidator = v1Hook(0)
	_ []skg.ErrorCode     = skg.ErrorCodes
	_ int                 = skg.MaxFileSize
	_ int                 = skg.MaxNestingDepth
	_ string              = skg.LanguageVersion
	_ int                 = skg.SupportedMajorVersion
	_ int                 = skg.SupportedMinorVersion
	_ int                 = skg.MaxNativeNestingDepth
	_ int                 = skg.MaxImportDepth
	_ int64               = skg.DefaultMaxResolveBytes
	_ int                 = skg.DefaultMaxResolveFiles
	_ int                 = skg.DefaultMaxResolveNodes
	_ int64               = skg.DefaultMaxResolveMergeWork
)

var v1ASTSurface = []any{
	skg.Array{ElementType: skg.TypeString, Items: []skg.Value{}},
	skg.Value{Type: skg.TypeString, Str: "", Int: 0, Float: 0, Bool: false, Array: nil, Object: []skg.Node{}},
	skg.Field{Path: "", Key: "key", Value: skg.Value{}, Line: 1, Col: 1},
	skg.Block{Path: "", Replace: false, Name: "block", Children: []skg.Node{}, Line: 1, Col: 1},
	skg.BlockArray{Path: "", Name: "items", Items: []skg.Value{}, Line: 1, Col: 1},
	skg.Delete{Path: "", Key: "key", Line: 1, Col: 1},
	skg.Node{Delete: nil, Field: nil, Block: nil, BlockArray: nil},
	skg.Position{Line: 1, Col: 1},
	skg.File{Path: "", SKGVersion: nil, SchemaVersion: nil, ImportPaths: []string{}, ImportPositions: []skg.Position{}, Children: []skg.Node{}, ImportsResolved: false},
	skg.Diagnostic{Code: skg.CodeUnknown, Path: "", Line: 0, Col: 0, Message: ""},
	skg.ParseError{Diag: skg.Diagnostic{}, Err: nil},
	skg.ResolveOptions{Root: "", MaxBytes: 1, MaxFiles: 1, MaxNodes: 1, MaxMergeWork: 1},
	skg.SourceLocation{Path: "", Line: 0, Col: 0},
	skg.DecodeOptions{RejectUnknownFields: false, AllowLossyNumbers: false, AllowMissingFields: false},
	skg.DecodeError{Code: skg.NativeCustomError, FieldPath: "", Source: skg.SourceLocation{}, Message: "", Err: nil},
	skg.DecodeContext{Options: skg.DecodeOptions{}, FieldPath: "", Source: skg.SourceLocation{}},
	skg.InvalidUnmarshalError{Type: nil},
}

var v1ValueTypes = [...]skg.ValueType{
	skg.TypeString, skg.TypeInt, skg.TypeFloat, skg.TypeBool,
	skg.TypeNull, skg.TypeArray, skg.TypeObject,
}

var v1ParseCodes = [...]skg.ErrorCode{
	skg.CodeUnexpectedChar, skg.CodeUnterminatedString, skg.CodeInvalidEscape, skg.CodeInvalidUTF8,
	skg.CodeExpectedColon, skg.CodeExpectedRbrace, skg.CodeExpectedRbracket, skg.CodeExpectedString,
	skg.CodeExpectedIdent, skg.CodeExpectedValue, skg.CodeExpectedComma, skg.CodeExpectedNodeBody,
	skg.CodeUnexpectedToken, skg.CodeUnterminatedBlock, skg.CodeUnterminatedBlockArray,
	skg.CodeUnterminatedArray, skg.CodeMixedArrayTypes, skg.CodeInvalidInt, skg.CodeInvalidFloat,
	skg.CodeUnknownOverlayOperation, skg.CodeExpectedReplacementBlock, skg.CodeDuplicateSKGVersion,
	skg.CodeDuplicateSchemaVersion, skg.CodeMalformedSKGVersion, skg.CodeUnsupportedSKGVersion,
	skg.CodeUnterminatedImportList, skg.CodeExpectedImportPath, skg.CodeAbsoluteImportPath,
	skg.CodeDirectiveAfterBody, skg.CodeNestingTooDeep, skg.CodeFileTooLarge, skg.CodeCircularImport,
	skg.CodeImportNotFound, skg.CodeImportChainTooDeep, skg.CodePathOutsideRoot,
	skg.CodeResolutionByteLimit, skg.CodeResolutionFileLimit, skg.CodeResolutionNodeLimit,
	skg.CodeResolutionWorkLimit, skg.CodeUnknown,
}

var v1NativeCodes = [...]skg.NativeCode{
	skg.NativeParseError, skg.NativeTypeMismatch, skg.NativeMissingField, skg.NativeUnknownField,
	skg.NativeInvalidEnum, skg.NativeNumberOutOfRange, skg.NativeInexactNumber,
	skg.NativeUnsupportedType, skg.NativeNestingTooDeep, skg.NativeCustomError,
}

var _, _, _, _ = v1ASTSurface, v1ValueTypes, v1ParseCodes, v1NativeCodes
