# SKG in Zig

A working example of loading an SKG config in Zig by walking the AST.

Source: [main.zig](main.zig) - reads [../app.skg](../app.skg) and
populates a `Config` struct.

## Run it

```sh
cd examples/zig
zig build run
```

You'll see every field from `app.skg` printed.

## How it works

### Your struct IS still the schema

There's no runtime reflection or struct tags in Zig. Instead you
define your struct (with defaults), then write a small walker that
maps config keys and block names to struct fields. This is more code
than Go's tags, but it's explicit: you see exactly what gets
populated, and validation lives right next to the field it validates.

```zig
const Config = struct {
    name: []const u8 = "",
    port: i64 = 0,
    debug: bool = false,
    motd: []const u8 = "",
    database: Database = .{},
    logging: Logging = .{},
};

fn walkConfig(nodes: []const skg.ast.Node) Config {
    var cfg = Config{};
    for (nodes) |node| {
        switch (node) {
            .field => |f| {
                if (std.mem.eql(u8, f.key, "name"))  cfg.name = f.value.string
                else if (std.mem.eql(u8, f.key, "port")) cfg.port = f.value.int
                // ... extra keys silently ignored
            },
            .block => |b| {
                if (std.mem.eql(u8, b.name, "database"))
                    cfg.database = walkDatabase(b.children);
            },
        }
    }
    return cfg;
}
```

### Parse the source

```zig
var result = skg.parseSource(allocator, data, "config.skg");
defer result.deinit();

if (result.file) |file| {
    const cfg = walkConfig(file.children);
    // use cfg
} else if (result.diagnostic) |d| {
    std.debug.print("{s}:{d}:{d}: {s}\n", .{ d.path, d.line, d.col, d.message });
}
```

The result struct holds either a parsed `file` or a `diagnostic`
(never both). `deinit()` frees the arena that backs the whole tree.

## AST shape

- `ast.File` - top-level: `skg_version`, `schema_version`, `imports`, `children`
- `ast.Node` - tagged union of `.field`, `.block`, `.block_array`
- `ast.Value` - tagged union over `.int`, `.float`, `.bool`, `.string`, `.null`, `.array`

The walker in [main.zig](main.zig) pattern-matches on the node tags.
Full type definitions live in [../../zig/ast.zig](../../zig/ast.zig).

## Importing

This example depends on the skg module via `build.zig.zon`:

```zig
.dependencies = .{
    .skg = .{ .path = "../.." },
},
```

In a real project, point the `path` at a local checkout or use a
remote URL with a content hash:

```zig
.skg = .{
    .url = "https://github.com/tenebrux/skg/archive/<commit>.tar.gz",
    .hash = "<hash>",
},
```

Then in `build.zig`:

```zig
const skg = b.dependency("skg", .{});
exe.root_module.addImport("skg", skg.module("skg"));
```

## Public interface

The main entry points in [../../zig/root.zig](../../zig/root.zig):

- `skg.parseSource(allocator, source, path) !ParseResult` - parse an
  in-memory buffer
- `skg.parse(allocator, path) !ParseResult` - parse a file, resolving
  imports
- `skg.ast` - the AST type definitions
- `skg.emit` - format an AST back to SKG text (round-trip)

All allocations live on an arena that `ParseResult.deinit()` frees in
one call.
