package skg

import (
	"reflect"
	"testing"
)

func TestMarshalQuotedKeys(t *testing.T) {
	type Config struct {
		Import  int               `skg:"import"`
		Schema  string            `skg:"schema_version"`
		Version bool              `skg:"skg_version"`
		Headers map[string]string `skg:"http.headers"`
		Routes  []map[string]int  `skg:"/routes"`
	}
	want := Config{1, "data", true, map[string]string{
		"": "empty", "Content-Type": "json", "true": "literal", "null": "literal",
		"日本語": "unicode", "a\nb\t\"\\": "escaped", "\x00é\r": "bytes",
	}, []map[string]int{{"404": 1}}}
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
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SKGVersion != nil || parsed.SchemaVersion != nil || len(parsed.ImportPaths) != 0 {
		t.Fatal("quoted data keys became directives")
	}
	if string(Emit(parsed)) != string(data) {
		t.Fatal("canonical output changed")
	}
}

func TestMarshalRejectsInvalidUTF8(t *testing.T) {
	for _, value := range []struct {
		Value map[string]string `skg:"value"`
	}{
		{map[string]string{"key": "\xff"}},
		{map[string]string{"\xff": "value"}},
	} {
		if _, err := Marshal(value); err == nil {
			t.Fatal("expected invalid UTF-8 to fail")
		}
	}
}
