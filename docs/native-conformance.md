# Native conversion contract

The native integration layer converts resolved SKG values into application
types in process. This contract describes the work a language package must do;
it does not introduce a user-facing schema language or a shared binary runtime.
See [native types](native-types.md) for the Go and Zig APIs and ownership rules.

## Boundary between parsing and native types

A port needs three separable operations:

1. Parse bytes plus a diagnostic path, with no filesystem access. Preserve
   imports and unresolved overlay operations in the returned document.
2. Resolve a file and its imports, compose overlays, then materialize the final
   value tree. Imported nodes retain source provenance.
3. Convert a materialized value tree using the destination's native type
   information and explicit conversion options. A byte-based typed loader
   materializes only the local document; a file-based loader resolves first.

The common value model is null, boolean, signed 64-bit integer, finite binary64
float, byte string, ordered array, and string-keyed object. Array homogeneity is
checked by the parser against each non-null element's outer value tag. Objects
inside arrays have the same semantics as ordinary named objects. Object keys
are literal decoded bytes; dots and slashes are not paths.

The native adapter needs the following information from the host type system:

| Native target | Information needed |
| --- | --- |
| Scalar | Scalar category and native numeric range/precision |
| Record | Input-visible field names, child types, absence/default policy |
| Map | String keys and the native value type |
| List/fixed array | Element type and any fixed length |
| Nullable value | The represented child type and null state |
| Enum | Allowed string tags, through native metadata or a native hook |
| Custom value | Application conversion hook and optional validation hook |
| Dynamic value | A representation preserving all SKG value categories |

Use reflection, compile-time type metadata, or idiomatic native facilities.
An implementation can organize these internally however it chooses. Native
field defaults are application data, and callbacks are application code;
neither is a portable schema expression language.

Unknown fields are ignored by default; explicit rejection is available.
Strict conversion rejects numeric range errors and inexact narrowing. A lossy
option permits native rounding, including underflow, but not infinity. An
integer must not first be converted through a narrower representation: for
example, `i64 -> f64 -> f32` can produce a different answer from `i64 -> f32`.
Parsing a decimal float into binary64 precedes native conversion; exactness is
relative to that parsed binary64 value, not an arbitrary-precision decimal.

Null and absence are separate. A missing required field fails; a missing field
with a native default uses that default; an optional field can use its absent
state. Explicit null requires a nullable target or a custom decoder. Native
defaults must not be silently substituted for explicit null.

Language-specific policy differences must be documented. Go's older
`Unmarshal` APIs intentionally retain their existing permissive policy. Shared
strict-conversion cases run the typed loaders, not those compatibility APIs.
Native representation also matters: a Go slice is nullable, while a Zig slice
needs `?` to express the equivalent optional collection.

Typed loaders expose no partial successful value on failure. Ownership and
allocation follow the host language and must be explicit. Diagnostics have a
stable classification, a JSON Pointer field path and source provenance. Message
wording is not compared. When an input has multiple independent errors, the
first error selected may depend on native field traversal; the shared contract
does not prescribe precedence between independent failures.

## Shared fixture format

[`testdata/native/cases.json`](../testdata/native/cases.json) is separate from the
parser fixtures. Its top-level `contract_version: 1` versions this fixture
format, while `language_version: "1.0"` binds it to the SKG language contract.
`cases` is a nonempty array. Each case has:

- A unique, nonempty `name` and a known `profile`.
- A required `source` string, which may be empty. It always uses the byte API.
- Optional `options` with booleans `reject_unknown_fields` and
  `allow_lossy_numbers`, both defaulting to false.
- Exactly one of `expected` (the native value, including JSON null) or
  `error: {"code": "...", "field_path": "..."}`.

Unknown properties, unknown profiles, unsupported fixture versions, missing
required properties, duplicate case names and empty suites must fail. Every
profile must run; no capability declaration can skip a native mapping case.
Fixture source is data and must not trigger import reads.

Successful output is compared structurally: integers exactly, float values as
binary64 numbers (widening a native binary32 result), strings byte-for-byte,
ordered lists by element, and records/maps by key. The expected JSON describes
the result, not a schema to interpret. The root native record has a field
named `value`; the fixture's `expected` describes that field's value.

| Profile | Native field contract | Go binding | Zig binding |
| --- | --- | --- | --- |
| `bool` | Required boolean | `bool` | `bool` |
| `u8` | Required unsigned 8-bit integer | `uint8` | `u8` |
| `optional_u8` | Nullable integer, absent becomes null | `*uint8` | `?u8` |
| `default_u8` | Integer with native default 7 | `Into` with 7 and `AllowMissingFields` | `u8 = 7` |
| `i64` | Required signed 64-bit integer | `int64` | `i64` |
| `f32`, `f64` | Required binary floating point | `float32`, `float64` | `f32`, `f64` |
| `string` | Required string | `string` | `[]const u8` |
| `bytes` | Nullable byte list, also accepts strings | `[]byte` | `?[]const u8` |
| `array2_u8` | Exactly two bytes | `[2]uint8` | `[2]u8` |
| `record` | Required record: required `id` u16, nullable `note` string | Tagged struct | Struct |
| `list_optional_records` | Nullable list of nullable `record` values | `[]*Record` | `?[]?Record` |
| `map_optional_u8` | Nullable string map of nullable unsigned 8-bit integers | `map[string]*uint8` | `?std.StringHashMap(?u8)` |
| `port` | Custom integer decoder to u16, validation rejects zero | `DecodeSKG` and `ValidateSKG` | `skgDecode` and `skgValidate` |
| `enum` | String tag: exactly `local` or `remote` | Named string with native decoder | `enum { local, remote }` |

For `bytes`, successful expected values are JSON integer arrays, never base64.
The port profile compares the custom type's integer payload. The enum profile
must reject unknown names as `invalid_enum`, independently of how the host
language expresses enum-like types. All Go root/profile fields are explicitly
tagged with their wire names.

## Running the contract

From the repository root:

```sh
(cd go && go test -v -run '^TestNativeConformance$' .)
zig test zig/native_test.zig
(cd rust && cargo test --test native_conformance)
```

The ordinary `go test ./...` within `go/` and `zig build test` at the root also
include the native cases. Both report the fixture version and executed count.
Host-specific tests additionally check ownership, allocation failures where
recoverable, caller defaults, callback errors, import locations and recursion
limits. A new package must bind the native profiles and run the same corpus,
then test its own ownership and failure behavior. It must not translate the
fixture's JSON expectations into runtime schema code for users. The Rust
runner binds each profile to a Serde-derived type and compares re-encoded
values structurally against the fixture expectations.
