#!/usr/bin/env bash
set -euo pipefail

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
date_value=${1:-$(date -u +%Y%m%d)}
commit=${2:-${GITHUB_SHA:-$(git -C "$repo" rev-parse HEAD)}}

version=$(sed -nE 's/.*\.version = "([^"]+)".*/\1/p' "$repo/build.zig.zon")
base=${version%%-*}

if [[ ! $base =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo "invalid build.zig.zon version: $version" >&2
  exit 1
fi
if [[ ! $date_value =~ ^[0-9]{8}$ ]]; then
  echo "edge date must be YYYYMMDD: $date_value" >&2
  exit 1
fi
if [[ ! $commit =~ ^[0-9a-fA-F]{12,40}$ ]]; then
  echo "edge commit must be a 12-40 character hexadecimal Git ID" >&2
  exit 1
fi

printf '%s-edge.%s.g%s\n' "$base" "$date_value" "${commit:0:12}" | tr 'A-F' 'a-f'
