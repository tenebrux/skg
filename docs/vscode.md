# vscode-skg

VS Code extension for `.skg` files. Provides syntax highlighting via a
TextMate grammar plus bracket matching, comment toggling, and
auto-closing pairs.

Source: [tools/vscode-skg/](../tools/vscode-skg/)

## What it covers

The TextMate grammar covers every construct in [spec.md](spec.md). It is a
**highlighting** grammar, not a validator, and is deliberately looser than the
parsers: it colours what a construct looks like without checking that it is
legal. It does not enforce header order, reject an absolute import path, notice
that an array mixes element types, or object to `007` or `5.`. An editor that
shows no red is not a claim that `skg` will accept the file.

Constructs covered: 

- `import` keyword (single and array forms)
- Named blocks and block arrays
- Fields, colonless scalar array shorthand
- Scalars: `int`, `float`, `bool`, `string`, `null`
- Single-line strings with valid escapes (`\"`, `\\`, `\n`, `\t`);
  illegal escapes are flagged with `invalid.illegal`
- Triple-quoted multiline strings
- Nested arrays and nested block-array items
- Line comments

Extension features from `language-configuration.json`:

- `#` as line comment token
- Auto-closing `{}`, `[]`, `""`
- Indent/dedent on brace lines

## Install

Every release attaches a packaged extension built from that tag:

```sh
# from https://github.com/tenebrux/skg/releases
code --install-extension skg-vscode-<version>.vsix
```

CI also uploads a `vscode-extension` artifact on every commit, so a PR can be
tried without building anything.

To build from the repo instead (requires Node.js):

```sh
mise run plugins:vscode:install
```

The `.vsix` is a build artifact and is not committed. One used to be, and it
spent most of its life shipping a grammar older than the source beside it.

Reload the VS Code window. Any `.skg` file will activate the
extension.

## Develop

Open `tools/vscode-skg/` in VS Code, press `F5` to launch an
Extension Development Host, then open a `.skg` file. Edits to
`syntaxes/skg.tmLanguage.json` take effect on window reload
(`Ctrl+R`) in the dev host.

To verify the grammar against the shared fixtures, open files from
[testdata/valid/](../testdata/valid/) and confirm colors match
expectations (strings, keywords, numbers, identifiers all distinctly
scoped).

## Scope names

Standard TextMate scopes - themes pick them up automatically:

- `keyword.control.import.skg`
- `entity.name.section.skg` (block and block-array names)
- `variable.other.property.skg` (field keys)
- `string.quoted.double.skg`, `string.quoted.triple.skg`
- `constant.numeric.integer.skg`, `constant.numeric.float.skg`
- `constant.language.boolean.skg`, `constant.language.null.skg`
- `constant.character.escape.skg`, `invalid.illegal.escape.skg`
- `comment.line.number-sign.skg`

## Publishing

Not published to the marketplace yet. The release workflow packages the
`.vsix` and attaches it to each release; point users at the releases page.
