package skg

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFiles materialises a set of files under a fresh temp dir and returns it.
// Keys are slash-separated paths relative to that dir.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// fieldValue returns the string value of a top-level or dotted field path,
// e.g. "theme.accent". It fails the test if the path is missing.
func fieldValue(t *testing.T, file *File, path string) Value {
	t.Helper()
	nodes := file.Children
	parts := strings.Split(path, ".")
	for i, part := range parts {
		var found *Node
		for j := range nodes {
			n := nodes[j]
			if key, ok := nodeKey(n); ok && key == part {
				found = &nodes[j]
				break
			}
		}
		if found == nil {
			t.Fatalf("no node %q in path %q", part, path)
		}
		if i == len(parts)-1 {
			if found.Field == nil {
				t.Fatalf("%q is not a field", path)
			}
			return found.Field.Value
		}
		if found.Block == nil {
			t.Fatalf("%q is not a block", part)
		}
		nodes = found.Block.Children
	}
	panic("unreachable")
}

func wantString(t *testing.T, file *File, path, want string) {
	t.Helper()
	v := fieldValue(t, file, path)
	if v.Type != TypeString {
		t.Fatalf("%s: expected string, got %v", path, v.Type)
	}
	if v.Str != want {
		t.Errorf("%s: expected %q, got %q", path, want, v.Str)
	}
}

func TestParseFileResolvesImport(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"theme.skg": "theme {\n  accent: \"purple\"\n}\n",
		"main.skg":  "import \"./theme.skg\"\n\nname: \"main\"\n",
	})

	file, err := ParseFile(filepath.Join(dir, "main.skg"))
	if err != nil {
		t.Fatal(err)
	}
	wantString(t, file, "theme.accent", "purple")
	wantString(t, file, "name", "main")

	// The declared paths are still reported verbatim.
	if len(file.ImportPaths) != 1 || file.ImportPaths[0] != "./theme.skg" {
		t.Errorf("expected ImportPaths [./theme.skg], got %v", file.ImportPaths)
	}
	// Imports load first, so they come first in the merged child list.
	if key, _ := nodeKey(file.Children[0]); key != "theme" {
		t.Errorf("expected imported node first, got %q", key)
	}
}

// The importing file always wins: "The main config file always loads after all
// its imports, so it always wins" (docs/spec.md).
func TestParseFileImportLastWins(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"theme.skg": "theme {\n  accent: \"purple\"\n  font: \"mono\"\n}\nname: \"theme\"\n",
		"main.skg":  "import \"./theme.skg\"\n\ntheme {\n  accent: \"green\"\n}\n",
	})

	file, err := ParseFile(filepath.Join(dir, "main.skg"))
	if err != nil {
		t.Fatal(err)
	}
	wantString(t, file, "theme.accent", "green") // overridden by the importer
	wantString(t, file, "theme.font", "mono")    // untouched keys survive
	wantString(t, file, "name", "theme")         // keys only the import sets survive
}

func TestParseFileMultipleImportsMergeInOrder(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a.skg":    "k: \"a\"\nonly_a: \"a\"\n",
		"b.skg":    "k: \"b\"\nonly_b: \"b\"\n",
		"main.skg": "import [\n  \"./a.skg\",\n  \"./b.skg\",\n]\n",
	})

	file, err := ParseFile(filepath.Join(dir, "main.skg"))
	if err != nil {
		t.Fatal(err)
	}
	wantString(t, file, "k", "b") // later import overlays the earlier one
	wantString(t, file, "only_a", "a")
	wantString(t, file, "only_b", "b")

	// And the importer still beats every import.
	main := filepath.Join(dir, "main.skg")
	if err := os.WriteFile(main, []byte("import [\"./a.skg\", \"./b.skg\"]\nk: \"main\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err = ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	wantString(t, file, "k", "main")
}

func TestParseFileMultiLevelImports(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"c.skg":    "k: \"c\"\nfrom_c: \"c\"\n",
		"b.skg":    "import \"./c.skg\"\nk: \"b\"\nfrom_b: \"b\"\n",
		"main.skg": "import \"./b.skg\"\nk: \"main\"\n",
	})

	file, err := ParseFile(filepath.Join(dir, "main.skg"))
	if err != nil {
		t.Fatal(err)
	}
	wantString(t, file, "k", "main") // main > b > c
	wantString(t, file, "from_b", "b")
	wantString(t, file, "from_c", "c")
}

// Each import resolves against the file that wrote it, not against the root
// file or the process working directory.
func TestParseFileImportPathIsRelativeToImportingFile(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"sub/inner.skg": "inner: \"yes\"\n",
		"sub/mid.skg":   "import \"./inner.skg\"\nmid: \"yes\"\n",
		"main.skg":      "import \"sub/mid.skg\"\nroot: \"yes\"\n",
	})

	file, err := ParseFile(filepath.Join(dir, "main.skg"))
	if err != nil {
		t.Fatal(err)
	}
	wantString(t, file, "inner", "yes")
	wantString(t, file, "mid", "yes")
	wantString(t, file, "root", "yes")
}

// A diamond is not a cycle: d is reached twice and must load both times.
func TestParseFileDiamondImportIsNotACycle(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"d.skg":    "shared: \"d\"\n",
		"b.skg":    "import \"./d.skg\"\nb: \"yes\"\n",
		"c.skg":    "import \"./d.skg\"\nc: \"yes\"\n",
		"main.skg": "import [\"./b.skg\", \"./c.skg\"]\n",
	})

	file, err := ParseFile(filepath.Join(dir, "main.skg"))
	if err != nil {
		t.Fatal(err)
	}
	wantString(t, file, "shared", "d")
	wantString(t, file, "b", "yes")
	wantString(t, file, "c", "yes")
}

// An absolute import is refused before any resolution happens, so the file it
// names is never opened even when it exists and is readable.
func TestParseFileAbsoluteImportPath(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"theme.skg": "accent: \"purple\"\n",
	})
	abs := filepath.Join(dir, "theme.skg")
	main := filepath.Join(dir, "nested", "main.skg")
	if err := os.MkdirAll(filepath.Dir(main), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte("import \""+filepath.ToSlash(abs)+"\"\nname: \"main\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseFile(main)
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Diag.Code != CodeAbsoluteImportPath {
		t.Fatalf("want ABSOLUTE_IMPORT_PATH, got %v", err)
	}
}

func TestAbsoluteImportPathSpellings(t *testing.T) {
	absolute := []string{"/etc/theme.skg", `\etc\theme.skg`, `C:\theme.skg`, "c:theme.skg"}
	for _, p := range absolute {
		if !isAbsoluteImportPath(p) {
			t.Errorf("isAbsoluteImportPath(%q) = false, want true", p)
		}
	}
	// Host-independent: the Windows spellings are rejected on every platform, so
	// a config cannot mean one thing on Linux and another on Windows.
	relative := []string{"theme.skg", "./theme.skg", "../theme.skg", "sub/theme.skg", "", "c/theme.skg"}
	for _, p := range relative {
		if isAbsoluteImportPath(p) {
			t.Errorf("isAbsoluteImportPath(%q) = true, want false", p)
		}
	}
}

// A diamond graph where every level imports the level below it twice is 2^depth
// files to load without memoisation. At 20 levels that was about a million
// parses and seven seconds; at 30 - still inside MaxImportDepth - it did not
// finish. umbra parses init.skg manifests as root, so this was a denial of
// service reachable from a config file.
//
// The test asserts the result rather than a wall-clock budget: it simply cannot
// complete in the time `go test` allows unless the cache is working.
func TestParseFileDiamondIsNotExponential(t *testing.T) {
	const depth = 30
	files := map[string]string{
		fmt.Sprintf("f%02d.skg", depth): "leaf: \"bottom\"\n",
	}
	for i := 0; i < depth; i++ {
		files[fmt.Sprintf("f%02d.skg", i)] = fmt.Sprintf(
			"import [\"./f%02d.skg\", \"./f%02d.skg\"]\n\nlevel%02d: %d\n", i+1, i+1, i, i)
	}
	dir := writeFiles(t, files)

	file, err := ParseFile(filepath.Join(dir, "f00.skg"))
	if err != nil {
		t.Fatal(err)
	}
	wantString(t, file, "leaf", "bottom")
	if got := len(file.Children); got != depth+1 {
		t.Errorf("expected %d children, got %d", depth+1, got)
	}
}

// Memoising completed files must not turn a genuine cycle into a cache hit: a
// file is only cached once it has been popped off the chain, so a file still
// being resolved is never in the cache.
func TestMemoisationDoesNotMaskACycle(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.skg":   "import [\"./a.skg\", \"./b.skg\"]\n",
		"a.skg":      "import \"./shared.skg\"\na: 1\n",
		"b.skg":      "import [\"./shared.skg\", \"./cyc.skg\"]\nb: 2\n",
		"shared.skg": "shared: 3\n",
		"cyc.skg":    "import \"./cyc2.skg\"\n",
		"cyc2.skg":   "import \"./cyc.skg\"\n",
	})
	_, err := ParseFile(filepath.Join(dir, "main.skg"))
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Diag.Code != CodeCircularImport {
		t.Fatalf("want CIRCULAR_IMPORT, got %v", err)
	}
}

// An import failure is reported at the import statement that named the file,
// not at 0:0. The diagnostic's path is the file that wrote the import.
func TestImportDiagnosticHasSourcePosition(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.skg": "import [\n  \"./ok.skg\",\n  \"./missing.skg\",\n]\n",
		"ok.skg":   "ok: 1\n",
	})
	_, err := ParseFile(filepath.Join(dir, "main.skg"))
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("want *ParseError, got %v", err)
	}
	if pe.Diag.Code != CodeImportNotFound {
		t.Fatalf("want IMPORT_NOT_FOUND, got %s", pe.Diag.Code)
	}
	if pe.Diag.Line != 3 || pe.Diag.Col != 3 {
		t.Errorf("want position 3:3 (the path token), got %d:%d", pe.Diag.Line, pe.Diag.Col)
	}
	if pe.Diag.Path != filepath.Join(dir, "main.skg") {
		t.Errorf("want the importing file as path, got %q", pe.Diag.Path)
	}
}

func TestParseFileCircularImport(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a.skg": "import \"./b.skg\"\na: 1\n",
		"b.skg": "import \"./a.skg\"\nb: 2\n",
	})

	_, err := ParseFile(filepath.Join(dir, "a.skg"))
	if err == nil {
		t.Fatal("expected a circular import error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "circular import") {
		t.Errorf("expected a circular import error, got %q", msg)
	}
	// The message must name the cycle, so the user can find it.
	for _, want := range []string{"a.skg", "b.skg"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not name %q", msg, want)
		}
	}
	if strings.Count(msg, "a.skg") < 2 {
		t.Errorf("expected the chain to show the file it returned to, got %q", msg)
	}
}

func TestParseFileSelfImport(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a.skg": "import \"./a.skg\"\na: 1\n",
	})

	_, err := ParseFile(filepath.Join(dir, "a.skg"))
	if err == nil {
		t.Fatal("expected a circular import error")
	}
	if !strings.Contains(err.Error(), "circular import") {
		t.Errorf("unexpected error: %v", err)
	}
}

// The cycle guard keys on the resolved path, so spelling the same file two
// different ways still trips it.
func TestParseFileCircularImportThroughDifferentSpelling(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"sub/b.skg": "import \"../sub/./b.skg\"\nb: 2\n",
	})

	_, err := ParseFile(filepath.Join(dir, "sub", "b.skg"))
	if err == nil {
		t.Fatal("expected a circular import error")
	}
	if !strings.Contains(err.Error(), "circular import") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseFileMissingImport(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.skg": "import \"./nope.skg\"\nname: \"main\"\n",
	})

	_, err := ParseFile(filepath.Join(dir, "main.skg"))
	if err == nil {
		t.Fatal("expected an error for a missing imported file")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected the underlying fs error to be unwrappable, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "nope.skg") {
		t.Errorf("error %q does not name the missing file", msg)
	}
	if !strings.Contains(msg, "main.skg") || !strings.Contains(msg, "import chain") {
		t.Errorf("error %q does not report the import chain", msg)
	}
}

// A parse failure inside an import must point at the imported file, not the
// file the caller named.
func TestParseFileImportParseErrorNamesFile(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"broken.skg": "key \"value\"\n",
		"main.skg":   "import \"./broken.skg\"\n",
	})

	_, err := ParseFile(filepath.Join(dir, "main.skg"))
	if err == nil {
		t.Fatal("expected the imported file's parse error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "broken.skg") || !strings.Contains(msg, "expected ':'") {
		t.Errorf("unexpected error: %q", msg)
	}
	if !strings.Contains(msg, "import chain") {
		t.Errorf("error %q does not report the import chain", msg)
	}
}

// The 10MB cap is per file, so it covers imported files too.
func TestParseFileImportedFileSizeLimit(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, MaxFileSize+1)
	for i := range big {
		big[i] = '#'
	}
	if err := os.WriteFile(filepath.Join(dir, "big.skg"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.skg"), []byte("import \"./big.skg\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseFile(filepath.Join(dir, "main.skg"))
	if err == nil {
		t.Fatal("expected the oversized import to be rejected")
	}
	if !strings.Contains(err.Error(), "file too large") {
		t.Errorf("unexpected error: %v", err)
	}
}

// chainOfImports writes n files where file i imports file i+1; file n-1 is a
// leaf. Returns the path of file 0.
func chainOfImports(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		body := "depth: " + itoa(i) + "\n"
		if i+1 < n {
			body = "import \"./f" + itoa(i+1) + ".skg\"\n" + body
		}
		if err := os.WriteFile(filepath.Join(dir, "f"+itoa(i)+".skg"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "f0.skg")
}

func TestImportDepthLimitAtBoundary(t *testing.T) {
	// Root file plus MaxImportDepth levels of imports below it.
	root := chainOfImports(t, MaxImportDepth+1)
	file, err := ParseFile(root)
	if err != nil {
		t.Fatalf("a chain of exactly MaxImportDepth imports should be accepted, got %v", err)
	}
	// The deepest file loads first, so the root's own value wins.
	v := fieldValue(t, file, "depth")
	if v.Type != TypeInt || v.Int != 0 {
		t.Errorf("expected the root file's depth to win, got %v", v)
	}
}

// Regression: an unbounded chain would recurse until the Go stack overflows,
// which is fatal and cannot be recovered from.
func TestImportDepthLimitExceeded(t *testing.T) {
	root := chainOfImports(t, MaxImportDepth+2)
	_, err := ParseFile(root)
	if err == nil {
		t.Fatal("expected an import depth error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "import chain too deep (max "+itoa(MaxImportDepth)+")") {
		t.Errorf("unexpected error: %q", msg)
	}
	if !strings.Contains(msg, "f0.skg") {
		t.Errorf("error %q does not report the chain", msg)
	}
}

// A missing root file keeps returning the untouched fs error, as before.
func TestParseFileMissingRootFile(t *testing.T) {
	_, err := ParseFile(filepath.Join(t.TempDir(), "absent.skg"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist, got %v", err)
	}
	if strings.Contains(err.Error(), "import chain") {
		t.Errorf("the caller's own file is not an import: %v", err)
	}
}

func TestUnmarshalFileResolvesImports(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"theme.skg": "theme {\n  accent: \"purple\"\n  font: \"mono\"\n}\n",
		"main.skg":  "import \"./theme.skg\"\ntheme {\n  accent: \"green\"\n}\n",
	})

	var cfg struct {
		Theme struct {
			Accent string `skg:"accent"`
			Font   string `skg:"font"`
		} `skg:"theme"`
	}
	if err := UnmarshalFile(filepath.Join(dir, "main.skg"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Accent != "green" {
		t.Errorf("expected accent green, got %q", cfg.Theme.Accent)
	}
	if cfg.Theme.Font != "mono" {
		t.Errorf("expected font mono, got %q", cfg.Theme.Font)
	}
}

// Parse, ParseSource and Unmarshal must never touch the filesystem. A consumer
// handing over untrusted bytes relies on that: no path traversal, no symlink
// following, no I/O. Imports are recorded and left for the caller to resolve.
//
// If a refactor ever routes these through the resolver, this test fails.
func TestByteEntryPointsDoNotResolveImports(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"theme.skg": "theme {\n  accent: \"purple\"\n}\n",
	})
	src := []byte("import \"./theme.skg\"\nname: \"main\"\n")
	mainPath := filepath.Join(dir, "main.skg")

	check := func(t *testing.T, file *File) {
		t.Helper()
		if len(file.ImportPaths) != 1 || file.ImportPaths[0] != "./theme.skg" {
			t.Errorf("expected the import to be recorded, got %v", file.ImportPaths)
		}
		if len(file.Children) != 1 {
			t.Fatalf("expected only the file's own child, got %d", len(file.Children))
		}
		if key, _ := nodeKey(file.Children[0]); key != "name" {
			t.Errorf("expected only %q, got %q - the import was resolved", "name", key)
		}
	}

	t.Run("Parse", func(t *testing.T) {
		file, err := Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		check(t, file)
	})

	// Even given a real path next to a real importable file, ParseSource must
	// not read it: the path is a diagnostic label, nothing more.
	t.Run("ParseSource", func(t *testing.T) {
		file, err := ParseSource(src, mainPath)
		if err != nil {
			t.Fatal(err)
		}
		check(t, file)
	})

	t.Run("Unmarshal", func(t *testing.T) {
		var cfg struct {
			Name  string `skg:"name"`
			Theme struct {
				Accent string `skg:"accent"`
			} `skg:"theme"`
		}
		if err := Unmarshal(src, &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Name != "main" {
			t.Errorf("expected name main, got %q", cfg.Name)
		}
		if cfg.Theme.Accent != "" {
			t.Errorf("Unmarshal resolved an import: accent = %q", cfg.Theme.Accent)
		}
	})

	// A cycle on disk is inert for the byte entry points - they never walk it.
	t.Run("CycleIsNotWalked", func(t *testing.T) {
		cyclic := writeFiles(t, map[string]string{
			"a.skg": "import \"./b.skg\"\na: 1\n",
			"b.skg": "import \"./a.skg\"\nb: 2\n",
		})
		data, err := os.ReadFile(filepath.Join(cyclic, "a.skg"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseSource(data, filepath.Join(cyclic, "a.skg")); err != nil {
			t.Fatalf("ParseSource must ignore imports entirely, got %v", err)
		}
	})
}

func TestResolveImportPath(t *testing.T) {
	cases := []struct {
		importing string
		imported  string
		want      string
	}{
		{"main.skg", "./theme.skg", "theme.skg"},
		{"main.skg", "theme.skg", "theme.skg"},
		{filepath.Join("cfg", "main.skg"), "./theme.skg", filepath.Join("cfg", "theme.skg")},
		{filepath.Join("cfg", "main.skg"), "../theme.skg", "theme.skg"},
		{filepath.Join("cfg", "main.skg"), "sub/theme.skg", filepath.Join("cfg", "sub", "theme.skg")},
	}
	for _, tc := range cases {
		if got := resolveImportPath(tc.importing, tc.imported); got != tc.want {
			t.Errorf("resolveImportPath(%q, %q) = %q, want %q", tc.importing, tc.imported, got, tc.want)
		}
	}
	// Absolute paths never reach resolveImportPath - the parser rejects them
	// with ABSOLUTE_IMPORT_PATH. See TestAbsoluteImportPathSpellings.
}
