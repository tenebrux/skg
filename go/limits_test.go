package skg

import (
	"strings"
	"testing"
)

func nestedArrays(n int) []byte {
	return []byte("x: " + strings.Repeat("[", n) + strings.Repeat("]", n))
}

func nestedBlocks(n int) []byte {
	return []byte(strings.Repeat("a { ", n) + strings.Repeat("} ", n))
}

func nestedBlockArrays(n int) []byte {
	// Each level contributes two nesting levels: the '[' and the item's '{'.
	return []byte(strings.Repeat("a [ { ", n) + strings.Repeat("} ] ", n))
}

func TestNestingDepthLimitAtBoundary(t *testing.T) {
	cases := []struct {
		name string
		src  []byte
	}{
		{"arrays", nestedArrays(MaxNestingDepth)},
		{"blocks", nestedBlocks(MaxNestingDepth)},
		{"block_arrays", nestedBlockArrays(MaxNestingDepth / 2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.src); err != nil {
				t.Fatalf("depth %d should be accepted, got %v", MaxNestingDepth, err)
			}
		})
	}
}

// Regression: unbounded recursion used to end in a fatal stack overflow that
// recover() cannot catch, so a consumer had no way to defend itself.
func TestNestingDepthLimitExceeded(t *testing.T) {
	cases := []struct {
		name string
		src  []byte
	}{
		{"arrays", nestedArrays(MaxNestingDepth + 1)},
		{"blocks", nestedBlocks(MaxNestingDepth + 1)},
		{"block_arrays", nestedBlockArrays(MaxNestingDepth/2 + 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Parse(tc.src)
			if err == nil {
				t.Fatalf("expected nesting depth error, got file %v", f)
			}
			pe, ok := err.(*ParseError)
			if !ok {
				t.Fatalf("expected *ParseError, got %T: %v", err, err)
			}
			if !strings.Contains(pe.Diag.Message, "nesting too deep") {
				t.Errorf("unexpected message: %q", pe.Diag.Message)
			}
			if pe.Diag.Line < 1 || pe.Diag.Col < 1 {
				t.Errorf("expected a source position, got %d:%d", pe.Diag.Line, pe.Diag.Col)
			}
		})
	}
}
