/// SKG conformance tests - validates the Zig parser against the shared
/// testdata/ fixtures.
///
/// Everything asserted here comes from testdata/, so the Zig and Go parsers are
/// held to one description of correct behaviour. The rules are specified in
/// docs/conformance.md; a third implementation should be portable from that
/// document alone.
///
/// Three properties make it hard to conform by accident:
///
///   1. Fixtures are enumerated from disk. A new fixture runs without being
///      registered anywhere, so it cannot be added on one side only.
///   2. expected.json is strictly validated. An unknown or misspelled key is a
///      hard failure, never a silently skipped assertion.
///   3. Capabilities are declared in zig/conformance.json. Declaring one
///      obliges this implementation to pass every fixture that needs it; not
///      declaring one skips those fixtures and reports the count.
const std = @import("std");
const testing = std.testing;
const skg_root = @import("root.zig");
const ast = @import("ast.zig");

// ─── Capabilities ───────────────────────────────────────────────────────────

/// The closed set of capability names. A manifest naming anything else fails
/// the run.
const Capability = enum { parse, emit, imports, comments };

const Capabilities = struct {
    parse: bool,
    emit: bool,
    imports: bool,
    comments: bool,
};

fn loadCapabilities(alloc: std.mem.Allocator) !Capabilities {
    const data = try readFile(alloc, "zig/conformance.json");
    const parsed = std.json.parseFromSlice(std.json.Value, alloc, data, .{}) catch |err| {
        std.debug.print("zig/conformance.json is not valid JSON: {}\n", .{err});
        return error.BadManifest;
    };
    // Not deinited on purpose: borrowed strings stay valid until the caller's
    // arena is released, which frees this too.

    const root_obj = expectJsonObject(parsed.value) orelse {
        std.debug.print("zig/conformance.json: top level must be an object\n", .{});
        return error.BadManifest;
    };

    const impl_json = root_obj.get("implementation") orelse {
        std.debug.print("zig/conformance.json: \"implementation\" is required\n", .{});
        return error.BadManifest;
    };
    const impl = expectJsonString(impl_json) orelse return error.BadManifest;
    if (impl.len == 0) {
        std.debug.print("zig/conformance.json: \"implementation\" must not be empty\n", .{});
        return error.BadManifest;
    }

    const caps_json = root_obj.get("capabilities") orelse {
        std.debug.print("zig/conformance.json: \"capabilities\" is required\n", .{});
        return error.BadManifest;
    };
    const caps_obj = expectJsonObject(caps_json) orelse return error.BadManifest;

    var it = caps_obj.iterator();
    while (it.next()) |kv| {
        if (std.meta.stringToEnum(Capability, kv.key_ptr.*) == null) {
            std.debug.print("zig/conformance.json: unknown capability \"{s}\"\n", .{kv.key_ptr.*});
            return error.UnknownCapability;
        }
    }

    var caps: Capabilities = undefined;
    inline for (std.meta.fields(Capability)) |f| {
        const v = caps_obj.get(f.name) orelse {
            std.debug.print("zig/conformance.json: capability \"{s}\" must be declared true or false\n", .{f.name});
            return error.MissingCapability;
        };
        @field(caps, f.name) = expectJsonBool(v) orelse {
            std.debug.print("zig/conformance.json: capability \"{s}\" must be a boolean\n", .{f.name});
            return error.BadManifest;
        };
    }

    if (!caps.parse) {
        std.debug.print("zig/conformance.json: the \"parse\" capability is mandatory\n", .{});
        return error.BadManifest;
    }

    // An undeclared capability is allowed, but it has to be a decision someone
    // wrote down. That is what keeps partial conformance honest rather than
    // quiet.
    const notes_obj: ?std.json.ObjectMap = if (root_obj.get("notes")) |n| expectJsonObject(n) else null;
    inline for (std.meta.fields(Capability)) |f| {
        if (!@field(caps, f.name)) {
            const note: []const u8 = blk: {
                const n = notes_obj orelse break :blk "";
                const v = n.get(f.name) orelse break :blk "";
                break :blk expectJsonString(v) orelse "";
            };
            if (note.len == 0) {
                std.debug.print(
                    "zig/conformance.json: capability \"{s}\" is not declared and has no entry in \"notes\" explaining why\n",
                    .{f.name},
                );
                return error.UndeclaredCapabilityWithoutNote;
            }
        }
    }

    return caps;
}

fn hasCapability(caps: Capabilities, cap: Capability) bool {
    return switch (cap) {
        .parse => caps.parse,
        .emit => caps.emit,
        .imports => caps.imports,
        .comments => caps.comments,
    };
}

// ─── Error-code registry ────────────────────────────────────────────────────

fn loadErrorCodes(alloc: std.mem.Allocator) !std.StringHashMap(void) {
    const data = try readFile(alloc, "testdata/error-codes.json");
    const parsed = std.json.parseFromSlice(std.json.Value, alloc, data, .{}) catch |err| {
        std.debug.print("testdata/error-codes.json is not valid JSON: {}\n", .{err});
        return error.BadErrorCodeRegistry;
    };
    // Not deinited on purpose: the returned set borrows the code strings.

    const root_obj = expectJsonObject(parsed.value) orelse return error.BadErrorCodeRegistry;
    const codes_json = root_obj.get("codes") orelse return error.BadErrorCodeRegistry;
    const entries = expectJsonArray(codes_json) orelse return error.BadErrorCodeRegistry;

    var set = std.StringHashMap(void).init(alloc);
    for (entries) |entry| {
        const obj = expectJsonObject(entry) orelse return error.BadErrorCodeRegistry;
        const code_json = obj.get("code") orelse return error.BadErrorCodeRegistry;
        const code = expectJsonString(code_json) orelse return error.BadErrorCodeRegistry;
        const summary_json = obj.get("summary") orelse return error.BadErrorCodeRegistry;
        const summary = expectJsonString(summary_json) orelse return error.BadErrorCodeRegistry;
        if (code.len == 0 or summary.len == 0) return error.BadErrorCodeRegistry;
        if (set.contains(code)) {
            std.debug.print("testdata/error-codes.json: duplicate code \"{s}\"\n", .{code});
            return error.BadErrorCodeRegistry;
        }
        try set.put(code, {});
    }
    if (set.count() == 0) return error.BadErrorCodeRegistry;
    return set;
}

// Fails if this parser can emit a code the shared registry does not list.
// Without it, a new diagnostic could be added in Zig and asserted in a
// Zig-only fixture that no other implementation can satisfy.
test "conformance: every parser error code is in the shared registry" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const alloc = arena.allocator();

    const codes = try loadErrorCodes(alloc);
    inline for (std.meta.fields(ast.ErrorCode)) |f| {
        if (!codes.contains(f.name)) {
            std.debug.print("ErrorCode .{s} is missing from testdata/error-codes.json\n", .{f.name});
            return error.ErrorCodeNotInRegistry;
        }
    }
}

// ─── Fixture discovery ──────────────────────────────────────────────────────

/// One conformance case: either a flat `<name>.skg` parsed from bytes, or a
/// `<name>/` directory whose `main.skg` is loaded through the import-resolving
/// file API.
const Fixture = struct {
    name: []const u8,
    is_dir: bool,
    entry_path: []const u8,
    json_path: []const u8,
    formatted_path: ?[]const u8,
};

fn fileExists(path: []const u8) bool {
    std.fs.cwd().access(path, .{}) catch return false;
    return true;
}

fn readFile(alloc: std.mem.Allocator, path: []const u8) ![]u8 {
    const file = std.fs.cwd().openFile(path, .{}) catch |err| {
        std.debug.print("cannot open {s}: {}\n", .{ path, err });
        return err;
    };
    defer file.close();
    return file.readToEndAlloc(alloc, 16 * 1024 * 1024);
}

/// Enumerate testdata/<subdir>. Nothing is hardcoded: dropping a fixture into
/// the tree is all it takes to make both implementations run it.
fn discoverFixtures(alloc: std.mem.Allocator, subdir: []const u8) ![]Fixture {
    const dir_path = try std.fmt.allocPrint(alloc, "testdata/{s}", .{subdir});
    var dir = std.fs.cwd().openDir(dir_path, .{ .iterate = true }) catch |err| {
        std.debug.print("cannot open {s}: {}\n", .{ dir_path, err });
        return err;
    };
    defer dir.close();

    var list: std.ArrayListUnmanaged(Fixture) = .empty;
    var it = dir.iterate();
    while (try it.next()) |entry| {
        // `entry.name` is only valid until the next iteration step, so dupe it
        // before building any path from it.
        if (entry.kind == .directory) {
            const name = try alloc.dupe(u8, entry.name);
            var f = Fixture{
                .name = name,
                .is_dir = true,
                .entry_path = try std.fmt.allocPrint(alloc, "{s}/{s}/main.skg", .{ dir_path, name }),
                .json_path = try std.fmt.allocPrint(alloc, "{s}/{s}/expected.json", .{ dir_path, name }),
                .formatted_path = null,
            };
            if (!fileExists(f.entry_path)) {
                std.debug.print("{s}/{s}: directory fixture has no main.skg\n", .{ subdir, name });
                return error.DirectoryFixtureWithoutMain;
            }
            if (!fileExists(f.json_path)) {
                std.debug.print("{s}/{s}: directory fixture has no expected.json\n", .{ subdir, name });
                return error.FixtureWithoutExpectedJson;
            }
            const formatted = try std.fmt.allocPrint(alloc, "{s}/{s}/formatted.skg", .{ dir_path, name });
            if (fileExists(formatted)) f.formatted_path = formatted;
            try list.append(alloc, f);
            continue;
        }

        // Sidecars are validated together with the fixture that owns them.
        if (std.mem.endsWith(u8, entry.name, ".formatted.skg")) continue;
        if (std.mem.endsWith(u8, entry.name, ".expected.json")) continue;
        if (!std.mem.endsWith(u8, entry.name, ".skg")) {
            std.debug.print(
                "{s}/{s}: unrecognised file in the fixture tree (expected .skg, .expected.json or .formatted.skg)\n",
                .{ subdir, entry.name },
            );
            return error.UnrecognisedFixtureFile;
        }

        const base = try alloc.dupe(u8, entry.name[0 .. entry.name.len - ".skg".len]);
        var f = Fixture{
            .name = base,
            .is_dir = false,
            .entry_path = try std.fmt.allocPrint(alloc, "{s}/{s}.skg", .{ dir_path, base }),
            .json_path = try std.fmt.allocPrint(alloc, "{s}/{s}.expected.json", .{ dir_path, base }),
            .formatted_path = null,
        };
        if (!fileExists(f.json_path)) {
            std.debug.print("{s}/{s}.skg: fixture has no {s}.expected.json\n", .{ subdir, base, base });
            return error.FixtureWithoutExpectedJson;
        }
        const formatted = try std.fmt.allocPrint(alloc, "{s}/{s}.formatted.skg", .{ dir_path, base });
        if (fileExists(formatted)) f.formatted_path = formatted;
        try list.append(alloc, f);
    }

    if (list.items.len == 0) {
        std.debug.print("no fixtures found in {s} - the suite would pass vacuously\n", .{dir_path});
        return error.NoFixturesFound;
    }
    return list.toOwnedSlice(alloc);
}

// ─── JSON helpers ───────────────────────────────────────────────────────────

fn expectJsonString(json: std.json.Value) ?[]const u8 {
    return switch (json) {
        .string => |s| s,
        else => null,
    };
}

fn expectJsonBool(json: std.json.Value) ?bool {
    return switch (json) {
        .bool => |b| b,
        else => null,
    };
}

fn expectJsonInt(json: std.json.Value) ?i64 {
    return switch (json) {
        .integer => |n| n,
        else => null,
    };
}

fn expectJsonFloat(json: std.json.Value) ?f64 {
    return switch (json) {
        .float => |f| f,
        .integer => |n| @as(f64, @floatFromInt(n)),
        else => null,
    };
}

fn expectJsonArray(json: std.json.Value) ?[]std.json.Value {
    return switch (json) {
        .array => |a| a.items,
        else => null,
    };
}

fn expectJsonObject(json: std.json.Value) ?std.json.ObjectMap {
    return switch (json) {
        .object => |o| o,
        else => null,
    };
}

// ─── Strict expected.json validation ────────────────────────────────────────
//
// A misspelled key such as "cod" used to mean the assertion silently did not
// happen. Validation walks the decoded JSON against a per-object allowlist and
// rejects anything unknown, so a typo is a hard failure instead.

/// What validation learns about a fixture beyond "it is legal": which optional
/// assertions it carries, and therefore which capabilities are needed to run it.
const SchemaReport = struct {
    asserts_comments: bool = false,
};

const root_valid_keys = [_][]const u8{ "skg_version", "schema_version", "imports", "children", "leading_comments", "trailing_comments" };
const root_invalid_keys = [_][]const u8{ "error", "code", "line", "col" };
const field_node_keys = [_][]const u8{ "type", "key", "value", "leading_comments", "trailing_comment" };
const block_node_keys = [_][]const u8{ "type", "name", "children", "leading_comments", "trailing_comments" };
const block_array_node_keys = [_][]const u8{ "type", "name", "items", "leading_comments", "trailing_comments" };
const scalar_value_keys = [_][]const u8{ "type", "data" };
const array_value_keys = [_][]const u8{ "type", "data", "element_type" };
const value_type_names = [_][]const u8{ "string", "int", "float", "bool", "null", "array" };

fn containsString(haystack: []const []const u8, needle: []const u8) bool {
    for (haystack) |s| {
        if (std.mem.eql(u8, s, needle)) return true;
    }
    return false;
}

fn checkKeys(ctx: []const u8, obj: std.json.ObjectMap, allowed: []const []const u8) !void {
    var it = obj.iterator();
    while (it.next()) |kv| {
        if (!containsString(allowed, kv.key_ptr.*)) {
            std.debug.print("{s}: unknown key \"{s}\" in expected.json\n", .{ ctx, kv.key_ptr.* });
            return error.UnknownExpectedKey;
        }
    }
}

fn checkStringArray(ctx: []const u8, json: std.json.Value) !void {
    const arr = expectJsonArray(json) orelse {
        std.debug.print("{s}: expected an array of strings\n", .{ctx});
        return error.BadExpectedJson;
    };
    for (arr) |item| {
        if (expectJsonString(item) == null) {
            std.debug.print("{s}: expected an array of strings\n", .{ctx});
            return error.BadExpectedJson;
        }
    }
}

fn checkStringOrNull(ctx: []const u8, json: std.json.Value) !void {
    switch (json) {
        .string, .null => {},
        else => {
            std.debug.print("{s}: expected a string or null\n", .{ctx});
            return error.BadExpectedJson;
        },
    }
}

fn checkPositiveInt(ctx: []const u8, json: std.json.Value) !void {
    const n = expectJsonInt(json) orelse {
        std.debug.print("{s}: expected a positive integer\n", .{ctx});
        return error.BadExpectedJson;
    };
    if (n < 1) {
        std.debug.print("{s}: expected a positive integer\n", .{ctx});
        return error.BadExpectedJson;
    }
}

fn validateValidExpected(ctx: []const u8, root_obj: std.json.ObjectMap, rep: *SchemaReport) !void {
    try checkKeys(ctx, root_obj, &root_valid_keys);

    if (root_obj.get("skg_version")) |v| {
        try checkStringOrNull(ctx, v);
    }
    if (root_obj.get("schema_version")) |v| {
        try checkStringOrNull(ctx, v);
    }
    if (root_obj.get("imports")) |v| {
        try checkStringArray(ctx, v);
    }
    if (root_obj.get("leading_comments")) |v| {
        rep.asserts_comments = true;
        try checkStringArray(ctx, v);
    }
    if (root_obj.get("trailing_comments")) |v| {
        rep.asserts_comments = true;
        try checkStringArray(ctx, v);
    }
    if (root_obj.get("children")) |v| {
        try validateNodes(ctx, v, rep);
    }
}

fn validateNodes(ctx: []const u8, json: std.json.Value, rep: *SchemaReport) !void {
    const arr = expectJsonArray(json) orelse {
        std.debug.print("{s}: children/items must be an array of nodes\n", .{ctx});
        return error.BadExpectedJson;
    };
    for (arr) |item| {
        const obj = expectJsonObject(item) orelse {
            std.debug.print("{s}: every node must be an object\n", .{ctx});
            return error.BadExpectedJson;
        };
        const type_json = obj.get("type") orelse {
            std.debug.print("{s}: every node needs a \"type\"\n", .{ctx});
            return error.BadExpectedJson;
        };
        const node_type = expectJsonString(type_json) orelse {
            std.debug.print("{s}: node \"type\" must be a string\n", .{ctx});
            return error.BadExpectedJson;
        };

        if (std.mem.eql(u8, node_type, "field")) {
            try checkKeys(ctx, obj, &field_node_keys);
            const key_json = obj.get("key") orelse {
                std.debug.print("{s}: a field node requires a string \"key\"\n", .{ctx});
                return error.BadExpectedJson;
            };
            if (expectJsonString(key_json) == null) {
                std.debug.print("{s}: a field node requires a string \"key\"\n", .{ctx});
                return error.BadExpectedJson;
            }
            if (obj.get("value")) |v| {
                try validateValue(ctx, v);
            }
            if (obj.get("leading_comments")) |v| {
                rep.asserts_comments = true;
                try checkStringArray(ctx, v);
            }
            if (obj.get("trailing_comment")) |v| {
                rep.asserts_comments = true;
                try checkStringOrNull(ctx, v);
            }
        } else if (std.mem.eql(u8, node_type, "block") or std.mem.eql(u8, node_type, "block_array")) {
            const is_block = std.mem.eql(u8, node_type, "block");
            const allowed: []const []const u8 = if (is_block) &block_node_keys else &block_array_node_keys;
            try checkKeys(ctx, obj, allowed);
            const name_json = obj.get("name") orelse {
                std.debug.print("{s}: a {s} node requires a string \"name\"\n", .{ ctx, node_type });
                return error.BadExpectedJson;
            };
            if (expectJsonString(name_json) == null) {
                std.debug.print("{s}: a {s} node requires a string \"name\"\n", .{ ctx, node_type });
                return error.BadExpectedJson;
            }
            if (is_block) {
                if (obj.get("children")) |v| {
                    try validateNodes(ctx, v, rep);
                }
            } else if (obj.get("items")) |v| {
                const items = expectJsonArray(v) orelse {
                    std.debug.print("{s}: block_array \"items\" must be an array of node arrays\n", .{ctx});
                    return error.BadExpectedJson;
                };
                for (items) |entry| {
                    try validateNodes(ctx, entry, rep);
                }
            }
            if (obj.get("leading_comments")) |v| {
                rep.asserts_comments = true;
                try checkStringArray(ctx, v);
            }
            if (obj.get("trailing_comments")) |v| {
                rep.asserts_comments = true;
                try checkStringArray(ctx, v);
            }
        } else {
            std.debug.print("{s}: \"{s}\" is not one of field, block, block_array\n", .{ ctx, node_type });
            return error.BadExpectedJson;
        }
    }
}

fn validateValue(ctx: []const u8, json: std.json.Value) !void {
    const obj = expectJsonObject(json) orelse {
        std.debug.print("{s}: a value must be an object\n", .{ctx});
        return error.BadExpectedJson;
    };
    const type_json = obj.get("type") orelse {
        std.debug.print("{s}: a value requires a \"type\"\n", .{ctx});
        return error.BadExpectedJson;
    };
    const value_type = expectJsonString(type_json) orelse {
        std.debug.print("{s}: value \"type\" must be a string\n", .{ctx});
        return error.BadExpectedJson;
    };
    if (!containsString(&value_type_names, value_type)) {
        std.debug.print("{s}: \"{s}\" is not a known value type\n", .{ ctx, value_type });
        return error.BadExpectedJson;
    }

    const is_array = std.mem.eql(u8, value_type, "array");
    const allowed: []const []const u8 = if (is_array) &array_value_keys else &scalar_value_keys;
    try checkKeys(ctx, obj, allowed);

    const maybe_data = obj.get("data");
    if (std.mem.eql(u8, value_type, "null")) {
        if (maybe_data != null) {
            std.debug.print("{s}: a null value carries no \"data\"\n", .{ctx});
            return error.BadExpectedJson;
        }
        return;
    }
    const data = maybe_data orelse {
        std.debug.print("{s}: \"data\" is required for a {s} value\n", .{ ctx, value_type });
        return error.BadExpectedJson;
    };

    if (std.mem.eql(u8, value_type, "string")) {
        if (expectJsonString(data) == null) {
            std.debug.print("{s}: string \"data\" must be a string\n", .{ctx});
            return error.BadExpectedJson;
        }
    } else if (std.mem.eql(u8, value_type, "int") or std.mem.eql(u8, value_type, "float")) {
        if (expectJsonFloat(data) == null) {
            std.debug.print("{s}: {s} \"data\" must be a number\n", .{ ctx, value_type });
            return error.BadExpectedJson;
        }
    } else if (std.mem.eql(u8, value_type, "bool")) {
        if (expectJsonBool(data) == null) {
            std.debug.print("{s}: bool \"data\" must be a boolean\n", .{ctx});
            return error.BadExpectedJson;
        }
    } else if (is_array) {
        const et_json = obj.get("element_type") orelse {
            std.debug.print("{s}: \"element_type\" is required for an array value\n", .{ctx});
            return error.BadExpectedJson;
        };
        const et = expectJsonString(et_json) orelse {
            std.debug.print("{s}: \"element_type\" must be a string\n", .{ctx});
            return error.BadExpectedJson;
        };
        if (!containsString(&value_type_names, et)) {
            std.debug.print("{s}: \"{s}\" is not a known element type\n", .{ ctx, et });
            return error.BadExpectedJson;
        }
        const items = expectJsonArray(data) orelse {
            std.debug.print("{s}: array \"data\" must be an array of value objects\n", .{ctx});
            return error.BadExpectedJson;
        };
        for (items) |item| {
            try validateValue(ctx, item);
        }
    }
}

fn validateInvalidExpected(ctx: []const u8, root_obj: std.json.ObjectMap, codes: std.StringHashMap(void)) !void {
    try checkKeys(ctx, root_obj, &root_invalid_keys);

    const error_json = root_obj.get("error") orelse {
        std.debug.print("{s}: \"error\" is required and must be true\n", .{ctx});
        return error.BadExpectedJson;
    };
    const is_error = expectJsonBool(error_json) orelse false;
    if (!is_error) {
        std.debug.print("{s}: \"error\" must be the literal true\n", .{ctx});
        return error.BadExpectedJson;
    }

    const code_json = root_obj.get("code") orelse {
        std.debug.print("{s}: \"code\" is required; message substrings are no longer accepted\n", .{ctx});
        return error.BadExpectedJson;
    };
    const code = expectJsonString(code_json) orelse {
        std.debug.print("{s}: \"code\" must be a string\n", .{ctx});
        return error.BadExpectedJson;
    };
    if (!codes.contains(code)) {
        std.debug.print("{s}: code \"{s}\" is not in testdata/error-codes.json\n", .{ ctx, code });
        return error.UnregisteredErrorCode;
    }
    if (std.mem.eql(u8, code, "UNKNOWN")) {
        std.debug.print("{s}: UNKNOWN is a parser bug marker and may not be asserted\n", .{ctx});
        return error.BadExpectedJson;
    }

    if (root_obj.get("line")) |v| try checkPositiveInt(ctx, v);
    if (root_obj.get("col")) |v| try checkPositiveInt(ctx, v);
}

// ─── AST comparison ─────────────────────────────────────────────────────────

fn compareValue(expected_obj: std.json.ObjectMap, actual: ast.Value) !void {
    const type_str = expectJsonString(expected_obj.get("type") orelse return error.MissingType) orelse return error.BadType;

    if (std.mem.eql(u8, type_str, "string")) {
        try testing.expectEqual(ast.ValueType.string, std.meta.activeTag(actual));
        const expected_data = expectJsonString(expected_obj.get("data") orelse return error.MissingData) orelse return error.BadData;
        try testing.expectEqualStrings(expected_data, actual.string);
    } else if (std.mem.eql(u8, type_str, "int")) {
        try testing.expectEqual(ast.ValueType.int, std.meta.activeTag(actual));
        const expected_data = expectJsonInt(expected_obj.get("data") orelse return error.MissingData) orelse return error.BadData;
        try testing.expectEqual(expected_data, actual.int);
    } else if (std.mem.eql(u8, type_str, "float")) {
        try testing.expectEqual(ast.ValueType.float, std.meta.activeTag(actual));
        const expected_data = expectJsonFloat(expected_obj.get("data") orelse return error.MissingData) orelse return error.BadData;
        try testing.expectApproxEqAbs(expected_data, actual.float, 1e-9);
    } else if (std.mem.eql(u8, type_str, "bool")) {
        try testing.expectEqual(ast.ValueType.bool, std.meta.activeTag(actual));
        const expected_data = expectJsonBool(expected_obj.get("data") orelse return error.MissingData) orelse return error.BadData;
        try testing.expectEqual(expected_data, actual.bool);
    } else if (std.mem.eql(u8, type_str, "null")) {
        try testing.expectEqual(ast.ValueType.null, std.meta.activeTag(actual));
    } else if (std.mem.eql(u8, type_str, "array")) {
        try testing.expectEqual(ast.ValueType.array, std.meta.activeTag(actual));
        const expected_items = expectJsonArray(expected_obj.get("data") orelse return error.MissingData) orelse return error.BadData;
        try testing.expectEqual(expected_items.len, actual.array.items.len);
        for (expected_items, 0..) |item_json, i| {
            const item_obj = expectJsonObject(item_json) orelse return error.BadArrayItem;
            try compareValue(item_obj, actual.array.items[i]);
        }
    } else {
        std.debug.print("Unknown type in expected JSON: {s}\n", .{type_str});
        return error.UnknownType;
    }
}

fn compareComments(expected_json: std.json.Value, actual: []const []const u8) !void {
    const expected = expectJsonArray(expected_json) orelse return error.BadExpectedJson;
    try testing.expectEqual(expected.len, actual.len);
    for (expected, 0..) |item, i| {
        const s = expectJsonString(item) orelse return error.BadExpectedJson;
        try testing.expectEqualStrings(s, actual[i]);
    }
}

fn compareTrailingComment(expected_json: std.json.Value, actual: ?[]const u8) !void {
    switch (expected_json) {
        .null => try testing.expect(actual == null),
        .string => |s| {
            try testing.expect(actual != null);
            try testing.expectEqualStrings(s, actual.?);
        },
        else => return error.BadExpectedJson,
    }
}

fn compareNodes(expected_children: []std.json.Value, actual_children: []const ast.Node) !void {
    try testing.expectEqual(expected_children.len, actual_children.len);

    for (expected_children, 0..) |child_json, i| {
        const child_obj = expectJsonObject(child_json) orelse return error.BadChild;
        const node_type = expectJsonString(child_obj.get("type") orelse return error.MissingNodeType) orelse return error.BadNodeType;

        if (std.mem.eql(u8, node_type, "field")) {
            const actual = actual_children[i];
            try testing.expect(actual == .field);
            const expected_key = expectJsonString(child_obj.get("key") orelse return error.MissingKey) orelse return error.BadKey;
            try testing.expectEqualStrings(expected_key, actual.field.key);

            if (child_obj.get("value")) |val_json| {
                const val_obj = expectJsonObject(val_json) orelse return error.BadValue;
                try compareValue(val_obj, actual.field.value);
            }
            if (child_obj.get("leading_comments")) |v| {
                try compareComments(v, actual.field.leading_comments);
            }
            if (child_obj.get("trailing_comment")) |v| {
                try compareTrailingComment(v, actual.field.trailing_comment);
            }
        } else if (std.mem.eql(u8, node_type, "block")) {
            const actual = actual_children[i];
            try testing.expect(actual == .block);
            const expected_name = expectJsonString(child_obj.get("name") orelse return error.MissingName) orelse return error.BadName;
            try testing.expectEqualStrings(expected_name, actual.block.name);

            if (child_obj.get("children")) |children_json| {
                const nested = expectJsonArray(children_json) orelse return error.BadChildren;
                try compareNodes(nested, actual.block.children);
            }
            if (child_obj.get("leading_comments")) |v| {
                try compareComments(v, actual.block.leading_comments);
            }
            if (child_obj.get("trailing_comments")) |v| {
                try compareComments(v, actual.block.trailing_comments);
            }
        } else if (std.mem.eql(u8, node_type, "block_array")) {
            const actual = actual_children[i];
            try testing.expect(actual == .block_array);
            const expected_name = expectJsonString(child_obj.get("name") orelse return error.MissingName) orelse return error.BadName;
            try testing.expectEqualStrings(expected_name, actual.block_array.name);

            if (child_obj.get("items")) |items_json| {
                const expected_items = expectJsonArray(items_json) orelse return error.BadData;
                try testing.expectEqual(expected_items.len, actual.block_array.items.len);
                for (expected_items, 0..) |item_json, j| {
                    const item_children = expectJsonArray(item_json) orelse return error.BadChildren;
                    try compareNodes(item_children, actual.block_array.items[j]);
                }
            }
            if (child_obj.get("leading_comments")) |v| {
                try compareComments(v, actual.block_array.leading_comments);
            }
            if (child_obj.get("trailing_comments")) |v| {
                try compareComments(v, actual.block_array.trailing_comments);
            }
        }
    }
}

fn compareNullableString(expected_json: std.json.Value, actual: ?[]const u8) !void {
    switch (expected_json) {
        .null => try testing.expect(actual == null),
        .string => |s| {
            try testing.expect(actual != null);
            try testing.expectEqualStrings(s, actual.?);
        },
        else => return error.BadExpectedJson,
    }
}

// ─── Skip accounting ────────────────────────────────────────────────────────
//
// Honest partial conformance is allowed; quiet partial conformance is not. Each
// skip prints as it happens and the totals print again at the end of the file.

var skipped_emit: usize = 0;
var skipped_imports: usize = 0;
var skipped_comments: usize = 0;

fn recordSkip(cap: Capability, subdir: []const u8, name: []const u8) void {
    switch (cap) {
        .parse => unreachable, // "parse" is mandatory; loadCapabilities rejects a manifest without it.
        .emit => skipped_emit += 1,
        .imports => skipped_imports += 1,
        .comments => skipped_comments += 1,
    }
    std.debug.print(
        "CONFORMANCE: skipping {s}/{s}: capability \"{s}\" not declared in zig/conformance.json\n",
        .{ subdir, name, @tagName(cap) },
    );
}

/// The first capability this fixture needs that the manifest does not declare,
/// or null when the fixture can run.
fn missingCapability(caps: Capabilities, f: Fixture, rep: SchemaReport) ?Capability {
    if (f.is_dir and !hasCapability(caps, .imports)) return .imports;
    if (f.formatted_path != null and !hasCapability(caps, .emit)) return .emit;
    if (rep.asserts_comments and !hasCapability(caps, .comments)) return .comments;
    return null;
}

// ─── Round-trip ─────────────────────────────────────────────────────────────

/// Pins the canonical text form: emitting the parsed fixture must reproduce the
/// sidecar byte for byte, and re-emitting the sidecar must be a fixed point.
fn checkRoundTrip(alloc: std.mem.Allocator, name: []const u8, formatted_path: []const u8, file: ast.File) !void {
    const want = try readFile(alloc, formatted_path);

    const got = try skg_root.emit.emitFile(testing.allocator, file);
    defer testing.allocator.free(got);
    if (!std.mem.eql(u8, want, got)) {
        std.debug.print(
            "FAIL {s}: emit does not match {s}\n--- want ---\n{s}\n--- got ---\n{s}\n",
            .{ name, formatted_path, want, got },
        );
        return error.EmitMismatch;
    }

    var again = skg_root.parseSource(testing.allocator, want, formatted_path);
    defer again.deinit();
    const again_file = again.file orelse {
        std.debug.print("FAIL {s}: {s} does not parse\n", .{ name, formatted_path });
        return error.FormattedDoesNotParse;
    };
    const out2 = try skg_root.emit.emitFile(testing.allocator, again_file);
    defer testing.allocator.free(out2);
    if (!std.mem.eql(u8, want, out2)) {
        std.debug.print(
            "FAIL {s}: emit is not idempotent for {s}\n--- want ---\n{s}\n--- got ---\n{s}\n",
            .{ name, formatted_path, want, out2 },
        );
        return error.EmitNotIdempotent;
    }
}

// ─── Runners ────────────────────────────────────────────────────────────────

fn runValidFixture(alloc: std.mem.Allocator, caps: Capabilities, f: Fixture) !void {
    const json_data = try readFile(alloc, f.json_path);
    const parsed_json = std.json.parseFromSlice(std.json.Value, alloc, json_data, .{}) catch |err| {
        std.debug.print("valid/{s}: expected.json is not valid JSON: {}\n", .{ f.name, err });
        return error.BadFixtureJson;
    };
    // Not deinited: the caller's arena owns everything.

    const root_obj = expectJsonObject(parsed_json.value) orelse {
        std.debug.print("valid/{s}: expected.json top level must be an object\n", .{f.name});
        return error.BadFixtureJson;
    };

    var rep = SchemaReport{};
    try validateValidExpected(f.name, root_obj, &rep);

    if (missingCapability(caps, f, rep)) |cap| {
        recordSkip(cap, "valid", f.name);
        return;
    }

    // The rule that separates the two entry points, and doubles as a security
    // pin: a directory fixture goes through the import-resolving file API, a
    // flat fixture is parsed from bytes and must never touch the filesystem.
    var result = if (f.is_dir)
        skg_root.parse(testing.allocator, f.entry_path)
    else
        skg_root.parseSource(testing.allocator, try readFile(alloc, f.entry_path), f.name);
    defer result.deinit();

    if (result.file == null) {
        std.debug.print("FAIL valid/{s}: parse failed", .{f.name});
        if (result.diagnostic) |d| {
            std.debug.print(": {s}:{d}:{d}: {s} [{s}]", .{ d.path, d.line, d.col, d.message, @tagName(d.code) });
        }
        std.debug.print("\n", .{});
        return error.ParseFailed;
    }
    const file = result.file.?;

    if (root_obj.get("skg_version")) |v| {
        try compareNullableString(v, file.skg_version);
    }
    if (root_obj.get("schema_version")) |v| {
        try compareNullableString(v, file.schema_version);
    }
    if (root_obj.get("imports")) |imports_json| {
        const expected_imports = expectJsonArray(imports_json) orelse return error.BadExpectedJson;
        try testing.expectEqual(expected_imports.len, file.import_paths.len);
        for (expected_imports, 0..) |imp_json, i| {
            const imp_str = expectJsonString(imp_json) orelse return error.BadImport;
            try testing.expectEqualStrings(imp_str, file.import_paths[i]);
        }
    }
    if (root_obj.get("children")) |children_json| {
        const expected_children = expectJsonArray(children_json) orelse return error.BadChildren;
        try compareNodes(expected_children, file.children);
    }
    if (root_obj.get("leading_comments")) |v| {
        try compareComments(v, file.leading_comments);
    }
    if (root_obj.get("trailing_comments")) |v| {
        try compareComments(v, file.trailing_comments);
    }

    if (f.formatted_path) |fp| {
        try checkRoundTrip(alloc, f.name, fp, file);
    }
}

fn runInvalidFixture(alloc: std.mem.Allocator, caps: Capabilities, codes: std.StringHashMap(void), f: Fixture) !void {
    const json_data = try readFile(alloc, f.json_path);
    const parsed_json = std.json.parseFromSlice(std.json.Value, alloc, json_data, .{}) catch |err| {
        std.debug.print("invalid/{s}: expected.json is not valid JSON: {}\n", .{ f.name, err });
        return error.BadFixtureJson;
    };
    // Not deinited: the caller's arena owns everything.

    const root_obj = expectJsonObject(parsed_json.value) orelse {
        std.debug.print("invalid/{s}: expected.json top level must be an object\n", .{f.name});
        return error.BadFixtureJson;
    };

    try validateInvalidExpected(f.name, root_obj, codes);

    const rep = SchemaReport{};
    if (missingCapability(caps, f, rep)) |cap| {
        recordSkip(cap, "invalid", f.name);
        return;
    }

    var result = if (f.is_dir)
        skg_root.parse(testing.allocator, f.entry_path)
    else
        skg_root.parseSource(testing.allocator, try readFile(alloc, f.entry_path), f.name);
    defer result.deinit();

    if (result.file != null) {
        std.debug.print("FAIL invalid/{s}: expected a parse error, got success\n", .{f.name});
        return error.ExpectedParseError;
    }
    const diag = result.diagnostic orelse {
        std.debug.print("FAIL invalid/{s}: parse failed without a diagnostic\n", .{f.name});
        return error.NoDiagnostic;
    };

    const expected_code = expectJsonString(root_obj.get("code").?).?;
    const actual_code = @tagName(diag.code);
    if (!std.mem.eql(u8, expected_code, actual_code)) {
        std.debug.print(
            "FAIL invalid/{s}: expected code {s}, got {s} (message: {s})\n",
            .{ f.name, expected_code, actual_code, diag.message },
        );
        return error.ErrorCodeMismatch;
    }
    if (root_obj.get("line")) |v| {
        const want = expectJsonInt(v).?;
        if (want != @as(i64, diag.line)) {
            std.debug.print("FAIL invalid/{s}: expected line {d}, got {d}\n", .{ f.name, want, diag.line });
            return error.LineMismatch;
        }
    }
    if (root_obj.get("col")) |v| {
        const want = expectJsonInt(v).?;
        if (want != @as(i64, diag.col)) {
            std.debug.print("FAIL invalid/{s}: expected col {d}, got {d}\n", .{ f.name, want, diag.col });
            return error.ColMismatch;
        }
    }
    if (diag.message.len == 0) {
        std.debug.print("FAIL invalid/{s}: diagnostic has an empty human-readable message\n", .{f.name});
        return error.EmptyDiagnosticMessage;
    }
}

// ─── Tests ──────────────────────────────────────────────────────────────────

test "conformance: valid fixtures" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const alloc = arena.allocator();

    const caps = try loadCapabilities(alloc);
    const fixtures = try discoverFixtures(alloc, "valid");

    // Each fixture is isolated: one failure must not hide the ones after it,
    // the way `go test` isolates subtests.
    var failures: usize = 0;
    for (fixtures) |f| {
        runValidFixture(alloc, caps, f) catch |err| {
            std.debug.print("FAIL valid/{s}: {}\n", .{ f.name, err });
            failures += 1;
        };
    }
    if (failures > 0) {
        std.debug.print("CONFORMANCE: {d} of {d} valid fixtures failed\n", .{ failures, fixtures.len });
        return error.ConformanceFailures;
    }
}

test "conformance: invalid fixtures" {
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const alloc = arena.allocator();

    const caps = try loadCapabilities(alloc);
    const codes = try loadErrorCodes(alloc);
    const fixtures = try discoverFixtures(alloc, "invalid");

    var failures: usize = 0;
    for (fixtures) |f| {
        runInvalidFixture(alloc, caps, codes, f) catch |err| {
            std.debug.print("FAIL invalid/{s}: {}\n", .{ f.name, err });
            failures += 1;
        };
    }
    if (failures > 0) {
        std.debug.print("CONFORMANCE: {d} of {d} invalid fixtures failed\n", .{ failures, fixtures.len });
        return error.ConformanceFailures;
    }
}

// Declared last so it runs after both suites: Zig executes tests in
// declaration order.
test "conformance: capability report" {
    var any_skipped = false;
    if (skipped_imports > 0) {
        std.debug.print(
            "CONFORMANCE: SKIPPED {d} fixtures: capability \"imports\" not declared in zig/conformance.json\n",
            .{skipped_imports},
        );
        any_skipped = true;
    }
    if (skipped_emit > 0) {
        std.debug.print(
            "CONFORMANCE: SKIPPED {d} fixtures: capability \"emit\" not declared in zig/conformance.json\n",
            .{skipped_emit},
        );
        any_skipped = true;
    }
    if (skipped_comments > 0) {
        std.debug.print(
            "CONFORMANCE: SKIPPED {d} fixtures: capability \"comments\" not declared in zig/conformance.json\n",
            .{skipped_comments},
        );
        any_skipped = true;
    }
    if (!any_skipped) {
        std.debug.print("CONFORMANCE: all fixtures ran; no capability was skipped\n", .{});
    }
}
