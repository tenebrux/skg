package skg

import (
	"errors"
	"fmt"
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
		if err == nil {
			canonical := Emit(file)
			if len(canonical) > MaxFileSize {
				return
			}
			reparsed, err := Parse(canonical)
			if err != nil {
				t.Fatalf("emitter produced invalid SKG: %q: %v", canonical, err)
			}
			if string(Emit(reparsed)) != string(canonical) {
				t.Fatal("canonical output is not stable")
			}
		}
	})
}

// FuzzMergeComposition treats three arbitrary operation streams as overlays.
// Overlay composition is associative before final materialization; grouping
// imports differently must not resurrect a deleted or replaced value.
func FuzzMergeComposition(f *testing.F) {
	for _, seed := range [][]byte{
		{}, {0}, {1, 2, 3}, {4, 5, 6, 7, 8}, {255, 0, 127, 64, 32, 16},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1024 {
			data = data[:1024]
		}
		one := len(data) / 3
		two := 2 * len(data) / 3
		base := fuzzOverlay(data[:one])
		middle := fuzzOverlay(data[one:two])
		top := fuzzOverlay(data[two:])

		left := MergeNodes(MergeNodes(base, middle), top)
		right := MergeNodes(base, MergeNodes(middle, top))
		leftText := Emit(&File{Children: left})
		rightText := Emit(&File{Children: right})
		if string(leftText) != string(rightText) {
			t.Fatalf("overlay grouping diverged\nleft:\n%s\nright:\n%s", leftText, rightText)
		}
		before := string(leftText)
		materialized := MaterializeNodes(left)
		if got := string(Emit(&File{Children: left})); got != before {
			t.Fatal("materialization mutated its input")
		}
		once := string(Emit(&File{Children: materialized}))
		twice := string(Emit(&File{Children: MaterializeNodes(materialized)}))
		if once != twice {
			t.Fatalf("materialization is not idempotent\nonce:\n%s\ntwice:\n%s", once, twice)
		}
	})
}

func fuzzOverlay(data []byte) []Node {
	keys := [...]string{"alpha", "beta", "gamma", "delta"}
	nodes := make([]Node, 0, len(data))
	for i, b := range data {
		key := keys[int(b>>5)%len(keys)]
		switch b % 6 {
		case 0:
			nodes = append(nodes, Node{Field: &Field{Key: key, Value: Value{Type: TypeInt, Int: int64(int8(b))}}})
		case 1:
			nodes = append(nodes, Node{Field: &Field{Key: key, Value: Value{Type: TypeNull}}})
		case 2:
			child := keys[(int(b)+i+1)%len(keys)]
			nodes = append(nodes, Node{Block: &Block{Name: key, Children: []Node{{Field: &Field{Key: child, Value: Value{Type: TypeBool, Bool: b&1 != 0}}}}}})
		case 3:
			nodes = append(nodes, Node{Delete: &Delete{Key: key}})
		case 4:
			nodes = append(nodes, Node{Block: &Block{Name: key, Replace: true, Children: []Node{{Field: &Field{Key: "value", Value: Value{Type: TypeInt, Int: int64(b)}}}}}})
		case 5:
			nodes = append(nodes, Node{BlockArray: &BlockArray{Name: key, Items: []Value{{Type: TypeObject, Object: []Node{{Field: &Field{Key: "id", Value: Value{Type: TypeInt, Int: int64(b)}}}}}, {Type: TypeNull}}}})
		}
	}
	return nodes
}

// FuzzResolveBudgets builds small valid import DAGs, then tightens every
// aggregate budget independently. A constrained success must equal the
// generously resolved graph; a rejection must use one of the stable resource
// codes. The explicit root also exercises containment on every run.
func FuzzResolveBudgets(f *testing.F) {
	for _, seed := range [][]byte{
		{1}, {2, 1, 3}, {8, 255, 0, 17, 42}, {4, 3, 2, 1, 0, 9, 8, 7},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			data = []byte{0}
		}
		if len(data) > 128 {
			data = data[:128]
		}
		count := 1 + int(data[0])%8
		dir := t.TempDir()
		for i := 0; i < count; i++ {
			first := i + 1
			last := min(count, first+int(data[(i+1)%len(data)])%3)
			var source strings.Builder
			if first < last {
				source.WriteString("import [")
				for child := first; child < last; child++ {
					if child > first {
						source.WriteString(", ")
					}
					fmt.Fprintf(&source, "\"f%d.skg\"", child)
				}
				source.WriteString("]\n")
			}
			fmt.Fprintf(&source, "node_%d { value: %d items: [1, null, 2] }\n", i, data[i%len(data)])
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.skg", i)), []byte(source.String()), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		entry := filepath.Join(dir, "f0.skg")
		generous := ResolveOptions{Root: dir, MaxBytes: 1 << 20, MaxFiles: 32, MaxNodes: 10_000, MaxMergeWork: 100_000}
		full, err := ParseFileWithOptions(entry, generous)
		if err != nil {
			t.Fatalf("generated import DAG did not resolve: %v", err)
		}

		pick := func(offset int, maximum int) int { return 1 + int(data[offset%len(data)])%maximum }
		limited := ResolveOptions{
			Root: dir, MaxBytes: int64(pick(1, 512)), MaxFiles: pick(2, 8),
			MaxNodes: pick(3, 64), MaxMergeWork: int64(pick(4, 128)),
		}
		got, err := ParseFileWithOptions(entry, limited)
		if err == nil {
			if string(Emit(got)) != string(Emit(full)) {
				t.Fatal("resource limits changed a successful resolution result")
			}
			return
		}
		var parseErr *ParseError
		if !errors.As(err, &parseErr) {
			t.Fatalf("resource rejection is not a ParseError: %T: %v", err, err)
		}
		switch parseErr.Diag.Code {
		case CodeResolutionByteLimit, CodeResolutionFileLimit, CodeResolutionNodeLimit, CodeResolutionWorkLimit:
		default:
			t.Fatalf("unexpected resource rejection %s: %v", parseErr.Diag.Code, err)
		}
	})
}
