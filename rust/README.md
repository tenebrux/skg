# SKG for Rust

Native SKG (Static Key Group) V1 support for Rust: parser, canonical
formatter, import resolution with overlay merge semantics, and Serde-based
typed decoding and encoding. Everything runs in process - no SKG CLI, no Go
or Zig toolchain, no code generation, no FFI, no subprocess.

## Installation

```sh
cargo add skg
```

or in `Cargo.toml`:

```toml
[dependencies]
skg = "0.1"
serde = { version = "1", features = ["derive"] }
```

The package ships as a single crate (`rust/` in this repository):

| Module | Contents |
| --- | --- |
| `lexer` | token scanner (comments become trivia, byte columns) |
| `parser` | recursive descent into the document model, with limits |
| `model` | `Document`, `Node`, `Value` - the public AST |
| `emit` | canonical serialization |
| `merge` | overlay composition, `@delete`/`@replace`, materialization |
| `resolve` | import resolution, budgets, the `Loader` boundary |
| `de` | Serde deserializer over materialized values |
| `ser` | Serde serializer to canonical SKG text |

The only dependency is `serde`. Tests use `serde_json` as a dev-dependency;
applications never need it.

## Typed usage

Your struct is the schema - there is no SKG schema language.

```rust
use serde::{Deserialize, Serialize};

#[derive(Debug, Deserialize, Serialize)]
struct Config {
    name: String,
    port: u16,
    debug: bool,
    tags: Vec<String>,
    database: Database,
}

#[derive(Debug, Deserialize, Serialize)]
struct Database {
    host: String,
    port: u16,
    ssl: bool,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    replicas: Vec<String>,
}

let text = std::fs::read_to_string("config.skg")?;
let config: Config = skg::from_str(&text)?;

// Canonical emission.
let canonical = skg::to_string(&config)?;
```

Field names are SKG keys; `#[serde(rename = "...")]` maps arbitrary keys,
including quoted ones. Unknown input fields are ignored by default and can be
rejected (see options below). Blocks decode from structs or string-keyed
maps; block arrays from `Vec` (or arrays); `Option<T>` accepts explicit null.

Import resolution before typed decoding:

```rust
let document = skg::resolve("config.skg", &skg::ResolveOptions::rooted("/etc/myapp"))?;
let config: Config = skg::from_document_with(&document, &skg::DecodeOptions::default())?;
```

## Dynamic documents

Use the AST when you need the shape of the data rather than a type:

```rust
let document = skg::parse(source)?;          // byte API: no filesystem access
for node in &document.children { /* ... */ } // fields, blocks, block arrays, deletes

let canonical = skg::emit(&document);        // canonical text
let formatted = skg::format(source)?;        // parse + emit convenience
```

A parsed document is a composed **overlay**: `@delete` markers and `@replace`
flags survive so the text can be formatted or imported elsewhere. File
resolution finalizes once at the end (deletes removed, flags cleared,
`imports_resolved` set) and emitting that document writes standalone final
data without import statements.

## Import resolution and custom loaders

The byte APIs (`parse`, `from_str`, `format`) never touch the filesystem.
File resolution goes through the [`Loader`] trait, so applications choose
their own store - the real filesystem, memory, or something sandboxed:

```rust
use skg::{Loader, MemoryLoader, ResolveOptions, resolve_with};

let loader = MemoryLoader::new()
    .with_file("main.skg", "import \"base.skg\"\nhost: \"main\"\n")
    .with_file("base.skg", "port: 80\n");

let document = resolve_with("main.skg", &loader, &ResolveOptions::new())?;
```

Semantics are frozen by the V1 contract:

- import paths are relative to the **canonical directory of the importing
  file**; absolute and Windows drive spellings are rejected at parse time;
- canonical identity is the host's real path (symlinks collapse, hard links
  stay distinct), so `./a.skg` and `a.skg` are one file;
- imports merge in declaration order and the importing file wins; blocks
  merge recursively, everything else replaces wholesale;
- diamonds are legal and linearly cached; cycles are
  `CIRCULAR_IMPORT`; chains deeper than 32 levels are
  `IMPORT_CHAIN_TOO_DEEP`;
- V1 default budgets per resolution: 64 MiB aggregate source, 1,024 unique
  files, 8,000,000 nodes and values, 64,000,000 merge-work units - all
  overridable through `ResolveOptions`;
- `ResolveOptions::rooted(path)` requires every canonical path (entry and
  imports) to stay inside the root or resolution fails with
  `PATH_OUTSIDE_ROOT`. Containment is checked on canonical paths; like the
  Zig implementation, a final path-use race remains - use process-level
  isolation when the config directory is controlled by an active adversary.

Resolution failures on imported files are reported at the import statement
that named them (file, line, byte column), never at 0:0; only a failure on
the entry file itself has no import position to report.

## Serde mapping rules

| SKG value | Rust target |
| --- | --- |
| bool | `bool` |
| int | any `i8`..`i64`, `u8`..`u64` (range-checked; `u64` above `i64::MAX` is rejected on encode) |
| float | `f32` / `f64`, exact by default (`allow_lossy_numbers` permits rounding, never infinity) |
| string | `String`, `&str`, `char` (single character) |
| null | `Option<T>`; explicit null never falls back to a default |
| object | struct, `HashMap`/`BTreeMap` with string keys |
| array | `Vec<T>`, slices, fixed-size arrays (exact length), nested freely |
| any value | `skg::Value`, the dynamic model |
| unit enum variant | string tag: `mode: "local"` |
| newtype / struct enum variant | single-entry block: `Choice { name: "a" }` |
| tuple enum variants | not representable; rejected on encode |

Supported Serde attributes: `rename`, `rename_all`, `default`,
`default = "path"`, `skip`, `skip_serializing_if`, `flatten`, `deny_unknown_fields`,
`try_from`, `from`, and manual `Deserialize`/`Serialize` implementations as
custom hooks. Errors raised through `serde::de::Error::custom` keep the
`custom_error` classification, while errors from nested conversions keep
theirs (`number_out_of_range`, `type_mismatch`, ...).

Runtime conversion options (no global state):

```rust
let options = skg::DecodeOptions {
    reject_unknown_fields: true,   // default: ignore unknown input fields
    allow_lossy_numbers: false,    // default: require exact numeric conversion
};
let config: Config = skg::from_str_with(&text, "config.skg", &options)?;
```

Diagnostics are structured: stable `code` (`missing_field`,
`type_mismatch`, `number_out_of_range`, `inexact_number`, `unknown_field`,
`invalid_enum`, `custom_error`, `parse_error`, ...), a JSON Pointer
`field_path` (`/service/port`, `~0`/`~1` escapes), and the source file, line
and byte column of the nearest named value. Missing fields report their
containing scope.

### Missing, null, and empty

The three states are distinct end to end:

| Input | Meaning | Decode |
| --- | --- | --- |
| key absent | field was not written | `#[serde(default)]` supplies a value, otherwise `missing_field` |
| `key: null` | explicit present null | requires `Option<T>` (or a custom decoder); never uses a default |
| `key {}` / `key: []` | present but empty | decodes as an empty struct/map/vec |

`@delete key` removes the key before native decoding, so deleted fields
follow the *absence* rule, not the null rule.

### Determinism and unsupported values

Maps encode with keys sorted; struct fields keep declaration order, so
`to_string` output is deterministic and a fixed point. Encoding rejects
non-finite floats, integers outside the signed 64-bit range, tuple enum
variants, non-string map keys, and outputs past the 10 MiB file cap. Cycles
in `Serialize` values are rejected at the 128-level nesting bound. Encoding
an empty `Vec` of structs writes `name: []` (a scalar empty array); it
decodes back to the same empty vector.

## Limits

Enforced in the parser, matching the frozen V1 contract and the Go/Zig
implementations: nesting depth 128 (`NESTING_TOO_DEEP`), 10 MiB per input
including the byte API (`FILE_TOO_LARGE`), and native conversion recursion
bounded at 256. Version rules: unversioned files use V1.0 semantics;
`skg_version: "1.0"` is accepted; anything else well-formed is
`UNSUPPORTED_SKG_VERSION`; not `major.minor` is `MALFORMED_SKG_VERSION`.

## Compatibility and MSRV

- **MSRV: Rust 1.71**, pinned by `rust-version` in `Cargo.toml`; stable Rust
  only, `#![forbid(unsafe_code)]`.
- The crate follows the repository [V1 compatibility
  policy](../docs/compatibility.md): the V1.0 grammar, canonical bytes,
  error codes, merge semantics, and default limits are frozen by the shared
  fixtures and will not change in 1.x. Additions (new mappings, new options)
  are allowed; removals and reinterpretations wait for V2.
- Dependency policy: `serde` is the only runtime dependency. It is upgraded
  conservatively; new Serde derive features are not required by this crate.

## Conformance

`rust/conformance.json` declares all five capabilities (`parse`, `emit`,
`imports`, `native`, `comments`). The test suite runs the shared
language-neutral corpora directly from `testdata/`:

- every `testdata/valid` and `testdata/invalid` fixture, with strict
  `expected.json` validation (unknown keys are hard failures);
- byte-for-byte canonical round trips for every `formatted` sidecar, plus
  parse - emit - parse idempotence everywhere;
- the full native conversion corpus (`testdata/native/cases.json`);
- the resolution corpus (`testdata/resolution/cases.json`), including
  rooted containment and exact budget boundaries;
- a 1,000-case deterministic generated corpus shared with the Go and Zig
  implementations (`tests/fixtures/differential.json`, produced by
  `tools/differential.go -dump`), checked byte for byte.

`node tools/check-contract.mjs` keeps the frozen fixture set immutable; the
Rust package never modifies shared fixtures to fit.

### Reusing the fixtures in another language

The corpora are language-neutral by design. A new implementation should, in
order (see [docs/conformance.md](../docs/conformance.md#11-porting-a-parser)):
enumerate `testdata/{valid,invalid}` from disk; strictly validate
`expected.json` against the per-key allowlists; drive flat fixtures through a
byte API and directory fixtures through a file API; run
`testdata/native/cases.json` against typed profiles with no skips; run
`testdata/resolution/cases.json` against the option-bearing file API; and
differential-check against the Go canonicalizer via
`tools/differential.go -dump`. Declare capabilities in a
`conformance.json` beside the implementation; skipping an optional
capability must be loud, never silent.

## License

MIT. See [LICENSE](../LICENSE).
