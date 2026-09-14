# SKG Conformance

This document specifies the cross-implementation conformance suite: the error
codes every parser reports, the fixture format, the capability levels, and a
checklist for porting SKG to a new language.

It is written to be sufficient on its own. You should be able to implement a
conforming SKG parser from [`spec.md`](spec.md) plus this document, without
reading `go/` or `zig/`.

- Language grammar and semantics: [`spec.md`](spec.md).
- Shared fixtures: [`../testdata/`](../testdata/).
- Reference implementations: `go/` (Go), `zig/` (Zig).
- Native type conversion: [`native-conformance.md`](native-conformance.md),
  with a separate shared corpus that both typed loaders run without skips.
- File-resolution policies and budgets:
  [`../testdata/resolution/cases.json`](../testdata/resolution/cases.json), run
  by every conforming package without skips.

---

## 1. What conformance means

An implementation conforms to the V1 core when it implements parsing, import
resolution, and native typed decoding and, for every applicable shared case:

- valid fixtures parse to the AST described by `expected.json`;
- invalid fixtures fail with the declared **error code** (and line/column when
  the fixture states them);
- the native conversion corpus passes without skips;
- optional emitter and comment-trivia fixtures pass when those capabilities are
  declared.

An implementation may decline emission or comment trivia. It may not claim V1
core conformance without parsing, import resolution, or native typed decoding.
See [§6](#6-capability-manifest).

### Non-goals

The tree-sitter grammar in `tools/tree-sitter-skg/` is **not** a conformance
peer. It is a highlighting grammar, deliberately more permissive, with no AST
contract. Its only gate is that every `testdata/valid/**/*.skg` file parses
without an `ERROR` or `MISSING` node (`npm run check:fixtures`).

---

## 2. Constants every implementation must honour

| Constant                | Value              | Why                                                                                                                                        |
| ----------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------ |
| Maximum nesting depth   | **128**            | One native stack frame per nested `{`, `[` (block, block array, or array). Beyond this, a recursive-descent parser overflows the stack, which most runtimes cannot catch. Must be enforced by the parser, not left to the runtime. |
| Maximum file size       | **10 MiB** (`10 * 1024 * 1024` bytes) | Required by the spec. Applies **per file**, not per import tree. |
| Maximum import depth    | **32**             | Levels of imports followed below the entry file. An independent recursion backstop. Exceeding it is `IMPORT_CHAIN_TOO_DEEP`. Pinned from both sides by `testdata/valid/imports-chain-at-limit/` and `testdata/invalid/import-chain-too-deep/`. |
| Aggregate source        | **64 MiB**         | Source bytes across unique canonical files in one default file-resolution call. |
| Canonical files         | **1,024**          | Unique real paths parsed by one default file-resolution call. Cache hits do not charge again. |
| Nodes and values        | **8,000,000**      | Named nodes plus every recursively nested value parsed across unique files. |
| Merge work              | **64,000,000**     | Node slots scanned across every recursive overlay merge in one resolution. |

Depth counting: the counter increases when the parser descends into a `{` of a
block, a `{` of a block-array entry, the `[` of a block array, or the `[` of a
scalar array; it decreases on the matching close. The error is reported at the
position of the **opening** delimiter that would exceed the cap - so the 129th
`{` in `testdata/invalid/nesting-too-deep.skg` is the one that fails.

The size cap applies to the **byte API too**, not only when reading from disk.
A caller that hands the parser an 11 MiB buffer must get `FILE_TOO_LARGE`.

Supported language version: `1.0`. A well-formed `skg_version` outside the
implementation's supported versions is `UNSUPPORTED_SKG_VERSION`; anything
that is not a `MAJOR.MINOR` pair of decimal integers is
`MALFORMED_SKG_VERSION`. Parse the components as wide unsigned integers:
`"300.0"` is well formed and unsupported, not malformed. This parser accepts
exactly `1.0`; unversioned files use 1.0 semantics while retaining a null header.
Each file in an import graph is checked independently.

Numeric range is part of the contract, not an implementation detail. An integer
literal outside the signed 64-bit range is `INVALID_INT`; a float literal whose
magnitude would become infinity as an IEEE-754 double is `INVALID_FLOAT`. Do not
saturate: an implementation that lets a literal become `inf` will emit `inf.0`,
which its own parser cannot read back. Underflow to `0.0` is accepted, because
zero is a value the language can write.

---

## 3. Diagnostics

A parse failure must produce a diagnostic with four parts:

| Part      | Contract                                                                                       |
| --------- | ---------------------------------------------------------------------------------------------- |
| `code`    | One of the codes in [§4](#4-error-code-registry). **Stable.** Fixtures assert this.             |
| `path`    | The file path the failure came from. Not asserted by fixtures.                                  |
| `line`    | 1-based line. Asserted when a fixture states it.                                                |
| `col`     | 1-based column, counted in **bytes**, not code points. Asserted when a fixture states it.       |
| `message` | Human-readable. **No compatibility promise** - word it however suits your language. Must not be empty. |

Splitting these is the point: Go says `malformed skg_version, expected
"major.minor"` and Zig says `skg_version is malformed, expected "MAJOR.MINOR"`.
Both report `MALFORMED_SKG_VERSION`, so a fixture can assert the failure exactly
instead of settling for a lowest-common-denominator substring.

---

## 4. Error-code registry

The registry is **closed**. The machine-readable copy is
[`../testdata/error-codes.json`](../testdata/error-codes.json); both runners load
it, reject any fixture naming a code that is not in it, and check their own code
set against it. A code cannot be used in a fixture until it is registered.

### Lexical

| Code                  | Raised when                                                                       |
| --------------------- | ---------------------------------------------------------------------------------- |
| `UNEXPECTED_CHAR`     | A byte appeared where no token can start (including a `-` not followed by a digit). |
| `UNTERMINATED_STRING` | A `"..."` hit a raw newline or end of input; a `"""..."""` hit end of input.        |
| `INVALID_ESCAPE`      | A backslash escape other than `\"`, `\\`, `\n`, `\t` inside a quoted string.        |
| `INVALID_UTF8`        | The first byte that cannot participate in a well-formed UTF-8 source.       |

### Syntax

| Code                       | Raised when                                                              |
| -------------------------- | -------------------------------------------------------------------------- |
| `EXPECTED_COLON`           | A `:` was required and something else was found.                            |
| `EXPECTED_RBRACE`          | A `}` was required and something else was found.                            |
| `EXPECTED_RBRACKET`        | A `]` was required and something else was found.                            |
| `EXPECTED_STRING`          | A quoted string was required (`skg_version`, `schema_version`, import path). |
| `EXPECTED_IDENT`           | A bare identifier or ordinary quoted key was required.                    |
| `EXPECTED_VALUE`           | A field or array value was required and the token cannot start one.         |
| `EXPECTED_COMMA`           | A comma was required between scalar array values or import paths.           |
| `EXPECTED_NODE_BODY`       | A key was not followed by `:`, `{` or `[`.                          |
| `UNEXPECTED_TOKEN`         | A required token kind has no more specific code.                            |
| `UNTERMINATED_BLOCK`       | A `{` block, or a block inside a block array, hit end of input.             |
| `UNTERMINATED_BLOCK_ARRAY` | A block array hit end of input before its `]`.                              |
| `UNTERMINATED_ARRAY`       | A scalar array hit end of input before its `]`.                             |
| `MIXED_ARRAY_TYPES`        | Non-null array elements did not all share one type tag, **or** a block array held a scalar / a scalar array held a block. |
| `INVALID_INT`              | An integer is out of i64 range, has a leading zero, or carries an identifier/underscore suffix. |
| `INVALID_FLOAT`            | A float is malformed/out of binary64 range, or uses exponent/leading-dot/suffix syntax. |

`EXPECTED_RBRACE` and `EXPECTED_RBRACKET` are registered but unreachable in both
reference parsers, so no fixture asserts them. They exist because a differently
structured parser can legitimately reach them. `UNEXPECTED_TOKEN` is reachable
but has no fixture of its own - every input that would produce it has a more
specific code available first.

`MIXED_ARRAY_TYPES` rejects incompatible non-null outer value tags. The first
non-null element determines the array type; null never masks an object/scalar
mismatch. `testdata/invalid/mixed-block-array` and
`testdata/invalid/mixed-array-block` pin both directions. Colon-form arrays and
colonless arrays use the same type rule.

For EOF diagnostics, a colonless array with no values yet or with inferred
object elements reports `UNTERMINATED_BLOCK_ARRAY`. Other arrays report
`UNTERMINATED_ARRAY`. An unterminated object reports `UNTERMINATED_BLOCK`,
including inside a nested value array.

### Overlay diagnostics

| Code | Raised when |
| --- | --- |
| `UNKNOWN_OVERLAY_OPERATION` | An identifier other than `delete` or `replace` follows `@`; reported at that identifier. |
| `EXPECTED_REPLACEMENT_BLOCK` | The key after `@replace` is not followed by `{`; reported at the unexpected token. |

An operation missing its name or key uses `EXPECTED_IDENT`. Operations in a
value position use `EXPECTED_VALUE`. Prefixes do not add nesting depth; object
bodies do. Unknown ordinary identifiers remain data keys.

### Header directives

| Code                       | Raised when                                                     |
| -------------------------- | ----------------------------------------------------------------- |
| `DUPLICATE_SKG_VERSION`    | `skg_version` declared more than once.                             |
| `DUPLICATE_SCHEMA_VERSION` | `schema_version` declared more than once.                          |
| `MALFORMED_SKG_VERSION`    | `skg_version` is not a `MAJOR.MINOR` pair of decimal integers.     |
| `UNSUPPORTED_SKG_VERSION`  | `skg_version` is well formed but outside the versions implemented. |
| `UNTERMINATED_IMPORT_LIST` | A bracketed `import [` list hit end of input before its `]`.       |
| `EXPECTED_IMPORT_PATH`     | `import` was not followed by a string or `[`.                      |
| `ABSOLUTE_IMPORT_PATH`     | An import path was absolute. See [§9](#absolute-import-paths).      |
| `DIRECTIVE_AFTER_BODY`     | A directive appeared after the first block or field.               |

Ordering note: the duplicate check runs **before** the version check, so
`skg_version: "1.0"` followed by `skg_version: "2.0"` is
`DUPLICATE_SKG_VERSION`, not `UNSUPPORTED_SKG_VERSION`.

`ABSOLUTE_IMPORT_PATH` is a **parse-time** failure, not a resolution failure: the
path is known from the bytes alone, so the byte API rejects it too, without
touching the filesystem. It is reported at the path's string token.

`skg_version`, `schema_version` and `import` are reserved as bare identifiers at the top level only. Quoted names are data keys.
Inside a block they are ordinary identifiers - a block has no header, so there is
nothing to be ambiguous with.

### Resource limits

| Code               | Raised when                                                   |
| ------------------ | --------------------------------------------------------------- |
| `NESTING_TOO_DEEP` | Nesting exceeded 128 levels.                                     |
| `FILE_TOO_LARGE`   | A single file, or a buffer handed to the byte API, exceeded 10 MiB. |
| `RESOLUTION_BYTE_LIMIT` | Aggregate bytes across unique files exceeded the resolution option. |
| `RESOLUTION_FILE_LIMIT` | Unique canonical files exceeded the resolution option. |
| `RESOLUTION_NODE_LIMIT` | Parsed nodes and recursively nested values exceeded the resolution option. |
| `RESOLUTION_WORK_LIMIT` | Recursive overlay merge work exceeded the resolution option. |

### Import resolution

Only reachable from the mandatory file API; the byte API never produces these.

| Code                    | Raised when                                                     |
| ----------------------- | ----------------------------------------------------------------- |
| `CIRCULAR_IMPORT`       | An import cycle was reached.                                       |
| `IMPORT_NOT_FOUND`      | An imported file could not be opened.                              |
| `IMPORT_CHAIN_TOO_DEEP` | Imports nested more than 32 levels below the entry file.           |
| `PATH_OUTSIDE_ROOT`     | A canonical entry or import target escaped the explicit root.      |

Import resolution failures are reported **at the import statement that named the file**: the
diagnostic's `path` is the importing file and its `line`/`col` are the position
of the path's string token. Reporting them at 0:0 is a bug - the whole point of
1-based positions is that a reader can go to the line. Only a failure on the
entry file itself, which no import statement named, has no position to report.

### Fallback

| Code      | Meaning                                                                            |
| --------- | ------------------------------------------------------------------------------------ |
| `UNKNOWN` | A diagnostic reached the caller without a code. A parser bug. Fixtures may not assert it - the runners reject a fixture that tries. |

---

## 5. Fixture format

`testdata/valid/` holds inputs that must parse. `testdata/invalid/` holds inputs
that must fail. Both are enumerated from disk: **no runner may hardcode a
fixture list**, so adding a file makes every implementation run it.

### 5.1 Flat fixtures

```
testdata/valid/simple-string.skg             # the input
testdata/valid/simple-string.expected.json   # required
testdata/valid/simple-string.formatted.skg   # optional; see §7
```

A flat fixture is parsed **from bytes**: read the `.skg` file yourself and hand
the buffer to the byte API (`ParseSource` in Go, `parseSource` in Zig). The
parser must not open any file.

### 5.2 Directory fixtures

```
testdata/valid/imports-basic/main.skg        # required entry point
testdata/valid/imports-basic/base.skg        # any number of other .skg files
testdata/valid/imports-basic/expected.json   # required
testdata/valid/imports-basic/formatted.skg   # optional; see §7
```

A directory fixture is loaded by passing the path of `main.skg` to the
**import-resolving file API** (`ParseFile` in Go, `parse` in Zig). Only
`main.skg` is an entry point; the sibling `.skg` files are reached through
imports, and are not fixtures in their own right.

### 5.3 The filesystem boundary

The flat/directory split is also a security pin. `testdata/valid/` contains
`flat-import-not-merged.skg`, which imports `flat-import-target.skg` - a file
that really exists next to it. Because the fixture is flat, it goes through the
byte API, and its `expected.json` asserts that the import path is **recorded**
and its contents are **not merged**. A byte API that quietly resolves imports
fails this fixture.

Rule: **the byte API never touches the filesystem.** Import resolution is a
property of the file API alone.

### 5.4 Runner failures

A runner must fail, not skip, when:

- a fixture has no `expected.json`;
- a directory fixture has no `main.skg`;
- an `expected.json` fails validation ([§5.5](#55-expectedjson));
- a fixture directory contains a file that is not `.skg`, `.expected.json` or
  `.formatted.skg`;
- a fixture directory yields **zero** fixtures - a suite that passes vacuously
  is worse than one that fails.

### 5.5 `expected.json`

Validation is **strict**: any key not listed below is a hard error. This is not
decoration. Before codes existed, a fixture that said `"cod"` instead of
`"code"` simply stopped asserting anything and still passed.

Implement it as a walk over the decoded JSON with a per-object allowlist (that
is what both reference runners do), or as a JSON Schema with
`additionalProperties: false`. Either is fine; silently ignoring unknown keys is
not.

#### Valid fixtures

| Key                 | Type                | Notes                                                       |
| ------------------- | ------------------- | ------------------------------------------------------------- |
| `skg_version`       | string \| null      | Optional. Absent means "do not assert".                        |
| `schema_version`    | string \| null      | Optional.                                                      |
| `imports`           | string[]            | Raw import paths **as written**, in declaration order, from the entry file only. |
| `children`          | node[]              | The merged top-level nodes.                                    |
| `leading_comments`  | string[]            | Comment trivia. Requires the `comments` capability.            |
| `trailing_comments` | string[]            | Comment trivia. Requires the `comments` capability.            |

Node objects:

| `type`        | Required keys | Optional keys                                                    |
| ------------- | ------------- | ------------------------------------------------------------------ |
| `delete`      | `key`         | `leading_comments`, `trailing_comment` |
| `field`       | `key`         | `value`, `leading_comments`, `trailing_comment`                     |
| `block`       | `name`        | `children`, `replace` (boolean), `leading_comments`, `trailing_comments`                 |
| `block_array` | `name`        | `items`, `leading_comments`, `trailing_comments`                    |

`items` is an array of node arrays or JSON nulls: an inner array denotes an
object entry, including `[]` for an empty object; null denotes a null entry.
This fixture representation maps to object/null Value entries in native ASTs.
`trailing_comment` (singular, `string | null`) exists only on fields; blocks and
block arrays use `trailing_comments` (plural, array).

Value objects:

| `type`   | `data`                  | Extra                            |
| -------- | ----------------------- | ---------------------------------- |
| `string` | JSON string             |                                    |
| `int`    | JSON number             | Compared as a 64-bit integer.      |
| `float`  | JSON number             | Parsed as binary64 and compared bit-for-bit. |
| `bool`   | JSON boolean            |                                    |
| `null`   | **must be absent**      |                                    |
| `array`  | array of value objects  | `element_type` **required**, compared with the parsed tag. |
| `object` | array of node objects   | Same node schema as block children. |

An empty array's `element_type` is `"string"` - that is the parser's default
when there is no element to infer from. All-null arrays have element type
`"null"`; otherwise it is the non-null element tag, including `"object"`.
A runner must assert this tag, not merely validate its spelling.

#### Invalid fixtures

```json
{ "error": true, "code": "MALFORMED_SKG_VERSION", "line": 1, "col": 14 }
```

| Key     | Type             | Notes                                                     |
| ------- | ---------------- | ----------------------------------------------------------- |
| `error` | `true`           | Required, literally `true`.                                  |
| `code`  | string           | Required. Must be in `testdata/error-codes.json`, and not `UNKNOWN`. |
| `line`  | positive integer | Optional. Asserted when present.                             |
| `col`   | positive integer | Optional. Asserted when present.                             |

`message_contains` is gone. A runner must reject it as an unknown key rather
than ignore it, so no fixture can silently regress to substring matching.

---

## 6. Capability manifest

Each implementation declares what it supports in a manifest beside its source:
`go/conformance.json`, `zig/conformance.json`.

```json
{
  "contract_version": 1,
  "language_version": "1.0",
  "implementation": "go",
  "capabilities": {
    "parse": true,
    "emit": true,
    "imports": true,
    "native": true,
    "comments": false
  },
  "notes": {
    "comments": "go/lexer.go discards '#' comments, so no trivia reaches the AST."
  }
}
```

`contract_version` versions the manifest and runner protocol. It is independent
of `language_version`, which states the SKG language contract implemented by the
package. V1 runners accept exactly contract version `1` and language version
`1.0`; later protocol or language support must change these fields explicitly.

### The five capabilities

| Capability | Meaning                                                                   | A fixture needs it when                            |
| ---------- | ------------------------------------------------------------------------- | ---------------------------------------------------- |
| `parse`    | Byte API producing the AST. **Core; mandatory.**                            | Always.                                              |
| `emit`     | Serialising an AST back to canonical SKG text.                             | The fixture has a `.formatted.skg` / `formatted.skg` sidecar. |
| `imports`  | File API that resolves and merges imports. **Core; mandatory.**             | The fixture is a directory.                          |
| `native`   | Strict conversion into the language's native static types. **Core; mandatory.** | The separate native corpus always runs.          |
| `comments` | Comment trivia attached to AST nodes and reproduced by the emitter.        | The fixture's `expected.json` contains any comment key. |

Requirement detection is **structural**, never declared by the fixture. Nobody
has to remember to tag a fixture, and nobody can mistag one.

### What the runner enforces

1. The manifest has exactly the five top-level fields shown above and lists
   exactly the five capability names. An unknown or missing field fails the run.
2. `contract_version` is `1`, `language_version` is `"1.0"`, and `parse`,
   `imports`, and `native` are `true`.
3. **Declared → obliged.** Every fixture needing a declared capability runs, and
   must pass.
4. **Optional and not declared → skipped, loudly.** Those fixtures are skipped
   and the runner prints an unconditional summary line per capability:

   ```
   CONFORMANCE: SKIPPED 7 fixtures: capability "comments" not declared in go/conformance.json (...)
   ```

   When nothing is skipped it says so instead. There is no silent path.
5. **Every undeclared optional capability needs a `notes` entry** saying why.
   Dropping one has to be a decision someone wrote down and a reviewer can see
   in the diff.

Because `go test` discards a passing package's output, the Go suite is run with
`-v` in CI and in `mise run test:go` so the summary always reaches the log. The
Zig suite writes to stderr through `std.debug.print`, which is unconditional.

---

## 7. Emit and round-trip

A fixture may carry a **formatted sidecar**: `<name>.formatted.skg` beside a
flat fixture, `formatted.skg` inside a directory fixture. When present, and when
the implementation declares `emit`, two things must hold:

1. `emit(parse(input))` equals the sidecar **byte for byte**;
2. `emit(parse(sidecar))` equals the sidecar - emitting is idempotent, so the
   formatted form is a fixed point.

### Canonical form

- Indentation is two spaces per depth.
- Header, in this order, each on its own line: `skg_version`, imports, then
  `schema_version` - the order [`spec.md`](spec.md) asks authors to write. A
  single import is `import "path"`. Two or more are

  ```
  import [
    "a.skg",
    "b.skg"
  ]
  ```

  with two-space indent and a comma after every entry but the last (the input's
  trailing comma is dropped).
- One blank line between the header and the body, if both are non-empty.
- A field is `key: value`.
- A block is `name {`, children at depth + 1, `}`.
- A block array is `name [`, then per object entry `{` at depth + 1, its children at
  depth + 2, and `}`; closed by `]`.
- A blank line precedes a top-level block or block array that is not the first
  node. Nested blocks get no blank line.
- Integers are plain decimal.
- Floats are the **shortest decimal that round-trips**, never exponent notation
  (the grammar has no exponent form), with `.0` appended when the shortest form
  has no fractional part. `13.0` stays `13.0`; `-0.5` stays `-0.5`. Large and
  small magnitudes therefore expand to long digit strings - `1e30` is 31 digits
  and a `.0`. `testdata/valid/emit-large-float` pins that both implementations
  produce the same expansion.
- `true`, `false`, `null` are literal.
- Arrays are `[a, b, c]` - comma **and** space between elements, `[]` when empty.
- Strings: if the value contains a newline **and** survives a `"""` literal, it
  is emitted as `"""content"""` verbatim. Otherwise it is a `"..."` literal with
  `"`, `\`, newline and tab escaped as `\"`, `\\`, `\n`, `\t`.

  "Survives a `"""` literal" means: the value contains no `"""`, and does not
  end with `"`. Triple-quoted literals do no escape processing, so either would
  terminate the literal early and produce output that does not re-parse. Those
  values **must** fall back to the escaped form - see
  `testdata/valid/emit-multiline-fallback.skg`.
- Comments (only with the `comments` capability): each leading comment on its own
  line at the owning node's indent, including its `#`; a field's trailing
  comment appended after the value separated by one space; a block's trailing
  comments at child indent, just before the closing delimiter.
- Blank lines are canonical whitespace rather than trivia: none between headers,
  one between a header and body, one before a non-first top-level block or block
  array, and none elsewhere.

NaN and infinity have no SKG literal. The emitter cannot report an error, so it
writes `null`; reject them earlier if your API can.

---

## 8. Comment trivia

Attachment rules are normative in [`spec.md`](spec.md#comment-attachment). The
conformance surface is:

| Owner         | Leading                        | Trailing                                            |
| ------------- | ------------------------------ | ----------------------------------------------------- |
| File          | before the first declaration   | after the last node                                    |
| Field         | own-line comments before it    | one comment on the same line as the value (`?string`)  |
| Block         | own-line comments before it    | comments between the last child and `}`                |
| Block array   | own-line comments before it    | comments between the last entry and `]`                |

Comment text includes the leading `#` and excludes the trailing newline.

Two fixture families exist deliberately:

- `comment-*.skg` assert **structure only** - that comments do not disturb
  parsing. Every implementation must pass these, including ones that discard
  comments.
- `trivia-*.skg` assert the **trivia itself** and therefore need the `comments`
  capability.

---

## 9. Imports

Resolution, merging and cycle detection are file-API behaviour.

1. Import paths are resolved **relative to the directory of the file containing
   the import statement**, not the process working directory and not the entry
   file. `testdata/valid/imports-subdir/` is the fixture that can tell the
   difference: every other import fixture keeps its files in one directory, so
   an implementation resolving against the entry file passes all of them.
2. Imports are processed in declaration order, depth first: resolve and fully
   load an import (including its own imports) before moving to the next.
3. Merge order is

   ```
   merged = {}
   for path in imports:            # declaration order
       merged = merge(merged, load(path).children)
   result.children = merge(merged, own_children)
   ```

   The importing file always overlays its imports, so it always wins.
4. Only `children` propagate. An imported file's `skg_version`,
   `schema_version` and `imports` do **not** appear on the importing file's AST.
   `expected.json`'s `imports` lists the entry file's own import paths only.
5. A file reached twice by different paths through the graph (a diamond) is
   **not** a cycle. Track the current chain, and remove a file from the visited
   set when you finish it - do not use a permanent "seen" set.
   `testdata/valid/imports-diamond/` pins this.
6. A cycle is `CIRCULAR_IMPORT`; an unopenable file is `IMPORT_NOT_FOUND`; a
   chain more than 32 levels below the entry file is `IMPORT_CHAIN_TOO_DEEP`.
7. **Canonicalise paths before the visited-set check**, and canonicalise the
   path you recurse on, not only the key. Comparing raw joined strings means
   `./b.skg` and `b.skg` look like different files - and worse, joining a
   directory onto `./b.skg` grows the path a segment at a time, so a cycle
   spelled the way `spec.md`'s own examples spell it is never detected at all:
   it runs until the path hits `PATH_MAX` and surfaces as `IMPORT_NOT_FOUND`
   with a multi-kilobyte path. `testdata/invalid/import-cycle-dotslash/` pins
   this; `testdata/invalid/import-cycle/` uses bare filenames and cannot.

   Canonical identity is the host filesystem's real absolute path (Go's
   `filepath.EvalSymlinks` plus `Abs`, Zig's `realpath`). Resolve imports from
   the canonical containing directory too: cache identity and relative-import
   meaning must not disagree for aliases. A path that cannot be canonicalized
   is `IMPORT_NOT_FOUND`; an operating-system symlink loop therefore fails
   there rather than as an SKG content cycle. Hard-link aliases remain distinct.
8. **Memoise files you have finished resolving**, keyed by canonical path. The
   visited set alone makes a diamond-shaped graph exponential: each level that
   imports the level below it twice doubles the work, so a 30-level graph - well
   inside the depth cap - never finishes. That is a denial of service reachable
   from a config file, and SKG's first consumer parses manifests as root.

   Memoising cannot mask a cycle: a file only enters the cache once it has been
   popped off the chain, so a file still being resolved is never a cache hit.

### Merge semantics

Raw byte fixtures assert composed overlays: `delete` nodes and a block's
optional `replace` boolean. Directory fixtures assert final values after all
imports; deletion markers are absent and replacement flags are false.
A runner must compare these markers when asserted. Final emission omits
active import statements for resolved files even though expected `imports`
still asserts their metadata.

Overlay operations must survive transitive and cached imports. Preserve
replacement barriers when a scalar, null or deletion precedes a block; simply
keeping the last block can resurrect earlier children. Compose all imports
and local instructions, then finalize once. Shared fixtures cover operation-only
imports, repeated imports, null versus absence, rebuilding deleted objects,
nested deletion and empty replacement. See the spec's explicit overlay section.

One namespace covers fields, blocks, block arrays and deletion operations; the merge key is the field
key or the block/block-array name.

- Overlaying a **block** onto a **block** merges their children recursively.
- A block marked `replace: true` replaces the base object without inheriting
  children. A later ordinary block may extend it while preserving the marker.
- A deletion overrides any prior value with a retained tombstone until finalization.
- Any other collision replaces the base node wholesale. In particular a block
  array replaces the previous value entirely - entries are never merged
  element-wise.
- A replaced node keeps the **slot of the first occurrence** - its place in the
  child ordering - and the **value of the last**. The `line`/`col` recorded on
  the node are the overlay's, because that is where the winning value was
  written; only a block-onto-block merge, which produces a genuinely new node,
  keeps the base's position.
- A new key is appended.

The same function deduplicates repeated keys *within* one file, so
`duplicate-lastwins.skg`, `duplicate-shapes.skg` and import last-wins are the
same rule. Bare and quoted spellings collide after decoding.

### Absolute import paths

**Rejected.** An import path must be relative to the file that wrote it. A path
beginning with `/` or `\`, or with a drive letter followed by `:`, is
`ABSOLUTE_IMPORT_PATH`.

The check is host-independent: the Windows spellings are rejected on Linux too,
so a config cannot mean one thing on one platform and something else on another.
Do not use your standard library's `IsAbs` - it answers a different,
host-dependent question.

Reasoning: an absolute path in a config file is not portable between machines,
and an import that escapes the config tree is a supply-chain hazard for a parser
running as root. Rejecting is also the smaller commitment - a later version can
bless absolute paths, but one that shipped them could never take them back.

Because the check runs at parse time, `testdata/invalid/absolute-import.skg` is
a flat fixture: every implementation runs it through the byte API.

---

## 10. Known gaps

Recorded here rather than left as folklore. The byte-API size cap and version
component-width divergences are fixed; unit tests cover oversized buffers, and
shared version fixtures cover wide components and invalid separators.

| Gap | Status |
| --- | ------ |
| **The Go parser discards comments.** `go/conformance.json` declares `comments: false`, so the `trivia-*` fixtures are skipped there and the skip is printed. | A real gap only if Go-side formatters matter. `skg fmt` is the Zig binary. |
| **Canonical formatting is not source-layout preservation.** Blank lines are normalized. Comments between headers become file-leading comments; comments between scalar array values become array-trailing comments. | This is the explicit V1 formatter contract. Every comment's text is preserved exactly once; source-exact tools must retain source bytes. |

---

## 11. Porting a parser

Work through this in order. Each step is checkable against the suite.

### Required

- [ ] **Lexer.** Tokens: identifier, int, float, string (`"..."` and `"""..."""`),
      `:` `{` `}` `[` `]` `,` `@`, comment, EOF. Identifiers are `[A-Za-z_][A-Za-z0-9_]*`;
      `true`, `false` and `null` lex as value literals, never identifiers.
      A number is a float only when it has a `.`; `13` is an int, `13.0` is a float.
      `-` starts a number only when a digit follows. Reject `13.` and a redundant
      leading zero (`007`, `00.5`), reported at the first byte of the literal.
      Reject exponent, plus, leading-dot, base-prefix, underscore and unit-suffix
      spellings. Preserve the sign bit of floating-point negative zero.
- [ ] **Escapes.** Exactly `\"`, `\\`, `\n`, `\t` inside `"..."`. Anything else is
      `INVALID_ESCAPE`. `"""..."""` does **no** escape processing - the content is
      literal, indentation included.
- [ ] **Encoding.** Validate the complete source as UTF-8 before lexing and
      report `INVALID_UTF8` at the first bad byte. Reject a UTF-8 BOM as
      `UNEXPECTED_CHAR`. Count columns in bytes and never normalize keys.
- [ ] **Parser.** Header directives (`skg_version`, `import`, `schema_version`),
      fields, blocks, block arrays. Accept ordinary double-quoted keys with decoded
      byte equality; reject triple-quoted keys. A colonless key followed by `[` whose
      first non-null value is not an object is a value array field.
      Objects are allowed at every value position; named objects normalize to
      blocks, and named object arrays to block arrays. Bare directive names are reserved at
      the top level and must all precede the first block or field.
- [ ] **Arrays.** All non-null elements share one type tag, checked one level deep. Nested
      arrays: the non-null outer elements must all be arrays; inner element types may
      differ. Null may occupy any array position; all-null arrays have
      element type `null`. Object and scalar entries cannot mix - the kind is
      chosen from the first non-null element and fixed thereafter. A colonless `[]` is an empty **block array**; an empty
      scalar array is `key: []`, element type `string`. Scalar/all-null arrays
      require one comma between values and permit one trailing comma. Object/null
      arrays make separators optional; leading or repeated commas are invalid.
- [ ] **Duplicates.** Within a file, a repeated key merges under the rules in
      [§9](#merge-semantics) - not an error.
- [ ] **Limits.** Depth 128, size 10 MiB, both enforced in the parser and both
      applied to the byte API. Numbers within 64-bit range, not saturated.
- [ ] **Version rules.** Treat omission as V1.0 semantics without inventing an
      AST header. Reject any declared `skg_version` you do not implement. Reject
      a duplicate `skg_version` or `schema_version`. Record `schema_version`
      without interpreting it. Check every imported file independently.
- [ ] **Diagnostics.** Code, path, 1-based line, 1-based byte column, message.
      Codes from [§4](#4-error-code-registry) only.
- [ ] **Byte API that never opens a file.**
- [ ] **Capability manifest** at `<impl>/conformance.json` with contract and
      language versions, all five capability keys, mandatory core capabilities
      enabled, and a `notes` entry for each optional capability you decline.
- [ ] **Runner** that enumerates `testdata/valid` and `testdata/invalid` from
      disk, strictly validates `expected.json`, enforces the capability rules in
      [§6](#6-capability-manifest), and fails on every condition in
      [§5.4](#54-runner-failures).

### Required V1 core

- [ ] `imports` - file API, canonical relative resolution, declaration-order
      merge, importer-wins, diamond-safe cycle detection, memoised so a diamond
      is linear, V1 aggregate budgets, chain depth capped at 32, explicit rooted
      policy, and failures reported at the import statement that named the file.
- [ ] `native` - strict native conversion in the host language, backed by the
      complete shared native corpus with no skips.

### Optional, but declare it either way

- [ ] `emit` - canonical form in [§7](#7-emit-and-round-trip), byte-exact and
      idempotent.
- [ ] `comments` - trivia captured on nodes per [§8](#8-comment-trivia) and
      reproduced by the emitter.

### Traps worth knowing before you start

- The 10 MiB cap belongs to the parser. Enforcing it only in your file reader
  leaves the byte API unguarded.
- Column numbers are byte offsets. Counting code points instead will pass every
  ASCII fixture and diverge on the first non-ASCII one.
- A permanent "visited" set turns a legitimate diamond into a false
  `CIRCULAR_IMPORT`. Push and pop the chain - and add a separate cache of
  *finished* files, or the same diamond becomes exponential instead.
- Decoding an expected `int` through a double passes every fixture until one
  uses `9223372036854775807`, and then compares the wrong number against
  itself. Both reference runners decode it as a 64-bit integer.
- A `"""` literal cannot carry a `"""`, nor a value ending in `"`. Your emitter
  must fall back to the escaped form or it will produce output it cannot read.
- Float emission must not use exponent notation - the grammar has no way to
  read it back.
