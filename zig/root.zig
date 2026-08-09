/// SKG public API.
///
/// Usage:
///   var result = try skg.parse(backing_allocator, "/path/to/config.skg");
///   defer result.deinit();
///   const file = result.file;
///   // walk file.children...
///
/// All AST memory lives in an internal arena. `result.deinit()` frees everything at once.
const std = @import("std");
const Allocator = std.mem.Allocator;
const parser = @import("parser.zig");
const merge = @import("merge.zig");

pub const ast = @import("ast.zig");
pub const emit = @import("emit.zig");
pub const File = ast.File;
pub const Node = ast.Node;
pub const Block = ast.Block;
pub const Field = ast.Field;
pub const Value = ast.Value;
pub const ValueType = ast.ValueType;
pub const Array = ast.Array;
pub const BlockArray = ast.BlockArray;

pub const ParseError = parser.ParseError;

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

    var visited = std.StringHashMap(void).init(alloc);
    defer visited.deinit();
    var chain = std.ArrayListUnmanaged([]const u8).empty;

    var diag: ?Diagnostic = null;
    const file = parseWithVisited(alloc, path, &visited, &chain, &diag) catch {
        return ParseResult{ .arena = arena, .diagnostic = diag };
    };
    return ParseResult{ .arena = arena, .file = file };
}

/// Parse SKG source from a string. No import resolution.
/// On parse failure, returns a ParseResult with `file = null` and a diagnostic.
pub fn parseSource(backing: Allocator, src: []const u8, path: []const u8) ParseResult {
    var arena = std.heap.ArenaAllocator.init(backing);
    var diag: ?Diagnostic = null;
    const file = parser.parseSource(arena.allocator(), src, path, &diag) catch {
        return ParseResult{ .arena = arena, .diagnostic = diag };
    };
    return ParseResult{ .arena = arena, .file = file };
}

fn parseWithVisited(
    allocator: Allocator,
    path: []const u8,
    visited: *std.StringHashMap(void),
    chain: *std.ArrayListUnmanaged([]const u8),
    diagnostic: *?Diagnostic,
) !ast.File {
    // Circular import guard
    if (visited.contains(path)) {
        diagnostic.* = .{
            .code = .CIRCULAR_IMPORT,
            .path = path,
            .line = 0,
            .col = 0,
            .message = try formatImportChain(allocator, chain, path),
        };
        return error.CircularImport;
    }
    try visited.put(path, {});
    try chain.append(allocator, path);
    defer {
        _ = visited.remove(path);
        _ = chain.pop();
    }

    // Read source
    const f = std.fs.cwd().openFile(path, .{}) catch {
        diagnostic.* = .{
            .code = .IMPORT_NOT_FOUND,
            .path = path,
            .line = 0,
            .col = 0,
            .message = "file not found",
        };
        return error.FileNotFound;
    };
    defer f.close();
    const max_file_size = 10 * 1024 * 1024;
    const src = f.readToEndAlloc(allocator, max_file_size) catch {
        diagnostic.* = .{
            .code = .FILE_TOO_LARGE,
            .path = path,
            .line = 0,
            .col = 0,
            .message = "file too large (max 10MB)",
        };
        return error.FileTooLarge;
    };

    // Parse this file
    var result = parser.parseSource(allocator, src, path, diagnostic) catch |err| return err;

    // Resolve imports
    if (result.import_paths.len > 0) {
        const dir = std.fs.path.dirname(path) orelse ".";
        var merged: []ast.Node = &.{};

        for (result.import_paths) |import_path| {
            const abs = try std.fs.path.join(allocator, &.{ dir, import_path });
            const imported = try parseWithVisited(allocator, abs, visited, chain, diagnostic);
            merged = try merge.mergeNodes(allocator, merged, imported.children);
        }

        // Main file's children overlay the merged imports
        result.children = try merge.mergeNodes(allocator, merged, result.children);
    }

    return result;
}

fn formatImportChain(
    allocator: Allocator,
    chain: *const std.ArrayListUnmanaged([]const u8),
    target: []const u8,
) ![]const u8 {
    var buf = std.ArrayListUnmanaged(u8).empty;
    try buf.appendSlice(allocator, "circular import: ");
    for (chain.items) |p| {
        try buf.appendSlice(allocator, p);
        try buf.appendSlice(allocator, " -> ");
    }
    try buf.appendSlice(allocator, target);
    return buf.toOwnedSlice(allocator);
}
