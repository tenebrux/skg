package skg

import (
	"reflect"
	"testing"
)

func TestStructuredValuesNativeRoundTrip(t *testing.T) {
	type Record struct {
		ID     int               `skg:"id"`
		Labels map[string]string `skg:"labels"`
	}
	type Config struct {
		Matrix  [][]*Record `skg:"matrix"`
		Records []*Record   `skg:"records"`
		Generic []any       `skg:"generic"`
	}
	record := &Record{1, map[string]string{"a.b": "c"}}
	want := Config{
		Matrix:  [][]*Record{{record, nil}, {&Record{Labels: map[string]string{}}, record}},
		Records: []*Record{nil, record, nil},
		Generic: []any{nil, map[string]any{"empty": map[string]any{}, "nested": []any{map[string]any{"id": int64(2)}, nil}}},
	}
	data, err := Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err = Unmarshal(data, &got); err != nil {
		t.Fatalf("%s: %v", data, err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestStructuredValueDepthBoundary(t *testing.T) {
	// Every object and list contributes one level, including anonymous objects.
	for _, levels := range []int{MaxNestingDepth / 2, MaxNestingDepth/2 + 1} {
		value := "{}"
		for i := 1; i < levels; i++ {
			value = "{x: [" + value + "]}"
		}
		source := []byte("root: [" + value + "]")
		_, err := Parse(source)
		if (err == nil) != (levels == MaxNestingDepth/2) {
			t.Fatalf("levels %d: %v", levels, err)
		}
	}
}

func TestEmptyObjectAndNullStayDistinct(t *testing.T) {
	var got struct {
		Items []any `skg:"items"`
	}
	if err := Unmarshal([]byte("items [null {} null]"), &got); err != nil {
		t.Fatal(err)
	}
	want := []any{nil, map[string]any{}, nil}
	if !reflect.DeepEqual(got.Items, want) {
		t.Fatalf("%#v", got.Items)
	}
}
