# Releasing SKG

Two supported paths to a release. Both end at the same place: a `vX.Y.Z`
(or `vX.Y.Z-rc.N`) tag triggers `.github/workflows/release.yml`, which
gates on the full test suite, cross-compiles the `skg` CLI for five
targets, and publishes a GitHub release with checksummed archives.

## Path 1: the Cut Release workflow (recommended)

Actions → **Cut Release** → pick a level → Run.

| Level | Effect (from latest stable `vX.Y.Z`) |
|---|---|
| `major` / `minor` / `patch` | Straight stable release: `v(X+1).0.0` / `vX.(Y+1).0` / `vX.Y.(Z+1)` |
| `rc-major` / `rc-minor` / `rc-patch` | Opens an RC series for that bump: e.g. `v2.0.0-rc.1` |
| `rc-next` | Next candidate in the open series: `-rc.2`, `-rc.3`, … |
| `rc-promote` | Ships the version the series was testing: `v2.0.0-rc.3` → `v2.0.0` |

The typical major flow: `rc-major` → fix → `rc-next` (repeat as needed) →
`rc-promote`. Only one RC series can be open at a time; the workflow
refuses to open a second and tells you so. A stable release "past" an
open series (e.g. shipping a `patch` while a major RC is cooking) is
allowed and closes nothing — the series stays iterable.

Cut Release also:

- runs the full Zig + Go suite **before** tagging, so a broken tree
  can't become a tag;
- stamps the version into `build.zig.zon` (and, for stable releases
  only, `tools/vscode-skg/package.json` + `tools/tree-sitter-skg/package.json`
  — the VS Code Marketplace rejects `-rc.N` versions);
- pushes the `go/vX.Y.Z` companion tag (see below);
- for `v2+`, refuses to release until `go/go.mod`'s module path carries
  the `/vN` suffix Go's semantic import versioning requires.

Version arithmetic lives in [.github/scripts/next-version.sh](../.github/scripts/next-version.sh)
— runnable locally (`.github/scripts/next-version.sh rc-next`) to preview
what a level would produce.

## Path 2: direct tagging

```bash
git tag -a v1.4.0 -m "skg v1.4.0"
git push origin v1.4.0
```

Equally supported. The release workflow will:

- warn (not fail) if the tag doesn't match `build.zig.zon`'s version;
- create the missing `go/v1.4.0` companion tag for you.

Use this when you need a release exactly at a specific commit, or when
the Actions UI is the wrong tool. RC tags work the same way
(`v2.0.0-rc.1`) and publish as prereleases.

## Why two tags per release?

The Go parser is a subdirectory module (`github.com/tenebrux/skg/go`).
Go tooling can only resolve a subdirectory module at a version whose tag
is prefixed with the directory: `go/vX.Y.Z`. The bare `vX.Y.Z` tag is
for everything else (the GitHub release, Zig consumers pinning
`build.zig.zon` URLs, humans). The `go/` tags never trigger workflows —
the `v*` filter doesn't cross `/`.

## RC semantics

- RC tags publish as **prereleases** and never move the `latest` release
  pointer. Consumers on `@latest` (Go or GitHub) never see an RC unless
  they ask for it (`go get github.com/tenebrux/skg/go@v2.0.0-rc.1`).
- Promotion re-tags the same intent, not the same commit: `rc-promote`
  releases whatever master is at promotion time. If commits landed since
  the last RC that should have been candidate-tested, cut one more
  `rc-next` first.

## Major releases and Go

From `v2.0.0` on, Go requires the module path to end in `/vN`
(`module github.com/tenebrux/skg/go/v2`). That's a source change that
must land **before** cutting the major — Cut Release enforces it. Plan
it as part of the major's final RC.
