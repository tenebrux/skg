# Cross-implementation parity

`mise run v1:parity` (part of `mise run v1:check`) builds every SKG
implementation registered in [`implementations.json`](implementations.json),
runs one deterministic generated corpus through each implementation's public
API, and fails on any disagreement. The run also fails when a
`*/conformance.json` manifest in the repository has no registered harness, so
a new language joins parity before it can claim conformance.

The shared fixtures under `testdata/` pin expected behavior. Parity adds the
inputs no fixture names: mutations, comments between every token pair, CRLF
variants, generated import graphs under tightened budgets, symlink containment
layouts and single-fault native inputs.

## What is compared

| Mode | Corpus | Compared |
| --- | --- | --- |
| `parse` | Every `testdata/**/*.skg`, its CRLF variant, a `#c` comment inserted before each token of every valid fixture, and seeded mutations | `ok`, error `code`, `file`, `line`, `col`; canonical bytes between implementations declaring `emit` (for input containing `#`, only between implementations that also declare `comments`) |
| `resolve` | Generated import graphs with overlays, diamonds, cycles and missing files at V1 defaults and three tightened budget sets; rooted and unrooted symlink containment layouts; every directory fixture | same as `parse` |
| `native` | Single-fault inputs for every profile in [`docs/native-conformance.md`](../../docs/native-conformance.md) under each option combination | `ok`, `code`, `field_path`, and decoded values structurally (floats as binary64) |

Native inputs carry one fault each, because the native contract leaves the
precedence of independent errors unspecified. Two documented host policies are
excluded rather than compared: Zig strings are byte slices that accept
integer arrays, and Rust decodes a single-entry block as a data-carrying enum
variant, so an unknown tag written that way is `invalid_enum` there.

## Harness protocol

A harness is a small program beside this README that depends on its
implementation through the language's ordinary package manifest. It is
invoked as:

```
<harness> <cases.json> <results.jsonl>
```

`cases.json` is `{"mode": "...", "cases": [...]}`. Case fields by mode:

- `parse`: `id`, `path`. Read the file's bytes and parse them through the byte
  API, labelling diagnostics with the file's base name.
- `resolve`: `id`, `entry`, and optional `root`, `max_bytes`, `max_files`,
  `max_nodes`, `max_merge_work`. Resolve `entry` through the file API. An absent
  or zero limit means the V1 default; an absent `root` means no containment.
- `native`: `id`, `profile`, `source`, and optional `reject_unknown_fields`,
  `allow_lossy_numbers`. Decode `source` through the byte-based typed loader
  into the profile's native binding, exactly as the shared native runner binds
  it.

Write one JSON object per case and line, in any order:

- success: `{"id", "ok": true, "emit": "<canonical text>"}` for `parse` and
  `resolve`, or `{"id", "ok": true, "value": <decoded value>}` for `native`;
- failure: `{"id", "ok": false, "code", "file", "line", "col"}`, with `file`
  the diagnostic path's base name, or `{"id", "ok": false, "code",
  "field_path"}` for `native`.

Native values use the fixture representation: the `bytes` profile as an
integer array, records as `{"id", "note"}`, absent or null optionals as
`null`, and floats widened to binary64. A harness exits nonzero only for its
own failures, never for an SKG error, which is a result.

## Adding a language

1. Add `<lang>/conformance.json` as described in
   [`docs/conformance.md`](../../docs/conformance.md#6-capability-manifest).
2. Add a harness directory here with the language's normal project manifest
   and a lock file where the ecosystem has one.
3. Register it in `implementations.json`: `build` runs in `dir` and must place
   the program at `binary`; `{work}` expands to the run's temporary directory.
4. Add the harness's format and lint checks to `tools/v1-check.sh`.

Run `go run . -keep` from this directory to keep the generated corpus and every
harness's results for inspection.
