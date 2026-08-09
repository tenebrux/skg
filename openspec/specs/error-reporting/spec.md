# error-reporting Specification

## Purpose
TBD - created by archiving change parser-v02-improvements. Update Purpose after archive.
## Requirements
### Requirement: Stable machine-readable error codes
Every parse failure SHALL carry an error code drawn from the closed registry in `testdata/error-codes.json`. The code is the stable, implementation-independent classification of the failure; the human-readable message carries no compatibility promise and MAY be worded differently by each implementation. Conformance fixtures assert the code, never a message substring. The registry is documented in `docs/conformance.md`.

#### Scenario: Same failure, different wording, one code
- **WHEN** `skg_version: "one.zero"` is parsed by two implementations that word the failure differently
- **THEN** both report the code `MALFORMED_SKG_VERSION`, and both messages are non-empty

#### Scenario: Code is registered
- **WHEN** an implementation defines an error code
- **THEN** that code appears in `testdata/error-codes.json`, and the conformance suite fails if it does not

#### Scenario: Uncoded diagnostic is a bug
- **WHEN** a diagnostic reaches the caller without a code
- **THEN** it reports `UNKNOWN`, which no fixture is permitted to assert

### Requirement: Structured parse error diagnostics
The parser SHALL return structured error information including an error code, file path, line number, column number, and a human-readable message for every parse failure. Line and column numbers are 1-based, and the column is counted in bytes.

#### Scenario: Error on malformed field
- **WHEN** input `key "value"` is parsed (missing colon)
- **THEN** the error includes path `"test.skg"`, line `1`, column `5`, and message containing `"expected ':'"` or equivalent

#### Scenario: Error on unclosed block
- **WHEN** input `theme {\n  accent: "green"\n` is parsed (missing closing brace)
- **THEN** the error includes the line/column of the opening brace or the EOF position, and a message mentioning the unclosed block

#### Scenario: Error on unterminated string
- **WHEN** input `key: "unterminated` is parsed
- **THEN** the error includes the line/column where the string began and a message about the missing closing quote

#### Scenario: Circular import error
- **WHEN** file A imports file B which imports file A
- **THEN** the error includes the file path and line of the import statement that created the cycle, and names both files in the message

