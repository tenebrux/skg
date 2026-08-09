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

---

## 1. What conformance means

An implementation conforms when, for every fixture in `testdata/`:

- valid fixtures parse to the AST described by `expected.json`;
- invalid fixtures fail with the declared **error code** (and line/column when
  the fixture states them);
- optional fixtures that exercise a capability the implementation **declares**
  also pass.

An implementation may decline a capability. It may not decline one quietly -
see [§6](#6-capability-manifest).

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
| Maximum import depth    | *unspecified*      | See [§10](#10-known-gaps). Reserve `IMPORT_CHAIN_TOO_DEEP` for it. |

Depth counting: the counter increases when the parser descends into a `{` of a
block, a `{` of a block-array entry, the `[` of a block array, or the `[` of a
scalar array; it decreases on the matching close. The error is reported at the
position of the **opening** delimiter that would exceed the cap - so the 129th
`{` in `testdata/invalid/nesting-too-deep.skg` is the one that fails.

The size cap applies to the **byte API too**, not only when reading from disk.
A caller that hands the parser an 11 MiB buffer must get `FILE_TOO_LARGE`.

Supported language version: `1.0`. A well-formed `skg_version` newer than the
implementation supports is `UNSUPPORTED_SKG_VERSION`; anything that is not a
`MAJOR.MINOR` pair of decimal integers is `MALFORMED_SKG_VERSION`. Parse the two
components as wide unsigned integers - `"300.0"` is well formed and too new, not
malformed.

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

### Syntax

| Code                       | Raised when                                                              |
| -------------------------- | -------------------------------------------------------------------------- |
| `EXPECTED_COLON`           | A `:` was required and something else was found.                            |
| `EXPECTED_RBRACE`          | A `}` was required and something else was found.                            |
| `EXPECTED_RBRACKET`        | A `]` was required and something else was found.                            |
| `EXPECTED_STRING`          | A quoted string was required (`skg_version`, `schema_version`, import path). |
| `EXPECTED_IDENT`           | An identifier was required and something else was found.                    |
| `EXPECTED_VALUE`           | A field value was required and the token cannot start one.                  |
| `EXPECTED_NODE_BODY`       | An identifier was not followed by `:`, `{` or `[`.                          |
| `UNEXPECTED_TOKEN`         | A required token kind has no more specific code.                            |
| `UNTERMINATED_BLOCK`       | A `{` block, or a block inside a block array, hit end of input.             |
| `UNTERMINATED_BLOCK_ARRAY` | A block array hit end of input before its `]`.                              |
| `UNTERMINATED_ARRAY`       | A scalar array hit end of input before its `]`.                             |
| `MIXED_ARRAY_TYPES`        | Array elements did not all share one type tag.                              |
| `INVALID_INT`              | An integer literal does not fit a signed 64-bit integer.                    |
| `INVALID_FLOAT`            | A float literal could not be converted to a 64-bit float.                   |

`EXPECTED_RBRACE`, `EXPECTED_RBRACKET` and `INVALID_FLOAT` are registered but
unreachable in both reference parsers, so no fixture asserts them. They exist
because a differently structured parser can legitimately reach them.

### Header directives

| Code                       | Raised when                                                     |
| -------------------------- | ----------------------------------------------------------------- |
| `DUPLICATE_SKG_VERSION`    | `skg_version` declared more than once.                             |
| `DUPLICATE_SCHEMA_VERSION` | `schema_version` declared more than once.                          |
| `MALFORMED_SKG_VERSION`    | `skg_version` is not a `MAJOR.MINOR` pair of decimal integers.     |
| `UNSUPPORTED_SKG_VERSION`  | `skg_version` is well formed but newer than the parser implements. |
| `UNTERMINATED_IMPORT_LIST` | A bracketed `import [` list hit end of input before its `]`.       |
| `EXPECTED_IMPORT_PATH`     | `import` was not followed by a string or `[`.                      |

Ordering note: the duplicate check runs **before** the version check, so
`skg_version: "1.0"` followed by `skg_version: "2.0"` is
`DUPLICATE_SKG_VERSION`, not `UNSUPPORTED_SKG_VERSION`.

### Resource limits

| Code               | Raised when                                                   |
| ------------------ | --------------------------------------------------------------- |
| `NESTING_TOO_DEEP` | Nesting exceeded 128 levels.                                     |
| `FILE_TOO_LARGE`   | A single file, or a buffer handed to the byte API, exceeded 10 MiB. |

### Import resolution

Only reachable from the file API. An implementation without the `imports`
capability never produces these.

| Code                    | Raised when                                                     |
| ----------------------- | ----------------------------------------------------------------- |
| `CIRCULAR_IMPORT`       | An import cycle was reached.                                       |
| `IMPORT_NOT_FOUND`      | An imported file could not be opened.                              |
| `IMPORT_CHAIN_TOO_DEEP` | Reserved; see [§10](#10-known-gaps). No implementation enforces it yet. |

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
| `field`       | `key`         | `value`, `leading_comments`, `trailing_comment`                     |
| `block`       | `name`        | `children`, `leading_comments`, `trailing_comments`                 |
| `block_array` | `name`        | `items`, `leading_comments`, `trailing_comments`                    |

`items` is an array of node arrays - one inner array per `{ ... }` entry.
`trailing_comment` (singular, `string | null`) exists only on fields; blocks and
block arrays use `trailing_comments` (plural, array).

Value objects:

| `type`   | `data`                  | Extra                            |
| -------- | ----------------------- | ---------------------------------- |
| `string` | JSON string             |                                    |
| `int`    | JSON number             | Compared as a 64-bit integer.      |
| `float`  | JSON number             | Compared with 1e-9 absolute tolerance. |
| `bool`   | JSON boolean            |                                    |
| `null`   | **must be absent**      |                                    |
| `array`  | array of value objects  | `element_type` **required**.       |

An empty array's `element_type` is `"string"` - that is the parser's default
when there is no element to infer from.

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
  "implementation": "go",
  "capabilities": {
    "parse": true,
    "emit": true,
    "imports": false,
    "comments": false
  },
  "notes": {
    "imports": "go/parser.go records import paths but never resolves them.",
    "comments": "go/lexer.go discards '#' comments, so no trivia reaches the AST."
  }
}
```

### The four capabilities

| Capability | Meaning                                                                   | A fixture needs it when                            |
| ---------- | ------------------------------------------------------------------------- | ---------------------------------------------------- |
| `parse`    | Byte API producing the AST. **Mandatory.**                                 | Always.                                              |
| `emit`     | Serialising an AST back to canonical SKG text.                             | The fixture has a `.formatted.skg` / `formatted.skg` sidecar. |
| `imports`  | File API that resolves and merges imports.                                 | The fixture is a directory.                          |
| `comments` | Comment trivia attached to AST nodes and reproduced by the emitter.        | The fixture's `expected.json` contains any comment key. |

Requirement detection is **structural**, never declared by the fixture. Nobody
has to remember to tag a fixture, and nobody can mistag one.

### What the runner enforces

1. The manifest lists exactly the four capability names. An unknown name, or a
   missing one, fails the run.
2. `parse` must be `true`.
3. **Declared → obliged.** Every fixture needing a declared capability runs, and
   must pass.
4. **Not declared → skipped, loudly.** Those fixtures are skipped and the runner
   prints an unconditional summary line per capability:

   ```
   CONFORMANCE: SKIPPED 7 fixtures: capability "imports" not declared in go/conformance.json (...)
   ```

   When nothing is skipped it says so instead. There is no silent path.
5. **Every undeclared capability needs a `notes` entry** saying why. Dropping a
   capability has to be a decision someone wrote down and a reviewer can see in
   the diff.

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
- Header, in this order, each on its own line: `skg_version`, `schema_version`,
  then imports. A single import is `import "path"`. Two or more are

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
- A block array is `name [`, then per entry `{` at depth + 1, its children at
  depth + 2, and `}`; closed by `]`.
- A blank line precedes a top-level block or block array that is not the first
  node. Nested blocks get no blank line.
- Integers are plain decimal.
- Floats are the **shortest decimal that round-trips**, never exponent notation
  (the grammar has no exponent form), with `.0` appended when the shortest form
  has no fractional part. `13.0` stays `13.0`; `-0.5` stays `-0.5`.
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
   file.
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
6. A cycle is `CIRCULAR_IMPORT`; an unopenable file is `IMPORT_NOT_FOUND`.
7. **Canonicalise paths before the visited-set check.** Comparing raw joined
   strings means `./b.skg` and `b.skg` look like different files, which delays
   cycle detection by a level.

### Merge semantics

One namespace covers fields, blocks and block arrays; the merge key is the field
key or the block/block-array name.

- Overlaying a **block** onto a **block** merges their children recursively.
- Any other collision replaces the base node wholesale. In particular a block
  array replaces the previous value entirely - entries are never merged
  element-wise.
- A replaced node keeps the **position of the first occurrence** and the
  **value of the last**.
- A new key is appended.

The same function deduplicates repeated keys *within* one file, so
`duplicate-lastwins.skg` and import last-wins are the same rule.

### Absolute import paths

**Explicitly unspecified in skg 1.0.** Do not rely on them.

Reasoning: an absolute path in a config file is not portable between machines,
and an import that escapes the config tree is a supply-chain hazard. Leaving it
unspecified is forward compatible - a later version can reject absolute import
paths outright, which is a strictly smaller behaviour surface than blessing
them, and a rule you can relax later but never tighten.

Current behaviour differs and neither is a promise: see
[§10](#10-known-gaps).

---

## 10. Known gaps

Recorded here rather than left as folklore. None of these has a fixture, and
each one says why.

| Gap | Status |
| --- | ------ |
| **No import-chain depth cap.** Nothing bounds how deep an import chain may go, so a long enough chain exhausts the stack. `IMPORT_CHAIN_TOO_DEEP` is reserved for it. | Needs a parser change in each implementation. Suggested cap: 64. No fixture, because a fixture would fail everywhere today. |
| **Zig re-interprets absolute import paths.** `std.fs.path.join(dir, "/etc/x.skg")` yields `<dir>/etc/x.skg`, silently turning an absolute path into a relative one. | Path-confusion bug. See [§9](#absolute-import-paths) for the recommended resolution. |
| **Zig's emitter aborts on very large or very small floats.** After writing the value it re-renders it into a fixed 32-byte buffer purely to test for a `.`, with `catch unreachable`. Any float whose decimal form exceeds 32 bytes (roughly `abs(x) >= 1e32`, or very small magnitudes) hits that `unreachable`. | Crash, not a wrong answer. No fixture: a panic aborts the whole test binary instead of failing one case. Fix by inspecting the bytes already written, or by sizing the buffer from the rendered length. |
| **Zig's byte API does not enforce the 10 MiB cap.** The cap is applied when reading from disk, so an oversized buffer passed directly to `parseSource` is accepted. Go rejects it. | Go is right: the cap is a parser property, not an I/O property. No fixture - a >10 MiB fixture is not worth committing. |
| **`skg_version` component width.** Zig parses each component as `u8`, so `"300.0"` is `MALFORMED_SKG_VERSION`; Go parses as 64-bit and reports `UNSUPPORTED_SKG_VERSION`. | Go is right: `"300.0"` is well formed, and the user deserves "too new" rather than "malformed". No fixture until Zig is fixed. |
| **Emitted header order contradicts the spec.** Both emitters write `skg_version`, `schema_version`, imports; `spec.md` lists imports before `schema_version`. The parser does not enforce order, so the emitted form re-parses. | Consistent across implementations, so not a conformance divergence. Pinned by `emit-header.formatted.skg`. The spec should be updated to match. |

---

## 11. Porting a parser

Work through this in order. Each step is checkable against the suite.

### Required

- [ ] **Lexer.** Tokens: identifier, int, float, string (`"..."` and `"""..."""`),
      `:` `{` `}` `[` `]` `,`, comment, EOF. Identifiers are `[A-Za-z_][A-Za-z0-9_]*`.
      A number is a float only when it has a `.`; `13` is an int, `13.0` is a float.
      `-` starts a number only when a digit follows.
- [ ] **Escapes.** Exactly `\"`, `\\`, `\n`, `\t` inside `"..."`. Anything else is
      `INVALID_ESCAPE`. `"""..."""` does **no** escape processing - the content is
      literal, indentation included.
- [ ] **Parser.** Header directives (`skg_version`, `import`, `schema_version`),
      fields, blocks, block arrays. A colonless identifier followed by `[` whose
      first token is not `{` is a scalar array field.
- [ ] **Arrays.** All elements share one type tag, checked one level deep. Nested
      arrays: the outer elements must all be arrays; inner element types may
      differ. `null` is its own type and cannot mix. Empty arrays default to
      element type `string`. Trailing commas are allowed; commas between block
      array entries are optional.
- [ ] **Duplicates.** Within a file, a repeated key merges under the rules in
      [§9](#merge-semantics) - not an error.
- [ ] **Limits.** Depth 128, size 10 MiB, both enforced in the parser and both
      applied to the byte API.
- [ ] **Version rules.** Reject a `skg_version` newer than you support. Reject a
      duplicate `skg_version` or `schema_version`. Record `schema_version` without
      interpreting it.
- [ ] **Diagnostics.** Code, path, 1-based line, 1-based byte column, message.
      Codes from [§4](#4-error-code-registry) only.
- [ ] **Byte API that never opens a file.**
- [ ] **Capability manifest** at `<impl>/conformance.json` with all four keys and
      a `notes` entry for each one you decline.
- [ ] **Runner** that enumerates `testdata/valid` and `testdata/invalid` from
      disk, strictly validates `expected.json`, enforces the capability rules in
      [§6](#6-capability-manifest), and fails on every condition in
      [§5.4](#54-runner-failures).

### Optional, but declare it either way

- [ ] `imports` - file API, relative resolution, declaration-order merge,
      importer-wins, diamond-safe cycle detection.
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
  `CIRCULAR_IMPORT`. Push and pop the chain.
- A `"""` literal cannot carry a `"""`, nor a value ending in `"`. Your emitter
  must fall back to the escaped form or it will produce output it cannot read.
- Float emission must not use exponent notation - the grammar has no way to
  read it back.
