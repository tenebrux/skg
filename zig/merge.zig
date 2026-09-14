/// SKG merge - overlays one node list on top of another.
///
/// Used by root.zig to apply import files before the main file.
/// Load order rule: later values overwrite earlier values.
/// Block children are merged recursively by name.
///
/// All allocations go into `allocator`. With an arena, this is free to discard.
const std = @import("std");
const Allocator = std.mem.Allocator;
const ast = @import("ast.zig");

/// Merge `overlay` nodes on top of `base`, returning a new owned slice.
///
/// - Fields with the same key: overlay wins.
/// - Blocks with the same name: children merged recursively, overlay children win.
/// - New keys/blocks from overlay are appended.
///
/// Neither `base` nor `overlay` is modified. The result may reference elements
/// from both - only free via the allocator/arena that owns this memory.
pub fn mergeNodes(allocator: Allocator, base: []const ast.Node, overlay: []const ast.Node) ![]ast.Node {
    var result: std.ArrayListUnmanaged(ast.Node) = .empty;
    var index = std.StringHashMapUnmanaged(usize){};

    for (base) |node| {
        const key = nodeKey(node);
        const pos = result.items.len;
        try result.append(allocator, node);
        try index.put(allocator, key, pos);
    }

    for (overlay) |ov_node| {
        const key = nodeKey(ov_node);
        if (index.get(key)) |pos| {
            switch (ov_node) {
                .field => result.items[pos] = ov_node,
                .block => |ov_block| {
                    // Only recursively merge if existing is also a block
                    if (result.items[pos] == .block) {
                        const existing = result.items[pos].block;
                        // Carry both sides' trivia. Dropping it here meant every
                        // duplicate block lost its comments, and `skg fmt`
                        // rewrites files in place - so the loss was permanent.
                        result.items[pos] = ast.Node{ .block = .{
                            .name = existing.name,
                            .children = try mergeNodes(allocator, existing.children, ov_block.children),
                            .line = existing.line,
                            .col = existing.col,
                            .leading_comments = try concatComments(allocator, existing.leading_comments, ov_block.leading_comments),
                            .trailing_comments = try concatComments(allocator, existing.trailing_comments, ov_block.trailing_comments),
                        } };
                    } else {
                        result.items[pos] = ov_node;
                    }
                },
                .block_array => result.items[pos] = ov_node,
            }
        } else {
            const pos = result.items.len;
            try result.append(allocator, ov_node);
            try index.put(allocator, key, pos);
        }
    }

    return result.toOwnedSlice(allocator);
}

/// Preserve each source comment once when cached imports meet again. Compare
/// source identity, not text: identical comments at different source locations
/// must both survive. Parser comment slices remain live in the parse arena.
fn concatComments(
    allocator: Allocator,
    a: []const []const u8,
    b: []const []const u8,
) ![]const []const u8 {
    if (b.len == 0) return a;
    if (a.len == 0) return b;
    if (a.ptr == b.ptr and a.len == b.len) return a;
    const Identity = struct { ptr: usize, len: usize };
    var seen: std.AutoHashMapUnmanaged(Identity, void) = .empty;
    defer seen.deinit(allocator);
    var out: std.ArrayListUnmanaged([]const u8) = .empty;
    for ([_][]const []const u8{ a, b }) |comments| {
        for (comments) |comment| {
            const entry = try seen.getOrPut(allocator, .{ .ptr = @intFromPtr(comment.ptr), .len = comment.len });
            if (!entry.found_existing) try out.append(allocator, comment);
        }
    }
    return out.toOwnedSlice(allocator);
}

fn nodeKey(node: ast.Node) []const u8 {
    return switch (node) {
        .field => |f| f.key,
        .block => |b| b.name,
        .block_array => |ba| ba.name,
    };
}
