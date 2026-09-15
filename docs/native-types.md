# Native types

SKG packages load configuration in process. Application structs and native types
supply the schema; there is no separate SKG schema language, required evaluator
binary, or runtime bridge.

## Zig typed loading

`decodeSource(T, allocator, bytes, path, options)` parses and applies the local
overlay before decoding into `T`. It records imports but does not read them.
`decodeFile(T, allocator, path, options)` resolves imports and applies all
overlays first. Both return `native.Result(T)`.
`decodeFileWithOptions(T, allocator, path, options, resolve_options)` adds the
same rooted filesystem and aggregate resource policy as `parseWithOptions`.

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

## Go typed loading

`DecodeFileWithOptions[T](path, decodeOptions, resolveOptions)` pairs strict
native decoding with `ResolveOptions`; `DecodeFileIntoWithOptions` does the same
for caller defaults. `ParseFileWithOptions` and legacy
`UnmarshalFileWithOptions` expose that resolution policy without changing the
established entry points. A zero Go `ResolveOptions` selects every V1 default.

`DecodeSource[T](bytes, path, options)` parses and applies local overlays without
reading imports. `DecodeFile[T](path, options)` resolves imports before decoding.
Both return `(T, error)`; failure returns the zero value of `T`.

```go
type Config struct {
    Host    string            `skg:"host"`
    Port    uint16            `skg:"port"`
    Token   *string           `skg:"token"`
    Headers map[string]string `skg:"http.headers"`
}

cfg, err := skg.DecodeFile[Config]("config.skg", skg.DecodeOptions{})
```

Every input-visible Go struct field needs an `skg:"key"` tag. Untagged fields,
unexported fields and `skg:"-"` fields are ignored. Tagged fields on anonymous
embedded structs are promoted; a shallower field shadows a promoted field.
Ambiguous tags at the same depth are errors in typed loading. Go's empty tag
means “ignore”; use a string-keyed map or a custom decoder for an empty wire key.

### Presence and native defaults

Without `AllowMissingFields`, an absent nonnullable tagged field is an error.
Pointers, maps, slices and `any` are nullable and may be absent. Explicit null
clears a nullable target; it fails for nonnullable scalars, records and fixed
arrays. Null does not request a default. Go slices/maps already carry a nil
state; Zig uses an optional wrapper to model the equivalent nullable collection.

Go does not declare defaults on fields. Supply native defaults through
`DecodeSourceInto(bytes, path, target, options)` or
`DecodeFileInto(path, target, options)` and opt into `AllowMissingFields`:

```go
cfg := Config{Host: "localhost", Port: 8080}
err := skg.DecodeFileInto("config.skg", &cfg, skg.DecodeOptions{
    AllowMissingFields: true,
})
```

Absent fields retain those defaults, including keys removed by overlays.
This option applies throughout the target graph. Use a native validation hook
for application-specific requirements, such as a required nonempty slice.
Present structs update a copy of their defaults; present maps and arrays replace
their container contents. An empty object therefore clears a map but leaves
absent struct fields governed by the struct's presence policy.

The `Into` functions copy defaults before conversion and only replace the target
on success. Supported pointer, map and slice defaults are copied recursively;
successful results do not share their mutable storage with the supplied defaults.
Cycles, excessive default depth, nonzero channels/functions/unsafe pointers,
non-string map keys, and nonzero mutable unexported state are rejected. These
are configuration-value APIs, not general-purpose copies of arbitrary objects
such as live handles, locks or runtime services. Hooks' external side effects
are application-owned and cannot be rolled back.

### Supported mappings and extensions

| SKG value | Go target |
| --- | --- |
| Boolean | `bool` or a named boolean type |
| Integer | Signed/unsigned integer type, checked for range |
| Integer or float | `float32`/`float64`, exact conversion by default |
| String | String type, byte slice, or `encoding.TextUnmarshaler` |
| Null | Pointer, map, slice or `any` |
| Object | Tagged struct or string-keyed map |
| Array | Slice or fixed array; fixed lengths must match |
| Any supported value | Pointer to the supported target |
| Any value | `skg.Value`, or `any` using maps/slices and scalar Go values |

Byte slices also accept checked integer arrays; valid non-ASCII UTF-8 bytes are
preserved exactly. Invalid UTF-8 source is rejected before conversion. `any`
represents integers as `int64`
and floats as `float64`, without converting integers through floating point.
Nonempty interfaces require an application wrapper/custom decoder.

Unknown keys remain ignored unless `RejectUnknownFields` is true.
`AllowLossyNumbers` explicitly permits floating-point rounding and underflow;
overflow to infinity remains an error. Integer-to-`float32` conversion rounds
directly, without an intermediate rounding to `float64`.

A type may implement `DecodeSKG(*skg.DecodeContext, skg.Value) error` on its
pointer receiver. Use `ctx.Decode(value, &target)` or
`ctx.DecodeChild(value, key, location, &target)` for ordinary nested conversions.
`ctx.Fail(code, message)` creates a diagnostic at the current field. A
`ValidateSKG(*skg.DecodeContext) error` method runs after successful conversion,
including a custom decoder. Decoding `null` directly into a nullable target does
not invoke a validation method on that target because no value was constructed;
this matches Zig optional decoding. Defaults retained for absent fields are
copied as supplied, not individually re-decoded or revalidated. A containing
validation hook can check the complete resulting object.

The SKG-specific decoder takes precedence over `encoding.TextUnmarshaler`.
Go enum-like types use native validation/custom decoding to restrict their
allowed names; declaring constants on a named string does not impose a
constraint. See the shared [native conformance profiles](native-conformance.md)
for an equivalent Go and Zig enum example contract.

Ordinary hook errors are wrapped as `custom_error` and retain their cause for
`errors.Is`/`errors.As`. A hook's `DecodeError` retains its classification.
Recursive hook conversion is bounded at 256 calls. Go pointer indirection has a
separate 128-step bound. `ctx.Decode` is a low-level hook helper and may modify
its supplied target on failure; only the top-level loaders provide the result
and target guarantees above. Application code should not retain a context.

Go values are garbage collected and remain valid independently of the input
buffer. `DecodeError` exposes `Code`, `FieldPath`, `Source`, `Message` and `Err`.
Paths and locations follow the Zig diagnostic convention above. A parse error
has code `parse_error` and preserves its underlying `*ParseError` when available.
Invalid `Into` target arguments return `*InvalidUnmarshalError`. Allocation
failure follows Go runtime behavior; it is not a recoverable typed diagnostic.

### Existing Go APIs

`Unmarshal` and `UnmarshalFile` retain their established behavior: absent fields
keep target values, null can zero a nonnullable target, representable float
rounding is allowed, unknown fields are ignored, and failure may leave earlier
target fields changed. Custom native decoding/validation hooks are not invoked.
These entry points remain useful to existing callers and do not opt into the
typed loaders' stricter policy implicitly.

`Marshal` encodes structs using the same tags, supports maps, nested slices and
fixed arrays, and checks integer ranges, finite floats and homogeneous array
values. It rejects invalid UTF-8 in native strings and map/tag keys. It is a
value encoder, not an inverse of arbitrary custom decode hooks:
it does not invoke those hooks or `encoding.TextMarshaler`. Byte slices encode
as integer arrays; nil slices/maps encode as empty collections, while nil
pointers/interfaces encode as null. Encoding into a present native struct can
therefore preserve application data without preserving every null/absence or
custom-type distinction. Use the AST APIs when those distinctions are required.
