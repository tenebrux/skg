# Standalone consumer smoke tests

These projects verify SKG from outside its implementation packages, through the
same public APIs and package wiring used by applications. They deliberately have
their own Go module and Zig package manifests.

Each maintained language package must have a consumer project here before it can
claim V1 core support. A consumer must cover:

- filesystem-free source parsing and file-based import resolution;
- overlays, deletion, replacement, and canonical emission where supported;
- native structs, maps, lists, nested values, nulls, and defaults;
- custom decoding or validation hooks where supported;
- strict-mode errors with stable field paths and source provenance;
- failure without a partially committed native result; and
- installation through the language's normal dependency mechanism.

These are application-boundary smoke tests. The immutable fixtures under the
other `testdata` directories remain the normative cross-language contract.
