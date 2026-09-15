package skg

import (
	"reflect"
	"testing"
)

func parsedNodes(t *testing.T, src string) []Node {
	t.Helper()
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return f.Children
}

func TestOverlayCompositionRetainsBarriers(t *testing.T) {
	base := parsedNodes(t, "x { inherited: 1 } y: 1")
	operations := []string{
		"@delete x", "@replace x {}", "x: null x { local: 2 }",
		"@delete x x { local: 2 }", "@replace x { local: 2 } x { later: 3 }",
	}
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			a := parsedNodes(t, operation)
			b := parsedNodes(t, "x { final: 4 }")
			left := MaterializeNodes(MergeNodes(MergeNodes(base, a), b))
			right := MaterializeNodes(MergeNodes(base, MergeNodes(a, b)))
			l, r := string(Emit(&File{Children: left})), string(Emit(&File{Children: right}))
			if l != r {
				t.Fatalf("grouping changes values: %s != %s", l, r)
			}
			var got map[string]any
			if err := Unmarshal([]byte(l), &got); err != nil {
				t.Fatal(err)
			}
			x := got["x"].(map[string]any)
			if _, ok := x["inherited"]; ok {
				t.Fatalf("inherited value resurrected: %s", l)
			}
		})
	}
}

func TestMaterializeDoesNotMutateOverlay(t *testing.T) {
	nodes := parsedNodes(t, "@replace x { @delete y z: 1 } @delete absent")
	before := string(Emit(&File{Children: nodes}))
	got := MaterializeNodes(nodes)
	if before != string(Emit(&File{Children: nodes})) {
		t.Fatal("input mutated")
	}
	if len(got) != 1 || got[0].Block.Replace || len(got[0].Block.Children) != 1 {
		t.Fatalf("%#v", got)
	}
}

func TestUnmarshalAppliesLocalOperations(t *testing.T) {
	src := []byte("gone: 1 @delete gone x { inherited: 1 } @replace x {} items [{ @delete gone kept: 2 } null]")
	var got map[string]any
	if err := Unmarshal(src, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"x": map[string]any{}, "items": []any{map[string]any{"kept": int64(2)}, nil}}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("%#v", got)
	}
}

func TestOverlayCompositionGrouping(t *testing.T) {
	sources := []string{
		"", "x: null", "@delete x", "x { a: 1 }", "@replace x {}",
		"x { @delete a b: 2 }", "x: 3", "x [{ a: 4 } null]",
	}
	base := parsedNodes(t, "x { inherited: 1 a: 0 }")
	parsed := make([][]Node, len(sources))
	for i, src := range sources {
		parsed[i] = parsedNodes(t, src)
	}
	for i, a := range parsed {
		for j, b := range parsed {
			for k, c := range parsed {
				left := MergeNodes(MergeNodes(MergeNodes(base, a), b), c)
				right := MergeNodes(base, MergeNodes(a, MergeNodes(b, c)))
				lhs, rhs := string(Emit(&File{Children: left})), string(Emit(&File{Children: right}))
				if lhs != rhs {
					t.Fatalf("grouping differs for %q / %q / %q:\n%s\n%s", sources[i], sources[j], sources[k], lhs, rhs)
				}
			}
		}
	}
}

func TestResolvedEmissionDoesNotReactivateImports(t *testing.T) {
	file, err := ParseFile("../testdata/valid/imports-overlay-operations/main.skg")
	if err != nil {
		t.Fatal(err)
	}
	if !file.ImportsResolved || len(file.ImportPaths) != 3 {
		t.Fatal("resolution metadata missing")
	}
	data := Emit(file)
	reparsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(reparsed.ImportPaths) != 0 {
		t.Fatal("resolved emission reactivated imports")
	}
	if string(Emit(&File{Children: reparsed.Children})) != string(data) {
		t.Fatal("final data changed on reparse")
	}
}
