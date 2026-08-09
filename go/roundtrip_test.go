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

func TestMarshalRejectsNonFiniteFloat(t *testing.T) {
	type Config struct {
		F float64 `skg:"f"`
	}
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := Marshal(Config{F: f}); err == nil {
			t.Errorf("Marshal(%v) succeeded, want an error", f)
		}
	}
}

func TestMarshalUnmarshalStringFloatRoundTrip(t *testing.T) {
	type Config struct {
		Note  string  `skg:"note"`
		Ratio float64 `skg:"ratio"`
	}

	orig := Config{Note: "a\"\"\"b\nc", Ratio: 1e20}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal of %q failed: %v", data, err)
	}
	if got != orig {
		t.Errorf("round-trip changed %+v to %+v", orig, got)
	}
}

func TestUnmarshalIntOverflow(t *testing.T) {
	type Config struct {
		N int8   `skg:"n"`
		U uint8  `skg:"u"`
		S uint16 `skg:"s"`
	}

	cases := []struct {
		name string
		src  string
	}{
		{"signed overflow", "n: 200\n"},
		{"signed underflow", "n: -200\n"},
		{"unsigned overflow", "u: 300\n"},
		{"unsigned negative", "s: -1\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			err := Unmarshal([]byte(tc.src), &cfg)
			if err == nil {
				t.Fatalf("expected an error, got %+v", cfg)
			}
			if !strings.Contains(err.Error(), "skg: field") {
				t.Errorf("error should name the field, got %v", err)
			}
			if cfg != (Config{}) {
				t.Errorf("target was modified on error: %+v", cfg)
			}
		})
	}
}

func TestUnmarshalFloatOverflow(t *testing.T) {
	type Config struct {
		F float32 `skg:"f"`
	}
	var cfg Config
	if err := Unmarshal([]byte("f: 1"+strings.Repeat("0", 300)+".0\n"), &cfg); err == nil {
		t.Fatalf("expected an overflow error, got %v", cfg.F)
	}
}

func TestUnsignedRoundTrip(t *testing.T) {
	type Config struct {
		Port uint16 `skg:"port"`
		Size uint64 `skg:"size"`
		Max  uint8  `skg:"max"`
	}

	orig := Config{Port: 8080, Size: 1 << 40, Max: 255}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal of %q failed: %v", data, err)
	}
	if got != orig {
		t.Errorf("round-trip changed %+v to %+v", orig, got)
	}
}

type embedBase struct {
	ID   string `skg:"id"`
	Rank int64  `skg:"rank"`
}

type EmbedMeta struct {
	Owner string `skg:"owner"`
}

func TestEmbeddedStructRoundTrip(t *testing.T) {
	type Config struct {
		embedBase
		*EmbedMeta
		Name string `skg:"name"`
	}

	orig := Config{
		embedBase: embedBase{ID: "abc", Rank: 3},
		EmbedMeta: &EmbedMeta{Owner: "levi"},
		Name:      "cfg",
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{`id: "abc"`, "rank: 3", `owner: "levi"`, `name: "cfg"`} {
		if !strings.Contains(out, want) {
			t.Errorf("emitted output missing %q:\n%s", want, out)
		}
	}

	var got Config
	if err := Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != orig.ID || got.Rank != orig.Rank || got.Name != orig.Name {
		t.Errorf("round-trip changed %+v to %+v", orig, got)
	}
	if got.EmbedMeta == nil || got.Owner != orig.Owner {
		t.Errorf("embedded pointer did not round-trip: %+v", got.EmbedMeta)
	}
}

func TestEmbeddedStructOuterFieldWins(t *testing.T) {
	// A field declared on the outer struct shadows a promoted one, so the
	// promoted field must be neither decoded into nor emitted.
	type Config struct {
		embedBase
		ID string `skg:"id"`
	}

	var got Config
	if err := Unmarshal([]byte(`id: "outer"`), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "outer" {
		t.Errorf("expected outer field to receive the value, got %q", got.ID)
	}
	if got.embedBase.ID != "" {
		t.Errorf("promoted field should have been shadowed, got %q", got.embedBase.ID)
	}

	data, err := Marshal(Config{embedBase: embedBase{ID: "inner"}, ID: "outer"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "id:") != 1 {
		t.Errorf("expected a single id field, got:\n%s", data)
	}
	if !strings.Contains(string(data), `id: "outer"`) {
		t.Errorf("expected the outer value to be emitted, got:\n%s", data)
	}
}

func TestEmbeddedNilPointerMarshal(t *testing.T) {
	type Config struct {
		*EmbedMeta
		Name string `skg:"name"`
	}

	data, err := Marshal(Config{Name: "cfg"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "owner") {
		t.Errorf("nil embedded pointer should emit nothing, got:\n%s", data)
	}
}

func TestEmbeddedUnexportedPointerErrors(t *testing.T) {
	// An embedded pointer to an unexported type cannot be allocated through
	// reflection. Decoding must report that clearly instead of panicking.
	type Config struct {
		*embedBase
	}

	var cfg Config
	err := Unmarshal([]byte(`id: "abc"`), &cfg)
	if err == nil {
		t.Fatal("expected an error for an unexported embedded pointer")
	}
	if !strings.Contains(err.Error(), "cannot allocate unexported embedded field") {
		t.Errorf("unexpected error: %v", err)
	}
}
