# SKG Documentation

Reference material for the SKG (Static Key Group) config language.

## Start here

- **[spec.md](spec.md)** - the language specification. Every construct,
  every rule, every escape sequence. If the parsers disagree with this
  file, the file wins.
- **[conformance.md](conformance.md)** - the cross-implementation
  contract: the stable error-code registry, the `testdata/` fixture
  format, the capability manifest, and a checklist for porting SKG to
  a new language. Read this before writing a third parser; it is meant
  to be sufficient without reading `go/` or `zig/`.

## Editor support

- **[tree-sitter.md](tree-sitter.md)** - tree-sitter grammar for
  Neovim, Helix, Zed, Emacs (treesit.el), and anything else driven by
  tree-sitter. Covers build, per-editor install, and highlight queries.
- **[vscode.md](vscode.md)** - VS Code extension (TextMate grammar,
  bracket matching, comment toggling). Covers build, install, and
  scope names for theme authors.

## Using the parsers

Pick your language and read the README in that directory:

- **[../examples/go/README.md](../examples/go/README.md)** - Go
  integration via `skg:"name"` struct tags, `UnmarshalFile`, and
  `Marshal` for round-trip.
- **[../examples/zig/README.md](../examples/zig/README.md)** - Zig
  integration via AST walker, struct defaults, and explicit field
  mapping.

Both examples read from [../examples/app.skg](../examples/app.skg) and
populate the same logical struct. Run them side by side to see the
same config in two languages.

## Example configs

More `.skg` files under [../examples/](../examples/) show real-world
config patterns in isolation:

- `app.skg` - web service config (kitchen sink: scalars, arrays,
  blocks, nested blocks, null, multiline strings, map-of-lists, bag)
- `ci-pipeline.skg` - ordered CI stages with nested steps (block
  arrays inside block arrays)
- `feature-flags.skg` - defaults + per-environment overrides + audience
  rollouts (blocks with dynamic keys)
- `servers.skg` - backend pool with health checks (block array,
  colonless array shorthand, `null` for absent optional fields)
- `users.skg` - user accounts as a block array of structured entries
- `theme.skg` - standalone theme block designed to be imported
- `main-with-imports.skg` - `import` statements in action
