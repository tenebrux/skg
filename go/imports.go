package skg

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxImportDepth bounds how many levels of imports the file-loading entry
// points follow below the file they were handed.
//
// Resolution recurses once per level. The cycle guard below catches an import
// graph that loops through paths it can recognise as the same file, but two
// spellings it cannot reconcile - a symlink loop, say, or a bind mount - would
// otherwise recurse until the Go stack overflows, which is fatal and cannot be
// recovered from. This cap is the backstop, and 32 is far past any real
// configuration layout.
//
// This is deliberately separate from MaxNestingDepth, which bounds syntactic
// nesting inside a single file: one file can be flat yet sit at the bottom of a
// long import chain, and vice versa.
const MaxImportDepth = 32

// importResolver carries the state of a single file-loading call.
type importResolver struct {
	// visited holds the canonical paths on the chain currently being resolved,
	// not every file ever loaded. Entries are removed on the way back out, so a
	// diamond - a imports b and c, both of which import d - is legal and loads d
	// twice. This mirrors zig/root.zig, which removes each path from its visited
	// set in a defer.
	visited map[string]bool

	// chain holds the same files as visited, in order and spelled as they will
	// be shown to the user, so an error can name the route that reached a file.
	chain []string
}

// resolveImports loads path, parses it, and recursively resolves its imports.
//
// Import semantics mirror the Zig implementation (zig/root.zig
// parseWithVisited): each import is resolved relative to the importing file,
// imports merge in declaration order, and the importing file's own values
// overlay everything it imported.
func resolveImports(path string) (*File, error) {
	r := &importResolver{visited: make(map[string]bool)}
	return r.load(path, true)
}

// load reads, parses and resolves one file. root reports whether this is the
// file the caller named, as opposed to one reached through an import: failures
// on the caller's own file are returned unwrapped, so ParseFile keeps handing
// back the untouched *fs.PathError that callers already match on.
func (r *importResolver) load(path string, root bool) (*File, error) {
	if err := r.enter(path); err != nil {
		return nil, err
	}
	defer r.leave(path)

	src, err := readCapped(path)
	if err != nil {
		if root {
			return nil, err
		}
		return nil, fmt.Errorf("skg: cannot read imported file: %w (import chain: %s)", err, r.chainString())
	}

	file, err := ParseSource(src, path)
	if err != nil {
		if root {
			return nil, err
		}
		return nil, fmt.Errorf("skg: %w (import chain: %s)", err, r.chainString())
	}

	if len(file.ImportPaths) == 0 {
		return file, nil
	}

	// Merge the imports into each other in declaration order, then let this
	// file's own children overlay the result: "the main config file always
	// loads after all its imports, so it always wins" (docs/spec.md).
	var merged []Node
	for _, importPath := range file.ImportPaths {
		imported, err := r.load(resolveImportPath(path, importPath), false)
		if err != nil {
			return nil, err
		}
		merged = MergeNodes(merged, imported.Children)
	}
	file.Children = MergeNodes(merged, file.Children)

	return file, nil
}

// enter records that path is being resolved, rejecting a cycle or an over-deep
// chain before recursing into it. A successful enter must be paired with leave.
func (r *importResolver) enter(path string) error {
	key := canonicalPath(path)
	if r.visited[key] {
		return &ParseError{Diag: Diagnostic{
			Path:    path,
			Message: "circular import: " + r.chainStringWith(path),
		}}
	}
	if len(r.chain) > MaxImportDepth {
		return &ParseError{Diag: Diagnostic{
			Path:    path,
			Message: "import chain too deep (max " + itoa(MaxImportDepth) + "): " + r.chainStringWith(path),
		}}
	}
	r.visited[key] = true
	r.chain = append(r.chain, path)
	return nil
}

func (r *importResolver) leave(path string) {
	delete(r.visited, canonicalPath(path))
	r.chain = r.chain[:len(r.chain)-1]
}

// chainString renders the files currently being resolved, outermost first:
// "main.skg -> theme.skg -> dusk.skg".
func (r *importResolver) chainString() string {
	return strings.Join(r.chain, " -> ")
}

// chainStringWith renders the chain with target appended, for reporting a file
// that was rejected before it could be entered.
func (r *importResolver) chainStringWith(target string) string {
	return strings.Join(append(append([]string{}, r.chain...), target), " -> ")
}

// resolveImportPath locates importPath as written inside importingFile.
//
// A relative path is joined onto the importing file's directory, per
// docs/spec.md ("Import paths are relative to the file containing the import
// statement") and matching zig/root.zig, which joins each import onto
// `std.fs.path.dirname(path) orelse "."`.
//
// An absolute path is used as written. The spec says nothing about absolute
// imports; taking them literally is the only reading that does what an author
// who typed one meant. (Note that the Zig implementation instead feeds them
// through its path join, which quietly reinterprets "/etc/theme.skg" as a
// subdirectory of the importing file's directory - it is a consequence of how
// join works rather than a decision, and worth reconciling across the two
// implementations.)
func resolveImportPath(importingFile, importPath string) string {
	if filepath.IsAbs(importPath) {
		return filepath.Clean(importPath)
	}
	return filepath.Join(filepath.Dir(importingFile), importPath)
}

// canonicalPath returns the key a file is tracked under while resolving, so
// that "./theme.skg" and "theme.skg" reached from different directories are
// recognised as the same file.
//
// It is lexical only: symlinks are not resolved, because doing so costs a
// syscall per import and fails on paths that do not exist yet, and because
// MaxImportDepth already bounds any loop this misses.
func canonicalPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// readCapped reads a file, refusing to buffer more than the parser will accept.
//
// It reads one byte past the cap rather than stat-ing: a size check alone lies
// for pipes and /proc entries, and os.ReadFile would buffer the whole input
// before ParseSource could reject it.
func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, MaxFileSize+1))
}
