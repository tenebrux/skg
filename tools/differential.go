// Command differential generates valid V1 sources and requires the Go and Zig
// parser/emitters to produce the same canonical bytes for every one.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	skg "github.com/tenebrux/skg/go"
)

type random uint64

func (r *random) next() uint64 {
	x := uint64(*r)
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	*r = random(x)
	return x
}

func main() {
	zig := flag.String("zig", "../zig-out/bin/skg", "path to the Zig skg executable")
	count := flag.Int("count", 1000, "number of deterministic sources")
	flag.Parse()
	if *count <= 0 {
		fatalf("count must be positive")
	}

	dir, err := os.MkdirTemp("", "skg-differential-")
	if err != nil {
		fatalf("create temporary directory: %v", err)
	}
	defer os.RemoveAll(dir)

	paths := make([]string, *count)
	wants := make([][]byte, *count)
	sources := make([][]byte, *count)
	rng := random(0x534b475631)
	for i := range *count {
		source := generate(i, &rng)
		parsed, err := skg.ParseSource(source, fmt.Sprintf("generated-%04d.skg", i))
		if err != nil {
			fatalf("generator produced invalid case %d:\n%s\n%v", i, source, err)
		}
		want := skg.Emit(parsed)
		path := filepath.Join(dir, fmt.Sprintf("case-%04d.skg", i))
		if err := os.WriteFile(path, source, 0o600); err != nil {
			fatalf("write case %d: %v", i, err)
		}
		paths[i], wants[i], sources[i] = path, want, source
	}

	args := append([]string{"fmt"}, paths...)
	command := exec.Command(*zig, args...)
	if output, err := command.CombinedOutput(); err != nil {
		fatalf("Zig formatter failed: %v\n%s", err, output)
	}
	for i, path := range paths {
		got, err := os.ReadFile(path)
		if err != nil {
			fatalf("read Zig result %d: %v", i, err)
		}
		if !bytes.Equal(got, wants[i]) {
			fatalf("case %d diverged\nsource:\n%s\nGo:\n%s\nZig:\n%s", i, sources[i], wants[i], got)
		}
		reparsed, err := skg.ParseSource(got, fmt.Sprintf("canonical-%04d.skg", i))
		if err != nil || !bytes.Equal(skg.Emit(reparsed), got) {
			fatalf("case %d Zig output is not a Go fixed point: %v\n%s", i, err, got)
		}
	}
	fmt.Printf("differential V1: Go and Zig agree on %d generated sources (seed 0x534b475631)\n", *count)
}

func generate(index int, rng *random) []byte {
	integer := int64(rng.next()%2_000_001) - 1_000_000
	whole := rng.next() % 100_000
	fraction := rng.next() % 1_000_000
	floatSign := ""
	if rng.next()&1 != 0 {
		floatSign = "-"
	}
	textValues := []string{
		"plain", "quote \" and slash \\", "line one\nline two", "tab\tvalue", "éclair", "雪",
	}
	text := textValues[rng.next()%uint64(len(textValues))]

	var out strings.Builder
	if index%2 == 0 {
		out.WriteString("skg_version: \"1.0\"\n")
	}
	if index%5 == 0 {
		out.WriteString("import [\"base.skg\", \"layer.skg\"]\n")
	}
	if index%3 == 0 {
		fmt.Fprintf(&out, "schema_version: %s\n", skgString(fmt.Sprintf("1.%d.%d", index%17, index%29)))
	}
	fmt.Fprintf(&out, "seed_%d: %d\n", index, integer)
	fmt.Fprintf(&out, "%s: %s%d.%06d\n", skgKey(fmt.Sprintf("quoted key %d/雪", index)), floatSign, whole, fraction)
	fmt.Fprintf(&out, "text: %s\n", skgString(text))
	fmt.Fprintf(&out, "enabled: %t\n", rng.next()&1 == 0)
	switch index % 8 {
	case 0:
		fmt.Fprintf(&out, "nullable: [null, %d, null, %d]\n", index%251, (index+1)%251)
		out.WriteString("matrix: [[1, 2], [3, null], []]\n")
		out.WriteString("nothing: [null, null]\n")
	case 1:
		fmt.Fprintf(&out, "items [ { id: %d } null { id: %d note: \"last\" } ]\n", index, index+1)
		out.WriteString("empty_items [ ]\n")
	case 2:
		fmt.Fprintf(&out, "grid: [[{ id: %d }, { id: %d nested: { ok: true } }], [null]]\n", index, index+1)
		out.WriteString("deep: [[[[1, 2]]], [[[3]]]]\n")
	case 3:
		fmt.Fprintf(&out, "settings { left: %d nested { first: true } }\n", index)
		fmt.Fprintf(&out, "settings { right: %d nested { second: false } }\n", index+1)
		out.WriteString("gone: true\n@delete gone\n")
	case 4:
		fmt.Fprintf(&out, "inline: { left: %d nested: { values: [1, null, 3] } }\n", index)
		out.WriteString("empty_value: []\nempty_object: {}\n")
	case 5:
		fmt.Fprintf(&out, "%s: { %s: %d }\n", skgKey("object/key~name"), skgKey("skg_version"), index)
		fmt.Fprintf(&out, "%s { %s: true }\n", skgKey("block key"), skgKey(""))
	case 6:
		fmt.Fprintf(&out, "shape: %d\nshape { nested: true }\n", index)
		fmt.Fprintf(&out, "old { stale: true }\n@replace old { fresh: %d }\nold { later: true }\n", index)
	case 7:
		out.WriteString("records: [{ name: \"a\" children: [{ id: 1 }, null] }, { name: \"b\" children: [] }]\n")
		fmt.Fprintf(&out, "tail: %d\n", index)
	}
	return []byte(out.String())
}

func skgKey(value string) string { return skgString(value) }

func skgString(value string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\n':
			out.WriteString("\\n")
		case '\t':
			out.WriteString("\\t")
		default:
			out.WriteRune(r)
		}
	}
	out.WriteByte('"')
	return out.String()
}

func fatalf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "differential check failed:", fmt.Sprintf(format, args...))
	os.Exit(1)
}
