/// SKG public API.
///
/// Usage:
///   var result = skg.parse(backing_allocator, "/path/to/config.skg");
///   defer result.deinit();
///   const file = result.file;
///   // walk file.children...
///
/// All AST memory lives in an internal arena. `result.deinit()` frees everything at once.
const std = @import("std");
const builtin = @import("builtin");
const Allocator = std.mem.Allocator;
const parser = @import("parser.zig");
pub const merge = @import("merge.zig");

pub const ast = @import("ast.zig");
pub const emit = @import("emit.zig");
pub const File = ast.File;
pub const Node = ast.Node;
pub const Block = ast.Block;
pub const Field = ast.Field;
pub const Delete = ast.Delete;
pub const Value = ast.Value;
pub const ValueType = ast.ValueType;
pub const Array = ast.Array;
pub const Object = ast.Object;
pub const BlockArray = ast.BlockArray;

pub const ParseError = parser.ParseError;

pub const language_version = "1.0";
pub const supported_major_version = parser.supported_major;
pub const supported_minor_version = parser.supported_minor;
pub const max_nesting_depth = parser.max_nesting_depth;
pub const max_file_size = parser.max_file_size;
pub const max_native_nesting_depth = native.max_nesting_depth;

/// How many levels of imports the file API follows below the file it was handed.
///
/// Real-path cycle detection catches ordinary content and symlink aliases. The
/// depth cap independently bounds recursion and is far past a normal
/// configuration layout.
///
/// It must stay equal to go/imports.go's `MaxImportDepth`: a value the two
/// implementations disagree on is itself a conformance divergence.
pub const max_import_depth: usize = 32;

pub const default_max_resolve_bytes: usize = 64 * 1024 * 1024;
pub const default_max_resolve_files: usize = 1024;
pub const default_max_resolve_nodes: usize = 8_000_000;
pub const default_max_resolve_merge_work: usize = 64_000_000;

/// File graph policy. Field defaults are the frozen V1 defaults. `root` enables
/// opt-in containment after symlink resolution; null retains unrestricted file
/// loading with relative imports.
pub const ResolveOptions = struct {
    root: ?[]const u8 = null,
    max_bytes: usize = default_max_resolve_bytes,
    max_files: usize = default_max_resolve_files,
    max_nodes: usize = default_max_resolve_nodes,
    max_merge_work: usize = default_max_resolve_merge_work,
};

/// A parsed SKG file tree with its owning arena.
/// Call `deinit()` to release all memory.
pub const Diagnostic = ast.Diagnostic;

pub const ParseResult = struct {
    arena: std.heap.ArenaAllocator,
    file: ?ast.File = null,
    diagnostic: ?Diagnostic = null,

    pub fn deinit(self: *ParseResult) void {
        self.arena.deinit();
    }
};

/// Parse an SKG file from disk, resolving imports recursively.
/// Returns a ParseResult whose arena owns all AST memory.
/// On failure, returns ParseResult with `file = null` and a diagnostic if available.
pub fn parse(backing: Allocator, path: []const u8) ParseResult {
    return parseWithOptions(backing, path, .{});
}

/// Parse a file graph under explicit aggregate limits and optional rooted
/// containment. Byte parsing remains filesystem-free in parseSource.
pub fn parseWithOptions(backing: Allocator, path: []const u8, options: ResolveOptions) ParseResult {
    var arena = std.heap.ArenaAllocator.init(backing);
    const alloc = arena.allocator();

    var diag: ?Diagnostic = null;
    const canonical_root: ?[]const u8 = if (options.root) |root_path| canonicalPath(alloc, root_path) catch |err| {
        const message = std.fmt.allocPrint(alloc, "resolution root not found: {s}", .{@errorName(err)}) catch "resolution root not found";
        diag = .{ .code = .IMPORT_NOT_FOUND, .path = alloc.dupe(u8, root_path) catch root_path, .line = 0, .col = 0, .message = message };
        return .{ .arena = arena, .diagnostic = diag };
    } else null;
    if (canonical_root) |root_path| {
        const stat = std.fs.cwd().statFile(root_path) catch {
            diag = .{ .code = .IMPORT_NOT_FOUND, .path = root_path, .line = 0, .col = 0, .message = "resolution root not found" };
            return .{ .arena = arena, .diagnostic = diag };
        };
        if (stat.kind != .directory) {
            diag = .{ .code = .IMPORT_NOT_FOUND, .path = root_path, .line = 0, .col = 0, .message = "resolution root is not a directory" };
            return .{ .arena = arena, .diagnostic = diag };
        }
    }
    var resolver = Resolver{
        .allocator = alloc,
        .options = options,
        .root = canonical_root,
        .remaining_merge_work = options.max_merge_work,
        .visited = .empty,
        .done = .empty,
        .chain = .empty,
        .diagnostic = &diag,
    };

    const file = resolver.load(path, null) catch {
        return ParseResult{ .arena = arena, .diagnostic = diag };
    };
    var result = file.file;
    result.children = merge.materializeNodes(alloc, result.children) catch return .{ .arena = arena };
    result.imports_resolved = true;
    return ParseResult{ .arena = arena, .file = result };
}

/// Parse SKG source into a composed overlay. No import resolution.
/// Operations remain until merge.materializeNodes or file loading.
/// Copies the source and path into the result arena; callers may release them.
/// On parse failure, returns a ParseResult with `file = null` and a diagnostic.
pub fn parseSource(backing: Allocator, src: []const u8, path: []const u8) ParseResult {
    var arena = std.heap.ArenaAllocator.init(backing);
    var diag: ?Diagnostic = null;
    const alloc = arena.allocator();
    const owned_path = alloc.dupe(u8, path) catch return .{ .arena = arena };
    // Even unescaped strings, keys and comments must outlive the caller's input.
    // Oversize input is rejected by the parser before it is read or copied.
    const owned_src = if (src.len > parser.max_file_size) src else alloc.dupe(u8, src) catch return .{ .arena = arena };
    const file = parser.parseSource(alloc, owned_src, owned_path, &diag) catch {
        return ParseResult{ .arena = arena, .diagnostic = diag };
    };
    return ParseResult{ .arena = arena, .file = file };
}

/// Where an import was written: the file that contains it and the position of
/// its path token. A resolution failure is reported there rather than at 0:0,
/// so the diagnostic points at a line the author can actually go and fix.
const Origin = struct {
    path: []const u8,
    pos: ast.Position,
};

/// State of one file-API call. Mirrors go/imports.go's importResolver.
const Resolver = struct {
    allocator: Allocator,
    options: ResolveOptions,
    root: ?[]const u8,
    total_bytes: usize = 0,
    total_files: usize = 0,
    total_nodes: usize = 0,
    remaining_merge_work: usize,
    /// Canonical paths on the chain currently being resolved - not every file
    /// ever loaded. Entries are removed on the way back out, so a diamond (a
    /// imports b and c, both of which import d) is legal and reuses d.
    visited: std.StringHashMapUnmanaged(void),
    /// Files fully resolved during this call, keyed by canonical path.
    ///
    /// Without it, resolution is exponential in the depth of a diamond-shaped
    /// import graph: each level that imports the level below it twice doubles
    /// the work, so 30 levels - still inside `max_import_depth` - never
    /// finishes. That is a denial of service reachable from a config file.
    ///
    /// The cache holds only completed files, and a completed file has already
    /// been popped off the chain, so a hit can never be a file that is still
    /// being resolved: memoising cannot mask a cycle.
    done: std.StringHashMapUnmanaged(Resolved),
    /// The same files, in order, for naming the route that reached one.
    chain: std.ArrayListUnmanaged([]const u8),
    diagnostic: *?Diagnostic,

    const Resolved = struct { file: ast.File, depth: usize };

    /// `origin` is null for the entry file. Every path is canonicalized before
    /// cache, cycle, root and budget checks.
    fn load(self: *Resolver, path: []const u8, origin: ?Origin) !Resolved {
        const canonical = canonicalPath(self.allocator, path) catch |err| {
            if (err == error.OutOfMemory) return err;
            self.fail(origin, path, .IMPORT_NOT_FOUND, "file not found");
            return error.FileNotFound;
        };
        if (self.root) |root_path| {
            if (!try pathWithinRoot(self.allocator, root_path, canonical)) {
                self.fail(origin, canonical, .PATH_OUTSIDE_ROOT, "resolved path is outside root");
                return error.PathOutsideRoot;
            }
        }
        if (self.done.get(canonical)) |cached| {
            if (self.chain.items.len + cached.depth > max_import_depth) {
                self.fail(origin, canonical, .IMPORT_CHAIN_TOO_DEEP, try self.formatChain("import chain too deep through cached file: ", canonical));
                return error.ImportChainTooDeep;
            }
            return cached;
        }
        if (self.visited.contains(canonical)) {
            self.fail(origin, canonical, .CIRCULAR_IMPORT, try self.formatChain("circular import: ", canonical));
            return error.CircularImport;
        }
        if (self.chain.items.len > max_import_depth) {
            self.fail(origin, canonical, .IMPORT_CHAIN_TOO_DEEP, try self.formatChain("import chain too deep: ", canonical));
            return error.ImportChainTooDeep;
        }
        if (self.total_files >= self.options.max_files) {
            self.fail(origin, canonical, .RESOLUTION_FILE_LIMIT, "resolution file limit exceeded");
            return error.ResolutionFileLimit;
        }
        self.total_files += 1;
        try self.visited.put(self.allocator, canonical, {});
        try self.chain.append(self.allocator, canonical);
        defer {
            _ = self.visited.remove(canonical);
            _ = self.chain.pop();
        }

        const f = std.fs.cwd().openFile(canonical, .{}) catch {
            self.fail(origin, canonical, .IMPORT_NOT_FOUND, "file not found");
            return error.FileNotFound;
        };
        defer f.close();
        const source_size_limit = parser.max_file_size;
        const src = f.readToEndAlloc(self.allocator, source_size_limit) catch {
            self.fail(origin, canonical, .FILE_TOO_LARGE, "file too large (max 10MB)");
            return error.FileTooLarge;
        };
        if (src.len > self.options.max_bytes -| self.total_bytes) {
            self.fail(origin, canonical, .RESOLUTION_BYTE_LIMIT, "resolution byte limit exceeded");
            return error.ResolutionByteLimit;
        }
        self.total_bytes += src.len;

        var result = try parser.parseSource(self.allocator, src, canonical, self.diagnostic);
        const parsed_nodes = countNodes(result.children);
        if (parsed_nodes > self.options.max_nodes -| self.total_nodes) {
            self.fail(origin, canonical, .RESOLUTION_NODE_LIMIT, "resolution node limit exceeded");
            return error.ResolutionNodeLimit;
        }
        self.total_nodes += parsed_nodes;

        var depth: usize = 0;
        if (result.import_paths.len > 0) {
            const dir = std.fs.path.dirname(canonical) orelse ".";
            var merged: []ast.Node = &.{};

            for (result.import_paths, result.import_positions) |import_path, pos| {
                // Canonicalising here, not just for the visited-set key, is what
                // makes cycle detection see through a `./`-spelled import: the
                // raw join grew `./a.skg` into `./././a.skg` a level at a time,
                // so the guard never matched and the chain ran to PATH_MAX.
                const child = try std.fs.path.join(self.allocator, &.{ dir, import_path });
                const imported = try self.load(child, .{ .path = canonical, .pos = pos });
                depth = @max(depth, 1 + imported.depth);
                merged = merge.mergeNodesWithBudget(self.allocator, merged, imported.file.children, &self.remaining_merge_work) catch |err| {
                    if (err == error.MergeWorkLimit) {
                        self.fail(.{ .path = canonical, .pos = pos }, canonical, .RESOLUTION_WORK_LIMIT, "resolution merge work limit exceeded");
                        return error.ResolutionWorkLimit;
                    }
                    return err;
                };
            }

            // Main file's children overlay the merged imports
            result.children = merge.mergeNodesWithBudget(self.allocator, merged, result.children, &self.remaining_merge_work) catch |err| {
                if (err == error.MergeWorkLimit) {
                    self.fail(origin, canonical, .RESOLUTION_WORK_LIMIT, "resolution merge work limit exceeded");
                    return error.ResolutionWorkLimit;
                }
                return err;
            };
        }

        const resolved = Resolved{ .file = result, .depth = depth };
        try self.done.put(self.allocator, canonical, resolved);
        return resolved;
    }

    fn fail(self: *Resolver, origin: ?Origin, path: []const u8, code: ast.ErrorCode, message: []const u8) void {
        if (origin) |o| {
            self.diagnostic.* = .{ .code = code, .path = o.path, .line = o.pos.line, .col = o.pos.col, .message = message };
        } else {
            self.diagnostic.* = .{ .code = code, .path = path, .line = 0, .col = 0, .message = message };
        }
    }

    fn formatChain(self: *Resolver, prefix: []const u8, target: []const u8) ![]const u8 {
        var buf = std.ArrayListUnmanaged(u8).empty;
        try buf.appendSlice(self.allocator, prefix);
        for (self.chain.items) |p| {
            try buf.appendSlice(self.allocator, p);
            try buf.appendSlice(self.allocator, " -> ");
        }
        try buf.appendSlice(self.allocator, target);
        return buf.toOwnedSlice(self.allocator);
    }
};

/// Real absolute identity shared by cache, cycle detection and rooted policy.
fn canonicalPath(allocator: Allocator, path: []const u8) ![]const u8 {
    return std.fs.cwd().realpathAlloc(allocator, path) catch |err| {
        // Zig 0.15's Windows realpath can reject an absolute path returned by
        // realpath itself. Resolve the basename relative to an opened parent
        // handle instead. This still follows the final symlink and retains a
        // real filesystem identity for cycle detection and rooted containment.
        if (builtin.os.tag == .windows and std.fs.path.basename(path).len > 0) {
            var parent = try std.fs.cwd().openDir(std.fs.path.dirname(path) orelse ".", .{});
            defer parent.close();
            return parent.realpathAlloc(allocator, std.fs.path.basename(path));
        }
        return err;
    };
}

fn pathWithinRoot(allocator: Allocator, root_path: []const u8, path: []const u8) !bool {
    const rel = try std.fs.path.relative(allocator, root_path, path);
    if (std.fs.path.isAbsolute(rel) or std.mem.eql(u8, rel, "..")) return false;
    return !std.mem.startsWith(u8, rel, "../") and !std.mem.startsWith(u8, rel, "..\\");
}

fn countNodes(nodes: []const ast.Node) usize {
    var total = nodes.len;
    for (nodes) |node| switch (node) {
        .delete => {},
        .field => |field| total += countValueNodes(field.value),
        .block => |block| total += countNodes(block.children),
        .block_array => |array| for (array.items) |value| {
            total += countValueNodes(value);
        },
    };
    return total;
}

fn countValueNodes(value: ast.Value) usize {
    var total: usize = 1;
    switch (value) {
        .object => |object| total += countNodes(object.children),
        .array => |array| for (array.items) |item| {
            total += countValueNodes(item);
        },
        else => {},
    }
    return total;
}

// Native typed integration; field schemas come from Zig types.
pub const native = @import("native.zig");

pub fn decodeSource(comptime T: type, backing: Allocator, src: []const u8, path: []const u8, options: native.Options) native.Result(T) {
    return native.fromParsed(T, backing, parseSource(backing, src, path), options);
}

pub fn decodeFile(comptime T: type, backing: Allocator, path: []const u8, options: native.Options) native.Result(T) {
    return native.fromParsed(T, backing, parse(backing, path), options);
}

pub fn decodeFileWithOptions(comptime T: type, backing: Allocator, path: []const u8, options: native.Options, resolve_options: ResolveOptions) native.Result(T) {
    return native.fromParsed(T, backing, parseWithOptions(backing, path, resolve_options), options);
}
