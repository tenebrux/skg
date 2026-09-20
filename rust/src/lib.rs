//! # skg - native SKG (Static Key Group) for Rust
//!
//! A native V1 implementation of the SKG configuration language: parser,
//! canonical formatter, import resolution with overlay merge semantics, and
//! Serde-based typed decoding and encoding. Everything runs in process - no
//! CLI, bridge, subprocess, or schema language.
//!
//! SKG is structured data: five scalar types, arrays, objects (blocks), and
//! block arrays. The application's own types define and validate the schema.
//!
//! ```text
//! skg_version: "1.0"
//!
//! service {
//!   host: "localhost"
//!   port: 8080
//!   tags: ["edge", "cache"]
//! }
//! ```
//!
//! ## Typed usage
//!
//! ```
//! use serde::{Deserialize, Serialize};
//!
//! #[derive(Debug, Deserialize, Serialize, PartialEq)]
//! struct Config {
//!     service: Service,
//! }
//!
//! #[derive(Debug, Deserialize, Serialize, PartialEq)]
//! struct Service {
//!     host: String,
//!     port: u16,
//!     #[serde(default, skip_serializing_if = "Vec::is_empty")]
//!     tags: Vec<String>,
//! }
//!
//! let source = "service { host: \"localhost\" port: 8080 }\n";
//!
//! let config: Config = skg::from_str(source)?;
//! assert_eq!(config.service.host, "localhost");
//! assert_eq!(config.service.port, 8080);
//! assert!(config.service.tags.is_empty()); // absent, so the default applies
//!
//! // Canonical emission round-trips.
//! assert_eq!(
//!     skg::to_string(&config)?,
//!     "service {\n  host: \"localhost\"\n  port: 8080\n}\n",
//! );
//! # Ok::<(), Box<dyn std::error::Error>>(())
//! ```
//!
//! ## Dynamic documents
//!
//! ```
//! let document = skg::parse("theme { accent: \"green\" }")?;
//! assert_eq!(
//!     skg::emit(&document),
//!     "theme {\n  accent: \"green\"\n}\n",
//! );
//!
//! // Formatting normalizes source layout to the canonical form.
//! let formatted = skg::format("theme{accent:\"green\"}")?;
//! assert_eq!(formatted, "theme {\n  accent: \"green\"\n}\n");
//! # Ok::<(), skg::ParseError>(())
//! ```
//!
//! ## Import resolution
//!
//! The byte APIs never touch the filesystem. File resolution goes through an
//! explicit [`Loader`], so applications can supply in-memory or sandboxed
//! stores:
//!
//! ```
//! use skg::{Loader, MemoryLoader, ResolveOptions, resolve_with};
//!
//! let loader = MemoryLoader::new()
//!     .with_file("main.skg", "import \"base.skg\"\nhost: \"main\"\n")
//!     .with_file("base.skg", "host: \"base\"\nport: 80\n");
//!
//! let document = resolve_with("main.skg", &loader, &ResolveOptions::new())?;
//! // The importer wins and keeps the key's first position; resolved imports
//! // are omitted from emission.
//! assert_eq!(skg::emit(&document), "host: \"main\"\nport: 80\n");
//! # Ok::<(), skg::ResolveError>(())
//! ```
//!
//! See `docs/conformance.md` in the repository for the frozen V1 contract and
//! `rust/README.md` for the full Serde mapping rules and MSRV policy.

#![forbid(unsafe_code)]

mod de;
mod emit;
mod error;
mod lexer;
mod merge;
mod model;
mod parser;
mod resolve;
mod ser;

pub use de::{
    from_bytes, from_bytes_with, from_document, from_document_with, from_str, from_str_with,
    DecodeOptions, MAX_NATIVE_NESTING_DEPTH,
};
pub use emit::emit;
pub use error::{
    DecodeError, Diagnostic, EncodeError, ErrorCode, NativeCode, ParseError, Position,
    ResolveError, SourceLocation,
};
pub use merge::{materialize_nodes, merge_overlay};
pub use model::{
    Array, Block, BlockArray, Comment, CommentOrigin, Delete, Document, Field, Node, ObjectBody,
    SourceIdentity, Value, ValueType,
};
pub use parser::{is_absolute_import_path, LANGUAGE_VERSION, MAX_FILE_SIZE, MAX_NESTING_DEPTH};
pub use resolve::{
    resolve, resolve_with, FsLoader, Loader, MemoryLoader, ResolveOptions,
    DEFAULT_MAX_RESOLVE_BYTES, DEFAULT_MAX_RESOLVE_FILES, DEFAULT_MAX_RESOLVE_MERGE_WORK,
    DEFAULT_MAX_RESOLVE_NODES, MAX_IMPORT_DEPTH,
};
pub use ser::{to_document, to_string};

/// Parse SKG source into a composed overlay [`Document`].
///
/// The path in diagnostics is `<string>`. Imports are recorded in
/// [`Document::import_paths`] and never loaded - the parser performs no
/// filesystem access. Use [`resolve`] to load and merge imports.
///
/// # Errors
///
/// Returns a [`ParseError`] with a stable [`ErrorCode`] and source position.
pub fn parse(source: &str) -> Result<Document, ParseError> {
    parse_source(source, "<string>")
}

/// Parse SKG source into a composed overlay [`Document`], labelling
/// diagnostics with `path`. The path is never opened.
///
/// # Errors
///
/// Returns a [`ParseError`] with a stable [`ErrorCode`] and source position.
pub fn parse_source(source: &str, path: impl Into<String>) -> Result<Document, ParseError> {
    parser::parse_bytes(source.as_bytes(), path)
}

/// Parse SKG source bytes into a composed overlay [`Document`].
///
/// Validates UTF-8 ([`ErrorCode::InvalidUtf8`]) and the 10 MiB per-file cap
/// ([`ErrorCode::FileTooLarge`]) before parsing.
///
/// # Errors
///
/// Returns a [`ParseError`] with a stable [`ErrorCode`] and byte-accurate
/// source position.
pub fn parse_bytes(source: &[u8], path: impl Into<String>) -> Result<Document, ParseError> {
    parser::parse_bytes(source, path)
}

/// Parse `source` and emit its canonical form.
///
/// This is what a formatter does: the composed overlay keeps operations and
/// import statements, so formatting a source preserves its effect when it is
/// imported elsewhere.
///
/// # Errors
///
/// Returns a [`ParseError`] if `source` does not parse.
pub fn format(source: &str) -> Result<String, ParseError> {
    let document = parse(source)?;
    Ok(emit(&document))
}
