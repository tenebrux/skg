package skg

import (
	"math"
	"strings"
	"testing"
)

// emitAndReparse emits a single-field file holding v and parses the result back.
func emitAndReparse(t *testing.T, v Value) Value {
	t.Helper()
	src := Emit(&File{Children: []Node{{Field: &Field{Key: "v", Value: v}}}})
	f, err := Parse(src)
	if err != nil {
		t.Fatalf("re-parse of %q failed: %v", src, err)
	}
	if len(f.Children) != 1 || f.Children[0].Field == nil {
		t.Fatalf("re-parse of %q produced %d children", src, len(f.Children))
	}
	return f.Children[0].Field.Value
}

func TestEmitFloatRoundTrip(t *testing.T) {
	cases := []float64{
		0,
		13,
		-13,
		0.92,
		-0.5,
		1e8,
		1e20,
		-1e20,
		1e-20,
		3.141592653589793,
		math.MaxFloat64,
		math.SmallestNonzeroFloat64,
	}

	for _, want := range cases {
		got := emitAndReparse(t, Value{Type: TypeFloat, Float: want})
		if got.Type != TypeFloat {
			t.Errorf("%v: round-tripped as %s, want float", want, got.Type)
			continue
		}
		if got.Float != want {
			t.Errorf("%v: round-tripped to %v", want, got.Float)
		}
	}
}

func TestEmitFloatHasFractionAndNoExponent(t *testing.T) {
	// The SKG grammar has no exponent form, and a float literal must carry a
	// fractional part to lex as a float rather than an int.
	for _, f := range []float64{13, 1e8, 1e20, -1e20} {
		s := formatFloat(f)
		if strings.ContainsAny(s, "eE") {
			t.Errorf("formatFloat(%v) = %q, want no exponent", f, s)
		}
		if !strings.Contains(s, ".") {
			t.Errorf("formatFloat(%v) = %q, want a fractional part", f, s)
		}
	}
}

func TestEmitStringRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		str  string
	}{
		{"empty", ""},
		{"plain", "hello"},
		{"quote", `a "quoted" word`},
		{"backslash", `C:\path\to`},
		{"tab", "a\tb"},
		{"newline", "line1\nline2"},
		{"newline and quote", "line1\n\"line2\""},
		{"triple quote", `a"""b`},
		{"triple quote with newline", "a\"\"\"b\nc"},
		{"trailing quote with newline", "a\nb\""},
		{"only quotes", `"""`},
		{"unicode", "héllo → 世界 🎉"},
		{"unicode multiline", "héllo\n世界"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := emitAndReparse(t, Value{Type: TypeString, Str: tc.str})
			if got.Type != TypeString {
				t.Fatalf("round-tripped as %s, want string", got.Type)
			}
			if got.Str != tc.str {
				t.Errorf("round-tripped %q to %q", tc.str, got.Str)
			}
		})
	}
}

func TestEmitScalarRoundTrip(t *testing.T) {
	cases := []Value{
		{Type: TypeInt, Int: 0},
		{Type: TypeInt, Int: -42},
		{Type: TypeInt, Int: math.MaxInt64},
		{Type: TypeInt, Int: math.MinInt64},
		{Type: TypeBool, Bool: true},
		{Type: TypeBool, Bool: false},
		{Type: TypeNull},
	}

	for _, want := range cases {
		got := emitAndReparse(t, want)
		if got.Type != want.Type || got.Int != want.Int || got.Bool != want.Bool {
			t.Errorf("%+v round-tripped to %+v", want, got)
		}
	}
}

func TestEmitArrayRoundTrip(t *testing.T) {
	want := Value{Type: TypeArray, Array: &Array{ElementType: TypeFloat, Items: []Value{
		{Type: TypeFloat, Float: 1e20},
		{Type: TypeFloat, Float: 0.5},
	}}}

	got := emitAndReparse(t, want)
	if got.Type != TypeArray || got.Array == nil {
		t.Fatalf("round-tripped as %+v, want array", got)
	}
	if len(got.Array.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got.Array.Items))
	}
	for i, item := range got.Array.Items {
		if item.Float != want.Array.Items[i].Float {
			t.Errorf("item %d: got %v, want %v", i, item.Float, want.Array.Items[i].Float)
		}
	}
}
