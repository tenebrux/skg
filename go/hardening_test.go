package skg

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCachedImportDepth(t *testing.T) {
	for _, length := range []int{31, 32} {
		for _, imports := range []string{`import "f1.skg"`, `import ["f2.skg", "f1.skg"]`, `import ["f1.skg", "f2.skg"]`} {
			t.Run(fmt.Sprintf("%d/%s", length, imports), func(t *testing.T) {
				files := map[string]string{"leaf.skg": "value: 1\n", "main.skg": imports + "\n"}
				for i := 1; i <= length; i++ {
					next := "leaf.skg"
					if i < length {
						next = fmt.Sprintf("f%d.skg", i+1)
					}
					files[fmt.Sprintf("f%d.skg", i)] = fmt.Sprintf("import %q\n", next)
				}
				dir := writeFiles(t, files)
				_, err := ParseFile(filepath.Join(dir, "main.skg"))
				if length == 31 {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				var pe *ParseError
				if !errors.As(err, &pe) || pe.Diag.Code != CodeImportChainTooDeep {
					t.Fatalf("expected depth rejection, got %v", err)
				}
				if pe.Diag.Line == 0 || pe.Diag.Col == 0 {
					t.Fatal("missing import position")
				}
			})
		}
	}
}

func TestUnmarshalNullAndInterfaces(t *testing.T) {
	t.Run("map null retains key", func(t *testing.T) {
		v := map[string]any{"x": "old"}
		if err := Unmarshal([]byte("x: null"), &v); err != nil {
			t.Fatal(err)
		}
		x, ok := v["x"]
		if !ok || x != nil {
			t.Fatalf("expected present null, got %#v", v)
		}
	})
	t.Run("interface null", func(t *testing.T) {
		v := struct {
			X any `skg:"x"`
		}{X: "old"}
		if err := Unmarshal([]byte("x: null"), &v); err != nil {
			t.Fatal(err)
		}
		if v.X != nil {
			t.Fatal(v)
		}
	})
	t.Run("nonempty interface", func(t *testing.T) {
		v := map[string]fmt.Stringer{}
		if err := Unmarshal([]byte("x: 1"), &v); err == nil {
			t.Fatal("expected type error")
		}
	})
	t.Run("pointer block array", func(t *testing.T) {
		type Item struct {
			Name string `skg:"name"`
		}
		var v map[string][]*Item
		if err := Unmarshal([]byte(`users [{ name: "a" }]`), &v); err != nil {
			t.Fatal(err)
		}
		if len(v["users"]) != 1 || v["users"][0].Name != "a" {
			t.Fatal(v)
		}
	})
}

func TestMarshalRejectsUnrepresentableInputs(t *testing.T) {
	type Cycle struct {
		Next *Cycle `skg:"next"`
	}
	c := &Cycle{}
	c.Next = c
	m := map[string]any{}
	m["self"] = m
	cases := []any{
		struct {
			X []any `skg:"x"`
		}{[]any{1, "two"}},
		c,
		struct {
			X map[string]any `skg:"x"`
		}{m},
	}
	for i, v := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if _, err := Marshal(v); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestMarshalNullMapRoundTrip(t *testing.T) {
	v := struct {
		X map[string]any `skg:"x"`
	}{map[string]any{"value": nil}}
	data, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		X map[string]any `skg:"x"`
	}
	if err := Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v, got) {
		t.Fatalf("%#v != %#v", v, got)
	}
}

func TestHeaderStringRoundTrip(t *testing.T) {
	for _, value := range []string{"a\"b", "a\\b", "a\nb", "a\rb", "\x00", "\xff"} {
		f := &File{SchemaVersion: &value, ImportPaths: []string{value}}
		data := Emit(f)
		got, err := Parse(data)
		if err != nil {
			t.Fatalf("%q: %v", data, err)
		}
		if *got.SchemaVersion != value || got.ImportPaths[0] != value {
			t.Fatalf("header changed: %q", data)
		}
	}
}

func TestMarshalDepthBoundaries(t *testing.T) {
	for _, depth := range []int{MaxNestingDepth, MaxNestingDepth + 1} {
		var value any = 1
		for i := 0; i < depth; i++ {
			value = []any{value}
		}
		config := struct {
			X any `skg:"x"`
		}{value}
		data, err := Marshal(config)
		if depth > MaxNestingDepth {
			if err == nil {
				t.Fatal("accepted excessive nesting")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(data); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMarshalPointerCycle(t *testing.T) {
	var value any
	value = &value
	if _, err := Marshal(struct {
		X any `skg:"x"`
	}{value}); err == nil {
		t.Fatal("accepted pointer/interface cycle")
	}
}

func TestGenericBlockArraysRoundTrip(t *testing.T) {
	type Item struct {
		Name string `skg:"name"`
	}
	original := struct {
		Items []*Item `skg:"items"`
	}{[]*Item{{Name: "a"}, {Name: "b"}}}
	data, err := Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Items []*Item `skg:"items"`
	}
	if err := Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("%#v != %#v", original, got)
	}
	var generic map[string]any
	if err := Unmarshal(data, &generic); err != nil {
		t.Fatal(err)
	}
	wrapped := struct {
		Config map[string]any `skg:"config"`
	}{generic}
	data, err = Marshal(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	var again struct {
		Config map[string]any `skg:"config"`
	}
	if err := Unmarshal(data, &again); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(wrapped, again) {
		t.Fatalf("%#v != %#v", wrapped, again)
	}
}

func TestMarshalMapOrderIsStable(t *testing.T) {
	v := struct {
		M map[string]int `skg:"m"`
	}{map[string]int{"z": 1, "a": 2, "m": 3}}
	data, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "m {\n  a: 2\n  m: 3\n  z: 1\n}\n" {
		t.Fatalf("unexpected order: %s", data)
	}
}

type recursivePointer *recursivePointer

func TestRecursivePointerTypesReturnErrors(t *testing.T) {
	var target recursivePointer
	if err := Unmarshal([]byte("x: 1"), &target); err == nil {
		t.Fatal("accepted recursive pointer target")
	}
	var field struct {
		X recursivePointer `skg:"x"`
	}
	if err := Unmarshal([]byte("x: 1"), &field); err == nil {
		t.Fatal("accepted recursive pointer field")
	}
	var array struct {
		X recursivePointer `skg:"x"`
	}
	if err := Unmarshal([]byte("x [{}]"), &array); err == nil {
		t.Fatal("accepted recursive pointer block-array target")
	}
	if _, err := Marshal(struct {
		X []recursivePointer `skg:"x"`
	}{nil}); err == nil {
		t.Fatal("accepted recursive element type")
	}
}
