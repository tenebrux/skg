package skg

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
	"unsafe"
)

func expectNativeError(t *testing.T, err error, code NativeCode, path string) *DecodeError {
	t.Helper()
	var d *DecodeError
	if !errors.As(err, &d) || d.Code != code || d.FieldPath != path {
		t.Fatalf("got %#v (%v), want %s at %q", d, err, code, path)
	}
	return d
}

func TestNativePresenceAndCompatibility(t *testing.T) {
	type Config struct {
		Count    uint8  `skg:"count"`
		Optional *uint8 `skg:"optional"`
		Values   []int  `skg:"values"`
	}
	for _, tc := range []struct {
		src  string
		code NativeCode
	}{
		{"", NativeMissingField}, {"count: null", NativeTypeMismatch},
		{"count: -1", NativeNumberOutOfRange}, {"count: 256", NativeNumberOutOfRange},
		{"count: \"1\"", NativeTypeMismatch},
	} {
		value, err := DecodeSource[Config]([]byte(tc.src), "config.skg", DecodeOptions{})
		expectNativeError(t, err, tc.code, "/count")
		if !reflect.DeepEqual(value, Config{}) {
			t.Fatal("failed result was not zero")
		}
	}
	value, err := DecodeSource[Config]([]byte("count: 0 ignored { any: true }"), "", DecodeOptions{})
	if err != nil || value.Count != 0 || value.Optional != nil || value.Values != nil {
		t.Fatalf("%#v %v", value, err)
	}
	_, err = DecodeSource[Config]([]byte("count: 1 extra: true"), "", DecodeOptions{RejectUnknownFields: true})
	expectNativeError(t, err, NativeUnknownField, "/extra")
	legacy := Config{Count: 7}
	if err := Unmarshal([]byte("optional: null"), &legacy); err != nil || legacy.Count != 7 {
		t.Fatalf("legacy absent: %#v %v", legacy, err)
	}
	if err := Unmarshal([]byte("count: null"), &legacy); err != nil || legacy.Count != 0 {
		t.Fatalf("legacy null: %#v %v", legacy, err)
	}
	type Interface struct {
		Value fmt.Stringer   `skg:"value"`
		Items []fmt.Stringer `skg:"items"`
	}
	var interfaces Interface
	if err := Unmarshal([]byte("value: null items: [null]"), &interfaces); err != nil || interfaces.Value != nil || len(interfaces.Items) != 1 || interfaces.Items[0] != nil {
		t.Fatal(interfaces, err)
	}
}

func TestNativeNumerics(t *testing.T) {
	type F32 struct {
		Value float32 `skg:"value"`
	}
	type F64 struct {
		Value float64 `skg:"value"`
	}
	for _, src := range []string{"value: 0.1", "value: 16777217"} {
		_, err := DecodeSource[F32]([]byte(src), "", DecodeOptions{})
		expectNativeError(t, err, NativeInexactNumber, "/value")
		if _, err := DecodeSource[F32]([]byte(src), "", DecodeOptions{AllowLossyNumbers: true}); err != nil {
			t.Fatal(err)
		}
	}
	for _, src := range []string{"value: 0.5", "value: 16777218", "value: -9223372036854775808"} {
		if _, err := DecodeSource[F32]([]byte(src), "", DecodeOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := DecodeSource[F64]([]byte("value: 9223372036854775807"), "", DecodeOptions{})
	expectNativeError(t, err, NativeInexactNumber, "/value")
	v, err := DecodeSource[F64]([]byte("value: -9223372036854775808"), "", DecodeOptions{})
	if err != nil || v.Value != math.MinInt64 {
		t.Fatalf("%#v %v", v, err)
	}
	_, err = DecodeSource[F32]([]byte("value: 10000000000000000000000000000000000000000.0"), "", DecodeOptions{AllowLossyNumbers: true})
	expectNativeError(t, err, NativeNumberOutOfRange, "/value")
	var legacy F32
	if err := Unmarshal([]byte("value: 0.1"), &legacy); err != nil || legacy.Value != 0.1 {
		t.Fatal(legacy, err)
	}
}

func TestNativeCollectionsAndBytes(t *testing.T) {
	type Record struct {
		ID   uint16  `skg:"id"`
		Note *string `skg:"note"`
	}
	type Config struct {
		Records []*Record         `skg:"records"`
		Matrix  [2][]int32        `skg:"matrix"`
		Headers map[string]*uint8 `skg:"headers"`
		Text    []byte            `skg:"text"`
		Dynamic Value             `skg:"dynamic"`
	}
	v, err := DecodeSource[Config]([]byte("records [null { id: 3 note: null }] matrix: [[1, 2], [3]] headers { \"a/b~c\": null b: 7 } text: \"hé😀\" dynamic: [[{ x: 1 }, null]]"), "", DecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if v.Records[0] != nil || v.Records[1].ID != 3 || v.Records[1].Note != nil || string(v.Text) != "hé😀" || *v.Headers["b"] != 7 || v.Dynamic.Type != TypeArray || v.Matrix[1][0] != 3 {
		t.Fatalf("%#v", v)
	}
	type Bytes struct {
		Text []byte `skg:"text"`
	}
	for _, src := range [][]byte{[]byte("text: [65, 255]")} {
		v, err := DecodeSource[Bytes](src, "", DecodeOptions{})
		if err != nil || !reflect.DeepEqual(v.Text, []byte{65, 255}) {
			t.Fatalf("%v %v", v, err)
		}
	}
	invalid := append(append([]byte("text: \""), 65, 255), '"')
	_, err = DecodeSource[Bytes](invalid, "invalid-utf8.skg", DecodeOptions{})
	d := expectNativeError(t, err, NativeParseError, "")
	var parse *ParseError
	if !errors.As(d, &parse) || parse.Diag.Code != CodeInvalidUTF8 || parse.Diag.Col != 9 {
		t.Fatal(err)
	}
	type Fixed struct {
		Items [2]uint8 `skg:"items"`
	}
	_, err = DecodeSource[Fixed]([]byte("items: [1]"), "", DecodeOptions{})
	expectNativeError(t, err, NativeTypeMismatch, "/items")
	encoded, err := Marshal(Fixed{Items: [2]uint8{2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeSource[Fixed](encoded, "", DecodeOptions{})
	if err != nil || got.Items != [2]uint8{2, 3} {
		t.Fatal(got, err)
	}
}

type nativePort uint16

func (p *nativePort) DecodeSKG(ctx *DecodeContext, value Value) error {
	var n uint16
	if err := ctx.Decode(value, &n); err != nil {
		return err
	}
	*p = nativePort(n)
	return nil
}
func (p *nativePort) ValidateSKG(ctx *DecodeContext) error {
	if *p == 0 {
		return ctx.Fail(NativeCustomError, "port must be nonzero")
	}
	return nil
}

type nativeRecursive struct{}

func (p *nativeRecursive) DecodeSKG(ctx *DecodeContext, v Value) error { return ctx.Decode(v, p) }

var nativeHookCause = errors.New("application decoder failed")

type nativeFailing struct{ Values []int }

func (p *nativeFailing) DecodeSKG(ctx *DecodeContext, v Value) error {
	if len(p.Values) > 0 {
		p.Values[0] = 99
	}
	return nativeHookCause
}

func TestNativeHooks(t *testing.T) {
	type Config struct {
		Port nativePort `skg:"port"`
		Time time.Time  `skg:"time"`
	}
	v, err := DecodeSource[Config]([]byte("port: 8080 time: \"2026-01-02T03:04:05Z\""), "", DecodeOptions{})
	if err != nil || v.Port != 8080 || v.Time.Year() != 2026 {
		t.Fatal(v, err)
	}
	_, err = DecodeSource[Config]([]byte("port: 0"), "", DecodeOptions{})
	expectNativeError(t, err, NativeCustomError, "/port")
	_, err = DecodeSource[nativeRecursive](nil, "", DecodeOptions{})
	expectNativeError(t, err, NativeNestingTooDeep, "")
	type Failing struct {
		Value nativeFailing `skg:"value"`
	}
	defaults := Failing{Value: nativeFailing{Values: []int{7}}}
	err = DecodeSourceInto([]byte("value: 1"), "", &defaults, DecodeOptions{})
	expectNativeError(t, err, NativeCustomError, "/value")
	if !errors.Is(err, nativeHookCause) || defaults.Value.Values[0] != 7 {
		t.Fatal(defaults, err)
	}
}

type nullValidatedSlice []int

func (*nullValidatedSlice) ValidateSKG(ctx *DecodeContext) error {
	return ctx.Fail(NativeCustomError, "null container must not be validated")
}

func TestNativeValidatorSkipsNullTargets(t *testing.T) {
	type Config struct {
		Items nullValidatedSlice `skg:"items"`
	}
	value, err := DecodeSource[Config]([]byte("items: null"), "null.skg", DecodeOptions{})
	if err != nil || value.Items != nil {
		t.Fatalf("null target validation mismatch: %#v, %v", value, err)
	}
}

func TestNativeDefaultsTransaction(t *testing.T) {
	type Config struct {
		Count uint8           `skg:"count"`
		Items []int           `skg:"items"`
		Map   map[string]*int `skg:"map"`
	}
	n := 4
	defaults := Config{Count: 7, Items: []int{1}, Map: map[string]*int{"a": &n}}
	originalItems, originalMap := defaults.Items, defaults.Map
	err := DecodeSourceInto([]byte("items: [2] count: 256"), "", &defaults, DecodeOptions{AllowMissingFields: true})
	expectNativeError(t, err, NativeNumberOutOfRange, "/count")
	if defaults.Count != 7 || defaults.Items[0] != 1 || *defaults.Map["a"] != 4 {
		t.Fatal(defaults)
	}
	if err := DecodeSourceInto([]byte("@delete count"), "", &defaults, DecodeOptions{AllowMissingFields: true}); err != nil {
		t.Fatal(err)
	}
	defaults.Items[0], *defaults.Map["a"] = 9, 9
	if originalItems[0] != 1 || *originalMap["a"] != 4 {
		t.Fatal("defaults retain mutable caller aliases")
	}
	if defaults.Count != 7 {
		t.Fatal("delete must use native absence/default rule")
	}
	if err := DecodeSourceInto([]byte("map {}"), "", &defaults, DecodeOptions{AllowMissingFields: true}); err != nil || len(defaults.Map) != 0 {
		t.Fatal(defaults, err)
	}
	type Cycle struct {
		Next *Cycle `skg:"next"`
	}
	cycle := Cycle{}
	cycle.Next = &cycle
	err = DecodeSourceInto(nil, "", &cycle, DecodeOptions{AllowMissingFields: true})
	expectNativeError(t, err, NativeNestingTooDeep, "")
	type Unsafe struct{ Pointer unsafe.Pointer }
	var safe Unsafe
	if err := DecodeSourceInto(nil, "", &safe, DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	safe.Pointer = unsafe.Pointer(&n)
	err = DecodeSourceInto(nil, "", &safe, DecodeOptions{})
	expectNativeError(t, err, NativeUnsupportedType, "")
}

func TestNativeLocationsImportsAndParseErrors(t *testing.T) {
	type Config struct {
		Values []uint8 `skg:"a/b~c"`
	}
	_, err := DecodeSource[Config]([]byte("\"a/b~c\": [1, 256]"), "range.skg", DecodeOptions{})
	d := expectNativeError(t, err, NativeNumberOutOfRange, "/a~1b~0c/1")
	if d.Source != (SourceLocation{"range.skg", 1, 1}) {
		t.Fatal(d.Source)
	}
	dir := t.TempDir()
	for name, src := range map[string]string{"base.skg": "\"a/b~c\": [256]\nremoved: 1", "main.skg": "import \"base.skg\"\n@delete removed"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, err = DecodeFile[Config](filepath.Join(dir, "main.skg"), DecodeOptions{RejectUnknownFields: true})
	d = expectNativeError(t, err, NativeNumberOutOfRange, "/a~1b~0c/0")
	if filepath.Base(d.Source.Path) != "base.skg" {
		t.Fatal(d.Source)
	}
	_, err = DecodeSource[Config]([]byte("x: ["), "broken.skg", DecodeOptions{})
	d = expectNativeError(t, err, NativeParseError, "")
	var parse *ParseError
	if !errors.As(err, &parse) || d.Source.Path != "broken.skg" {
		t.Fatal(err)
	}
	if _, err := DecodeSource[Config]([]byte("import \"does-not-exist.skg\""), "", DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeAmbiguousMapping(t *testing.T) {
	type Duplicate struct {
		A int `skg:"a"`
		B int `skg:"a"`
	}
	_, err := DecodeSource[Duplicate](nil, "", DecodeOptions{})
	expectNativeError(t, err, NativeUnsupportedType, "")
	var legacy Duplicate
	if err := Unmarshal([]byte("a: 1"), &legacy); err != nil || legacy.A != 1 || legacy.B != 0 {
		t.Fatal(legacy, err)
	}
}

func TestNativeDynamicCycleAndChildContext(t *testing.T) {
	array := &Array{ElementType: TypeArray}
	value := Value{Type: TypeArray, Array: array}
	array.Items = []Value{value}
	ctx := DecodeContext{Source: SourceLocation{Path: "custom.skg"}}
	var out any
	err := ctx.Decode(value, &out)
	var d *DecodeError
	if !errors.As(err, &d) || d.Code != NativeNestingTooDeep {
		t.Fatal(err)
	}
	var n uint8
	err = ctx.DecodeChild(Value{Type: TypeInt, Int: 256}, "a/b~c", SourceLocation{"child.skg", 3, 5}, &n)
	d = expectNativeError(t, err, NativeNumberOutOfRange, "/a~1b~0c")
	if d.Source != (SourceLocation{"child.skg", 3, 5}) || ctx.FieldPath != "" {
		t.Fatal(d, ctx)
	}
}

func FuzzNativeDecode(f *testing.F) {
	for _, src := range []string{"", "count: 2 items: [1, null]", "count: null", "map { \"a/b\": 999 }", "items: [[{x:1}]]", "@delete count"} {
		f.Add(src)
	}
	type Config struct {
		Count uint8             `skg:"count"`
		Items []*int16          `skg:"items"`
		Map   map[string]*uint8 `skg:"map"`
	}
	f.Fuzz(func(t *testing.T, src string) {
		value, err := DecodeSource[Config]([]byte(src), "fuzz.skg", DecodeOptions{})
		if err != nil && !reflect.DeepEqual(value, Config{}) {
			t.Fatal("partial result")
		}
		defaults := Config{Count: 7, Items: []*int16{nil}}
		if err := DecodeSourceInto([]byte(src), "fuzz.skg", &defaults, DecodeOptions{}); err != nil {
			if defaults.Count != 7 || len(defaults.Items) != 1 || defaults.Items[0] != nil || defaults.Map != nil {
				t.Fatal("partial mutation")
			}
		}
	})
}
