# Releasing SKG

Two supported paths to a release. Both end at the same place: a `vX.Y.Z`
(or `vX.Y.Z-rc.N`) tag triggers `.github/workflows/release.yml`, which
gates on the complete V1 contract, cross-compiles the `skg` CLI for five
targets, and publishes a GitHub release with checksummed archives.

## Release checklist

Before any V1 stable or release-candidate tag:

1. Classify every public change under [the V1 compatibility
   policy](compatibility.md). A breaking change requires V2.
2. Update [the changelog](../CHANGELOG.md). New normative fixtures must belong
   to a new additive V1.x contract manifest; never rewrite `v1.0.json`.
3. Run `mise run v1:check` from a clean Linux checkout. This is the same command
   used by pull requests, Cut Release, and tagged releases.
4. For a direct tag, stamp `build.zig.zon` first. Stable releases must also
   stamp both editor `package.json` and `package-lock.json` files. Run
   `node tools/check-release.mjs --tag vX.Y.Z` before pushing.
5. Prefer at least one RC before `v1.0.0`. Install its CLI and VSIX artifacts,
   and consume its Go and Zig packages from a small external project.

The gate verifies the immutable V1 corpus, both public API surfaces, Debug and
ReleaseSafe suites, Go race/vet, generated Go/Zig differential cases, standalone
Go and Zig consumer projects, examples, both editor grammars, VSIX packaging,
all shipped Zig targets, and the Go portability compile matrix. The consumer
projects have their own module/package manifests and exercise public APIs from
application code. Pull-request CI also runs the Go and Zig suites on macOS and
Windows so their resolver and formatter behavior is exercised on the host
operating system.

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

- runs the complete V1 release gate **before** tagging, so a broken tree cannot
  become a tag;
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

Equally supported after the metadata step in the checklist. The release
workflow rejects a tag that does not exactly match `build.zig.zon` and creates
the missing `go/v1.4.0` companion tag only at the same commit. If that companion
tag already exists at another commit, publishing fails.

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
