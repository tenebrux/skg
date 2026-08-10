# SKG Config for VS Code

Syntax highlighting for [SKG](https://github.com/tenebrux/skg) (`.skg`)
configuration files, plus bracket matching, comment toggling and auto-closing
pairs.

This is a **highlighting** grammar, not a validator. It colours what a construct
looks like without checking that it is legal: it will not flag an out-of-order
header, an absolute import path, an array that mixes element types, or a number
written `007`. Run `skg fmt --check` for that.

Full documentation: [docs/vscode.md](https://github.com/tenebrux/skg/blob/master/docs/vscode.md).

## Install

Download the `.vsix` from a [release](https://github.com/tenebrux/skg/releases)
and run:

```sh
code --install-extension skg-vscode-<version>.vsix
```

Or build it from the repo with `mise run plugins:vscode:install`.
