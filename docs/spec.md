# SKG - Static Key Group

## Config Language Specification

Version: 1.0
Extension: `.skg`
Encoding: UTF-8 (no BOM)
Status: Draft

---

## Overview

SKG (Static Key Group) is a simple, hierarchical configuration language. It is designed to be human-readable, easy to extend, and unambiguous. There is one way to write each construct - no alternatives, no shortcuts, no implicit behavior.

SKG is not a general-purpose language. It has no variables, no templates, no expressions, no computation. It is structured data. The application consuming the config defines and validates the schema.

---

## File Structure

A `.skg` file consists of a **header** followed by a **body**:

1. An optional `skg_version` declaration
2. Zero or more `import` statements
3. An optional `schema_version` declaration
4. Zero or more blocks and fields

Items 1-3 are the header. **Every header directive must appear before the first
block or field**; one that follows the body is an error
(`DIRECTIVE_AFTER_BODY`). The three directives may appear in any order among
themselves, but the formatter always writes them in the order above, so
canonical text matches the order shown here.

`skg_version`, `schema_version` and `import` are reserved as **bare** names at
the top level: they introduce directives. Inside a block they are ordinary
identifiers. Bare `true`, `false` and `null` are value literals everywhere.

Field keys, block names and block-array names may be bare identifiers
(`[A-Za-z_][A-Za-z0-9_]*`) or ordinary double-quoted strings. Quoted keys use
the same escapes as string values; triple-quoted keys are not supported.
Empty keys, Unicode, punctuation and escaped newlines are allowed. Keys are
compared by their decoded bytes without normalization: `name` and `"name"`
identify the same key. A quoted name is always data, so `"import": 1` is a
field, including at the top level. Quoting also permits `"true"` and `"null"`.

The formatter uses a bare key wherever that spelling preserves its meaning,
and an escaped double-quoted key otherwise.

```
skg_version: "1.0"

import [
  "./theme.skg",
  "./keybinds.skg",
]

schema_version: "1.0.0"

theme {
  accent: "green"
}
```

Files must be well-formed UTF-8, including strings, quoted keys, comments and
import paths. Invalid encoding is `INVALID_UTF8` at the first invalid byte. No
byte-order mark: a leading `EF BB BF` is valid UTF-8 but rejected separately as
`UNEXPECTED_CHAR` at 1:1, because no token can start with it. Keys are still
compared by their encoded UTF-8 bytes without Unicode normalization. Columns in
diagnostics count bytes, not code points.

Line endings are LF (`\n`). The parser treats `\r` as whitespace - CRLF files will parse correctly, and line separators are normalized by the formatter. Carriage returns inside
string values remain part of those values.

---

## Comments

Comments begin with `#` and run to the end of the line. Comments are preserved through parse-emit round-trips.

```
# This is a comment
accent: "green"  # This is also a comment
```

### Comment Attachment

Comments attach to AST nodes as trivia using these rules:

1. A comment on the same line as a field value attaches as that field's **trailing comment**.
2. Comments between the last child and a closing `}` or `]` attach as **trailing comments** on the enclosing block or block array. This takes precedence over rule 3 - if there is no next node before the closing delimiter, the comment belongs to the block, not to a nonexistent next node.
3. A comment on its own line attaches as a **leading comment** on the next node.
4. Comments at the very top of a file (before any declaration) are **file leading comments**.
5. Comments at the very bottom of a file (after all nodes) are **file trailing comments**.

Multiple consecutive comments follow the same rules - they all attach to the same target. Three comments before a field are all leading comments on that field. Three comments before a `}` are all trailing comments on the block.

```
# leading comment on the field
name: "hello" # trailing comment on the field

theme {
  accent: "green"
  # trailing comment on the block (no next node before })
  # also a trailing comment on the block
}
```

---

## Whitespace

Whitespace (spaces, tabs, newlines) is not significant except as a separator between tokens. Indentation is conventional, not required.

---

## Load Order

Files are loaded top to bottom. Later values overwrite earlier values.

Imports are processed where they appear. The main config file always loads after all its imports, so it always wins.

```
import "./theme.skg"    # loaded first
import "./keybinds.skg" # loaded second

theme {
  accent: "purple"  # overwrites whatever theme.skg set
}
```

Repeated keys are not an error. One namespace covers fields, blocks, block
arrays and operations, and the first occurrence determines the key's position.
Two objects merge their children recursively; every other collision takes the
later value wholesale. A scalar/null/deletion followed by an object starts a
fresh object and does not resurrect older children. Bare and quoted spellings
of the same decoded key collide. These are the same rules used across imports.

---

## Imports

Imports load and merge one or more `.skg` files before continuing to parse the current file.

Single import:

```
import "./theme.skg"
```

Multiple imports (ordered, top to bottom) use exactly one comma between paths
and may have one trailing comma. Leading, repeated and omitted commas are invalid:

```
import [
  "./theme.skg",
  "./keybinds.skg",
]
```

Import paths are relative to the canonical file containing the import statement
- not to the entry file, and not to the process working directory. Canonical
identity follows symlinks, so imports in a symlinked file resolve beside its
referent. Reaching the same canonical file through multiple aliases is one file
for parsing, caching and aggregate budgets. Hard-link aliases are distinct.

**Absolute import paths are rejected** (`ABSOLUTE_IMPORT_PATH`). A path is
absolute when it begins with `/` or `\`, or when it begins with a drive letter
followed by `:` (`C:\theme.skg`, `c:theme.skg`). All of those spellings are
rejected on every platform, so a file cannot mean one thing on Linux and
another on Windows. The check happens at parse time, so the byte API rejects an absolute import
without touching the filesystem. This portability rule alone provides no
containment: `..` components and symlinks can still lead outside the config
tree. The options file API can set a canonical root; then the entry and every
import target must remain within it or resolution fails with
`PATH_OUTSIDE_ROOT`. Real-path checking followed by ordinary file opening is not
an operating-system sandbox against an attacker concurrently changing the
filesystem. Use an OS sandbox or trusted file store for that threat model.

Circular imports are an error (`CIRCULAR_IMPORT`). The parser detects and
rejects them, comparing paths in canonical form so `./theme.skg` and
`theme.skg` are recognised as the same file.

A file reached twice by different routes through the graph (a diamond) is not a
cycle.

Import chains are followed to **32 levels** below the entry file; deeper is
`IMPORT_CHAIN_TOO_DEEP`, including paths through already cached imports. This
is a recursion backstop independent of canonical cycle detection.

One file-resolution call also has V1 defaults of **64 MiB aggregate source**,
**1,024 unique canonical files**, **8,000,000 nodes and recursively nested
values**, and **64,000,000 merge work units**. A merge work unit is one node
slot scanned at one overlay level; recursive block merges charge each level.
The options API may set positive lower or higher limits. Lowering these defaults
in V1.x would be a breaking change.

---

## Explicit overlay operations

`@delete key` removes a key, and `@replace key { ... }` replaces an entire
object. The `@` prefix keeps ordinary `delete` and `replace` keys available.

```
import "defaults.skg"

@delete legacy_mode
@replace routes {}
server {
  @delete old_port
  @replace headers { "Content-Type": "application/json" }
}
```

Keys use the same bare or quoted spelling as ordinary fields. They address one
literal key in the current object: `@delete "a.b"` deletes the key `a.b`, not
a nested path. Use nested blocks to target nested keys. A nested block still creates its
object when absent, even if its only child operation deletes an absent key.
Operations are body
statements; header directives must still precede them.

- Deleting an absent key is valid. Deletion is distinct from assigning null:
  null remains a present value, whereas a deleted key is absent.
- Replacement requires an object body in braces, without a colon. It discards
  all inherited children whether the prior value is an object, another type,
  or absent. An empty body clears the object.
- Ordinary blocks continue to merge recursively. Later blocks can add to an
  earlier replacement. Scalars and arrays already replace wholesale and do
  not need a separate replacement operation.
- Operations apply in source order within a scope, after imports in declared
  order. Arrays contain independent values; their objects can contain local
  operations, but no operation addresses an array index or another entry.
- A deletion or scalar followed by an object is also a replacement boundary.
  For example, `x: null x { fresh: 1 }` must not resurrect old children of
  `x` from an earlier import.

Parsing and loading are separate contracts. Byte parsing composes the local
body as an **unresolved overlay**, retaining delete markers and replacement
flags. Formatting that overlay preserves its effect when imported elsewhere.
A composed overlay keeps the first occurrence's key position, including delete
markers. Finalization removes deleted positions and clears replacement flags.

`MergeNodes` (Go) and `merge.mergeNodes` (Zig) compose overlays without
finalizing. Compose all layers first, then call `MaterializeNodes` /
`merge.materializeNodes` to obtain ordinary data. These functions do not
mutate their inputs. A finalized tree is data, not reusable operation history.
The file-loading APIs finalize once after all imports; Go `Unmarshal`
finalizes its local body without loading imports.

A resolved file retains import paths as diagnostic metadata and sets
`ImportsResolved` (Go) / `imports_resolved` (Zig). Emitting that file writes
**standalone final data without import statements**. Keeping imports active
after removing delete markers could recreate deleted values on the next load.
To format an original source while preserving its imports and operations, use
the byte-parsing API, as `skg fmt` does.

---

## Value Types

There are five scalar value types and two collection types (arrays and objects). The type is determined by syntax - no type annotations.

### Int

A whole number, positive or negative. No quotes.

```
timeout: 5000
max_crashes: 3
weight: 400
offset: -7
```

### Float

A number with a decimal point. No quotes. May be negative.

```
opacity: 0.92
size_base: 13.0
fade_in_step: 0.03
adjustment: -0.5
```

A trailing zero after the decimal is required. `13` is an int. `13.0` is a
float. `13.` is neither - it is `INVALID_FLOAT`, because there is one way to
write each value and `13.0` is it.

Scientific notation, a leading `+`, leading-dot decimals, hexadecimal/binary
integers, numeric separators and unit suffixes are not part of V1. Write a
decimal integer or a decimal-point float directly: `1000`, `0.5`, and `1.0`.
`-0` is accepted as integer zero and canonicalizes to `0`; `-0.0` preserves its
IEEE-754 sign. Extra fractional zeroes are accepted and canonicalized away.

Neither ints nor floats may carry a redundant leading zero: the integer part is
`0` or begins with a non-zero digit. `007` is `INVALID_INT` and `00.5` is
`INVALID_FLOAT`; write `7` and `0.5`.

A literal too large for a 64-bit value is an error rather than a saturated
result: outside the signed 64-bit range is `INVALID_INT`, and a magnitude that
would become infinity as an IEEE-754 double is `INVALID_FLOAT`. (A magnitude too
small to represent underflows to `0.0` and is accepted - unlike infinity, zero
is a value the language can write back out.)

### Bool

Exactly `true` or `false`. No quotes.

```
managed: true
vsync: false
```

### Null

The literal `null` represents an absent value. No quotes.

```
background: null
```

Null replaces an inherited value with an explicit null; it does not delete the key. Null may also appear in arrays alongside one non-null element type, including objects.

### String

Any value that is not an int, float, bool, or null must be quoted with double quotes `"`.

```
accent: "green"
position: "top"
family: "JetBrains Mono"
background: "#0d0d0d"
schema_version: "1.0.0"
```

Single quotes are not valid. Escape sequences within strings:

| Sequence | Meaning              |
| -------- | -------------------- |
| `\"`     | Literal double quote |
| `\\`     | Literal backslash    |
| `\n`     | Newline              |
| `\t`     | Tab                  |

### Multiline Strings

Triple-quoted strings (`"""..."""`) span multiple lines. No escape processing is performed inside triple-quoted strings - the content between the delimiters is taken literally, including leading whitespace on continuation lines.

```
description: """This is a
multiline string that preserves
newlines exactly as written."""
```

If the string is inside an indented block, the indentation becomes part of the string content:

```
theme {
  description: """line one
  line two"""
}
```

In this example, "line two" is preceded by two spaces. There is no automatic indentation stripping - literal means literal.

### Array

An ordered list of values enclosed in `[ ]`. Scalar, nested-array and all-null
lists use exactly one comma between adjacent values and may have one trailing
comma. Leading, repeated and omitted commas are invalid. All non-null elements
must be the same type. Null elements are allowed at any position.

```
bindings: ["super+1", "super+2", "super+3"]

sizes: [8.0, 12.0, 16.0]
```

Type uniformity is checked one level deep: every non-null element in an array must have the same type tag. For nested arrays, the outer array requires all non-null elements to be arrays, but inner arrays may have different element types:

```
# valid - outer elements are both arrays
matrix: [[1, 2], [3, 4]]

# also valid - outer elements are both arrays, inner types differ
mixed: [[1, 2], ["a", "b"]]

# invalid - outer elements are mixed (int and string)
bad: [1, "two", 3]

# valid - null does not change the non-null element type
nullable: [1, null, 3]

# invalid - null does not permit incompatible non-null types
also_bad: [1, null, "three"]
```

An empty scalar array has no values; its AST element-type sentinel is `string`.
An all-null array has element type `null`. Otherwise the element type is that
of the non-null values. Null entries preserve their positions. Native decoding
must still use a destination type capable of representing the intended values.

Arrays may span multiple lines:

```
import [
  "./theme.skg",
  "./keybinds.skg",
]
```

Empty arrays are valid: `tags: []`

---

## Blocks

A block is a named scope containing fields and/or nested blocks. Blocks use `{ }`.

```
theme {
  accent: "green"

  colors {
    background: "#0d0d0d"
  }
}
```

Block names are unique within their parent scope. If the same block name appears twice, the contents are merged with last-wins semantics.

Blocks may be empty:

```
defaults {}
```

### Object values

An object is an anonymous block: `{ key: value nested { ... } }`. Its fields
use the same key, duplicate and recursive-merge rules as named blocks. It may
appear anywhere a value is expected, including inside nested arrays:

```
matrix: [
  [{ name: "primary" }, null],
  [{ name: "secondary" }, {}]
]
```

Objects use SKG field separators, not JSON object commas. A named object
`service: { port: 8080 }` is equivalent to `service { port: 8080 }` and
normalizes to a block. The canonical formatter favors the existing block
spelling. Duplicate named objects therefore merge recursively regardless of
whether the colon was written. Arrays replace wholesale; objects in separate
array positions do not merge with each other. Empty `{}` and `null` are
distinct values.

---

## Block Arrays

A block array is an ordered list of anonymous objects, optionally containing null entries. The syntax is `name [ { ... } { ... } ]`.

```
users [
  {
    name: "admin"
    sudo: true
    groups: ["wheel", "video"]
  }
  {
    name: "guest"
    sudo: false
    groups: ["users"]
  }
]
```

Each `{ }` entry in the array is an independent block with its own fields and
nested blocks. Entries are ordered - position is significant. Object arrays are
the one collection whose commas are optional between entries, including null
entries. When a comma is written, only one is allowed. A single trailing comma
is allowed; leading and repeated commas are invalid. This permits the normal SKG
block layout while keeping scalar arrays unambiguous.

Block arrays are the way to represent ordered collections of structured items - panels, zones, users, rules, etc.

When merging (via imports), a block array replaces the entire previous value - items are not merged individually.

Block arrays are the named spelling of arrays whose non-null elements are
objects. Both `users [ { name: "admin" } null ]` and
`users: [{ name: "admin" }, null]` parse to the same block-array node.
The formatter uses the colonless block-array spelling.

A colonless key followed by `[` also supports ordinary value arrays:

```
tags ["alpha", "beta"]
# equivalent to:
tags: ["alpha", "beta"]
```

The **first non-null element** determines the outer element type. Every later
non-null element must match it. Null entries preserve their positions and do
not permit incompatible non-null types:

```
users [null { name: "admin" } null]  # valid

users [ { name: "admin" } 99 ]      # invalid: object and int
tags [ "alpha" { name: "beta" } ]   # invalid: string and object
```

An all-null list is a value array with element type `null`; it has no inferred
object type. The destination native schema may still decode it into a list of
optional records.

### The empty case

A colonless `[]` has no first element to choose from. It is always an **empty
block array**:

```
panels []      # empty block array
list: []       # empty scalar array, element type "string"
```

Use the colon form when the value is a scalar array that happens to be empty.
Empty blocks are written `defaults {}`.

---

## Fields

A field is a key-value pair. The key is a bare identifier or an ordinary double-quoted string. The value is a scalar, array, or object. Named structured values normalize to blocks or block arrays as described above.

```
key: value
```

Bare keys use ASCII letters, digits, and underscores and may not start with a digit. Quote other keys using the string escapes described above.

```
accent: "green"   # valid
size_base: 13.0   # valid
"max-crashes": 3  # valid - punctuation requires quotes
true: 1           # invalid - reserved literal, never an identifier
```

---

## SKG Version

`skg_version` declares which version of the SKG language spec this file uses. It is a quoted string in `major.minor` format.

```
skg_version: "1.0"
```

Parsers accept only versions they implement. A V1 parser supporting through
`1.N` accepts declared `1.0` through `1.N`; another major and a later minor are
`UNSUPPORTED_SKG_VERSION`. This V1.0 parser therefore rejects `0.9`, `1.1` and
`2.0`. A well-formed declaration is not proof that a corresponding spec exists.

If omitted, the document uses V1.0 language semantics. This default is permanent
within V1: new V1.x syntax must require the minor version that introduced it,
so a future parser cannot reinterpret an unversioned or `1.0` document using a
later grammar. The AST keeps an omitted declaration as null; tools do not invent
a header merely because they apply V1.0 semantics.

Each imported file declares (or omits) its own language version. Mixed explicit
`1.0` and unversioned V1.0 files are valid, and an import graph is rejected if
any file declares a version the parser does not support. An imported header does
not replace or propagate into the entry file's AST. An importer does not lower
or raise a dependency's declared version.

The language version, native package/tool release, and application-owned
`schema_version` are independent. Adding another native language package for
the same SKG grammar does not change `skg_version`.

---

## Schema Version

`schema_version` declares which version of the consuming application's config schema this file targets. It is a string. The parser records it on the AST (`File.schema_version`) but does not interpret it - validation is the consuming application's responsibility.

```
schema_version: "1.0.0"
```

---

## Validation

The parser enforces:

- Correct token types
- Balanced braces and brackets
- Valid import paths: relative only, no circular imports, chain depth at most 32
- Array element type uniformity (one level deep), including block-versus-scalar
- Number literals in their one legal spelling, and within 64-bit range
- Header directives before the body, and no duplicate `skg_version` or
  `schema_version` declarations

**Semantic validation** - unknown fields, wrong types for a schema, missing required fields - is the responsibility of the consuming application. The application maps the parsed AST onto its own types and produces schema errors.

---

## AST

The parser produces a tree of nodes. Each node is one of:

| Node         | Contents                                                 |
| ------------ | -------------------------------------------------------- |
| `File`       | skg_version, imports, schema_version, children, comments |
| `Block`      | name, children, replace flag, comments                                 |
| `BlockArray` | name, items (each item is an object or null value), comments  |
| `Delete`     | key, comments; retained until finalization                |
| `Field`      | key, value, comments                                     |
| `Value`      | type (Int/Float/Bool/String/Null/Array/Object), data            |

The Go and Zig ASTs represent block-array entries as object/null values,
rather than child lists with a separate null marker. This is a pre-V1 API
change: Go callers read object children through `item.Object`; Zig callers use
`item.object.children` after checking the tag. Nested objects use the same
value representation.

Comment trivia is attached to nodes, not stored as standalone AST nodes:

- **Fields**: `leading_comments` (before the field) and `trailing_comment` (inline, same line)
- **Blocks/BlockArrays**: `leading_comments` (before the block) and `trailing_comments` (before closing delimiter)
- **File**: `leading_comments` (before first declaration) and `trailing_comments` (after last node)

The consuming application walks this tree against its own type definitions to populate its config struct.

---

## Error Messages

Errors include a stable error code, the file path, line number, column, and a clear description.

```text
theme.skg:4:3 - expected value, found end of file
dusk.skg:12:1 - circular import: dusk.skg → theme.skg → dusk.skg
dusk.skg:7:12 - string value must be quoted: use "top" not top
```

Line and column numbers are 1-based; the column counts bytes. The message wording is not part of the contract and differs between implementations. The error code is: it comes from a closed registry, and it is what conformance fixtures assert. See [conformance.md](conformance.md) for the registry and the full diagnostic contract.

---

## Full Example

```
# main application config

skg_version: "1.0"

import [
  "./theme.skg",
  "./keybinds.skg",
]

schema_version: "1.0.0"

theme {
  accent: "green"

  colors {
    background: "#0d0d0d"
    surface: "#161616"
    border: "#2a2a2a"
    border_active: "#3a3a3a"
    text: "#e5e5e5"
    text_dim: "#6b6b6b"
  }
}

# panels are ordered - first entry is primary
panels [
  {
    position: "top"
    opacity: 0.92
    height: 32.0

    zones [
      {
        alignment: "start"
        grow: false
        modules: ["workspaces", "windowlist"]
      }
      {
        alignment: "center"
        grow: true
      }
      {
        alignment: "end"
        grow: false
        modules: ["tray", "audio", "clock"]
      }
    ]
  }
  {
    position: "bottom"
    opacity: 0.85
    height: 28.0
  }
]

keybinds {
  launcher: "alt+space"
  terminal: "ctrl+t"
}

session {
  wm: "openbox"
  startup_method: "systemd"
}

logging {
  level: "info"
  max_size_mb: 5
  keep_rotations: 3
}
```
