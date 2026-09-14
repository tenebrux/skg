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
// Resolution recurses once per level. Real-path cycle detection catches
// ordinary content cycles and symlink aliases; this independent cap prevents a
// very deep acyclic graph from exhausting the Go stack. Thirty-two levels are
// far past a normal configuration layout.
//
// This is deliberately separate from MaxNestingDepth, which bounds syntactic
// nesting inside a single file: one file can be flat yet sit at the bottom of a
// long import chain, and vice versa.
const MaxImportDepth = 32

const (
	// V1 file-resolution defaults. These are compatibility limits: lowering a
	// default in a 1.x release would reject a graph accepted by V1.0.
	DefaultMaxResolveBytes     int64 = 64 * 1024 * 1024
	DefaultMaxResolveFiles           = 1024
	DefaultMaxResolveNodes           = 8_000_000
	DefaultMaxResolveMergeWork int64 = 64_000_000
)

// ResolveOptions controls file graph resolution. Zero limits select the V1
// defaults above. Positive values may deliberately tighten or raise a limit.
// Root enables opt-in containment after resolving symlinks; an empty Root keeps
// the historical unrestricted, relative-import behavior.
type ResolveOptions struct {
	Root         string
	MaxBytes     int64
	MaxFiles     int
	MaxNodes     int
	MaxMergeWork int64
}

type resolveLimits struct {
	maxBytes, maxMergeWork int64
	maxFiles, maxNodes     int
}

// origin is where an import was written: the file that contains it and the
// position of its path token. A resolution failure is reported there rather
// than at 0:0, so the diagnostic points at a line the author can go and fix.
type origin struct {
	path string
	pos  Position
}

// resolvedImport retains the longest suffix so cache hits enforce the same
// graph-depth limit regardless of import order.
type resolvedImport struct {
	file  *File
	depth int
}

// importResolver carries the state of a single file-loading call.
type importResolver struct {
	limits resolveLimits
	root   string
	bytes  int64
	files  int
	nodes  int
	work   int64
	// visited holds the canonical paths on the chain currently being resolved,
	// not every file ever loaded. Entries are removed on the way back out, so a
	// diamond - a imports b and c, both of which import d - is legal and reuses d. Each path is removed from the active set on return.
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
	done map[string]resolvedImport

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
	return resolveImportsWithOptions(path, ResolveOptions{})
}

func resolveImportsWithOptions(path string, options ResolveOptions) (*File, error) {
	limits, err := normalizedResolveLimits(options)
	if err != nil {
		return nil, err
	}
	root := ""
	if options.Root != "" {
		root, err = canonicalPath(options.Root)
		if err != nil {
			return nil, &ParseError{Diag: Diagnostic{Code: CodeImportNotFound, Path: options.Root, Message: "resolution root not found"}, Err: err}
		}
		info, statErr := os.Stat(root)
		if statErr != nil {
			return nil, &ParseError{Diag: Diagnostic{Code: CodeImportNotFound, Path: options.Root, Message: "resolution root not found"}, Err: statErr}
		}
		if !info.IsDir() {
			return nil, &ParseError{Diag: Diagnostic{Code: CodeImportNotFound, Path: options.Root, Message: "resolution root is not a directory"}}
		}
	}
	r := &importResolver{
		limits:  limits,
		root:    root,
		work:    limits.maxMergeWork,
		visited: make(map[string]bool),
		done:    make(map[string]resolvedImport),
	}
	file, err := r.load(path, nil)
	if err != nil {
		return nil, err
	}
	result := *file
	result.Children = MaterializeNodes(file.Children)
	result.ImportsResolved = true
	return &result, nil
}

func normalizedResolveLimits(options ResolveOptions) (resolveLimits, error) {
	if options.MaxBytes < 0 || options.MaxFiles < 0 || options.MaxNodes < 0 || options.MaxMergeWork < 0 {
		return resolveLimits{}, fmt.Errorf("skg: resolution limits cannot be negative")
	}
	limits := resolveLimits{
		maxBytes: options.MaxBytes, maxFiles: options.MaxFiles,
		maxNodes: options.MaxNodes, maxMergeWork: options.MaxMergeWork,
	}
	if limits.maxBytes == 0 {
		limits.maxBytes = DefaultMaxResolveBytes
	}
	if limits.maxFiles == 0 {
		limits.maxFiles = DefaultMaxResolveFiles
	}
	if limits.maxNodes == 0 {
		limits.maxNodes = DefaultMaxResolveNodes
	}
	if limits.maxMergeWork == 0 {
		limits.maxMergeWork = DefaultMaxResolveMergeWork
	}
	return limits, nil
}

// load reads, parses and resolves one file. from is nil for the file the caller
// named, as opposed to one reached through an import: failures on the caller's
// own file are returned unwrapped, so ParseFile keeps handing back the untouched
// *fs.PathError that callers already match on.
func (r *importResolver) load(path string, from *origin) (*File, error) {
	key, err := canonicalPath(path)
	if err != nil {
		if from == nil {
			return nil, err
		}
		return nil, &ParseError{Diag: Diagnostic{
			Path: from.path, Line: from.pos.Line, Col: from.pos.Col,
			Code:    CodeImportNotFound,
			Message: fmt.Sprintf("cannot resolve imported file: %v (import chain: %s)", err, r.chainStringWith(path)),
		}, Err: err}
	}
	if r.root != "" && !pathWithinRoot(r.root, key) {
		return nil, r.reject(key, from, CodePathOutsideRoot,
			"resolved path is outside root: "+key)
	}
	if cached, ok := r.done[key]; ok {
		if len(r.chain)+cached.depth > MaxImportDepth {
			return nil, r.reject(path, from, CodeImportChainTooDeep, "import chain too deep through cached file: "+r.chainStringWith(path))
		}
		return cached.file, nil
	}
	if err := r.enter(key, path, from); err != nil {
		return nil, err
	}
	defer r.leave(key)
	if r.files >= r.limits.maxFiles {
		return nil, r.reject(key, from, CodeResolutionFileLimit,
			"resolution file limit exceeded (max "+itoa(r.limits.maxFiles)+")")
	}
	r.files++

	src, err := readCapped(key)
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
	if int64(len(src)) > r.limits.maxBytes-r.bytes {
		return nil, r.reject(key, from, CodeResolutionByteLimit,
			fmt.Sprintf("resolution byte limit exceeded (max %d)", r.limits.maxBytes))
	}
	r.bytes += int64(len(src))

	file, err := ParseSource(src, key)
	if err != nil {
		if from == nil {
			return nil, err
		}
		return nil, fmt.Errorf("skg: %w (import chain: %s)", err, r.chainString())
	}
	count := countFileNodes(file)
	if count > r.limits.maxNodes-r.nodes {
		return nil, r.reject(key, from, CodeResolutionNodeLimit,
			"resolution node limit exceeded (max "+itoa(r.limits.maxNodes)+")")
	}
	r.nodes += count

	if len(file.ImportPaths) == 0 {
		r.done[key] = resolvedImport{file: file}
		return file, nil
	}

	// Merge the imports into each other in declaration order, then let this
	// file's own children overlay the result: "the main config file always
	// loads after all its imports, so it always wins" (docs/spec.md).
	var merged []Node
	depth := 0
	for i, importPath := range file.ImportPaths {
		childPath := resolveImportPath(key, importPath)
		imported, err := r.load(childPath, &origin{path: key, pos: file.ImportPositions[i]})
		if err != nil {
			return nil, err
		}
		childKey, _ := canonicalPath(childPath)
		depth = max(depth, 1+r.done[childKey].depth)
		merged, err = mergeNodesBudget(merged, imported.Children, &r.work)
		if err != nil {
			return nil, r.reject(key, &origin{path: key, pos: file.ImportPositions[i]},
				CodeResolutionWorkLimit, fmt.Sprintf("resolution merge work limit exceeded (max %d)", r.limits.maxMergeWork))
		}
	}
	file.Children, err = mergeNodesBudget(merged, file.Children, &r.work)
	if err != nil {
		return nil, r.reject(key, from, CodeResolutionWorkLimit,
			fmt.Sprintf("resolution merge work limit exceeded (max %d)", r.limits.maxMergeWork))
	}

	r.done[key] = resolvedImport{file: file, depth: depth}
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

// canonicalPath returns the real absolute path used for cache identity, cycle
// detection and rooted containment. Imports written by a symlinked file are
// consequently relative to the referent's directory in every implementation.
func canonicalPath(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(real)
}

func pathWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func countFileNodes(file *File) int {
	return countNodes(file.Children)
}

func countNodes(nodes []Node) int {
	total := len(nodes)
	for _, node := range nodes {
		switch {
		case node.Field != nil:
			total += countValueNodes(node.Field.Value)
		case node.Block != nil:
			total += countNodes(node.Block.Children)
		case node.BlockArray != nil:
			for _, value := range node.BlockArray.Items {
				total += countValueNodes(value)
			}
		}
	}
	return total
}

func countValueNodes(value Value) int {
	total := 1
	switch value.Type {
	case TypeObject:
		total += countNodes(value.Object)
	case TypeArray:
		if value.Array != nil {
			for _, item := range value.Array.Items {
				total += countValueNodes(item)
			}
		}
	}
	return total
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
