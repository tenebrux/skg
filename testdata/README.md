# SKG Conformance Test Fixtures

Shared fixtures for validating SKG parser implementations across languages.
Runners enumerate this tree from disk, so dropping a fixture in makes every
implementation run it.

**The normative description of this format is
[../docs/conformance.md](../docs/conformance.md).** This file is a map.

## Layout

```
error-codes.json                 closed registry of stable parse-error codes
valid/<name>.skg                 flat fixture: parsed from BYTES, no filesystem access
valid/<name>.expected.json       required
valid/<name>.formatted.skg       optional: parse -> emit must equal this, byte for byte
valid/<name>/main.skg            directory fixture: loaded through the FILE API (imports resolve)
valid/<name>/expected.json       required
valid/<name>/formatted.skg       optional
invalid/...                      same two shapes; expected.json declares the error code
```

Flat versus directory is not cosmetic. A flat fixture must go through the byte
API and the parser must not open a file; a directory fixture goes through the
import-resolving file API. `valid/flat-import-not-merged.skg` imports a file
that really exists beside it and asserts the contents are *not* merged, which
pins that boundary.

## Expected JSON

Validation is strict - an unknown or misspelled key is a hard failure, never a
silently skipped assertion.

### Valid fixtures

```json
{
  "skg_version": "1.0",
  "schema_version": "1.0.0",
  "imports": ["./other.skg"],
  "children": [
    {
      "type": "field",
      "key": "name",
      "value": { "type": "string", "data": "hello" }
    },
    { "type": "block", "name": "theme", "children": [] }
  ]
}
```

Value objects:

| JSON                                                       | Meaning                       |
| ---------------------------------------------------------- | ------------------------------- |
| `{"type": "string", "data": "hello"}`                       | string                          |
| `{"type": "int", "data": 42}`                               | integer                         |
| `{"type": "float", "data": 1.5}`                            | float                           |
| `{"type": "bool", "data": true}`                            | boolean                         |
| `{"type": "null"}`                                          | null (carries no `data`)        |
| `{"type": "array", "element_type": "string", "data": [...]}`| array of value objects          |

Comment trivia is optional and asserted with `leading_comments`,
`trailing_comment` (fields) and `trailing_comments` (blocks, block arrays, file
level). A fixture that uses any of them requires the `comments` capability.

### Invalid fixtures

```json
{ "error": true, "code": "MALFORMED_SKG_VERSION", "line": 1, "col": 14 }
```

`code` must appear in [error-codes.json](error-codes.json). `line` and `col` are
optional and asserted when present. `message_contains` no longer exists -
implementations word the same failure differently, so fixtures assert the code.

## Capabilities

An implementation declares what it supports in `<impl>/conformance.json`
(`parse`, `emit`, `imports`, `comments`). A fixture needing a capability that is
not declared is skipped and counted in a summary line the runner always prints.
Which fixtures need what is derived structurally: directory means `imports`, a
formatted sidecar means `emit`, comment keys mean `comments`.

## Fixture families

| Prefix        | Covers                                                                 |
| ------------- | ------------------------------------------------------------------------ |
| `emit-*`      | Round-trip: floats (including magnitudes past 1e30), strings and escapes, multiline fallback, collections, header |
| `imports-*`   | Import resolution: basic merge, last-wins, multiple, chain, diamond, subdirectory, chain at the 32-level cap |
| `import-*`    | Import failures: missing file, cycle, `./`-spelled cycle, chain past the cap |
| `comment-*`   | Comments do not disturb parsing (structure only - no capability needed)  |
| `trivia-*`    | Comment trivia itself (requires the `comments` capability)               |
| `nesting-*`   | The 128-level depth boundary, accepted and rejected                      |
| `mixed-*`     | Array element uniformity, including block-versus-scalar in both directions |
| `int-*`, `float-*` | Number literal spelling and 64-bit range boundaries                 |

Two fixtures look redundant and are not. `import-cycle` spells its cycle with
bare filenames and `import-cycle-dotslash` spells it `./b.skg`; only the second
catches a resolver that compares raw joined path strings. `imports-chain` is two
levels deep and `imports-chain-at-limit` is 32, which is the last depth the cap
accepts - `import-chain-too-deep` is one past it.
