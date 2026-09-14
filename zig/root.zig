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

/// How many levels of imports the file API follows below the file it was handed.
///
/// Cycle detection catches an import graph that loops through paths it can
/// recognise as the same file, but two spellings it cannot reconcile - a symlink
/// loop, say, or a bind mount - would otherwise recurse until the thread stack
/// is exhausted. This cap is the backstop, and 32 is far past any real
/// configuration layout.
///
/// It must stay equal to go/imports.go's `MaxImportDepth`: a value the two
/// implementations disagree on is itself a conformance divergence.
pub const max_import_depth: usize = 32;

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
    var arena = std.heap.ArenaAllocator.init(backing);
    const alloc = arena.allocator();

    var diag: ?Diagnostic = null;
    var resolver = Resolver{
        .allocator = alloc,
        .visited = .empty,
        .done = .empty,
        .chain = .empty,
        .diagnostic = &diag,
    };

    const canonical = canonicalPath(alloc, path) catch {
        return ParseResult{ .arena = arena, .diagnostic = diag };
    };
    const file = resolver.load(canonical, null) catch {
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

    /// `path` must already be canonical. `origin` is null for the entry file.
    fn load(self: *Resolver, path: []const u8, origin: ?Origin) !Resolved {
        if (self.done.get(path)) |cached| {
            if (self.chain.items.len + cached.depth > max_import_depth) {
                self.fail(origin, path, .IMPORT_CHAIN_TOO_DEEP, try self.formatChain("import chain too deep through cached file: ", path));
                return error.ImportChainTooDeep;
            }
            return cached;
        }
        if (self.visited.contains(path)) {
            self.fail(origin, path, .CIRCULAR_IMPORT, try self.formatChain("circular import: ", path));
            return error.CircularImport;
        }
        if (self.chain.items.len > max_import_depth) {
            self.fail(origin, path, .IMPORT_CHAIN_TOO_DEEP, try self.formatChain("import chain too deep: ", path));
            return error.ImportChainTooDeep;
        }
        try self.visited.put(self.allocator, path, {});
        try self.chain.append(self.allocator, path);
        defer {
            _ = self.visited.remove(path);
            _ = self.chain.pop();
        }

        const f = std.fs.cwd().openFile(path, .{}) catch {
            self.fail(origin, path, .IMPORT_NOT_FOUND, "file not found");
            return error.FileNotFound;
        };
        defer f.close();
        const max_file_size = parser.max_file_size;
        const src = f.readToEndAlloc(self.allocator, max_file_size) catch {
            self.fail(origin, path, .FILE_TOO_LARGE, "file too large (max 10MB)");
            return error.FileTooLarge;
        };

        var result = try parser.parseSource(self.allocator, src, path, self.diagnostic);

        var depth: usize = 0;
        if (result.import_paths.len > 0) {
            const dir = std.fs.path.dirname(path) orelse ".";
            var merged: []ast.Node = &.{};

            for (result.import_paths, result.import_positions) |import_path, pos| {
                // Canonicalising here, not just for the visited-set key, is what
                // makes cycle detection see through a `./`-spelled import: the
                // raw join grew `./a.skg` into `./././a.skg` a level at a time,
                // so the guard never matched and the chain ran to PATH_MAX.
                const child = try canonicalPath(self.allocator, try std.fs.path.join(self.allocator, &.{ dir, import_path }));
                const imported = try self.load(child, .{ .path = path, .pos = pos });
                depth = @max(depth, 1 + imported.depth);
                merged = try merge.mergeNodes(self.allocator, merged, imported.file.children);
            }

            // Main file's children overlay the merged imports
            result.children = try merge.mergeNodes(self.allocator, merged, result.children);
        }

        const resolved = Resolved{ .file = result, .depth = depth };
        try self.done.put(self.allocator, path, resolved);
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

/// Return the key a file is tracked under while resolving, so that `./theme.skg`
/// and `theme.skg` reached from different directories are recognised as the same
/// file.
///
/// Lexical only: symlinks are not resolved, because that costs a syscall per
/// import and `max_import_depth` already bounds any loop this misses. Matches
/// go/imports.go's canonicalPath, which uses filepath.Abs for the same reason.
fn canonicalPath(allocator: Allocator, path: []const u8) ![]const u8 {
    return std.fs.path.resolve(allocator, &.{path});
}

// Native typed integration; field schemas come from Zig types.
pub const native = @import("native.zig");

pub fn decodeSource(comptime T: type, backing: Allocator, src: []const u8, path: []const u8, options: native.Options) native.Result(T) {
    return native.fromParsed(T, backing, parseSource(backing, src, path), options);
}

pub fn decodeFile(comptime T: type, backing: Allocator, path: []const u8, options: native.Options) native.Result(T) {
    return native.fromParsed(T, backing, parse(backing, path), options);
}
