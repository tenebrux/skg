#!/usr/bin/env bash
set -euo pipefail

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
artifacts=$(mktemp -d)
trap 'rm -rf "$artifacts"' EXIT
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
bash -n tools/v1-check.sh .github/scripts/next-version.sh
shellcheck tools/v1-check.sh .github/scripts/next-version.sh
actionlint

echo "[v1] source formatting"
zig fmt --check build.zig zig/
mapfile -t go_files < <(find go -type f -name '*.go' -print | sort)
go_files+=(tools/differential.go)
unformatted=$(gofmt -l "${go_files[@]}")
if [[ -n "$unformatted" ]]; then
  printf 'Go files need formatting:\n%s\n' "$unformatted" >&2
  exit 1
fi

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
