package skg

// tokenTag identifies the kind of token.
type tokenTag int

const (
	tokInt       tokenTag = iota // 42, -3
	tokFloat                    // 0.92, -13.0
	tokBoolTrue                 // true
	tokBoolFalse                // false
	tokNullLit                  // null
	tokString                   // "..." including quotes
	tokIdent                    // bare word
	tokColon                    // :
	tokLBrace                   // {
	tokRBrace                   // }
	tokLBracket                 // [
	tokRBracket                 // ]
	tokComma                    // ,
	tokEOF
)

type token struct {
	tag  tokenTag
	text string
	line int
	col  int
}

type lexer struct {
	src  []byte
	pos  int
	line int
	col  int
}

func newLexer(src []byte) *lexer {
	return &lexer{src: src, pos: 0, line: 1, col: 1}
}

func (l *lexer) peek() (byte, bool) {
	if l.pos >= len(l.src) {
		return 0, false
	}
	return l.src[l.pos], true
}

func (l *lexer) peekAhead(offset int) (byte, bool) {
	idx := l.pos + offset
	if idx >= len(l.src) {
		return 0, false
	}
	return l.src[idx], true
}

func (l *lexer) advance() byte {
	c := l.src[l.pos]
	l.pos++
	if c == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return c
}

func (l *lexer) skipWhitespaceAndComments() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '#' {
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		} else if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			l.advance()
		} else {
			break
		}
	}
}

func (l *lexer) next() (token, error) {
	l.skipWhitespaceAndComments()

	if l.pos >= len(l.src) {
		return token{tag: tokEOF, line: l.line, col: l.col}, nil
	}

	line := l.line
	col := l.col
	c := l.src[l.pos]

	switch c {
	case ':':
		l.advance()
		return token{tag: tokColon, text: ":", line: line, col: col}, nil
	case '{':
		l.advance()
		return token{tag: tokLBrace, text: "{", line: line, col: col}, nil
	case '}':
		l.advance()
		return token{tag: tokRBrace, text: "}", line: line, col: col}, nil
	case '[':
		l.advance()
		return token{tag: tokLBracket, text: "[", line: line, col: col}, nil
	case ']':
		l.advance()
		return token{tag: tokRBracket, text: "]", line: line, col: col}, nil
	case ',':
		l.advance()
		return token{tag: tokComma, text: ",", line: line, col: col}, nil
	case '"':
		return l.lexString(line, col)
	case '-':
		return l.lexNegativeNumber(line, col)
	}

	if c >= '0' && c <= '9' {
		return l.lexNumber(line, col)
	}
	if isIdentStart(c) {
		return l.lexIdent(line, col)
	}

	return token{}, &ParseError{Diag: Diagnostic{Code: CodeUnexpectedChar, Line: line, Col: col, Message: "unexpected character"}}
}

func (l *lexer) lexString(line, col int) (token, error) {
	start := l.pos
	l.advance() // consume opening "

	// Check for triple-quote
	if c, ok := l.peek(); ok && c == '"' {
		if c2, ok2 := l.peekAhead(1); ok2 && c2 == '"' {
			l.advance() // second "
			l.advance() // third "
			return l.lexMultilineString(start, line, col)
		}
	}

	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' {
			if l.pos+1 >= len(l.src) {
				return token{}, &ParseError{Diag: Diagnostic{Code: CodeUnterminatedString, Line: l.line, Col: l.col, Message: "unterminated string literal"}}
			}
			switch l.src[l.pos+1] {
			case '"', '\\', 'n', 't':
			default:
				return token{}, &ParseError{Diag: Diagnostic{Code: CodeInvalidEscape, Line: l.line, Col: l.col, Message: "invalid escape sequence"}}
			}
			l.pos += 2
			l.col += 2
		} else if c == '"' {
			l.advance() // consume closing "
			return token{tag: tokString, text: string(l.src[start:l.pos]), line: line, col: col}, nil
		} else if c == '\n' {
			return token{}, &ParseError{Diag: Diagnostic{Code: CodeUnterminatedString, Line: l.line, Col: l.col, Message: "unterminated string literal"}}
		} else {
			l.advance()
		}
	}
	return token{}, &ParseError{Diag: Diagnostic{Code: CodeUnterminatedString, Line: l.line, Col: l.col, Message: "unterminated string literal"}}
}

func (l *lexer) lexMultilineString(start int, line, col int) (token, error) {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '"' {
			if c2, ok := l.peekAhead(1); ok && c2 == '"' {
				if c3, ok2 := l.peekAhead(2); ok2 && c3 == '"' {
					l.advance()
					l.advance()
					l.advance()
					return token{tag: tokString, text: string(l.src[start:l.pos]), line: line, col: col}, nil
				}
			}
		}
		l.advance()
	}
	return token{}, &ParseError{Diag: Diagnostic{Code: CodeUnterminatedString, Line: l.line, Col: l.col, Message: "unterminated string literal"}}
}

func (l *lexer) lexNegativeNumber(line, col int) (token, error) {
	if next, ok := l.peekAhead(1); ok && next >= '0' && next <= '9' {
		return l.lexNumber(line, col)
	}
	return token{}, &ParseError{Diag: Diagnostic{Code: CodeUnexpectedChar, Line: line, Col: col, Message: "unexpected character"}}
}

func (l *lexer) lexNumber(line, col int) (token, error) {
	start := l.pos
	if c, _ := l.peek(); c == '-' {
		l.advance()
	}
	for {
		c, ok := l.peek()
		if !ok || c < '0' || c > '9' {
			break
		}
		l.advance()
	}
	if c, ok := l.peek(); ok && c == '.' {
		l.advance()
		for {
			c, ok := l.peek()
			if !ok || c < '0' || c > '9' {
				break
			}
			l.advance()
		}
		return token{tag: tokFloat, text: string(l.src[start:l.pos]), line: line, col: col}, nil
	}
	return token{tag: tokInt, text: string(l.src[start:l.pos]), line: line, col: col}, nil
}

func (l *lexer) lexIdent(line, col int) (token, error) {
	start := l.pos
	for {
		c, ok := l.peek()
		if !ok || !isIdentChar(c) {
			break
		}
		l.advance()
	}
	text := string(l.src[start:l.pos])
	tag := tokIdent
	switch text {
	case "true":
		tag = tokBoolTrue
	case "false":
		tag = tokBoolFalse
	case "null":
		tag = tokNullLit
	}
	return token{tag: tag, text: text, line: line, col: col}, nil
}

func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}
