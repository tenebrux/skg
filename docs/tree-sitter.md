# tree-sitter-skg

Tree-sitter grammar for `.skg` files. Drives syntax highlighting and
structural editing in editors that use tree-sitter: Neovim
(nvim-treesitter), Helix, Zed, Emacs (treesit.el), and others.

Source: [tools/tree-sitter-skg/](../tools/tree-sitter-skg/)

## What it covers

The grammar matches the full language spec in [spec.md](spec.md):

- `import "..."` and `import [ ... ]`
- Named blocks: `name { ... }`
- Block arrays: `name [ { ... } { ... } ]`
- Fields with colon: `key: value`
- Colonless scalar array shorthand: `tags ["a", "b"]`
- All scalar types: `int`, `float`, `bool`, `string`, `null`
- Single-line strings with escapes limited to `\"`, `\\`, `\n`, `\t`
- Triple-quoted multiline strings (`"""..."""`) with literal content
- Nested arrays
- Line comments (`#`)

Node types exposed to queries:

`import`, `block`, `block_array`, `block_array_item`, `pair`,
`scalar_array_field`, `array`, `string`, `multiline_string`, `integer`,
`float`, `boolean`, `null`, `identifier`, `comment`.

## Build

Requires Node.js and the tree-sitter CLI.

```sh
cd tools/tree-sitter-skg
npm install
npx tree-sitter generate        # regenerates src/parser.c
npx tree-sitter test            # runs grammar tests (if corpus present)
npx tree-sitter parse file.skg  # prints the parse tree for a file
```

For WASM builds (Zed, browser-based editors):

```sh
npx tree-sitter build --wasm
```

For native shared library builds (Neovim with custom parser):

```sh
npx tree-sitter build
```

## Install per editor

### Neovim (nvim-treesitter)

Register SKG as a custom parser in your Neovim config:

```lua
local parser_config = require('nvim-treesitter.parsers').get_parser_configs()
parser_config.skg = {
  install_info = {
    url = 'https://github.com/tenebrux/skg',
    files = { 'tools/tree-sitter-skg/src/parser.c' },
    branch = 'master',
    generate_requires_npm = false,
    requires_generate_from_grammar = false,
  },
  filetype = 'skg',
}

vim.filetype.add({ extension = { skg = 'skg' } })
```

Then `:TSInstall skg` and add highlight query symlinks or copy
`tools/tree-sitter-skg/queries/highlights.scm` into
`~/.config/nvim/queries/skg/`.

### Helix

In `~/.config/helix/languages.toml`:

```toml
[[language]]
name = "skg"
scope = "source.skg"
file-types = ["skg"]
roots = []
comment-token = "#"
indent = { tab-width = 2, unit = "  " }

[[grammar]]
name = "skg"
source = { git = "https://github.com/tenebrux/skg", rev = "master", subpath = "tools/tree-sitter-skg" }
```

Then `hx --grammar fetch && hx --grammar build`.

Copy `tools/tree-sitter-skg/queries/highlights.scm` to
`~/.config/helix/runtime/queries/skg/highlights.scm`.

### Zed

Build the WASM artifact (`npx tree-sitter build --wasm`) and register
the grammar via a Zed extension that references the WASM file. See the
Zed docs for extension scaffolding.

## Queries

Highlight captures are in
[tools/tree-sitter-skg/queries/highlights.scm](../tools/tree-sitter-skg/queries/highlights.scm).
They use standard capture names - any editor with sensible defaults for
`@string`, `@number`, `@keyword.import`, `@variable.member`, etc. will
pick up colors without extra configuration.

## Development

When the language spec changes, update `grammar.js`, run
`npx tree-sitter generate`, and verify `tree-sitter parse` output on
the shared fixtures in [testdata/valid/](../testdata/valid/). The
grammar must accept every file there.
