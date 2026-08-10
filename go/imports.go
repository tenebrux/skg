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

// origin is where an import was written: the file that contains it and the
// position of its path token. A resolution failure is reported there rather
// than at 0:0, so the diagnostic points at a line the author can go and fix.
type origin struct {
	path string
	pos  Position
}

// importResolver carries the state of a single file-loading call.
type importResolver struct {
	// visited holds the canonical paths on the chain currently being resolved,
	// not every file ever loaded. Entries are removed on the way back out, so a
	// diamond - a imports b and c, both of which import d - is legal and loads d
	// twice. This mirrors zig/root.zig, which removes each path from its visited
	// set in a defer.
	visited map[string]bool

	// done memoises files that have been fully resolved during this call, keyed
	// by canonical path.
	//
	// Without it, resolution is exponential in the depth of a diamond-shaped
	// import graph: each level that imports the level below it twice doubles the
	// work, so 20 levels is around a million parses and 30 - still inside
	// MaxImportDepth - does not finish. That is a denial of service reachable
	// from a config file, and umbra parses config files as root.
	//
	// The cache holds only completed files, and a completed file has already
	// been popped off the chain, so a hit can never be a file that is still
	// being resolved: memoising cannot mask a cycle.
	done map[string]*File

	// chain holds the same files as visited, in order and spelled as they will
	// be shown to the user, so an error can name the route that reached a file.
	chain []string
}

// resolveImports loads path, parses it, and recursively resolves its imports.
//
// Import semantics mirror the Zig implementation (zig/root.zig Resolver.load):
// each import is resolved relative to the importing file, imports merge in
// declaration order, and the importing file's own values overlay everything it
// imported.
func resolveImports(path string) (*File, error) {
	r := &importResolver{visited: make(map[string]bool), done: make(map[string]*File)}
	return r.load(path, nil)
}

// load reads, parses and resolves one file. from is nil for the file the caller
// named, as opposed to one reached through an import: failures on the caller's
// own file are returned unwrapped, so ParseFile keeps handing back the untouched
// *fs.PathError that callers already match on.
func (r *importResolver) load(path string, from *origin) (*File, error) {
	key := canonicalPath(path)
	if cached, ok := r.done[key]; ok {
		return cached, nil
	}
	if err := r.enter(key, path, from); err != nil {
		return nil, err
	}
	defer r.leave(key)

	src, err := readCapped(path)
	if err != nil {
		if from == nil {
			return nil, err
		}
		// Wrap in a ParseError so the failure carries IMPORT_NOT_FOUND like
		// every other diagnostic, while %w keeps errors.Is(err, fs.ErrNotExist)
		// working for callers that check for a missing file.
		return nil, &ParseError{Diag: Diagnostic{
			Path:    from.path,
			Line:    from.pos.Line,
			Col:     from.pos.Col,
			Code:    CodeImportNotFound,
			Message: fmt.Sprintf("cannot read imported file: %v (import chain: %s)", err, r.chainString()),
		}, Err: err}
	}

	file, err := ParseSource(src, path)
	if err != nil {
		if from == nil {
			return nil, err
		}
		return nil, fmt.Errorf("skg: %w (import chain: %s)", err, r.chainString())
	}

	if len(file.ImportPaths) == 0 {
		r.done[key] = file
		return file, nil
	}

	// Merge the imports into each other in declaration order, then let this
	// file's own children overlay the result: "the main config file always
	// loads after all its imports, so it always wins" (docs/spec.md).
	var merged []Node
	for i, importPath := range file.ImportPaths {
		imported, err := r.load(resolveImportPath(path, importPath), &origin{path: path, pos: file.ImportPositions[i]})
		if err != nil {
			return nil, err
		}
		merged = MergeNodes(merged, imported.Children)
	}
	file.Children = MergeNodes(merged, file.Children)

	r.done[key] = file
	return file, nil
}

// enter records that key is being resolved, rejecting a cycle or an over-deep
// chain before recursing into it. A successful enter must be paired with leave.
func (r *importResolver) enter(key, path string, from *origin) error {
	if r.visited[key] {
		return r.reject(path, from, CodeCircularImport, "circular import: "+r.chainStringWith(path))
	}
	if len(r.chain) > MaxImportDepth {
		return r.reject(path, from, CodeImportChainTooDeep,
			"import chain too deep (max "+itoa(MaxImportDepth)+"): "+r.chainStringWith(path))
	}
	r.visited[key] = true
	r.chain = append(r.chain, path)
	return nil
}

// reject builds the diagnostic for a file that was refused before it could be
// entered, anchored at the import statement that named it when there is one.
func (r *importResolver) reject(path string, from *origin, code ErrorCode, message string) error {
	d := Diagnostic{Path: path, Code: code, Message: message}
	if from != nil {
		d.Path = from.path
		d.Line = from.pos.Line
		d.Col = from.pos.Col
	}
	return &ParseError{Diag: d}
}

func (r *importResolver) leave(key string) {
	delete(r.visited, key)
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
// The path is joined onto the importing file's directory, per docs/spec.md
// ("Import paths are relative to the file containing the import statement") and
// matching zig/root.zig, which joins each import onto
// `std.fs.path.dirname(path) orelse "."`.
//
// Absolute paths never reach here: the parser rejects them with
// ABSOLUTE_IMPORT_PATH before resolution begins (go/parser.go
// isAbsoluteImportPath), so the two implementations no longer have to reconcile
// two different wrong answers for "/etc/theme.skg".
func resolveImportPath(importingFile, importPath string) string {
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
