# Native types

SKG packages load configuration in process. Application structs and native types
supply the schema; there is no separate SKG schema language, required evaluator
binary, or runtime bridge.

## Zig typed loading

`decodeSource(T, allocator, bytes, path, options)` parses and applies the local
overlay before decoding into `T`. It records imports but does not read them.
`decodeFile(T, allocator, path, options)` resolves imports and applies all
overlays first. Both return `native.Result(T)`.

```zig
const Config = struct {
    host: []const u8 = "localhost",
    port: u16,
    token: ?[]const u8,
    headers: std.StringHashMap([]const u8),

    pub const skg_fields = .{ .headers = "http.headers" };
};

var result = skg.decodeFile(Config, allocator, "config.skg", .{});
defer result.deinit();

if (result.value) |config| {
    // Use config while result is alive.
    _ = config;
} else if (result.diagnostic) |diagnostic| {
    // Inspect diagnostic.code, field_path, source and message.
    _ = diagnostic;
}
```

The result owns an arena at a stable address. Strings, defaults, pointers,
slices and map storage allocated by decoding remain valid until `deinit()`.
A managed string map's allocator also remains valid if the result moves.
Treat the result as a unique owner: do not deinitialize copies of it, separately
free its values, or retain values after it is deinitialized. Caller input bytes
may be released as soon as `decodeSource` returns. Custom hooks must respect
this ownership contract.

A failure has no partially initialized `value`. The result still owns allocations
and diagnostic strings made before the failure, so always call `deinit()`.

### Fields, defaults and presence

Struct field names are SKG keys by default. A `pub const skg_fields` declaration
may map native names to arbitrary string keys. A mapping to `null` excludes a
field from input; that field still needs a native default or optional type.
Duplicate wire names and mappings naming nonexistent fields are errors.
Comptime fields are not decoded. Tuple structs require a custom decoder.

An absent field uses its declared native default. Without a default, an absent
optional field becomes null and an absent non-optional field is an error.
Defaults containing supported pointers or containers are copied into the owning
arena. Explicit null is accepted only by optional types (or a custom decoder);
it does not mean “use the default.”

Unknown input fields are ignored by default. Set
`.{ .unknown_fields = .reject }` to reject them. Ignoring fields does not bypass
parsing or overlay validation.

Deleting a key removes it from the configuration before native decoding. It
therefore invokes the native absence/default rule, rather than assigning null.
An empty object remains a present object.

### Supported mappings

| SKG value | Zig target |
| --- | --- |
| Boolean | `bool` |
| Integer | Signed or unsigned integer type, checked for range |
| Integer or float | Floating type, checked for range and exact conversion by default |
| String | `[]u8`, `[]const u8`, sentinel byte slices, or an enum tag |
| Null | `?T` |
| Object | Struct, `std.StringHashMap(T)`, or `std.StringHashMapUnmanaged(T)` |
| Array | Slice or fixed-length array; fixed lengths must match |
| Any supported value | A single-item pointer to a supported target |
| Any value | `skg.Value`, an arena-owned dynamic value tree |

Byte slices also accept integer arrays with checked byte ranges. Lists and maps
can nest and contain optional records. Multi-item pointers, C pointers, arbitrary
union selection, tuple records and other unsupported targets need custom
decoding. The decoder bounds native recursion at 256 conversion/default-copy
steps, including optional and pointer wrappers; this is separate from the
language's syntax-depth limit.

Float narrowing and integer-to-float conversion reject precision loss by
default. For example, `0.5` decodes exactly into `f32`; `0.1` does not.
Set `.{ .allow_lossy_numbers = true }` to permit native rounding, including
underflow to zero. Overflow is still an error. Integer range checks remain
mandatory. Enum names are case-sensitive strings; unknown tags fail.

### Custom native types

A container type can implement:

```zig
pub fn skgDecode(ctx: *skg.native.Context, value: skg.Value) skg.native.Error!@This()
```

Use `ctx.decode(T, value)` to reuse a supported conversion,
`ctx.decodeChild(T, value, key, location)` to add a nested diagnostic path, and
`ctx.allocator` for result-owned storage. Return
`ctx.fail(.custom_error, "explanation")` for a diagnostic with an owned message.

An optional `skgValidate(self: @This(), ctx: *skg.native.Context)
skg.native.Error!void` method runs after that type is decoded, including after
its custom decoder. Native default values are copied as supplied; they are not
reinterpreted through custom decoders or independently revalidated. Hooks are application code, not portable
schema constraints, and must not retain temporary external buffers.

### Diagnostics

`diagnostic.code` classifies parse failures, missing fields, unknown fields,
wrong types, invalid enum tags, range errors, inexact numbers, unsupported native
targets, excessive native recursion, custom errors, and allocation failures.
Human-readable message wording is not a compatibility surface.

`field_path` is a JSON Pointer: `/items/2/name`, with `~` escaped as `~0` and `/`
as `~1`. The empty pointer denotes the root. Source paths survive import and
merge operations. A conversion reports the nearest named node's source line
and byte column; an array index appears in the field path, but its location
currently points to its containing named value. Missing fields report their
containing scope, with line/column zero at the document root. Parse failures
retain the original parser diagnostic in `parse_diagnostic`.

## Go API boundary

The existing Go `Unmarshal` and `UnmarshalFile` APIs use `skg:"name"` tags,
ignore unknown fields, and preserve existing target values for absent fields.
`Unmarshal` applies the local overlay without reading imports; `UnmarshalFile`
resolves imports first. Objects decode to structs or string-keyed maps, and
arrays decode to slices, including nested objects and nullable pointer entries.

These APIs currently differ from the new Zig typed loader: Go permits null to
zero a non-pointer target, permits representable floating-point rounding, and
may update earlier target fields before a later decoding error. Zig uses the
strict rules above and returns an owned result. Applications must not assume
these policies are interchangeable. The public native integration contract
must account for these differences before a V1 compatibility freeze.
