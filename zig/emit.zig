/// SKG emit - serializes an AST back to canonical SKG text.
///
/// Produces valid SKG that round-trips through the parser.
/// Comments attached to AST nodes are preserved.
const std = @import("std");
const ast = @import("ast.zig");

pub const EmitError = error{OutOfMemory};

pub fn emitFile(allocator: std.mem.Allocator, file: ast.File) EmitError![]u8 {
    var buf = std.ArrayListUnmanaged(u8).empty;
    errdefer buf.deinit(allocator);
    const w = buf.writer(allocator);

    // File-level leading comments
    try emitCommentLines(w, file.leading_comments, 0);

    if (file.skg_version) |v| {
        try w.print("skg_version: \"{s}\"\n", .{v});
    }
    if (file.schema_version) |v| {
        try w.print("schema_version: \"{s}\"\n", .{v});
    }

    if (file.import_paths.len > 0) {
        if (file.import_paths.len == 1) {
            try w.print("import \"{s}\"\n", .{file.import_paths[0]});
        } else {
            try w.writeAll("import [\n");
            for (file.import_paths, 0..) |p, i| {
                try w.print("  \"{s}\"", .{p});
                if (i + 1 < file.import_paths.len) try w.writeByte(',');
                try w.writeByte('\n');
            }
            try w.writeAll("]\n");
        }
    }

    if ((file.skg_version != null or file.schema_version != null or file.import_paths.len > 0) and file.children.len > 0) {
        try w.writeByte('\n');
    }

    try emitNodes(w, file.children, 0);

    // File-level trailing comments
    try emitCommentLines(w, file.trailing_comments, 0);

    return buf.toOwnedSlice(allocator);
}

fn emitNodes(w: anytype, nodes: []const ast.Node, depth: usize) !void {
    for (nodes, 0..) |node, i| {
        switch (node) {
            .field => |f| {
                try emitCommentLines(w, f.leading_comments, depth);
                try writeIndent(w, depth);
                try w.print("{s}: ", .{f.key});
                try emitValue(w, f.value, depth);
                if (f.trailing_comment) |tc| {
                    try w.print(" {s}", .{tc});
                }
                try w.writeByte('\n');
            },
            .block => |b| {
                if (i > 0 and depth == 0) try w.writeByte('\n');
                try emitCommentLines(w, b.leading_comments, depth);
                try writeIndent(w, depth);
                try w.print("{s} {{\n", .{b.name});
                try emitNodes(w, b.children, depth + 1);
                try emitCommentLines(w, b.trailing_comments, depth + 1);
                try writeIndent(w, depth);
                try w.writeAll("}\n");
            },
            .block_array => |ba| {
                if (i > 0 and depth == 0) try w.writeByte('\n');
                try emitCommentLines(w, ba.leading_comments, depth);
                try writeIndent(w, depth);
                try w.print("{s} [\n", .{ba.name});
                for (ba.items) |item| {
                    try writeIndent(w, depth + 1);
                    try w.writeAll("{\n");
                    try emitNodes(w, item, depth + 2);
                    try writeIndent(w, depth + 1);
                    try w.writeAll("}\n");
                }
                try emitCommentLines(w, ba.trailing_comments, depth + 1);
                try writeIndent(w, depth);
                try w.writeAll("]\n");
            },
        }
    }
}

fn emitValue(w: anytype, value: ast.Value, depth: usize) !void {
    switch (value) {
        .int => |v| try w.print("{d}", .{v}),
        .float => |v| {
            // Render once, then append ".0" when the decimal point is missing
            // so the value re-parses as a float rather than an int.
            //
            // The buffer must fit the widest `{d}` output for a finite f64:
            // ~309 digits for the largest magnitude, and a few hundred more
            // for denormals, which print as 0.000...  A 32-byte buffer used to
            // sit here behind `catch unreachable`, so any value past roughly
            // 1e32 panicked the process instead of emitting.
            var fmtbuf: [1024]u8 = undefined;
            if (std.fmt.bufPrint(&fmtbuf, "{d}", .{v})) |s| {
                try w.writeAll(s);
                if (std.mem.indexOfScalar(u8, s, '.') == null) {
                    try w.writeAll(".0");
                }
            } else |_| {
                // Not reachable for finite f64 with the buffer above; degrade
                // to a plain write rather than crashing if it ever is.
                try w.print("{d}", .{v});
            }
        },
        .bool => |v| try w.writeAll(if (v) "true" else "false"),
        .string => |s| {
            if (std.mem.indexOfScalar(u8, s, '\n') != null and canEmitMultiline(s)) {
                try w.writeAll("\"\"\"");
                try w.writeAll(s);
                try w.writeAll("\"\"\"");
            } else {
                try w.writeByte('"');
                try writeEscaped(w, s);
                try w.writeByte('"');
            }
        },
        .null => try w.writeAll("null"),
        .array => |arr| {
            try w.writeByte('[');
            for (arr.items, 0..) |item, i| {
                if (i > 0) try w.writeAll(", ");
                try emitValue(w, item, depth);
            }
            try w.writeByte(']');
        },
    }
}

/// Emit comment lines at the given indentation depth.
/// Each comment string includes the '#' prefix from the lexer.
fn emitCommentLines(w: anytype, comments: []const []const u8, depth: usize) !void {
    for (comments) |c| {
        try writeIndent(w, depth);
        try w.writeAll(c);
        try w.writeByte('\n');
    }
}

/// Reports whether `s` survives a `"""..."""` round-trip.
///
/// Multiline literals are taken verbatim with no escape processing, so an
/// embedded `"""` - or a trailing `"` that merges with the closing delimiter -
/// would end the literal early and the output would not re-parse. Such values
/// are emitted as an escaped double-quoted string instead. go/emit.go applies
/// the same rule; testdata/valid/emit-multiline-fallback pins both.
fn canEmitMultiline(s: []const u8) bool {
    if (std.mem.indexOf(u8, s, "\"\"\"") != null) return false;
    return !std.mem.endsWith(u8, s, "\"");
}

fn writeEscaped(w: anytype, s: []const u8) !void {
    for (s) |c| {
        switch (c) {
            '"' => try w.writeAll("\\\""),
            '\\' => try w.writeAll("\\\\"),
            '\n' => try w.writeAll("\\n"),
            '\t' => try w.writeAll("\\t"),
            else => try w.writeByte(c),
        }
    }
}

fn writeIndent(w: anytype, depth: usize) !void {
    for (0..depth) |_| {
        try w.writeAll("  ");
    }
}
