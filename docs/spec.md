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

A `.skg` file consists of, in order:

1. An optional `skg_version` declaration
2. Zero or more `import` statements
3. An optional `schema_version` declaration
4. Zero or more blocks and fields

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

Files must be UTF-8. No byte-order mark. Line endings are LF (`\n`). The parser treats `\r` as whitespace - CRLF files will parse correctly, but `\r` is stripped on round-trip through the formatter.

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

Import paths are relative to the file containing the import statement.

Circular imports are an error. The parser detects and rejects them.

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

A trailing zero after the decimal is required. `13` is an int. `13.0` is a float.

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

Block arrays may be empty:

```
panels []
```

When merging (via imports), a block array replaces the entire previous value - items are not merged individually.

Block arrays are distinct from scalar arrays (`[1, 2, 3]`). Scalar arrays appear as field values after a colon. Block arrays appear after an identifier without a colon, just like blocks.

A colonless identifier followed by `[` where the first element is not `{` is treated as a scalar array field:

```
tags ["alpha", "beta"]
# equivalent to:
tags: ["alpha", "beta"]
```

---

## Fields

A field is a key-value pair. The key is an unquoted identifier. The value is one of the scalar types or an array.

```
key: value
```

Keys may contain letters, digits, and underscores. Keys may not start with a digit.

```
accent: "green"   # valid
size_base: 13.0   # valid
max-crashes: 3    # invalid - hyphens not allowed in keys
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
- Valid import paths (no circular imports)
- Array element type uniformity (one level deep)
- No duplicate `skg_version` or `schema_version` declarations

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
