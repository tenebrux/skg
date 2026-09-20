#!/usr/bin/env bash
set -euo pipefail

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
artifacts=$(mktemp -d)
temporary_edge_tag=
edge_worktree=
cleanup() {
  if [[ -n $temporary_edge_tag ]]; then
    git tag -d "$temporary_edge_tag" >/dev/null 2>&1 || true
  fi
  if [[ -n $edge_worktree ]]; then
    git worktree remove --force "$edge_worktree" >/dev/null 2>&1 || true
  fi
  rm -rf "$artifacts"
}
trap cleanup EXIT
vsix_out=${SKG_VSIX_OUT:-$artifacts/skg-vscode.vsix}
mkdir -p "$(dirname "$vsix_out")"
cd "$repo"

echo "[v1] frozen behavior and release metadata"
node tools/check-contract.mjs
release_args=()
if [[ -n "${SKG_RELEASE_TAG:-}" ]]; then
  release_args=(--tag "$SKG_RELEASE_TAG")
fi
node tools/check-release.mjs "${release_args[@]}"
node .github/scripts/edge-release-complete.mjs --self-test \
  "0.1.0-edge.19700101.g000000000000"
bash -n tools/v1-check.sh .github/scripts/next-version.sh .github/scripts/edge-version.sh
shellcheck tools/v1-check.sh .github/scripts/next-version.sh .github/scripts/edge-version.sh
actionlint

echo "[v1] edge release package pipeline"
edge_worktree="$artifacts/edge-release"
git worktree add --quiet --detach "$edge_worktree" HEAD
edge_tag=$("$edge_worktree/.github/scripts/edge-version.sh" 19700101 "$(git rev-parse HEAD)")
if [[ ! $edge_tag =~ ^[0-9]+\.[0-9]+\.[0-9]+-edge\.19700101\.g[0-9a-f]{12}$ ]]; then
  echo "edge tag is not semver-compatible: $edge_tag" >&2
  exit 1
fi
if git rev-parse --quiet --verify "refs/tags/$edge_tag" >/dev/null; then
  echo "temporary edge validation tag already exists: $edge_tag" >&2
  exit 1
fi
git tag "$edge_tag"
temporary_edge_tag=$edge_tag
(
  cd "$edge_worktree"
  GORELEASER_CURRENT_TAG="$edge_tag" \
    goreleaser release --clean --skip=publish --config .goreleaser.edge.yaml
)
git tag -d "$temporary_edge_tag" >/dev/null
temporary_edge_tag=
git worktree remove --force "$edge_worktree"
edge_worktree=

echo "[v1] source formatting"
# mise's rust plugin installs the minimal rustup profile, so the gate makes
# sure the rustfmt and clippy components exist before using them.
rustup component add rustfmt clippy
zig fmt --check build.zig zig/
mapfile -t go_files < <(find go -type f -name '*.go' -print | sort)
go_files+=(tools/differential.go)
unformatted=$(gofmt -l "${go_files[@]}")
if [[ -n "$unformatted" ]]; then
  printf 'Go files need formatting:\n%s\n' "$unformatted" >&2
  exit 1
fi
(
  cd rust
  cargo fmt --check
)
(
  cd examples/rust
  cargo fmt --check
)
(
  cd testdata/consumers/rust
  cargo fmt --check
)

echo "[v1] Zig debug and release-safe suites"
zig build test
zig build test -Doptimize=ReleaseSafe

echo "[v1] Go vet, race suite, and examples"
(
  cd go
  go vet ./...
  go test -race -v ./...
  go build ./...
  govulncheck ./...
)
(
  cd examples/go
  go build -o "$artifacts/go-example" .
)

echo "[v1] Rust clippy, full suites, and example"
(
  cd rust
  cargo clippy --all-targets -- -D warnings
  cargo test -- --nocapture
  cargo test --release
)
(
  cd examples/rust
  cargo clippy --locked --all-targets -- -D warnings
  cargo build --locked
)

echo "[v1] standalone native consumer projects"
(
  cd testdata/consumers/go
  go vet ./...
  go test -race ./...
)
(
  cd testdata/consumers/zig
  zig build test
  zig build test -Doptimize=ReleaseSafe
)
(
  cd testdata/consumers/rust
  cargo clippy --all-targets -- -D warnings
  cargo test
)

echo "[v1] cross-implementation generated corpus"
zig build
(
  cd go
  go run ../tools/differential.go
)

echo "[v1] formatter and examples"
mapfile -d '' example_files < <(find examples -type f -name '*.skg' -print0 | sort -z)
zig-out/bin/skg fmt --check "${example_files[@]}"
(
  cd examples/zig
  zig build
)

echo "[v1] editor grammars"
(
  cd tools/tree-sitter-skg
  npm ci
  npm audit --audit-level=high
  npm run check:fixtures
)
git diff --exit-code -- tools/tree-sitter-skg/src/
(
  cd tools/vscode-skg
  npm ci --ignore-scripts
  npm audit --audit-level=high
  npm test
  npx --no-install vsce package --out "$vsix_out"
)

echo "[v1] shipped Zig targets"
for target in x86_64-linux-musl aarch64-linux-musl x86_64-macos aarch64-macos x86_64-windows; do
  zig build -Doptimize=ReleaseSafe -Dtarget="$target"
done

echo "[v1] Go portability compile matrix"
for platform in linux/amd64 linux/arm64 linux/386 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 freebsd/amd64; do
  target_os=${platform%/*}
  target_arch=${platform#*/}
  (
    cd go
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go test -c -o "$artifacts/go-${target_os}-${target_arch}.test" .
  )
done

echo "V1 RELEASE GATE PASSED"
