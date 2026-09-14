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

/// Compose overlays, retaining deletion/replacement markers through imports.
/// Call materializeNodes once after the final merge to obtain final data.
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
    defer index.deinit(allocator);

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
                .delete, .field => result.items[pos] = ov_node,
                .block => |ov_block| {
                    // Only recursively merge if existing is also a block
                    if (!ov_block.replace and result.items[pos] == .block) {
                        const existing = result.items[pos].block;
                        // Carry both sides' trivia. Dropping it here meant every
                        // duplicate block lost its comments, and `skg fmt`
                        // rewrites files in place - so the loss was permanent.
                        result.items[pos] = ast.Node{ .block = .{
                            .name = existing.name,
                            .replace = existing.replace,
                            .children = try mergeNodes(allocator, existing.children, ov_block.children),
                            .line = existing.line,
                            .col = existing.col,
                            .leading_comments = try concatComments(allocator, existing.leading_comments, ov_block.leading_comments),
                            .trailing_comments = try concatComments(allocator, existing.trailing_comments, ov_block.trailing_comments),
                        } };
                    } else {
                        var replacement = ov_block;
                        replacement.replace = true;
                        result.items[pos] = .{ .block = replacement };
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
        .delete => |d| d.key,
        .field => |f| f.key,
        .block => |b| b.name,
        .block_array => |ba| ba.name,
    };
}

/// Finish a composed overlay against an empty base without mutating its inputs.
/// Allocate in an arena, like parse/merge: the result shares string/trivia slices.
pub fn materializeNodes(allocator: Allocator, nodes: []const ast.Node) Allocator.Error![]ast.Node {
    var out: std.ArrayListUnmanaged(ast.Node) = .empty;
    for (nodes) |node| {
        const value: ast.Node = switch (node) {
            .delete => continue,
            .field => |f| blk: {
                var copy = f;
                copy.value = try materializeValue(allocator, f.value);
                break :blk .{ .field = copy };
            },
            .block => |b| blk: {
                var copy = b;
                copy.replace = false;
                copy.children = try materializeNodes(allocator, b.children);
                break :blk .{ .block = copy };
            },
            .block_array => |b| blk: {
                var copy = b;
                copy.items = try materializeItems(allocator, b.items);
                break :blk .{ .block_array = copy };
            },
        };
        try out.append(allocator, value);
    }
    return out.toOwnedSlice(allocator);
}

fn materializeItems(allocator: Allocator, items: []const ast.Value) Allocator.Error![]ast.Value {
    const out = try allocator.alloc(ast.Value, items.len);
    for (items, 0..) |v, i| out[i] = try materializeValue(allocator, v);
    return out;
}

fn materializeValue(allocator: Allocator, value: ast.Value) Allocator.Error!ast.Value {
    return switch (value) {
        .object => |o| blk: {
            var copy = o;
            copy.children = try materializeNodes(allocator, o.children);
            break :blk .{ .object = copy };
        },
        .array => |a| blk: {
            var copy = a;
            copy.items = try materializeItems(allocator, a.items);
            break :blk .{ .array = copy };
        },
        else => value,
    };
}
