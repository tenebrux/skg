package skg

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Emit serializes an AST File back to canonical SKG text.
func Emit(f *File) []byte {
	var buf strings.Builder

	if f.SKGVersion != nil {
		fmt.Fprintf(&buf, "skg_version: %q\n", *f.SKGVersion)
	}
	if f.SchemaVersion != nil {
		fmt.Fprintf(&buf, "schema_version: %q\n", *f.SchemaVersion)
	}

	if len(f.ImportPaths) > 0 {
		if len(f.ImportPaths) == 1 {
			fmt.Fprintf(&buf, "import %q\n", f.ImportPaths[0])
		} else {
			buf.WriteString("import [\n")
			for i, p := range f.ImportPaths {
				fmt.Fprintf(&buf, "  %q", p)
				if i+1 < len(f.ImportPaths) {
					buf.WriteByte(',')
				}
				buf.WriteByte('\n')
			}
			buf.WriteString("]\n")
		}
	}

	hasHeader := f.SKGVersion != nil || f.SchemaVersion != nil || len(f.ImportPaths) > 0
	if hasHeader && len(f.Children) > 0 {
		buf.WriteByte('\n')
	}

	emitNodes(&buf, f.Children, 0)
	return []byte(buf.String())
}

func emitNodes(buf *strings.Builder, nodes []Node, depth int) {
	for i, n := range nodes {
		if n.Field != nil {
			writeIndent(buf, depth)
			buf.WriteString(n.Field.Key)
			buf.WriteString(": ")
			emitValue(buf, n.Field.Value, depth)
			buf.WriteByte('\n')
		} else if n.Block != nil {
			if i > 0 && depth == 0 {
				buf.WriteByte('\n')
			}
			writeIndent(buf, depth)
			buf.WriteString(n.Block.Name)
			buf.WriteString(" {\n")
			emitNodes(buf, n.Block.Children, depth+1)
			writeIndent(buf, depth)
			buf.WriteString("}\n")
		} else if n.BlockArray != nil {
			if i > 0 && depth == 0 {
				buf.WriteByte('\n')
			}
			writeIndent(buf, depth)
			buf.WriteString(n.BlockArray.Name)
			buf.WriteString(" [\n")
			for _, item := range n.BlockArray.Items {
				writeIndent(buf, depth+1)
				buf.WriteString("{\n")
				emitNodes(buf, item, depth+2)
				writeIndent(buf, depth+1)
				buf.WriteString("}\n")
			}
			writeIndent(buf, depth)
			buf.WriteString("]\n")
		}
	}
}

func emitValue(buf *strings.Builder, v Value, depth int) {
	switch v.Type {
	case TypeInt:
		fmt.Fprintf(buf, "%d", v.Int)
	case TypeFloat:
		buf.WriteString(formatFloat(v.Float))
	case TypeBool:
		if v.Bool {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case TypeString:
		if strings.Contains(v.Str, "\n") && canEmitMultiline(v.Str) {
			buf.WriteString(`"""`)
			buf.WriteString(v.Str)
			buf.WriteString(`"""`)
		} else {
			buf.WriteByte('"')
			writeEscaped(buf, v.Str)
			buf.WriteByte('"')
		}
	case TypeNull:
		buf.WriteString("null")
	case TypeArray:
		buf.WriteByte('[')
		if v.Array != nil {
			for i, item := range v.Array.Items {
				if i > 0 {
					buf.WriteString(", ")
				}
				emitValue(buf, item, depth)
			}
		}
		buf.WriteByte(']')
	}
}

// formatFloat renders a float as an SKG float literal. The SKG grammar has no
// exponent form, so the value is written as plain decimal digits and always
// carries a fractional part - large or small magnitudes therefore expand to
// long strings, matching the Zig emitter.
//
// SKG has no literal for NaN or infinity. Marshal rejects those values up
// front; Emit cannot report an error, so it degrades them to null rather than
// writing output that will not parse.
func formatFloat(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "null"
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// canEmitMultiline reports whether s survives a `"""..."""` round-trip.
// Multiline literals are taken verbatim, with no escape processing, so an
// embedded `"""` - or a trailing `"` that merges with the closing delimiter -
// would end the literal early. Such values are emitted as an escaped
// double-quoted string instead.
func canEmitMultiline(s string) bool {
	return !strings.Contains(s, `"""`) && !strings.HasSuffix(s, `"`)
}

func writeEscaped(buf *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			buf.WriteByte(s[i])
		}
	}
}

func writeIndent(buf *strings.Builder, depth int) {
	for i := 0; i < depth; i++ {
		buf.WriteString("  ")
	}
}
