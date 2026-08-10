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

`skg_version`, `schema_version` and `import` are reserved at the top level of a
file: they always introduce a directive and can never name a block or a field
there. Inside a block they are ordinary identifiers, because a block has no
header. `true`, `false` and `null` are reserved everywhere - they are value
literals, never identifiers, so they can never be used as a key.

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

Files must be UTF-8. No byte-order mark: a leading `EF BB BF` is rejected as
`UNEXPECTED_CHAR` at 1:1, because no token can start with those bytes. Beyond
that the parser is byte-transparent - it does not validate that string contents
are well-formed UTF-8, and passes the bytes through unchanged. Columns in
diagnostics count bytes, not code points.

Line endings are LF (`\n`). The parser treats `\r` as whitespace - CRLF files will parse correctly, but `\r` is stripped on round-trip through the formatter.

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

If a block or field appears twice in the same file, the second occurrence overwrites the first. This is not an error - it is the user's responsibility.

---

## Imports

Imports load and merge one or more `.skg` files before continuing to parse the current file.

Single import:

```
import "./theme.skg"
```

Multiple imports (ordered, top to bottom):

```
import [
  "./theme.skg",
  "./keybinds.skg",
]
```

Import paths are relative to the file containing the import statement - not to
the entry file, and not to the process working directory.

**Absolute import paths are rejected** (`ABSOLUTE_IMPORT_PATH`). A path is
absolute when it begins with `/` or `\`, or when it begins with a drive letter
followed by `:` (`C:\theme.skg`, `c:theme.skg`). All of those spellings are
rejected on every platform, so a file cannot mean one thing on Linux and
another on Windows. Absolute paths are not portable between machines, and an
import that escapes the config tree is a supply-chain hazard for a parser
running as root. The check happens at parse time, so the byte API rejects an
absolute import without touching the filesystem.

Circular imports are an error (`CIRCULAR_IMPORT`). The parser detects and
rejects them, comparing paths in canonical form so `./theme.skg` and
`theme.skg` are recognised as the same file.

A file reached twice by different routes through the graph (a diamond) is not a
cycle.

Import chains are followed to **32 levels** below the entry file; deeper is
`IMPORT_CHAIN_TOO_DEEP`. This is a backstop against a loop that cycle detection
cannot see - a symlink loop, say - exhausting the stack.

---

## Value Types

There are five scalar value types and one collection type. The type is determined by syntax - no type annotations.

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

Null is useful for explicitly unsetting an inherited value from an import. Null is not a valid array element - it is its own type and arrays require uniform types.

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

An ordered list of values enclosed in `[ ]`, comma-separated. All elements must be the same type. Trailing comma is allowed.

```
bindings: ["super+1", "super+2", "super+3"]

sizes: [8.0, 12.0, 16.0]
```

Type uniformity is checked one level deep: every element in an array must have the same type tag. For nested arrays, the outer array requires all elements to be arrays, but inner arrays may have different element types:

```
# valid - outer elements are both arrays
matrix: [[1, 2], [3, 4]]

# also valid - outer elements are both arrays, inner types differ
mixed: [[1, 2], ["a", "b"]]

# invalid - outer elements are mixed (int and string)
bad: [1, "two", 3]

# invalid - null is its own type, cannot mix with others
also_bad: [1, null, 3]
```

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

---

## Block Arrays

A block array is an ordered list of anonymous blocks. The syntax is `name [ { ... } { ... } ]`.

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

Each `{ }` entry in the array is an independent block with its own fields and nested blocks. Entries are ordered - position is significant. Commas between entries are optional.

Block arrays are the way to represent ordered collections of structured items - panels, zones, users, rules, etc.

When merging (via imports), a block array replaces the entire previous value - items are not merged individually.

Block arrays are distinct from scalar arrays (`[1, 2, 3]`). Scalar arrays appear as field values after a colon. Block arrays appear after an identifier without a colon, just like blocks.

A colonless identifier followed by `[` where the first element is not `{` is treated as a scalar array field:

```
tags ["alpha", "beta"]
# equivalent to:
tags: ["alpha", "beta"]
```

That choice is made **once, from the first element**. After the first element
the collection has committed to one kind, and mixing the other kind into it is
`MIXED_ARRAY_TYPES` - the same rule that forbids `[1, "two"]`:

```
# invalid - a block array cannot hold a scalar
users [ { name: "admin" } 99 ]

# invalid - a scalar array cannot hold a block
tags [ "alpha" { name: "beta" } ]
```

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

A field is a key-value pair. The key is an unquoted identifier. The value is one of the scalar types or an array.

```
key: value
```

Keys may contain letters, digits, and underscores. Keys may not start with a digit. Keys are never quoted, so a name outside that alphabet has no spelling in the language at all.

```
accent: "green"   # valid
size_base: 13.0   # valid
max-crashes: 3    # invalid - hyphens not allowed in keys
true: 1           # invalid - reserved literal, never an identifier
```

---

## SKG Version

`skg_version` declares which version of the SKG language spec this file uses. It is a quoted string in `major.minor` format.

```
skg_version: "1.0"
```

Parsers must reject files declaring an `skg_version` newer than the parser supports. A file declaring `skg_version: "1.1"` will fail to parse on a parser that only supports `1.0`. This ensures files don't silently lose meaning when parsed by an older tool.

If omitted, the parser accepts the file without a version check.

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
| `Block`      | name, children, comments                                 |
| `BlockArray` | name, items (each item is a list of children), comments  |
| `Field`      | key, value, comments                                     |
| `Value`      | type (Int/Float/Bool/String/Null/Array), data            |

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
