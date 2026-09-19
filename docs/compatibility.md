# V1 compatibility policy

SKG language version 1.0 is frozen by the shared conformance corpus. The
package-level compatibility promise below begins with the first `v1.0.0`
release. Until that tag exists, the repository is a V1 release candidate and
the package metadata remains `0.x`.

## The 1.x promise

For the lifetime of package major version 1:

- A document accepted as SKG 1.0 remains accepted and produces the same
  materialized data. An unversioned document always uses 1.0 semantics.
- Existing canonical formatter output stays byte-identical for the frozen V1.0
  corpus. The documented comment-placement limits remain part of that contract.
- Existing parser and native-decoder error codes retain their meaning. Human
  messages may improve and filesystem paths may reflect the host.
- Exported Go, Zig and Rust V1 declarations, signatures, enum tags, and public
  struct fields remain source compatible. Existing Go structs do not gain
  fields; additive options use a new type or API. Zig structs may gain only
  defaulted fields whose omission preserves existing source and behavior. Rust
  public items remain compatible; additive options extend existing option
  structs with defaulted fields only when that preserves source and behavior.
- Default syntax, import-depth, file, aggregate-resolution, merge-work, and
  native-recursion limits will not be lowered.
- Native mappings, null/default rules, strict numeric conversion, transactional
  decoding, and the default policy of ignoring unknown configuration fields
  remain available.
- Parsing source bytes remains filesystem-free. File APIs continue to resolve
  relative imports and apply overlays before native decoding.

The compatibility gate compiles standalone downstream-style projects for each
public package and runs their application-level workflows. It also verifies
hashes for every V1.0 normative fixture. The 1.0 syntax, value model, error
registry, and canonical representation remain frozen throughout package 1.x. A
later `skg_version` feature is allowed in V1 only when it can be represented
without changing an existing public declaration and when its new fixtures live
in a separate additive contract manifest. Otherwise it waits for V2.
Unversioned input always remains 1.0.

## Additions allowed in 1.x

Compatible additions include new language packages, new APIs, new native target
mappings, opt-in options, editor integrations, and new syntax behind a later V1
minor language declaration. Each language package must implement the V1 core
in process: byte parsing, import resolution and overlays, and strict decoding
into native static types. A package cannot claim conformance while delegating
those features to a command-line binary, bridge, evaluator service, or separate
schema language.

Deprecated V1 APIs remain present throughout 1.x. Documentation may direct new
code to a better additive API, but removal waits for V2.

## Changes reserved for V2

A V2 release is required to remove or rename a public declaration, alter an
existing function signature or public field type, reinterpret accepted V1
source, change existing canonical bytes, reuse an error code for another
condition, lower a default limit, weaken the byte/filesystem boundary, or alter
an established native mapping or default/null rule.

## Supported toolchains and platforms

| Component | V1 build contract | Gate coverage |
| --- | --- | --- |
| Zig package and CLI | Zig **0.15.2** exactly | Debug and ReleaseSafe tests on Linux; ReleaseSafe runtime suites on macOS and Windows; Linux musl x86-64/ARM64, macOS x86-64/ARM64, Windows x86-64 cross-builds |
| Go package | Go **1.26 or newer** | Race tests on Linux 1.26.8; runtime suites on macOS and Windows; compile checks for Linux x86-64/ARM64/386, macOS x86-64/ARM64, Windows x86-64/ARM64, FreeBSD x86-64; CI also tests Go 1.27 |
| Rust package | Stable Rust **1.71 or newer** (`rust-version` in `rust/Cargo.toml`) | Debug and release suites on Linux 1.98 including the shared corpora; runtime suites on macOS and Windows; CI also compiles and tests on exactly MSRV 1.71 |
| Editor and contract tools | Node.js **24 LTS** | tree-sitter corpus, TextMate corpus/probes, VSIX packaging, contract locks |

Zig is pre-1.0 and regularly changes source APIs, so V1 supports the exact
compiler named above. Updating that compiler in a 1.x package release is
allowed only when existing SKG source, package APIs, and contract behavior stay
compatible. Go and Zig packages have no runtime dependency on Node or on each
other.

## Deliberate boundaries

SKG has no expressions, interpolation, environment lookup, remote imports,
schema DSL, code generation requirement, or general constraint evaluator.
Applications express cross-field rules in ordinary native validation hooks.

The Go parser does not retain comments. The Zig formatter retains comment text
under its documented placement rules; see [formatter.md](formatter.md). Go
rooted resolution opens files through `os.Root`, so path traversal and symlink
replacement cannot escape the opened root. Zig rooted resolution enforces
containment after canonical path resolution and retains a final path-use race;
use process-level isolation when the configuration directory is controlled by
an active adversary. The formatter's strongest metadata guarantee is on Linux;
macOS/BSD extended attributes and ACLs, Windows DACLs and alternate streams,
and hard-linked files remain outside the portable in-place guarantee.

Release archives and the VSIX include SHA-256 checksum files. V1 does not yet
promise signed artifacts or build-provenance attestations; verify downloads
against the checksums published in the same GitHub release.

Run the complete release gate from a Linux checkout with:

```sh
mise run v1:check
```
