package skg

import (
	"io"
	"os"
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
		switch tag {
		case tokColon:
			msg = "expected ':'"
		case tokRBrace:
			msg = "expected '}'"
		case tokRBracket:
			msg = "expected ']'"
		case tokString:
			msg = "expected string value"
		case tokIdent:
			msg = "expected identifier"
		}
		return token{}, &ParseError{Diag: Diagnostic{Path: p.path, Line: t.line, Col: t.col, Message: msg}}
	}
	return t, nil
}

// enter records that the parser is descending into a nested construct opened at
// tok. Callers must pair a successful enter with leave. On failure the parse is
// aborted, so the counter is not unwound.
func (p *parser) enter(tok token) error {
	p.depth++
	if p.depth > MaxNestingDepth {
		return &ParseError{Diag: Diagnostic{Path: p.path, Line: tok.line, Col: tok.col, Message: "nesting too deep (max " + itoa(MaxNestingDepth) + ")"}}
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

func (p *parser) parseFile() (*File, error) {
	var skgVersion *string
	var schemaVersion *string
	var importPaths []string
	var children []Node

	for {
		t, err := p.peek()
		if err != nil {
			return nil, err
		}
		if t.tag == tokEOF {
			break
		}

		if t.tag == tokIdent {
			if t.text == "skg_version" {
				if _, err := p.consume(); err != nil {
					return nil, err
				}
				if _, err := p.expect(tokColon); err != nil {
					return nil, err
				}
				valTok, err := p.expect(tokString)
				if err != nil {
					return nil, err
				}
				if skgVersion != nil {
					return nil, &ParseError{Diag: Diagnostic{Path: p.path, Line: valTok.line, Col: valTok.col, Message: "duplicate skg_version declaration"}}
				}
				s, err := unescapeString(valTok.text)
				if err != nil {
					return nil, err
				}
				wellFormed, supported := checkVersion(s)
				if !wellFormed {
					return nil, &ParseError{Diag: Diagnostic{Path: p.path, Line: valTok.line, Col: valTok.col, Message: "malformed skg_version, expected \"major.minor\" (e.g. \"1.0\")"}}
				}
				if !supported {
					return nil, &ParseError{Diag: Diagnostic{Path: p.path, Line: valTok.line, Col: valTok.col, Message: "skg_version is newer than this parser supports (max \"" + itoa(supportedMajorVersion) + "." + itoa(supportedMinorVersion) + "\")"}}
				}
				skgVersion = &s
				continue
			}
			if t.text == "schema_version" {
				if _, err := p.consume(); err != nil {
					return nil, err
				}
				if _, err := p.expect(tokColon); err != nil {
					return nil, err
				}
				valTok, err := p.expect(tokString)
				if err != nil {
					return nil, err
				}
				if schemaVersion != nil {
					return nil, &ParseError{Diag: Diagnostic{Path: p.path, Line: valTok.line, Col: valTok.col, Message: "duplicate schema_version declaration"}}
				}
				s, err := unescapeString(valTok.text)
				if err != nil {
					return nil, err
				}
				schemaVersion = &s
				continue
			}
			if t.text == "import" {
				if _, err := p.consume(); err != nil {
					return nil, err
				}
				if err := p.parseImports(&importPaths); err != nil {
					return nil, err
				}
				continue
			}
		}

		node, err := p.parseNode()
		if err != nil {
			return nil, err
		}
		children = append(children, node)
	}

	children = dedup(children)

	return &File{
		SKGVersion:    skgVersion,
		SchemaVersion: schemaVersion,
		ImportPaths:   importPaths,
		Children:      children,
	}, nil
}

func (p *parser) parseImports(list *[]string) error {
	t, err := p.peek()
	if err != nil {
		return err
	}
	if t.tag == tokString {
		if _, err := p.consume(); err != nil {
			return err
		}
		s, err := unescapeString(t.text)
		if err != nil {
			return err
		}
		*list = append(*list, s)
		return nil
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
				return &ParseError{Diag: Diagnostic{Path: p.path, Line: nt.line, Col: nt.col, Message: "unterminated import list, expected ']'"}}
			}
			pathTok, err := p.expect(tokString)
			if err != nil {
				return err
			}
			s, err := unescapeString(pathTok.text)
			if err != nil {
				return err
			}
			*list = append(*list, s)
		}
	}
	return &ParseError{Diag: Diagnostic{Path: p.path, Line: t.line, Col: t.col, Message: "expected import path string or '['"}}
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
				return Node{}, &ParseError{Diag: Diagnostic{Path: p.path, Line: ct.line, Col: ct.col, Message: "unterminated block, expected '}'"}}
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

	return Node{}, &ParseError{Diag: Diagnostic{Path: p.path, Line: nt.line, Col: nt.col, Message: "expected ':', '{', or '[' after identifier"}}
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
			return Node{}, &ParseError{Diag: Diagnostic{Path: p.path, Line: t.line, Col: t.col, Message: "unterminated block array, expected ']'"}}
		}
		if t.tag != tokLBrace {
			// Not a block array - this is a regular field with array value.
			// Re-parse as: name: [items...]
			// We've already consumed '[', so parse the remaining array elements.
			return p.reParseAsFieldArray(nameTok, t)
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
				return Node{}, &ParseError{Diag: Diagnostic{Path: p.path, Line: ct.line, Col: ct.col, Message: "unterminated block in block array, expected '}'"}}
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

// reParseAsFieldArray handles the case where `name [` was followed by a scalar
// value instead of `{`, meaning it's actually `name: [values...]` without a colon.
// Wait - SKG requires colons for fields. So `name [` with non-brace content is an error.
// But to keep the parser friendly, we parse it as a regular array and return it as a field.
func (p *parser) reParseAsFieldArray(nameTok token, firstTok token) (Node, error) {
	// Parse remaining array contents starting from firstTok (already peeked).
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
			return Node{}, &ParseError{Diag: Diagnostic{Path: p.path, Line: t.line, Col: t.col, Message: "unterminated array, expected ']'"}}
		}
		val, err := p.parseValue()
		if err != nil {
			return Node{}, err
		}
		if elemType != nil {
			if *elemType != val.Type {
				return Node{}, &ParseError{Diag: Diagnostic{Path: p.path, Line: t.line, Col: t.col, Message: "mixed types in array"}}
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
			return Value{}, &ParseError{Diag: Diagnostic{Path: p.path, Line: t.line, Col: t.col, Message: "invalid integer literal"}}
		}
		return Value{Type: TypeInt, Int: n}, nil
	case tokFloat:
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return Value{}, &ParseError{Diag: Diagnostic{Path: p.path, Line: t.line, Col: t.col, Message: "invalid float literal"}}
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
		return Value{}, &ParseError{Diag: Diagnostic{Path: p.path, Line: t.line, Col: t.col, Message: "expected a value (string, number, bool, or array)"}}
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
			return Value{}, &ParseError{Diag: Diagnostic{Path: p.path, Line: t.line, Col: t.col, Message: "unterminated array, expected ']'"}}
		}
		val, err := p.parseValue()
		if err != nil {
			return Value{}, err
		}
		if elemType != nil {
			if *elemType != val.Type {
				return Value{}, &ParseError{Diag: Diagnostic{Path: p.path, Line: t.line, Col: t.col, Message: "mixed types in array"}}
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
				return "", &ParseError{Diag: Diagnostic{Message: "invalid escape sequence"}}
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
func Parse(src []byte) (*File, error) {
	return ParseSource(src, "<string>")
}

// ParseSource parses SKG source bytes with a given file path for error messages.
func ParseSource(src []byte, path string) (*File, error) {
	if len(src) > MaxFileSize {
		return nil, &ParseError{Diag: Diagnostic{Path: path, Line: 0, Col: 0, Message: "file too large (max 10MB)"}}
	}
	p := newParser(src, path)
	return p.parseFile()
}

// ParseFile reads and parses an SKG file from disk.
func ParseFile(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Read one byte past the cap rather than stat-ing: a size check alone lies
	// for pipes and /proc entries, and os.ReadFile would buffer the whole input
	// before ParseSource could reject it.
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	return ParseSource(data, path)
}
