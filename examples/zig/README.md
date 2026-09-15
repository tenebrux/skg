# SKG in Zig

This example loads [../app.skg](../app.skg) directly into native Zig structs.
The fields and defaults in [main.zig](main.zig) supply the application schema.
There is no handwritten AST walker or external evaluator.

## Run it

```sh
cd examples/zig
zig build run
```

The example prints the service name, port, database settings, logging settings,
and message of the day. Extra configuration keys are ignored by default.

## Typed loading

```zig
const Config = struct {
    name: []const u8,
    port: u16,
    debug: bool = false,
};

var result = skg.decodeFile(Config, allocator, "../app.skg", .{});
defer result.deinit();
if (result.value) |config| {
    _ = config; // use native fields while result remains alive
} else if (result.diagnostic) |d| {
    std.debug.print("{s}: {s}\n", .{ d.field_path, d.message });
}
```

Absent fields use native defaults, or null for optional fields. Other fields
are required. Explicit null requires an optional target. Struct names are SKG
keys by default; `skg_fields` provides optional mappings. Native string maps,
lists, nested records, enums, pointers and fixed arrays are supported.

The result owns its memory and can be moved. Deinitialize it once; do not free
individual decoded values or retain them afterward. Conversion failures expose
no partial value. See [native types](../../docs/native-types.md) for numeric
rules, diagnostics, custom hooks, supported mappings and the Go API boundary.

## Importing the package

This example uses the repository through `build.zig.zon`:

```zig
.dependencies = .{
    .skg = .{ .path = "../.." },
},
```

In your build, obtain the dependency and add its module:

```zig
const skg = b.dependency("skg", .{});
exe.root_module.addImport("skg", skg.module("skg"));
```

The package currently targets Zig 0.15.2. Pin a repository revision or release
when adding a remote dependency.

## API choices

- `decodeSource(T, allocator, source, path, options)` decodes an in-memory body
  without reading imports.
- `decodeFile(T, allocator, path, options)` resolves imports before decoding.
- `parseSource(allocator, source, path)` returns an unresolved overlay AST for
  source tools and formatting.
- `parse(allocator, path)` returns final AST values after import resolution.
- `emit.emitFile(allocator, file)` emits canonical SKG.

Parsing returns `ParseResult` directly, not an error union. Typed loading returns
`native.Result(T)` directly. Inspect the optional value/file and diagnostic,
and call the corresponding result's `deinit()` on success or failure.
