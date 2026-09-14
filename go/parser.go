package skg

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxNestingDepth bounds how deeply blocks, block arrays, and arrays may nest.
//
// The parser uses one stack frame per nesting level, and a Go stack overflow is
// a fatal error that recover() cannot catch - so the cap must be enforced by the
// parser rather than left to the runtime. 128 is far past any legitimate config
// yet well under the ~10_000 levels at which the Zig implementation faults, so
// both implementations can enforce the same limit and stay behaviorally
// consistent. Sibling implementations should mirror this value.
const MaxNestingDepth = 128

// MaxFileSize is the largest input the parser accepts, per the SKG spec
// ("The parser SHALL reject files larger than 10MB with a clear error").
const MaxFileSize = 10 * 1024 * 1024

// LanguageVersion is the highest SKG language contract this package accepts.
// The numeric components are exposed as well for callers that gate features
// without parsing the display string.
const (
	LanguageVersion       = "1.0"
	SupportedMajorVersion = 1
	SupportedMinorVersion = 0
)

type parser struct {
	lex    *lexer
	peeked *token
	path   string
	depth  int
}

func newParser(src []byte, path string) *parser {
	return &parser{lex: newLexer(src), path: path}
}

func (p *parser) peek() (token, error) {
	if p.peeked != nil {
		return *p.peeked, nil
	}
	t, err := p.lex.next()
	if err != nil {
		if pe, ok := err.(*ParseError); ok {
			pe.Diag.Path = p.path
		}
		return token{}, err
	}
	p.peeked = &t
	return t, nil
}

func (p *parser) consume() (token, error) {
	if p.peeked != nil {
		t := *p.peeked
		p.peeked = nil
		return t, nil
	}
	t, err := p.lex.next()
	if err != nil {
		if pe, ok := err.(*ParseError); ok {
			pe.Diag.Path = p.path
		}
		return token{}, err
	}
	return t, nil
}

func (p *parser) expect(tag tokenTag) (token, error) {
	t, err := p.consume()
	if err != nil {
		return token{}, err
	}
	if t.tag != tag {
		msg := "unexpected token"
		code := CodeUnexpectedToken
		switch tag {
		case tokColon:
			msg = "expected ':'"
			code = CodeExpectedColon
		case tokRBrace:
			msg = "expected '}'"
			code = CodeExpectedRbrace
		case tokRBracket:
			msg = "expected ']'"
			code = CodeExpectedRbracket
		case tokString:
			msg = "expected string value"
			code = CodeExpectedString
		case tokIdent:
			msg = "expected identifier"
			code = CodeExpectedIdent
		}
		return token{}, &ParseError{Diag: Diagnostic{Code: code, Path: p.path, Line: t.line, Col: t.col, Message: msg}}
	}
	return t, nil
}

// enter records that the parser is descending into a nested construct opened at
// tok. Callers must pair a successful enter with leave. On failure the parse is
// aborted, so the counter is not unwound.
func (p *parser) enter(tok token) error {
	p.depth++
	if p.depth > MaxNestingDepth {
		return &ParseError{Diag: Diagnostic{Code: CodeNestingTooDeep, Path: p.path, Line: tok.line, Col: tok.col, Message: "nesting too deep (max " + itoa(MaxNestingDepth) + ")"}}
	}
	return nil
}

func (p *parser) leave() {
	p.depth--
}

// checkVersion classifies a skg_version string. wellFormed reports whether it is
// a "major.minor" pair of decimal numbers; supported reports whether that pair
// is within what this parser implements (meaningless when wellFormed is false).
func checkVersion(v string) (wellFormed, supported bool) {
	dot := strings.IndexByte(v, '.')
	if dot < 0 {
		return false, false
	}
	major, err := strconv.ParseUint(v[:dot], 10, 64)
	if err != nil {
		return false, false
	}
	minor, err := strconv.ParseUint(v[dot+1:], 10, 64)
	if err != nil {
		return false, false
	}
	if major != SupportedMajorVersion {
		return true, false
	}
	if major == SupportedMajorVersion && minor > SupportedMinorVersion {
		return true, false
	}
	return true, true
}

// isDirective reports whether name is a header directive rather than a node
// name. These three words are reserved at the top level of a file only; inside
// a block they are ordinary identifiers, because a block has no header.
func isDirective(name string) bool {
	return name == "skg_version" || name == "schema_version" || name == "import"
}

func (p *parser) parseFile() (*File, error) {
	var skgVersion *string
	var schemaVersion *string
	var importPaths []string
	var importPositions []Position
	var children []Node

	for {
		t, err := p.peek()
		if err != nil {
			return nil, err
		}
		if t.tag == tokEOF {
			break
		}

		if t.tag == tokIdent && isDirective(t.text) {
			// Every directive belongs to the header, and the header comes
			// before the body (docs/spec.md, "File Structure"). Accepting one
			// after a block or field would freeze a second spelling of the same
			// file at V1, and the emitter has no way to reproduce it.
			if len(children) > 0 {
				return nil, &ParseError{Diag: Diagnostic{Code: CodeDirectiveAfterBody, Path: p.path, Line: t.line, Col: t.col, Message: "header directives must appear before the first block or field"}}
			}
			if _, err := p.consume(); err != nil {
				return nil, err
			}

			if t.text == "import" {
				if err := p.parseImports(&importPaths, &importPositions); err != nil {
					return nil, err
				}
				continue
			}

			if _, err := p.expect(tokColon); err != nil {
				return nil, err
			}
			valTok, err := p.expect(tokString)
			if err != nil {
				return nil, err
			}
			s, err := unescapeString(valTok.text)
			if err != nil {
				return nil, err
			}

			if t.text == "skg_version" {
				if skgVersion != nil {
					return nil, &ParseError{Diag: Diagnostic{Code: CodeDuplicateSKGVersion, Path: p.path, Line: valTok.line, Col: valTok.col, Message: "duplicate skg_version declaration"}}
				}
				wellFormed, supported := checkVersion(s)
				if !wellFormed {
					return nil, &ParseError{Diag: Diagnostic{Code: CodeMalformedSKGVersion, Path: p.path, Line: valTok.line, Col: valTok.col, Message: "malformed skg_version, expected \"major.minor\" (e.g. \"1.0\")"}}
				}
				if !supported {
					return nil, &ParseError{Diag: Diagnostic{Code: CodeUnsupportedSKGVersion, Path: p.path, Line: valTok.line, Col: valTok.col, Message: "skg_version is not supported by this parser"}}
				}
				skgVersion = &s
				continue
			}

			if schemaVersion != nil {
				return nil, &ParseError{Diag: Diagnostic{Code: CodeDuplicateSchemaVersion, Path: p.path, Line: valTok.line, Col: valTok.col, Message: "duplicate schema_version declaration"}}
			}
			schemaVersion = &s
			continue
		}

		node, err := p.parseNode()
		if err != nil {
			return nil, err
		}
		children = append(children, node)
	}

	children = dedup(children)

	return &File{
		Path: p.path, SKGVersion: skgVersion,
		SchemaVersion:   schemaVersion,
		ImportPaths:     importPaths,
		ImportPositions: importPositions,
		Children:        children,
	}, nil
}

func (p *parser) parseImports(list *[]string, positions *[]Position) error {
	t, err := p.peek()
	if err != nil {
		return err
	}
	if t.tag == tokString {
		if _, err := p.consume(); err != nil {
			return err
		}
		return p.appendImport(list, positions, t)
	}
	if t.tag == tokLBracket {
		if _, err := p.consume(); err != nil {
			return err
		}
		needPath := true
		for {
			nt, err := p.peek()
			if err != nil {
				return err
			}
			if nt.tag == tokRBracket {
				p.consume()
				return nil
			}
			if nt.tag == tokEOF {
				return &ParseError{Diag: Diagnostic{Code: CodeUnterminatedImportList, Path: p.path, Line: nt.line, Col: nt.col, Message: "unterminated import list, expected ']'"}}
			}
			if !needPath {
				if nt.tag != tokComma {
					return &ParseError{Diag: Diagnostic{Code: CodeExpectedComma, Path: p.path, Line: nt.line, Col: nt.col, Message: "expected ',' or ']' in import list"}}
				}
				p.consume()
				needPath = true
				continue
			}
			pathTok, err := p.expect(tokString)
			if err != nil {
				return err
			}
			if err := p.appendImport(list, positions, pathTok); err != nil {
				return err
			}
			needPath = false
		}
	}
	return &ParseError{Diag: Diagnostic{Code: CodeExpectedImportPath, Path: p.path, Line: t.line, Col: t.col, Message: "expected import path string or '['"}}
}

// appendImport records one import path and where it was written, rejecting
// absolute paths.
func (p *parser) appendImport(list *[]string, positions *[]Position, tok token) error {
	s, err := unescapeString(tok.text)
	if err != nil {
		return err
	}
	if isAbsoluteImportPath(s) {
		return &ParseError{Diag: Diagnostic{Code: CodeAbsoluteImportPath, Path: p.path, Line: tok.line, Col: tok.col, Message: "import paths must be relative to the importing file"}}
	}
	*list = append(*list, s)
	*positions = append(*positions, Position{Line: tok.line, Col: tok.col})
	return nil
}

// isAbsoluteImportPath reports whether an import path escapes the relative-path
// grammar.
//
// Absolute imports are rejected outright (docs/spec.md, "Imports"): they are not
// portable between machines. This is not containment: parent components and
// symlinks may still reach outside the config tree.
// The Windows spellings are rejected too so a file cannot mean different things
// on different hosts. This is deliberately not filepath.IsAbs, which is
// host-dependent and would let "/etc/x.skg" through on Windows.
// zig/parser.zig carries the same rule.
func isAbsoluteImportPath(path string) bool {
	if path == "" {
		return false
	}
	if path[0] == '/' || path[0] == '\\' {
		return true
	}
	// Drive-relative or drive-absolute Windows path: "C:", `C:\x`, "C:x".
	if len(path) >= 2 && path[1] == ':' {
		c := path[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

// Keys use bare identifiers or ordinary double-quoted strings. Quoting a
// header name makes it data, even at the document root.
func (p *parser) parseKey() (token, error) {
	t, err := p.peek()
	if err != nil {
		return token{}, err
	}
	if t.tag != tokString || strings.HasPrefix(t.text, `"""`) {
		return p.expect(tokIdent)
	}
	if _, err := p.consume(); err != nil {
		return token{}, err
	}
	t.text, err = unescapeString(t.text)
	return t, err
}

func (p *parser) parseNode() (Node, error) {
	node, err := p.parseNodeBody()
	if err != nil {
		return Node{}, err
	}
	switch {
	case node.Field != nil:
		node.Field.Path = p.path
	case node.Block != nil:
		node.Block.Path = p.path
	case node.BlockArray != nil:
		node.BlockArray.Path = p.path
	case node.Delete != nil:
		node.Delete.Path = p.path
	}
	return node, nil
}

func (p *parser) parseNodeBody() (Node, error) {
	start, err := p.peek()
	if err != nil {
		return Node{}, err
	}
	if start.tag == tokAt {
		return p.parseOperation()
	}
	nameTok, err := p.parseKey()
	if err != nil {
		return Node{}, err
	}
	nt, err := p.peek()
	if err != nil {
		return Node{}, err
	}

	if nt.tag == tokColon {
		p.consume()
		value, err := p.parseValue()
		if err != nil {
			return Node{}, err
		}
		return valueNode(nameTok, value), nil
	}
	if nt.tag == tokLBrace {
		value, err := p.parseValue()
		if err != nil {
			return Node{}, err
		}
		return valueNode(nameTok, value), nil
	}
	if nt.tag == tokLBracket {
		p.consume()
		if err := p.enter(nt); err != nil {
			return Node{}, err
		}
		defer p.leave()
		value, err := p.parseArray(true)
		if err != nil {
			return Node{}, err
		}
		if len(value.Array.Items) == 0 {
			return Node{BlockArray: &BlockArray{Name: nameTok.text, Items: value.Array.Items, Line: nameTok.line, Col: nameTok.col}}, nil
		}
		return valueNode(nameTok, value), nil
	}
	return Node{}, &ParseError{Diag: Diagnostic{Code: CodeExpectedNodeBody, Path: p.path, Line: nt.line, Col: nt.col, Message: "expected ':', '{', or '[' after key"}}
}

func (p *parser) parseOperation() (Node, error) {
	p.consume() // @
	op, err := p.expect(tokIdent)
	if err != nil {
		return Node{}, err
	}
	if op.text != "delete" && op.text != "replace" {
		return Node{}, &ParseError{Diag: Diagnostic{Code: CodeUnknownOverlayOperation, Path: p.path, Line: op.line, Col: op.col, Message: "unknown overlay operation"}}
	}
	key, err := p.parseKey()
	if err != nil {
		return Node{}, err
	}
	if op.text == "delete" {
		return Node{Delete: &Delete{Key: key.text, Line: key.line, Col: key.col}}, nil
	}
	open, err := p.peek()
	if err != nil {
		return Node{}, err
	}
	if open.tag != tokLBrace {
		return Node{}, &ParseError{Diag: Diagnostic{Code: CodeExpectedReplacementBlock, Path: p.path, Line: open.line, Col: open.col, Message: "@replace requires an object body in braces"}}
	}
	p.consume()
	value, err := p.parseObject(open)
	if err != nil {
		return Node{}, err
	}
	node := valueNode(key, value)
	node.Block.Replace = true
	return node, nil
}

// Normalize named structured values to SKG's block syntax.
func valueNode(key token, v Value) Node {
	if v.Type == TypeObject {
		return Node{Block: &Block{Name: key.text, Children: v.Object, Line: key.line, Col: key.col}}
	}
	if v.Type == TypeArray && v.Array != nil && v.Array.ElementType == TypeObject {
		return Node{BlockArray: &BlockArray{Name: key.text, Items: v.Array.Items, Line: key.line, Col: key.col}}
	}
	return Node{Field: &Field{Key: key.text, Value: v, Line: key.line, Col: key.col}}
}

// The opening brace has already been consumed.
func (p *parser) parseObject(open token) (Value, error) {
	if err := p.enter(open); err != nil {
		return Value{}, err
	}
	defer p.leave()
	var children []Node
	for {
		t, err := p.peek()
		if err != nil {
			return Value{}, err
		}
		if t.tag == tokRBrace {
			p.consume()
			break
		}
		if t.tag == tokEOF {
			return Value{}, &ParseError{Diag: Diagnostic{Code: CodeUnterminatedBlock, Path: p.path, Line: t.line, Col: t.col, Message: "unterminated block, expected '}'"}}
		}
		child, err := p.parseNode()
		if err != nil {
			return Value{}, err
		}
		children = append(children, child)
	}
	return Value{Type: TypeObject, Object: dedup(children)}, nil
}

func (p *parser) parseValue() (Value, error) {
	t, err := p.consume()
	if err != nil {
		return Value{}, err
	}
	switch t.tag {
	case tokInt:
		n, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			return Value{}, &ParseError{Diag: Diagnostic{Code: CodeInvalidInt, Path: p.path, Line: t.line, Col: t.col, Message: "invalid integer literal"}}
		}
		return Value{Type: TypeInt, Int: n}, nil
	case tokFloat:
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return Value{}, &ParseError{Diag: Diagnostic{Code: CodeInvalidFloat, Path: p.path, Line: t.line, Col: t.col, Message: "invalid float literal"}}
		}
		return Value{Type: TypeFloat, Float: f}, nil
	case tokBoolTrue:
		return Value{Type: TypeBool, Bool: true}, nil
	case tokBoolFalse:
		return Value{Type: TypeBool, Bool: false}, nil
	case tokNullLit:
		return Value{Type: TypeNull}, nil
	case tokString:
		s, err := unescapeString(t.text)
		if err != nil {
			return Value{}, err
		}
		return Value{Type: TypeString, Str: s}, nil
	case tokLBrace:
		return p.parseObject(t)
	case tokLBracket:
		if err := p.enter(t); err != nil {
			return Value{}, err
		}
		defer p.leave()
		return p.parseArray(false)
	default:
		return Value{}, &ParseError{Diag: Diagnostic{Code: CodeExpectedValue, Path: p.path, Line: t.line, Col: t.col, Message: "expected a value (string, number, bool, or array)"}}
	}
}

func (p *parser) parseArray(colonless bool) (Value, error) {
	var items []Value
	var elemType *ValueType
	needValue := true
	var firstMissingSeparator *token

	for {
		t, err := p.peek()
		if err != nil {
			return Value{}, err
		}
		if t.tag == tokRBracket {
			p.consume()
			break
		}
		if !needValue && t.tag == tokComma {
			p.consume()
			needValue = true
			continue
		}
		if t.tag == tokEOF {
			code := CodeUnterminatedArray
			if colonless && (elemType == nil || *elemType == TypeObject) {
				code = CodeUnterminatedBlockArray
			}
			return Value{}, &ParseError{Diag: Diagnostic{Code: code, Path: p.path, Line: t.line, Col: t.col, Message: "unterminated array, expected ']'"}}
		}
		if needValue && t.tag == tokComma {
			return Value{}, &ParseError{Diag: Diagnostic{Code: CodeExpectedValue, Path: p.path, Line: t.line, Col: t.col, Message: "expected an array value after ','"}}
		}
		if !needValue {
			if elemType != nil && *elemType != TypeNull && *elemType != TypeObject {
				return Value{}, &ParseError{Diag: Diagnostic{Code: CodeExpectedComma, Path: p.path, Line: t.line, Col: t.col, Message: "expected ',' or ']' in value array"}}
			}
			if firstMissingSeparator == nil {
				missing := t
				firstMissingSeparator = &missing
			}
		}
		val, err := p.parseValue()
		if err != nil {
			return Value{}, err
		}
		if elemType != nil && *elemType != TypeNull {
			if val.Type != TypeNull && *elemType != val.Type {
				return Value{}, &ParseError{Diag: Diagnostic{Code: CodeMixedArrayTypes, Path: p.path, Line: t.line, Col: t.col, Message: "mixed types in array"}}
			}
		} else {
			et := val.Type
			elemType = &et
		}
		items = append(items, val)
		needValue = false
		if elemType != nil && *elemType == TypeObject {
			firstMissingSeparator = nil
		} else if elemType != nil && *elemType != TypeNull && firstMissingSeparator != nil {
			return Value{}, &ParseError{Diag: Diagnostic{Code: CodeExpectedComma, Path: p.path, Line: firstMissingSeparator.line, Col: firstMissingSeparator.col, Message: "expected ',' or ']' in value array"}}
		}
	}
	if firstMissingSeparator != nil {
		return Value{}, &ParseError{Diag: Diagnostic{Code: CodeExpectedComma, Path: p.path, Line: firstMissingSeparator.line, Col: firstMissingSeparator.col, Message: "expected ',' or ']' in value array"}}
	}

	et := TypeString // default for empty array
	if elemType != nil {
		et = *elemType
	}
	return Value{Type: TypeArray, Array: &Array{ElementType: et, Items: items}}, nil
}

func unescapeString(raw string) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return raw, nil
	}

	// Triple-quoted multiline string
	if len(raw) >= 6 && raw[1] == '"' && raw[2] == '"' &&
		raw[len(raw)-2] == '"' && raw[len(raw)-3] == '"' {
		return raw[3 : len(raw)-3], nil
	}

	inner := raw[1 : len(raw)-1]

	// Fast path: no escapes
	if !strings.ContainsRune(inner, '\\') {
		return inner, nil
	}

	var buf strings.Builder
	buf.Grow(len(inner))
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' {
			i++
			switch inner[i] {
			case '"':
				buf.WriteByte('"')
			case '\\':
				buf.WriteByte('\\')
			case 'n':
				buf.WriteByte('\n')
			case 't':
				buf.WriteByte('\t')
			default:
				return "", &ParseError{Diag: Diagnostic{Code: CodeInvalidEscape, Message: "invalid escape sequence"}}
			}
		} else {
			buf.WriteByte(inner[i])
		}
	}
	return buf.String(), nil
}

func dedup(nodes []Node) []Node {
	return MergeNodes(nil, nodes)
}

// Parse parses SKG source bytes into a composed overlay AST File.
// Deletion/replacement markers remain until MaterializeNodes or file loading.
//
// Parse does not touch the filesystem. Any `import` statement is recorded in
// File.ImportPaths and nothing is loaded - see ParseSource for why, and use
// ParseFile when imports should be resolved.
func Parse(src []byte) (*File, error) {
	return ParseSource(src, "<string>")
}

// ParseSource parses SKG source bytes with a given file path for error messages.
//
// ParseSource performs no filesystem access whatsoever, and the path argument
// is used only to label diagnostics - it is not opened, and imports are not
// resolved relative to it. That is a deliberate guarantee, not an omission: a
// caller handing untrusted bytes to the parser gets no path traversal, no
// symlink following, no cyclic-import work, and no I/O of any kind. Imports are
// still recorded in File.ImportPaths so a caller can resolve them under its own
// policy.
//
// Use ParseFile or UnmarshalFile to have imports loaded and merged.
func ParseSource(src []byte, path string) (*File, error) {
	if len(src) > MaxFileSize {
		return nil, &ParseError{Diag: Diagnostic{Code: CodeFileTooLarge, Path: path, Line: 0, Col: 0, Message: "file too large (max 10MB)"}}
	}
	if line, col, invalid := firstInvalidUTF8(src); invalid {
		return nil, &ParseError{Diag: Diagnostic{Code: CodeInvalidUTF8, Path: path, Line: line, Col: col, Message: "source is not valid UTF-8"}}
	}
	p := newParser(src, path)
	return p.parseFile()
}

func firstInvalidUTF8(src []byte) (line, col int, invalid bool) {
	line, col = 1, 1
	for i := 0; i < len(src); {
		r, size := utf8.DecodeRune(src[i:])
		if r == utf8.RuneError && size == 1 {
			return line, col, true
		}
		if src[i] == '\n' {
			line, col = line+1, 1
		} else {
			col += size
		}
		i += size
	}
	return line, col, false
}

// ParseFile reads and parses an SKG file from disk, resolving its imports.
//
// Each imported file is located relative to the file that imports it, parsed
// under the same limits (MaxFileSize, MaxNestingDepth), and merged in
// declaration order; the importing file's own values overlay everything it
// imported. Import chains are followed to MaxImportDepth levels, and a cycle is
// rejected with an error naming the chain that closed it. File.ImportPaths on
// the returned File still holds the paths exactly as they were written.
//
// The byte-oriented entry points (Parse, ParseSource, Unmarshal) never resolve
// imports - see ParseSource.
func ParseFile(path string) (*File, error) {
	return resolveImports(path)
}

// ParseFileWithOptions resolves imports with explicit aggregate limits and an
// optional canonical root. See ResolveOptions. The returned tree is otherwise
// identical to ParseFile.
func ParseFileWithOptions(path string, options ResolveOptions) (*File, error) {
	return resolveImportsWithOptions(path, options)
}
