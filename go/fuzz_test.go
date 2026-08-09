package skg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzParse asserts that Parse is total: for any input it returns either a
// *File or an error, never a panic and never a process-killing fatal error
// (an unbounded-recursion stack overflow is not recoverable, so the parser has
// to reject deep input itself - see MaxNestingDepth).
//
// Run the fuzzer with:
//
//	go test -fuzz=FuzzParse
//
// Without -fuzz only the seed corpus below runs, which keeps `go test` fast.
func FuzzParse(f *testing.F) {
	for _, dir := range []string{"valid", "invalid"} {
		paths, err := filepath.Glob(filepath.Join(testdataDir(), dir, "*.skg"))
		if err != nil {
			f.Fatalf("glob %s fixtures: %v", dir, err)
		}
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil {
				f.Fatalf("read %s: %v", p, err)
			}
			f.Add(data)
		}
	}

	for _, seed := range []string{
		"x: " + strings.Repeat("[", 200) + strings.Repeat("]", 200),
		strings.Repeat("a { ", 200) + strings.Repeat("} ", 200),
		"a [ { b: 1 } { b: 2 } ]",
		"a {",
		"a [",
		"x: \"unterminated",
		"x: \"\"\"unterminated multiline",
		"x: \\",
		"x: 99999999999999999999999999",
		"x: -99999999999999999999999999",
		"x: 1.7976931348623159e309",
		"x: 0.",
		"x: [1, \"two\"]",
		"skg_version: \"1.0\"\nskg_version: \"1.0\"",
		"skg_version: \"\xff\xfe\"",
		"ü: \"snowman ☃\"",
		"x: \"\x00\x01\x02\"",
		"# comment only",
		": : :",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := Parse(data)
		switch {
		case err != nil && file != nil:
			t.Fatalf("Parse returned both a file and an error: %v", err)
		case err == nil && file == nil:
			t.Fatal("Parse returned neither a file nor an error")
		}
	})
}
