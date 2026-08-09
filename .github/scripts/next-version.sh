#!/usr/bin/env bash
# next-version.sh <level>
#
# Computes the next release version from the latest v* tags.
#   Levels:
#     major | minor | patch   straight stable release
#     rc-major | rc-minor | rc-patch
#                             start a new RC series for the next X / X.Y / X.Y.Z
#     rc-next                 next rc.N in the current RC series
#     rc-promote              stable release of the version the RC series tested
#
# Prints the bare version (no leading v) on stdout. Everything else on stderr.
set -euo pipefail

level="${1:?usage: next-version.sh <level>}"

# Latest stable tag (vX.Y.Z) and latest RC tag (vX.Y.Z-rc.N), by version order.
latest_stable=$(git tag --list 'v[0-9]*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 || true)
latest_rc=$(git tag --list 'v[0-9]*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+$' | sort -V | tail -1 || true)

# No stable tag yet: seed from build.zig.zon so the first cut continues the
# version the tree already claims instead of restarting at 0.0.0.
if [ -z "$latest_stable" ]; then
  seed=$(grep -oE '\.version = "[0-9]+\.[0-9]+\.[0-9]+"' build.zig.zon | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
  echo "no stable v* tag found; seeding from build.zig.zon ($seed)" >&2
  latest_stable="v${seed}"
fi

IFS=. read -r maj min pat <<<"${latest_stable#v}"

bump() {
  case "$1" in
    major) echo "$((maj + 1)).0.0" ;;
    minor) echo "${maj}.$((min + 1)).0" ;;
    patch) echo "${maj}.${min}.$((pat + 1))" ;;
  esac
}

# An RC series is "open" if the latest RC sorts after the latest stable tag
# (i.e. it hasn't been promoted or abandoned by a newer stable release).
rc_open=""
if [ -n "$latest_rc" ]; then
  rc_base="${latest_rc#v}"; rc_base="${rc_base%%-rc.*}"
  newest=$(printf '%s\n%s\n' "${latest_stable#v}" "$rc_base" | sort -V | tail -1)
  [ "$newest" = "$rc_base" ] && [ "$rc_base" != "${latest_stable#v}" ] && rc_open="$latest_rc"
fi

case "$level" in
  major|minor|patch)
    bump "$level"
    ;;
  rc-major|rc-minor|rc-patch)
    if [ -n "$rc_open" ]; then
      echo "error: RC series $rc_open is already open — use rc-next or rc-promote (or release past it with a stable level)" >&2
      exit 1
    fi
    echo "$(bump "${level#rc-}")-rc.1"
    ;;
  rc-next)
    if [ -z "$rc_open" ]; then
      echo "error: no open RC series to iterate — start one with rc-major/rc-minor/rc-patch" >&2
      exit 1
    fi
    n="${rc_open##*-rc.}"
    base="${rc_open#v}"; base="${base%%-rc.*}"
    echo "${base}-rc.$((n + 1))"
    ;;
  rc-promote)
    if [ -z "$rc_open" ]; then
      echo "error: no open RC series to promote" >&2
      exit 1
    fi
    base="${rc_open#v}"
    echo "${base%%-rc.*}"
    ;;
  *)
    echo "error: unknown level '$level'" >&2
    exit 1
    ;;
esac
