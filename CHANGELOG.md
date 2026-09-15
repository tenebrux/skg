# Changelog

This project follows semantic versioning for packages and explicit
`skg_version` declarations for language semantics.

## Unreleased — V1 candidate

- Froze SKG language 1.0 syntax, scalar ranges, arrays, arbitrary quoted keys,
  nested object values, null behavior, duplicate-key overlays, `@delete`, and
  `@replace` in a shared immutable corpus.
- Added native static-type decoding for Go and Zig with strict conversions,
  defaults, optionals, maps, arrays, enums, custom hooks, validation, structured
  diagnostics, and transactional failure behavior.
- Bounded import graphs by bytes, canonical files, nodes/values, merge work, and
  depth; added opt-in rooted containment and stable resource error codes.
- Hardened canonical formatting with atomic replacement, concurrent-change and
  hard-link rejection, Linux metadata preservation, and an explicit trivia
  contract.
- Added versioned core-capability manifests, public API compatibility checks,
  generated Go/Zig differential coverage, scheduled parser/merge/resolver
  fuzzing, and executable tree-sitter and VS Code grammar gates.
- Added one reproducible V1 release gate, pinned build tools, cross-platform
  compile and runtime checks, immutable CI action references, dependency update
  automation, and an explicit 1.x compatibility policy.
- Added standalone Go and Zig consumer projects to the release gate, exercising
  public package installation, file resolution, native types, hooks,
  diagnostics, canonical output, and failure behavior from application code.
- Hardened the V1 candidate after cross-platform review: safe rooted Go file
  opens, stable cached import depth, cached reflection metadata, consistent
  null validation, raw-AST delete handling, nested-array formatting, FIFO
  rejection, and reliable Windows formatter metadata queries.
- Expanded differential generation across object, array, overlay, replacement,
  and quoted-key shapes, and pinned CI to LF fixtures and a macOS image supported
  by the V1 Zig compiler.
- Removed the committed editor `node_modules` tree and its host-specific binary;
  locked clean installs now reproduce all editor build dependencies.
