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

/// Append `b`'s comments after `a`'s, avoiding an allocation when either side
/// is empty (which is almost always).
fn concatComments(
    allocator: Allocator,
    a: []const []const u8,
    b: []const []const u8,
) ![]const []const u8 {
    if (b.len == 0) return a;
    if (a.len == 0) return b;
    const out = try allocator.alloc([]const u8, a.len + b.len);
    @memcpy(out[0..a.len], a);
    @memcpy(out[a.len..], b);
    return out;
}

fn nodeKey(node: ast.Node) []const u8 {
    return switch (node) {
        .field => |f| f.key,
        .block => |b| b.name,
        .block_array => |ba| ba.name,
    };
}
