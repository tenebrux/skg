// Command parity requires every SKG implementation to agree.
//
// It builds each harness listed in implementations.json, generates one
// deterministic corpus, runs every implementation over it through its public
// API, and fails on any disagreement:
//
//   - parse: every fixture, CRLF variants, a comment between every token pair
//     of every valid fixture, and seeded mutations. Status, error code and
//     position always compare; canonical bytes compare between implementations
//     that declare the emit capability, and for input containing '#' only
//     between implementations that also declare comments.
//   - resolve: generated import graphs with overlays, diamonds, cycles and
//     missing files under default and tightened aggregate budgets, symlink
//     containment cases, and every directory fixture.
//   - native: single-fault cases for every shared native profile.
//
// Every conformance.json in the repository must have a registered harness, so
// a new language cannot claim conformance without joining this check.
//
// Run from the repository root under the pinned toolchains:
//
//	(cd tools/parity && go run .)
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type implementation struct {
	Name     string   `json:"name"`
	Manifest string   `json:"manifest"`
	Dir      string   `json:"dir"`
	Build    []string `json:"build"`
	Binary   string   `json:"binary"`

	emit, comments, imports, native bool
	binary                          string
}

type caseSpec struct {
	ID                  string `json:"id"`
	Path                string `json:"path,omitempty"`
	Entry               string `json:"entry,omitempty"`
	Root                string `json:"root,omitempty"`
	MaxBytes            int64  `json:"max_bytes,omitempty"`
	MaxFiles            int    `json:"max_files,omitempty"`
	MaxNodes            int    `json:"max_nodes,omitempty"`
	MaxMergeWork        int64  `json:"max_merge_work,omitempty"`
	Profile             string `json:"profile,omitempty"`
	Source              string `json:"source,omitempty"`
	RejectUnknownFields bool   `json:"reject_unknown_fields,omitempty"`
	AllowLossyNumbers   bool   `json:"allow_lossy_numbers,omitempty"`

	hasComment bool
}

type result map[string]json.RawMessage

func main() {
	repo := flag.String("repo", "../..", "repository root")
	mutations := flag.Int("mutations", 20000, "seeded mutation cases")
	graphs := flag.Int("graphs", 1500, "generated import graphs")
	keep := flag.Bool("keep", false, "keep the work directory")
	flag.Parse()

	root, err := filepath.Abs(*repo)
	check(err)
	work, err := os.MkdirTemp("", "skg-parity-")
	check(err)
	if !*keep {
		defer os.RemoveAll(work)
	} else {
		fmt.Println("work directory:", work)
	}

	impls := loadImplementations(root, work)
	for _, impl := range impls {
		fmt.Printf("[parity] building %s\n", impl.Name)
		build(root, work, impl)
	}

	failed := false
	modes := []struct {
		name   string
		cases  []caseSpec
		usable func(implementation) bool
	}{
		{"parse", parseCorpus(root, filepath.Join(work, "parse"), *mutations), func(implementation) bool { return true }},
		{"resolve", resolveCorpus(root, filepath.Join(work, "resolve"), *graphs), func(i implementation) bool { return i.imports }},
		{"native", nativeCorpus(), func(i implementation) bool { return i.native }},
	}
	for _, mode := range modes {
		var active []implementation
		for _, impl := range impls {
			if mode.usable(impl) {
				active = append(active, impl)
			}
		}
		if len(active) < 2 {
			fail("%s: fewer than two implementations to compare", mode.name)
		}
		results := make([]map[string]result, len(active))
		for i, impl := range active {
			results[i] = run(work, impl, mode.name, mode.cases)
		}
		divergences := compare(mode.name, mode.cases, active, results)
		if len(divergences) > 0 {
			failed = true
			fmt.Printf("[parity] %s: %d of %d cases diverge\n", mode.name, len(divergences), len(mode.cases))
			for i, d := range divergences {
				if i == 10 {
					fmt.Printf("  ... %d more\n", len(divergences)-10)
					break
				}
				fmt.Println(d)
			}
			continue
		}
		names := make([]string, len(active))
		for i, impl := range active {
			names[i] = impl.Name
		}
		fmt.Printf("[parity] %s: %s agree on %d cases\n", mode.name, strings.Join(names, ", "), len(mode.cases))
	}
	if failed {
		fmt.Println("PARITY FAILED")
		os.Exit(1)
	}
	fmt.Println("PARITY PASSED")
}

// loadImplementations reads the registry and binds each entry to its
// conformance manifest. Every manifest in the repository must be registered.
func loadImplementations(root, work string) []implementation {
	data, err := os.ReadFile(filepath.Join(root, "tools/parity/implementations.json"))
	check(err)
	var registry struct {
		Implementations []implementation `json:"implementations"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	check(decoder.Decode(&registry))

	registered := map[string]bool{}
	for i := range registry.Implementations {
		impl := &registry.Implementations[i]
		data, err := os.ReadFile(filepath.Join(root, impl.Manifest))
		check(err)
		var manifest struct {
			Implementation string          `json:"implementation"`
			Capabilities   map[string]bool `json:"capabilities"`
		}
		check(json.Unmarshal(data, &manifest))
		if manifest.Implementation != impl.Name {
			fail("%s names implementation %q, registered as %q", impl.Manifest, manifest.Implementation, impl.Name)
		}
		if !manifest.Capabilities["parse"] {
			fail("%s does not declare parse", impl.Manifest)
		}
		impl.emit = manifest.Capabilities["emit"]
		impl.comments = manifest.Capabilities["comments"]
		impl.imports = manifest.Capabilities["imports"]
		impl.native = manifest.Capabilities["native"]
		impl.binary = strings.ReplaceAll(impl.Binary, "{work}", work)
		if runtime.GOOS == "windows" {
			impl.binary += ".exe"
		}
		registered[filepath.ToSlash(filepath.Clean(impl.Manifest))] = true
	}
	manifests, err := filepath.Glob(filepath.Join(root, "*", "conformance.json"))
	check(err)
	for _, manifest := range manifests {
		rel, err := filepath.Rel(root, manifest)
		check(err)
		if !registered[filepath.ToSlash(rel)] {
			fail("%s has no parity harness in tools/parity/implementations.json", filepath.ToSlash(rel))
		}
	}
	return registry.Implementations
}

func build(root, work string, impl implementation) {
	args := make([]string, len(impl.Build))
	for i, arg := range impl.Build {
		args[i] = strings.ReplaceAll(arg, "{work}", work)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = filepath.Join(root, impl.Dir)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fail("building %s: %v", impl.Name, err)
	}
}

func run(work string, impl implementation, mode string, cases []caseSpec) map[string]result {
	input := filepath.Join(work, mode+".cases.json")
	if _, err := os.Stat(input); errors.Is(err, os.ErrNotExist) {
		data, err := json.Marshal(map[string]any{"mode": mode, "cases": cases})
		check(err)
		check(os.WriteFile(input, data, 0o644))
	}
	output := filepath.Join(work, mode+"."+impl.Name+".jsonl")
	cmd := exec.Command(impl.binary, input, output)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fail("%s harness, %s mode: %v", impl.Name, mode, err)
	}
	data, err := os.ReadFile(output)
	check(err)
	results := map[string]result{}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var r result
		if err := json.Unmarshal(line, &r); err != nil {
			fail("%s harness wrote invalid JSON: %v: %s", impl.Name, err, line)
		}
		var id string
		check(json.Unmarshal(r["id"], &id))
		results[id] = r
	}
	if len(results) != len(cases) {
		fail("%s harness answered %d of %d %s cases", impl.Name, len(results), len(cases), mode)
	}
	return results
}

func compare(mode string, cases []caseSpec, impls []implementation, results []map[string]result) []string {
	var out []string
	for _, c := range cases {
		var problems []string
		base := results[0][c.ID]
		for i := 1; i < len(impls); i++ {
			other := results[i][c.ID]
			for _, key := range []string{"ok", "code", "file", "line", "col", "field_path"} {
				if !bytes.Equal(base[key], other[key]) {
					problems = append(problems, fmt.Sprintf("%s: %s=%s, %s=%s", key, impls[0].Name, base[key], impls[i].Name, other[key]))
				}
			}
			if mode == "native" && string(base["ok"]) == "true" && !sameJSON(base["value"], other["value"]) {
				problems = append(problems, fmt.Sprintf("value: %s=%s, %s=%s", impls[0].Name, base["value"], impls[i].Name, other["value"]))
			}
		}
		if mode != "native" {
			problems = append(problems, compareEmit(c, impls, results)...)
		}
		if len(problems) > 0 {
			out = append(out, fmt.Sprintf("  %s %s\n    %s", c.ID, describe(c), strings.Join(problems, "\n    ")))
		}
	}
	return out
}

// compareEmit compares canonical bytes between implementations whose declared
// capabilities let them reproduce the input.
func compareEmit(c caseSpec, impls []implementation, results []map[string]result) []string {
	var problems []string
	ref := -1
	for i, impl := range impls {
		if !impl.emit || string(results[i][c.ID]["ok"]) != "true" || (c.hasComment && !impl.comments) {
			continue
		}
		if ref < 0 {
			ref = i
			continue
		}
		if !bytes.Equal(results[ref][c.ID]["emit"], results[i][c.ID]["emit"]) {
			problems = append(problems, fmt.Sprintf("emit: %s=%s\n      %s=%s", impls[ref].Name, results[ref][c.ID]["emit"], impls[i].Name, results[i][c.ID]["emit"]))
		}
	}
	// Implementations without comment trivia must still agree with each other.
	if c.hasComment {
		ref = -1
		for i, impl := range impls {
			if !impl.emit || impl.comments || string(results[i][c.ID]["ok"]) != "true" {
				continue
			}
			if ref < 0 {
				ref = i
			} else if !bytes.Equal(results[ref][c.ID]["emit"], results[i][c.ID]["emit"]) {
				problems = append(problems, fmt.Sprintf("emit: %s=%s\n      %s=%s", impls[ref].Name, results[ref][c.ID]["emit"], impls[i].Name, results[i][c.ID]["emit"]))
			}
		}
	}
	return problems
}

func describe(c caseSpec) string {
	switch {
	case c.Path != "":
		src, _ := os.ReadFile(c.Path)
		if len(src) > 160 {
			src = append(src[:160], "..."...)
		}
		return fmt.Sprintf("source %q", src)
	case c.Entry != "":
		return fmt.Sprintf("entry %s root=%q limits=%d/%d/%d/%d", c.Entry, c.Root, c.MaxBytes, c.MaxFiles, c.MaxNodes, c.MaxMergeWork)
	default:
		return fmt.Sprintf("profile %s reject=%t lossy=%t source %q", c.Profile, c.RejectUnknownFields, c.AllowLossyNumbers, c.Source)
	}
}

// sameJSON compares native results structurally: numbers by value (floats as
// binary64), objects by key, arrays by position.
func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	if da.Decode(&x) != nil || db.Decode(&y) != nil {
		return false
	}
	return sameValue(x, y)
}

func sameValue(x, y any) bool {
	switch a := x.(type) {
	case json.Number:
		b, ok := y.(json.Number)
		if !ok {
			return false
		}
		if ia, err := strconv.ParseInt(string(a), 10, 64); err == nil {
			if ib, err := strconv.ParseInt(string(b), 10, 64); err == nil {
				return ia == ib
			}
		}
		fa, errA := strconv.ParseFloat(string(a), 64)
		fb, errB := strconv.ParseFloat(string(b), 64)
		return errA == nil && errB == nil && math.Float64bits(fa) == math.Float64bits(fb)
	case []any:
		b, ok := y.([]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for i := range a {
			if !sameValue(a[i], b[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		b, ok := y.(map[string]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for k, v := range a {
			w, ok := b[k]
			if !ok || !sameValue(v, w) {
				return false
			}
		}
		return true
	default:
		return x == y
	}
}

// ─── Parse corpus ────────────────────────────────────────────────────────────

func fixtureFiles(root string) []string {
	var files []string
	check(filepath.WalkDir(filepath.Join(root, "testdata"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".skg") {
			files = append(files, path)
		}
		return nil
	}))
	sort.Strings(files)
	return files
}

var tokenPattern = regexp.MustCompile(`"""[\s\S]*?"""|"(?:\\.|[^"\\\n])*"|#[^\n]*|[{}\[\]:,@]|[^\s{}\[\]:,@"#]+`)

func parseCorpus(root, dir string, mutations int) []caseSpec {
	check(os.MkdirAll(dir, 0o755))
	var cases []caseSpec
	add := func(src []byte) {
		id := fmt.Sprintf("p%06d", len(cases))
		path := filepath.Join(dir, id+".skg")
		check(os.WriteFile(path, src, 0o644))
		cases = append(cases, caseSpec{ID: id, Path: path, hasComment: bytes.ContainsRune(src, '#')})
	}
	var seeds [][]byte
	for _, file := range fixtureFiles(root) {
		src, err := os.ReadFile(file)
		check(err)
		add(src)
		add(bytes.ReplaceAll(src, []byte("\n"), []byte("\r\n")))
		if len(src) < 20000 {
			seeds = append(seeds, src)
		}
		if strings.Contains(filepath.ToSlash(file), "/testdata/valid/") {
			spans := tokenPattern.FindAllIndex(src, -1)
			for _, span := range spans[min(1, len(spans)):] {
				add(append(append(append([]byte{}, src[:span[0]]...), "#c\n"...), src[span[0]:]...))
			}
		}
	}
	tokens := []string{"{", "}", "[", "]", ",", ":", "@delete ", "@replace ", "null", `"k"`, "\"\"\"a\nb\"\"\"", "1", "-0", "1.5", "#c\n", "\n", " ", "x", `""`, `"\n"`, "{}", "[]", "@", "true", "0.", ".5", "1e5", "\"\r\"", "\r\n", "\t", "\x00", "import \"a.skg\"\n", "skg_version: \"1.0\"\n", `"import"`, "\xff"}
	rng := xorshift(0x534b47504152)
	for range mutations {
		b := append([]byte{}, seeds[rng.intn(len(seeds))]...)
		for range 1 + rng.intn(4) {
			p := rng.intn(len(b) + 1)
			switch op := rng.intn(10); {
			case op < 4:
				b = append(b[:p], append([]byte(tokens[rng.intn(len(tokens))]), b[p:]...)...)
			case op < 7 && len(b) > 0:
				b = append(b[:p], b[min(len(b), p+1+rng.intn(6)):]...)
			case len(b) > 0:
				segment := append([]byte{}, b[p:min(len(b), p+1+rng.intn(20))]...)
				at := rng.intn(len(b) + 1)
				b = append(b[:at], append(segment, b[at:]...)...)
			}
		}
		add(b)
	}
	return cases
}

// ─── Resolve corpus ──────────────────────────────────────────────────────────

func resolveCorpus(root, dir string, graphs int) []caseSpec {
	check(os.MkdirAll(dir, 0o755))
	var cases []caseSpec
	budgets := []caseSpec{{}, {MaxNodes: 25}, {MaxMergeWork: 30}, {MaxBytes: 250, MaxFiles: 3}}
	rng := xorshift(0x534b475245)
	names := []string{"main", "a", "b", "c", "d"}
	for g := range graphs {
		gdir := filepath.Join(dir, fmt.Sprintf("g%05d", g))
		check(os.MkdirAll(gdir, 0o755))
		files := names[:1+rng.intn(len(names))]
		for _, f := range files {
			var imports []string
			for _, other := range files {
				if other != f && rng.intn(100) < 35 {
					imports = append(imports, other)
				}
			}
			if f == "main" && rng.intn(10) == 0 {
				imports = append(imports, "main")
			}
			if rng.intn(20) == 0 {
				imports = append(imports, "missing")
			}
			var body strings.Builder
			switch len(imports) {
			case 0:
			case 1:
				fmt.Fprintf(&body, "import %q\n", imports[0]+".skg")
			default:
				quoted := make([]string, len(imports))
				for i, name := range imports {
					quoted[i] = strconv.Quote(name + ".skg")
				}
				fmt.Fprintf(&body, "import [%s]\n", strings.Join(quoted, ", "))
			}
			for range rng.intn(6) {
				body.WriteString(rng.node(0))
				body.WriteByte('\n')
			}
			check(os.WriteFile(filepath.Join(gdir, f+".skg"), []byte(body.String()), 0o644))
		}
		for b, budget := range budgets {
			budget.ID = fmt.Sprintf("g%05d-%d", g, b)
			budget.Entry = filepath.Join(gdir, "main.skg")
			cases = append(cases, budget)
		}
	}
	cases = append(cases, containmentCases(filepath.Join(dir, "containment"))...)
	for _, fixture := range []string{"valid", "invalid"} {
		entries, err := filepath.Glob(filepath.Join(root, "testdata", fixture, "*", "main.skg"))
		check(err)
		sort.Strings(entries)
		for _, entry := range entries {
			name := filepath.Base(filepath.Dir(entry))
			cases = append(cases, caseSpec{ID: fixture + "/" + name, Entry: entry, hasComment: treeHasComment(filepath.Dir(entry))})
		}
	}
	return cases
}

func treeHasComment(dir string) bool {
	found := false
	check(filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".skg") {
			src, _ := os.ReadFile(path)
			found = found || bytes.ContainsRune(src, '#')
		}
		return err
	}))
	return found
}

// containmentCases builds rooted graphs that try to leave cfg/ through parent
// components and symlinks. Every case uses cfg/ as the containment root.
func containmentCases(dir string) []caseSpec {
	type layout struct {
		files    map[string]string
		symlinks map[string]string // link -> target, relative to the link
	}
	layouts := map[string]layout{
		"inside":             {files: map[string]string{"cfg/main.skg": "import \"a.skg\"\n", "cfg/a.skg": "x: 1\n"}},
		"parent":             {files: map[string]string{"cfg/main.skg": "import \"../out.skg\"\n", "out.skg": "x: 1\n"}},
		"file-link-out":      {files: map[string]string{"cfg/main.skg": "import \"link.skg\"\n", "out.skg": "x: 1\n"}, symlinks: map[string]string{"cfg/link.skg": "../out.skg"}},
		"dir-link-out":       {files: map[string]string{"cfg/main.skg": "import \"sub/x.skg\"\n", "outdir/x.skg": "x: 1\n"}, symlinks: map[string]string{"cfg/sub": "../outdir"}},
		"file-link-in":       {files: map[string]string{"cfg/main.skg": "import \"alias.skg\"\n", "cfg/real.skg": "x: 1\n"}, symlinks: map[string]string{"cfg/alias.skg": "real.skg"}},
		"entry-link-out":     {files: map[string]string{"out_main.skg": "x: 1\n"}, symlinks: map[string]string{"cfg/main.skg": "../out_main.skg"}},
		"dotdot-after-link":  {files: map[string]string{"cfg/main.skg": "import \"sub/../a.skg\"\n", "cfg/a.skg": "where: \"lexical\"\n", "a.skg": "where: \"physical\"\n", "outdir/x.skg": ""}, symlinks: map[string]string{"cfg/sub": "../outdir"}},
		"dotdot-inside":      {files: map[string]string{"cfg/main.skg": "import \"a.skg\"\n", "cfg/a.skg": "import \"d/../b.skg\"\n", "cfg/b.skg": "y: 2\n", "cfg/d/keep.skg": ""}},
		"directory":          {files: map[string]string{"cfg/main.skg": "import \"dir.skg\"\n", "cfg/dir.skg/keep.skg": ""}},
		"reenter-root":       {files: map[string]string{"cfg/main.skg": "import \"sub/x.skg\"\n", "outdir/x.skg": "import \"../cfg/in.skg\"\n", "cfg/in.skg": "in: 1\n"}, symlinks: map[string]string{"cfg/sub": "../outdir"}},
		"dangling":           {files: map[string]string{"cfg/main.skg": "import \"dangling.skg\"\n"}, symlinks: map[string]string{"cfg/dangling.skg": "nowhere.skg"}},
		"dotdot-through-dir": {files: map[string]string{"cfg/main.skg": "import \"sub/../cfg/q.skg\"\n", "cfg/q.skg": "q: 1\n", "inner/keep.skg": ""}, symlinks: map[string]string{"cfg/sub": "../inner"}},
	}
	names := make([]string, 0, len(layouts))
	for name := range layouts {
		names = append(names, name)
	}
	sort.Strings(names)
	var cases []caseSpec
	for _, name := range names {
		base := filepath.Join(dir, name)
		check(os.MkdirAll(filepath.Join(base, "cfg"), 0o755))
		for rel, body := range layouts[name].files {
			path := filepath.Join(base, filepath.FromSlash(rel))
			check(os.MkdirAll(filepath.Dir(path), 0o755))
			check(os.WriteFile(path, []byte(body), 0o644))
		}
		for link, target := range layouts[name].symlinks {
			if err := os.Symlink(filepath.FromSlash(target), filepath.Join(base, filepath.FromSlash(link))); err != nil {
				fail("containment case %s needs symlinks: %v", name, err)
			}
		}
		entry := filepath.Join(base, "cfg", "main.skg")
		cases = append(cases,
			caseSpec{ID: "contain/" + name, Entry: entry, Root: filepath.Join(base, "cfg")},
			caseSpec{ID: "unrooted/" + name, Entry: entry})
	}
	return cases
}

// ─── Native corpus ───────────────────────────────────────────────────────────

// nativeCorpus enumerates single-fault inputs for every shared native profile,
// so the first error is never a matter of traversal order, which the native
// contract leaves unspecified. Documented host-language policies are excluded:
// Zig strings are byte slices that also accept integer arrays, and Rust
// decodes a single-entry block as a data-carrying enum variant, so an unknown
// tag in that form is invalid_enum there.
func nativeCorpus() []caseSpec {
	scalars := []string{
		"0", "1", "-1", "7", "80", "255", "256", "-129", "65535", "65536", "9007199254740993", "16777217",
		"9223372036854775807", "-9223372036854775808",
		"0.0", "-0.0", "0.5", "0.1", "1.0", "255.0", "16777217.0", "3.4028234663852886e38",
		"340282356779733661637539395458142568448.0", "0.000000000000000000000000000000000000000000001",
		"true", "false", "null", `""`, `"local"`, `"remote"`, `"LOCAL"`, `"ab"`, `"é"`, `"\n"`,
	}
	arrays := []string{"[]", "[1, 2]", "[1, 2, 3]", "[256, 1]", "[null, 1]", "[null]", `["a"]`}
	objects := []string{"{}", "{ a: 1 }"}
	values := map[string][]string{}
	profiles := []string{"bool", "u8", "optional_u8", "default_u8", "i64", "f32", "f64", "string", "bytes", "array2_u8", "record", "list_optional_records", "map_optional_u8", "port", "enum"}
	for _, p := range profiles {
		v := append([]string{}, scalars...)
		if p != "string" {
			v = append(v, arrays...)
		}
		if p != "enum" && p != "record" {
			v = append(v, objects...)
		}
		values[p] = v
	}
	ids := []string{"1", "65535", "65536", "-1", "null", `"x"`, "1.0"}
	notes := []string{`"n"`, "null", "1", "{}"}
	for _, id := range ids {
		values["record"] = append(values["record"], "{ id: "+id+" }", "{ id: "+id+` note: "n" }`)
	}
	for _, note := range notes {
		values["record"] = append(values["record"], "{ id: 1 note: "+note+" }")
	}
	values["record"] = append(values["record"], `{ note: "n" }`, "{ id: 1 extra: 1 }")
	values["list_optional_records"] = append(values["list_optional_records"],
		"[{ id: 1 }]", "[{ id: 1 }, null]", "[null, { id: 2 note: \"n\" }]", "[{ id: 65536 }]", "[{ id: 1 }, { note: \"n\" }]", "[{ id: 1 note: 1 }]", "[{ id: 1 extra: 1 }]")
	values["map_optional_u8"] = append(values["map_optional_u8"],
		"{ a: null }", "{ a: 256 }", `{ "k/~": 1 }`, `{ "k/~": "x" }`, "{ a: 1 b: 2 }", "{ a: -1 }")
	values["enum"] = append(values["enum"], `{ local {} }`)

	// A valid value per profile, so an added unknown field is the only fault.
	baseline := map[string]string{
		"bool": "true", "u8": "7", "optional_u8": "7", "default_u8": "7", "i64": "7",
		"f32": "0.5", "f64": "0.5", "string": `"ab"`, "bytes": `"ab"`, "array2_u8": "[1, 2]",
		"record": "{ id: 1 }", "list_optional_records": "[{ id: 1 }]", "map_optional_u8": "{ a: 1 }",
		"port": "80", "enum": `"local"`,
	}
	var cases []caseSpec
	options := []struct{ reject, lossy bool }{{false, false}, {true, false}, {false, true}}
	for _, p := range profiles {
		sources := []string{""}
		for _, v := range values[p] {
			sources = append(sources, "value: "+v)
		}
		sources = append(sources, "value: "+baseline[p]+"\nother: 1")
		for _, src := range sources {
			for _, o := range options {
				cases = append(cases, caseSpec{
					ID: fmt.Sprintf("n%05d", len(cases)), Profile: p, Source: src,
					RejectUnknownFields: o.reject, AllowLossyNumbers: o.lossy,
				})
			}
		}
	}
	return cases
}

// ─── Deterministic generation ────────────────────────────────────────────────

type xorshift uint64

func (r *xorshift) next() uint64 {
	x := uint64(*r)
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	*r = xorshift(x)
	return x
}

func (r *xorshift) intn(n int) int { return int(r.next() % uint64(n)) }

func (r *xorshift) pick(options ...string) string { return options[r.intn(len(options))] }

func (r *xorshift) key() string { return r.pick("k0", "k1", "k2", "blk", "arr", `"q k"`) }

func (r *xorshift) value(depth int) string {
	switch n := r.intn(10); {
	case n < 2:
		return strconv.Itoa(r.intn(11) - 5)
	case n < 4:
		return fmt.Sprintf(`"s%d"`, r.intn(4))
	case n < 5:
		return "null"
	case n < 6:
		return r.pick("true", "false")
	case n < 7:
		items := make([]string, r.intn(4))
		for i := range items {
			items[i] = strconv.Itoa(r.intn(4))
		}
		return "[" + strings.Join(items, ", ") + "]"
	case n < 8 && depth < 2:
		return "{ " + r.nodes(depth+1, 3) + " }"
	default:
		return fmt.Sprintf("%d.5", r.intn(10))
	}
}

func (r *xorshift) nodes(depth, most int) string {
	parts := make([]string, r.intn(most+1))
	for i := range parts {
		parts[i] = r.node(depth)
	}
	return strings.Join(parts, " ")
}

func (r *xorshift) node(depth int) string {
	switch n := r.intn(20); {
	case n < 9:
		return r.key() + ": " + r.value(depth)
	case n < 13 && depth < 3:
		return r.key() + " { " + r.nodes(depth+1, 3) + " }"
	case n < 15:
		return "@delete " + r.key()
	case n < 17 && depth < 3:
		return "@replace " + r.key() + " { " + r.nodes(depth+1, 2) + " }"
	default:
		items := make([]string, r.intn(3))
		for i := range items {
			if r.intn(2) == 0 {
				items[i] = "null"
			} else {
				items[i] = "{ " + r.node(depth+1) + " }"
			}
		}
		return r.key() + " [ " + strings.Join(items, ", ") + " ]"
	}
}

func check(err error) {
	if err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "parity: "+format+"\n", args...)
	os.Exit(1)
}
