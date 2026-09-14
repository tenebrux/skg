package skg

import (
	"reflect"
	"testing"
)

func TestNullableArraysNativeRoundTrip(t *testing.T) {
	one, two := 1, 2
	type Config struct {
		Values  []*int   `skg:"values"`
		Generic []any    `skg:"generic"`
		Nested  [][]*int `skg:"nested"`
	}
	for _, values := range [][]*int{{nil, &one, &two}, {&one, nil, &two}, {&one, &two, nil}, {nil, nil}} {
		want := Config{values, []any{nil, int64(1), nil}, [][]*int{{nil, &one}, {&two, nil}}}
		data, err := Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		var got Config
		if err := Unmarshal(data, &got); err != nil {
			t.Fatalf("%s: %v", data, err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}
func TestNullDoesNotMaskMixedMarshalTypes(t *testing.T) {
	for _, values := range [][]any{{nil, 1, nil, "x"}, {1, nil, 2.0}, {nil, []int{1}, false}} {
		_, err := Marshal(struct {
			Values []any `skg:"values"`
		}{values})
		if err == nil {
			t.Fatalf("accepted mixed types: %#v", values)
		}
	}
}
