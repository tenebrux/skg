/// Native typed decoding. All returned allocations belong to an owning Result.
/// No external evaluator, runtime bridge, or generated schema is required.
const std = @import("std");
const ast = @import("ast.zig");
const merge = @import("merge.zig");
const Allocator = std.mem.Allocator;

pub const Error = error{ DecodeFailed, OutOfMemory };

/// Recursive native conversion and default-copy limit. Lowering it in 1.x
/// would reject a target accepted by V1.0.
pub const max_nesting_depth: usize = 256;

pub const Code = enum {
    parse_error,
    type_mismatch,
    missing_field,
    unknown_field,
    invalid_enum,
    number_out_of_range,
    inexact_number,
    unsupported_type,
    nesting_too_deep,
    custom_error,
    out_of_memory,
};

pub const Location = struct {
    path: []const u8 = "",
    line: u32 = 0,
    col: u32 = 0,
};

pub const Diagnostic = struct {
    code: Code,
    /// JSON Pointer: empty at the root, /items/2/name for a nested field.
    field_path: []const u8 = "",
    source: Location = .{},
    message: []const u8,
    parse_diagnostic: ?ast.Diagnostic = null,
};

pub const Options = struct {
    unknown_fields: enum { ignore, reject } = .ignore,
    allow_lossy_numbers: bool = false,
};

pub fn Result(comptime T: type) type {
    return struct {
        backing: Allocator,
        arena: ?*std.heap.ArenaAllocator = null,
        value: ?T = null,
        diagnostic: ?Diagnostic = null,

        /// Release the result and every value/container allocated by decoding.
        /// Treat this as a unique owner, like std.heap.ArenaAllocator itself.
        pub fn deinit(self: *@This()) void {
            if (self.arena) |arena| {
                arena.deinit();
                self.backing.destroy(arena);
            }
            self.arena = null;
            self.value = null;
            self.diagnostic = null;
        }
    };
}

/// Takes ownership of a parse result's arena, including on failure.
pub fn fromParsed(comptime T: type, backing: Allocator, parsed_value: anytype, options: Options) Result(T) {
    var parsed = parsed_value;
    const arena = backing.create(std.heap.ArenaAllocator) catch {
        parsed.deinit();
        return .{ .backing = backing, .diagnostic = .{ .code = .out_of_memory, .message = "out of memory" } };
    };
    arena.* = parsed.arena;
    var result = Result(T){ .backing = backing, .arena = arena };
    const file = parsed.file orelse {
        if (parsed.diagnostic) |d| {
            result.diagnostic = .{ .code = .parse_error, .message = d.message, .source = .{ .path = d.path, .line = d.line, .col = d.col }, .parse_diagnostic = d };
        } else {
            result.diagnostic = .{ .code = .out_of_memory, .message = "parsing failed without a diagnostic" };
        }
        return result;
    };
    var ctx = Context{ .allocator = arena.allocator(), .options = options, .source = .{ .path = file.path } };
    const nodes = if (file.imports_resolved) file.children else merge.materializeNodes(ctx.allocator, file.children) catch {
        result.diagnostic = .{ .code = .out_of_memory, .message = "out of memory" };
        return result;
    };
    result.value = ctx.decode(T, .{ .object = .{ .children = nodes } }) catch |err| {
        result.diagnostic = if (err == error.OutOfMemory)
            .{ .code = .out_of_memory, .message = "out of memory", .field_path = ctx.field_path, .source = ctx.source }
        else
            ctx.diagnostic orelse .{
                .code = .custom_error,
                .message = "custom decoder failed",
                .field_path = ctx.field_path,
                .source = ctx.source,
            };
        return result;
    };
    return result;
}

pub const Context = struct {
    allocator: Allocator,
    options: Options = .{},
    diagnostic: ?Diagnostic = null,
    field_path: []const u8 = "",
    source: Location = .{},
    depth: usize = 0,

    /// Custom hooks can report a stable classification and an owned message.
    pub fn fail(self: *Context, code: Code, message: []const u8) Error {
        self.diagnostic = .{ .code = code, .message = self.allocator.dupe(u8, message) catch return error.OutOfMemory, .field_path = self.field_path, .source = self.source };
        return error.DecodeFailed;
    }

    pub fn decode(self: *Context, comptime T: type, value: ast.Value) Error!T {
        if (self.depth >= max_nesting_depth) return self.fail(.nesting_too_deep, "native target nesting exceeds 256");
        self.depth += 1;
        defer self.depth -= 1;
        const result = try self.decodeInner(T, value);
        if (comptime hasHook(T, "skgValidate")) try result.skgValidate(self);
        return result;
    }

    fn decodeInner(self: *Context, comptime T: type, value: ast.Value) Error!T {
        if (comptime hasHook(T, "skgDecode")) return T.skgDecode(self, value);
        // The parser tree is already owned by this result's arena.
        if (T == ast.Value) return value;
        if (comptime mapValueType(T)) |V| {
            if (value != .object) return self.fail(.type_mismatch, "expected an object for a string map");
            var out: T = if (T == std.StringHashMap(V)) T.init(self.allocator) else .empty;
            for (value.object.children) |node| {
                if (node == .delete) continue;
                const key = nodeKey(node);
                const item = try self.decodeChild(V, nodeValue(node), key, nodeLocation(node));
                const owned_key = try self.allocator.dupe(u8, key);
                if (T == std.StringHashMap(V)) try out.put(owned_key, item) else try out.put(self.allocator, owned_key, item);
            }
            return out;
        }
        switch (@typeInfo(T)) {
            .optional => |info| {
                if (value == .null) return null;
                return try self.decode(info.child, value);
            },
            .bool => {
                if (value != .bool) return self.fail(.type_mismatch, "expected a boolean");
                return value.bool;
            },
            .int => {
                if (value != .int) return self.fail(.type_mismatch, "expected an integer");
                return std.math.cast(T, value.int) orelse return self.fail(.number_out_of_range, "integer does not fit the native type");
            },
            .float => {
                if (value != .float and value != .int) return self.fail(.type_mismatch, "expected a number");
                const out: T = if (value == .int) @floatFromInt(value.int) else @floatCast(value.float);
                if (!std.math.isFinite(out)) return self.fail(.number_out_of_range, "number does not fit the native float");
                if (!self.options.allow_lossy_numbers) {
                    // f128 represents every i64 exactly, making this safe at both endpoints.
                    const exact: f128 = if (value == .int) @floatFromInt(value.int) else @floatCast(value.float);
                    if (@as(f128, @floatCast(out)) != exact) return self.fail(.inexact_number, "conversion would lose numeric precision");
                }
                return out;
            },
            .@"enum" => {
                if (value != .string) return self.fail(.type_mismatch, "expected an enum tag string");
                return std.meta.stringToEnum(T, value.string) orelse return self.fail(.invalid_enum, "unknown enum tag");
            },
            .pointer => |info| {
                if (info.is_volatile or info.is_allowzero or info.address_space != .generic)
                    return self.fail(.unsupported_type, "volatile, allowzero and special-address pointers are unsupported");
                if (info.size == .one) {
                    const child = try self.decode(info.child, value);
                    const out = try self.allocator.create(info.child);
                    out.* = child;
                    return out;
                }
                if (info.size != .slice) return self.fail(.unsupported_type, "use a slice or single-item pointer");
                if (info.child == u8 and value == .string) {
                    if (comptime info.sentinel()) |sentinel| {
                        const out = try self.allocator.allocSentinel(u8, value.string.len, sentinel);
                        @memcpy(out, value.string);
                        return out;
                    }
                    return self.allocator.dupe(u8, value.string);
                }
                if (value != .array) return self.fail(.type_mismatch, "expected an array");
                const out = if (comptime info.sentinel()) |sentinel|
                    try self.allocator.allocSentinel(info.child, value.array.items.len, sentinel)
                else
                    try self.allocator.alloc(info.child, value.array.items.len);
                for (value.array.items, 0..) |item, i| {
                    const index = try std.fmt.allocPrint(self.allocator, "{d}", .{i});
                    out[i] = try self.decodeChild(info.child, item, index, self.source);
                }
                return out;
            },
            .array => |info| {
                if (value != .array) return self.fail(.type_mismatch, "expected an array");
                if (value.array.items.len != info.len) return self.fail(.type_mismatch, "array length does not match the native array");
                var out: T = undefined;
                for (value.array.items, 0..) |item, i| {
                    const index = try std.fmt.allocPrint(self.allocator, "{d}", .{i});
                    out[i] = try self.decodeChild(info.child, item, index, self.source);
                }
                if (comptime info.sentinel()) |sentinel| out[info.len] = sentinel;
                return out;
            },
            .@"struct" => |info| {
                if (info.is_tuple) return self.fail(.unsupported_type, "tuple targets need a custom decoder");
                if (value != .object) return self.fail(.type_mismatch, "expected an object");
                try self.checkFieldNames(T);
                if (self.options.unknown_fields == .reject) {
                    for (value.object.children) |node| {
                        if (node == .delete) continue;
                        var known = false;
                        inline for (info.fields) |field| {
                            if (comptime !field.is_comptime) {
                                if (comptime wireName(T, field.name)) |key| {
                                    if (std.mem.eql(u8, key, nodeKey(node))) known = true;
                                }
                            }
                        }
                        if (!known) {
                            const old = self.field_path;
                            const location = self.source;
                            self.field_path = try self.childPath(nodeKey(node));
                            self.source = nodeLocation(node);
                            defer {
                                self.field_path = old;
                                self.source = location;
                            }
                            return self.fail(.unknown_field, "unknown field");
                        }
                    }
                }
                var out: T = undefined;
                inline for (info.fields) |field| {
                    if (comptime !field.is_comptime) {
                        var found: ?ast.Node = null;
                        const wire_key = comptime wireName(T, field.name);
                        if (wire_key) |key| for (value.object.children) |node| {
                            if (node == .delete) continue;
                            if (std.mem.eql(u8, key, nodeKey(node))) {
                                found = node;
                                break;
                            }
                        };
                        if (found) |node| {
                            @field(out, field.name) = try self.decodeChild(field.type, nodeValue(node), wire_key.?, nodeLocation(node));
                        } else if (comptime field.defaultValue()) |default| {
                            @field(out, field.name) = try self.clone(field.type, default);
                        } else if (@typeInfo(field.type) == .optional) {
                            @field(out, field.name) = null;
                        } else {
                            const old = self.field_path;
                            self.field_path = try self.childPath(wire_key orelse field.name);
                            defer self.field_path = old;
                            return self.fail(.missing_field, "required native field is absent");
                        }
                    }
                }
                return out;
            },
            else => return self.fail(.unsupported_type, "unsupported native target type; provide skgDecode"),
        }
    }

    /// Decode a nested value from a custom hook with an unambiguous field path.
    pub fn decodeChild(self: *Context, comptime T: type, value: ast.Value, key: []const u8, location: Location) Error!T {
        const old_path = self.field_path;
        const old_source = self.source;
        self.field_path = try self.childPath(key);
        self.source = location;
        defer {
            self.field_path = old_path;
            self.source = old_source;
        }
        return self.decode(T, value);
    }

    fn childPath(self: *Context, key: []const u8) Error![]const u8 {
        var path: std.ArrayListUnmanaged(u8) = .empty;
        try path.appendSlice(self.allocator, self.field_path);
        try path.append(self.allocator, '/');
        for (key) |c| {
            switch (c) {
                '~' => try path.appendSlice(self.allocator, "~0"),
                '/' => try path.appendSlice(self.allocator, "~1"),
                else => try path.append(self.allocator, c),
            }
        }
        return path.toOwnedSlice(self.allocator);
    }

    fn checkFieldNames(self: *Context, comptime T: type) Error!void {
        if (@hasDecl(T, "skg_fields")) {
            inline for (@typeInfo(@TypeOf(T.skg_fields)).@"struct".fields) |mapping| {
                if (!@hasField(T, mapping.name)) return self.fail(.unsupported_type, "skg_fields names a nonexistent native field");
            }
        }
        const fields = @typeInfo(T).@"struct".fields;
        inline for (fields, 0..) |field, i| {
            if (comptime wireName(T, field.name)) |name| {
                inline for (fields[0..i]) |prior| {
                    if (comptime wireName(T, prior.name)) |other| {
                        if (std.mem.eql(u8, name, other)) return self.fail(.unsupported_type, "two native fields map to the same SKG key");
                    }
                }
            }
        }
    }

    // Copy defaults and dynamic AST values into the result's arena. No returned
    // slice, pointer, or map allocator borrows caller-owned temporary storage.
    fn clone(self: *Context, comptime T: type, value: T) Error!T {
        if (self.depth >= max_nesting_depth) return self.fail(.nesting_too_deep, "native default nesting exceeds 256");
        self.depth += 1;
        defer self.depth -= 1;
        if (comptime mapValueType(T)) |V| {
            var out: T = if (T == std.StringHashMap(V)) T.init(self.allocator) else .empty;
            var it = value.iterator();
            while (it.next()) |entry| {
                const key = try self.allocator.dupe(u8, entry.key_ptr.*);
                const item = try self.clone(V, entry.value_ptr.*);
                if (T == std.StringHashMap(V)) try out.put(key, item) else try out.put(self.allocator, key, item);
            }
            return out;
        }
        switch (@typeInfo(T)) {
            .bool, .int, .float, .@"enum", .void => return value,
            .optional => |info| return if (value) |v| try self.clone(info.child, v) else null,
            .pointer => |info| {
                if (info.size == .one) {
                    const out = try self.allocator.create(info.child);
                    out.* = try self.clone(info.child, value.*);
                    return out;
                }
                if (info.size != .slice) return self.fail(.unsupported_type, "unsupported pointer default");
                const out = if (comptime info.sentinel()) |sentinel| try self.allocator.allocSentinel(info.child, value.len, sentinel) else try self.allocator.alloc(info.child, value.len);
                for (value, 0..) |v, i| out[i] = try self.clone(info.child, v);
                return out;
            },
            .array => |info| {
                var out: T = undefined;
                for (value, 0..) |v, i| out[i] = try self.clone(info.child, v);
                if (comptime info.sentinel()) |sentinel| out[info.len] = sentinel;
                return out;
            },
            .@"struct" => |info| {
                var out: T = undefined;
                inline for (info.fields) |field| {
                    if (!field.is_comptime) @field(out, field.name) = try self.clone(field.type, @field(value, field.name));
                }
                return out;
            },
            .@"union" => |info| {
                if (info.tag_type == null) return self.fail(.unsupported_type, "untagged union default");
                switch (value) {
                    inline else => |v, tag| return @unionInit(T, @tagName(tag), try self.clone(@TypeOf(v), v)),
                }
            },
            else => return self.fail(.unsupported_type, "unsupported native default"),
        }
    }
};

fn hasHook(comptime T: type, comptime name: []const u8) bool {
    return switch (@typeInfo(T)) {
        .@"struct", .@"enum", .@"union", .@"opaque" => @hasDecl(T, name),
        else => false,
    };
}

fn wireName(comptime T: type, comptime field: []const u8) ?[]const u8 {
    if (@hasDecl(T, "skg_fields") and @hasField(@TypeOf(T.skg_fields), field))
        return @as(?[]const u8, @field(T.skg_fields, field));
    return field;
}

fn mapValueType(comptime T: type) ?type {
    if (@typeInfo(T) != .@"struct" or !@hasDecl(T, "KV")) return null;
    if (@typeInfo(T.KV) != .@"struct" or !@hasField(T.KV, "key") or !@hasField(T.KV, "value")) return null;
    const V = @FieldType(T.KV, "value");
    if (T == std.StringHashMap(V) or T == std.StringHashMapUnmanaged(V)) return V;
    return null;
}

fn nodeKey(node: ast.Node) []const u8 {
    return switch (node) {
        .field => |v| v.key,
        .block => |v| v.name,
        .block_array => |v| v.name,
        .delete => |v| v.key,
    };
}
fn nodeValue(node: ast.Node) ast.Value {
    return switch (node) {
        .field => |v| v.value,
        .block => |v| .{ .object = .{ .children = v.children } },
        .block_array => |v| .{ .array = .{ .element_type = .object, .items = v.items } },
        .delete => unreachable, // fromParsed materializes before decoding
    };
}
fn nodeLocation(node: ast.Node) Location {
    return switch (node) {
        inline else => |v| .{ .path = v.path, .line = v.line, .col = v.col },
    };
}
