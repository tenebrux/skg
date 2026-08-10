package skg

import (
	"strconv"
	"strings"
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

// Highest skg_version this parser understands. A file declaring a newer
// version is rejected so it cannot silently lose meaning.
const (
	supportedMajorVersion = 1
	supportedMinorVersion = 0
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
	if major > supportedMajorVersion {
		return true, false
	}
	if major == supportedMajorVersion && minor > supportedMinorVersion {
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
					return nil, &ParseError{Diag: Diagnostic{Code: CodeUnsupportedSKGVersion, Path: p.path, Line: valTok.line, Col: valTok.col, Message: "skg_version is newer than this parser supports (max \"" + itoa(supportedMajorVersion) + "." + itoa(supportedMinorVersion) + "\")"}}
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
		SKGVersion:      skgVersion,
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
		for {
			nt, err := p.peek()
			if err != nil {
				return err
			}
			if nt.tag == tokRBracket {
				p.consume()
				return nil
			}
			if nt.tag == tokComma {
				p.consume()
				continue
			}
			if nt.tag == tokEOF {
				return &ParseError{Diag: Diagnostic{Code: CodeUnterminatedImportList, Path: p.path, Line: nt.line, Col: nt.col, Message: "unterminated import list, expected ']'"}}
			}
			pathTok, err := p.expect(tokString)
			if err != nil {
				return err
			}
			if err := p.appendImport(list, positions, pathTok); err != nil {
				return err
			}
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
// portable between machines, and a config parsed as root - which is how umbra
// reads its manifests - has no business following a path out of the config tree.
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

func (p *parser) parseNode() (Node, error) {
	nameTok, err := p.expect(tokIdent)
	if err != nil {
		return Node{}, err
	}
	nt, err := p.peek()
	if err != nil {
		return Node{}, err
	}

	if nt.tag == tokColon {
		if _, err := p.consume(); err != nil {
			return Node{}, err
		}
		value, err := p.parseValue()
		if err != nil {
			return Node{}, err
		}
		return Node{Field: &Field{Key: nameTok.text, Value: value, Line: nameTok.line, Col: nameTok.col}}, nil
	}
	if nt.tag == tokLBrace {
		if _, err := p.consume(); err != nil {
			return Node{}, err
		}
		if err := p.enter(nt); err != nil {
			return Node{}, err
		}
		defer p.leave()
		var children []Node
		for {
			ct, err := p.peek()
			if err != nil {
				return Node{}, err
			}
			if ct.tag == tokRBrace {
				p.consume()
				break
			}
			if ct.tag == tokEOF {
				return Node{}, &ParseError{Diag: Diagnostic{Code: CodeUnterminatedBlock, Path: p.path, Line: ct.line, Col: ct.col, Message: "unterminated block, expected '}'"}}
			}
			child, err := p.parseNode()
			if err != nil {
				return Node{}, err
			}
			children = append(children, child)
		}
		children = dedup(children)
		return Node{Block: &Block{Name: nameTok.text, Children: children, Line: nameTok.line, Col: nameTok.col}}, nil
	}
	if nt.tag == tokLBracket {
		if _, err := p.consume(); err != nil {
			return Node{}, err
		}
		if err := p.enter(nt); err != nil {
			return Node{}, err
		}
		defer p.leave()
		return p.parseBlockArray(nameTok)
	}

	return Node{}, &ParseError{Diag: Diagnostic{Code: CodeExpectedNodeBody, Path: p.path, Line: nt.line, Col: nt.col, Message: "expected ':', '{', or '[' after identifier"}}
}

func (p *parser) parseBlockArray(nameTok token) (Node, error) {
	// '[' already consumed. Expect a sequence of { children } blocks until ']'.
	// Commas between entries are optional.
	var items [][]Node

	for {
		t, err := p.peek()
		if err != nil {
			return Node{}, err
		}
		if t.tag == tokRBracket {
			p.consume()
			break
		}
		if t.tag == tokComma {
			p.consume()
			continue
		}
		if t.tag == tokEOF {
			return Node{}, &ParseError{Diag: Diagnostic{Code: CodeUnterminatedBlockArray, Path: p.path, Line: t.line, Col: t.col, Message: "unterminated block array, expected ']'"}}
		}
		if t.tag != tokLBrace {
			// `name [` whose first element is not `{` is the colonless
			// scalar-array shorthand (docs/spec.md, "Block Arrays"), so hand
			// the rest of the elements to the scalar-array parser.
			//
			// Once an entry has been parsed this is no longer a choice between
			// two readings: the collection has already committed to being a
			// block array, and a scalar here mixes element kinds. Falling back
			// at that point silently discarded every entry parsed so far,
			// turning `users [ {name: "a"} 99 ]` into `users: [99]` with no
			// diagnostic.
			if len(items) > 0 {
				return Node{}, &ParseError{Diag: Diagnostic{Code: CodeMixedArrayTypes, Path: p.path, Line: t.line, Col: t.col, Message: "mixed block and scalar elements in an array"}}
			}
			return p.reParseAsFieldArray(nameTok)
		}
		p.consume() // consume '{'
		if err := p.enter(t); err != nil {
			return Node{}, err
		}
		var children []Node
		for {
			ct, err := p.peek()
			if err != nil {
				return Node{}, err
			}
			if ct.tag == tokRBrace {
				p.consume()
				break
			}
			if ct.tag == tokEOF {
				return Node{}, &ParseError{Diag: Diagnostic{Code: CodeUnterminatedBlock, Path: p.path, Line: ct.line, Col: ct.col, Message: "unterminated block in block array, expected '}'"}}
			}
			child, err := p.parseNode()
			if err != nil {
				return Node{}, err
			}
			children = append(children, child)
		}
		p.leave()
		children = dedup(children)
		items = append(items, children)
	}
	return Node{BlockArray: &BlockArray{Name: nameTok.text, Items: items, Line: nameTok.line, Col: nameTok.col}}, nil
}

// reParseAsFieldArray parses the colonless scalar-array shorthand: `name [`
// followed by something other than `{` means `name: [values...]` written
// without the colon (docs/spec.md, "Block Arrays"). The `[` is already
// consumed, so only the elements remain.
func (p *parser) reParseAsFieldArray(nameTok token) (Node, error) {
	var items []Value
	var elemType *ValueType

	for {
		t, err := p.peek()
		if err != nil {
			return Node{}, err
		}
		if t.tag == tokRBracket {
			p.consume()
			break
		}
		if t.tag == tokComma {
			p.consume()
			continue
		}
		if t.tag == tokEOF {
			return Node{}, &ParseError{Diag: Diagnostic{Code: CodeUnterminatedArray, Path: p.path, Line: t.line, Col: t.col, Message: "unterminated array, expected ']'"}}
		}
		if t.tag == tokLBrace {
			// The mirror of the check in parseBlockArray: this collection has
			// committed to holding scalars, so a block entry mixes kinds.
			return Node{}, &ParseError{Diag: Diagnostic{Code: CodeMixedArrayTypes, Path: p.path, Line: t.line, Col: t.col, Message: "mixed block and scalar elements in an array"}}
		}
		val, err := p.parseValue()
		if err != nil {
			return Node{}, err
		}
		if elemType != nil {
			if *elemType != val.Type {
				return Node{}, &ParseError{Diag: Diagnostic{Code: CodeMixedArrayTypes, Path: p.path, Line: t.line, Col: t.col, Message: "mixed types in array"}}
			}
		} else {
			et := val.Type
			elemType = &et
		}
		items = append(items, val)
	}

	et := TypeString
	if elemType != nil {
		et = *elemType
	}
	return Node{Field: &Field{Key: nameTok.text, Value: Value{Type: TypeArray, Array: &Array{ElementType: et, Items: items}}, Line: nameTok.line, Col: nameTok.col}}, nil
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
	case tokLBracket:
		if err := p.enter(t); err != nil {
			return Value{}, err
		}
		defer p.leave()
		return p.parseArray()
	default:
		return Value{}, &ParseError{Diag: Diagnostic{Code: CodeExpectedValue, Path: p.path, Line: t.line, Col: t.col, Message: "expected a value (string, number, bool, or array)"}}
	}
}

func (p *parser) parseArray() (Value, error) {
	var items []Value
	var elemType *ValueType

	for {
		t, err := p.peek()
		if err != nil {
			return Value{}, err
		}
		if t.tag == tokRBracket {
			p.consume()
			break
		}
		if t.tag == tokComma {
			p.consume()
			continue
		}
		if t.tag == tokEOF {
			return Value{}, &ParseError{Diag: Diagnostic{Code: CodeUnterminatedArray, Path: p.path, Line: t.line, Col: t.col, Message: "unterminated array, expected ']'"}}
		}
		val, err := p.parseValue()
		if err != nil {
			return Value{}, err
		}
		if elemType != nil {
			if *elemType != val.Type {
				return Value{}, &ParseError{Diag: Diagnostic{Code: CodeMixedArrayTypes, Path: p.path, Line: t.line, Col: t.col, Message: "mixed types in array"}}
			}
		} else {
			et := val.Type
			elemType = &et
		}
		items = append(items, val)
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

// Parse parses SKG source bytes into an AST File.
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
	p := newParser(src, path)
	return p.parseFile()
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
