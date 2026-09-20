//! Shared resolution-corpus runner (`testdata/resolution/cases.json`).
//!
//! Every case builds an isolated temporary file tree and runs through the
//! option-bearing file API with explicit budgets, pinning exact limits, stable
//! limit codes, rooted containment, and resolved merge output.

use std::path::{Path, PathBuf};

use serde::Deserialize;
use serde_json::Value as Json;

use skg::{emit, resolve_with, FsLoader, ResolveError, ResolveOptions};

#[derive(Debug, Deserialize)]
struct Limits {
    bytes: u64,
    files: usize,
    nodes: usize,
    merge_work: u64,
}

#[derive(Debug, Deserialize)]
struct FileSpec {
    path: String,
    source: String,
}

#[derive(Debug, Deserialize)]
struct Case {
    name: String,
    entry: String,
    #[serde(default)]
    root: Option<String>,
    limits: Limits,
    files: Vec<FileSpec>,
    #[serde(default)]
    expected_formatted: Option<String>,
    #[serde(default)]
    expected_code: Option<String>,
}

fn repo_root() -> PathBuf {
    std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("manifest dir has a parent")
        .to_path_buf()
}

/// Join a fixture's slash-separated path onto the temp tree, refusing
/// anything that escapes it.
fn safe_path(base: &Path, slash_path: &str) -> PathBuf {
    assert!(!slash_path.is_empty(), "empty contract path");
    let mut out = base.to_path_buf();
    for component in Path::new(slash_path).components() {
        match component {
            std::path::Component::Normal(part) => out.push(part),
            std::path::Component::RootDir => {}
            other => panic!("unsafe resolution contract path {slash_path:?}: {other:?}"),
        }
    }
    assert!(
        out.starts_with(base),
        "unsafe resolution contract path {slash_path:?}"
    );
    out
}

#[test]
fn resolution_conformance() {
    let raw = std::fs::read_to_string(repo_root().join("testdata/resolution/cases.json"))
        .expect("testdata/resolution/cases.json is readable");
    let suite: Json = serde_json::from_str(&raw).expect("resolution suite is valid JSON");
    let object = suite.as_object().expect("resolution suite is an object");
    assert_eq!(
        object.get("contract_version").and_then(Json::as_i64),
        Some(1),
        "resolution contract_version must be 1"
    );
    assert_eq!(
        object.get("language_version").and_then(Json::as_str),
        Some("1.0"),
        "resolution language_version must be 1.0"
    );

    let cases: Vec<Case> =
        serde_json::from_value(object.get("cases").cloned().expect("cases is required"))
            .expect("resolution cases decode");
    assert!(!cases.is_empty(), "resolution suite is empty");

    let mut seen = std::collections::BTreeSet::new();
    let codes = load_registry_codes();
    for case in &cases {
        assert!(!case.name.is_empty(), "case name is empty");
        assert!(
            seen.insert(&case.name),
            "duplicate resolution case {}",
            case.name
        );
        assert_eq!(
            case.expected_formatted.is_some(),
            case.expected_code.is_none(),
            "{}: exactly one of expected_formatted or expected_code",
            case.name
        );
        if let Some(code) = &case.expected_code {
            assert!(
                codes.contains(code),
                "{}: unregistered error code {code:?}",
                case.name
            );
        }
        assert!(
            case.limits.bytes > 0
                && case.limits.files > 0
                && case.limits.nodes > 0
                && case.limits.merge_work > 0,
            "{}: limits must be positive",
            case.name
        );
        assert!(!case.files.is_empty(), "{}: no files", case.name);
    }

    for case in &cases {
        run_case(case);
    }
    eprintln!(
        "resolution contract v1: {} cases, none skipped",
        cases.len()
    );
}

fn load_registry_codes() -> std::collections::BTreeSet<String> {
    let raw = std::fs::read_to_string(repo_root().join("testdata/error-codes.json"))
        .expect("error-codes.json is readable");
    let json: Json = serde_json::from_str(&raw).expect("registry is valid JSON");
    json.get("codes")
        .and_then(Json::as_array)
        .expect("codes is an array")
        .iter()
        .map(|entry| {
            entry
                .get("code")
                .and_then(Json::as_str)
                .expect("code")
                .to_string()
        })
        .collect()
}

fn run_case(case: &Case) {
    let base = tempfile_tree(case);
    let mut options = ResolveOptions {
        root: case.root.as_ref().map(|root| safe_path(&base, root)),
        max_bytes: case.limits.bytes,
        max_files: case.limits.files,
        max_nodes: case.limits.nodes,
        max_merge_work: case.limits.merge_work,
    };
    if case.root.is_none() {
        options.root = None;
    }

    let _entry = safe_path(&base, &case.entry);
    match resolve_with(&_entry, &FsLoader, &options) {
        Err(ResolveError::Resolution(diagnostic)) if case.expected_code.is_some() => {
            assert_eq!(
                case.expected_code.as_deref(),
                Some(diagnostic.code.as_str()),
                "{}: code mismatch (message: {})",
                case.name,
                diagnostic.message
            );
            // Import-resolution failures are reported at the import statement
            // that named the file (1-based position); only a failure on the
            // entry file itself, which no import statement named, reports 0:0.
            assert!(
                (diagnostic.line == 0) == (diagnostic.col == 0),
                "{}: half-positioned diagnostic",
                case.name
            );
            assert!(
                !diagnostic.message.is_empty(),
                "{}: empty message",
                case.name
            );
            assert!(!diagnostic.path.is_empty(), "{}: empty path", case.name);
        }
        Err(ResolveError::Parse(diagnostic)) if case.expected_code.is_some() => {
            panic!(
                "{}: expected {}, got parse error {} ({})",
                case.name,
                case.expected_code.as_deref().unwrap_or_default(),
                diagnostic.code,
                diagnostic.message
            );
        }
        Err(error) => panic!("{}: unexpected resolution failure: {error}", case.name),
        Ok(document) => {
            let Some(expected) = &case.expected_formatted else {
                panic!(
                    "{}: expected {} but resolution succeeded",
                    case.name,
                    case.expected_code.as_deref().unwrap_or_default()
                );
            };
            assert!(
                document.imports_resolved,
                "{}: imports not marked resolved",
                case.name
            );
            let got = emit(&document);
            assert_eq!(
                expected, &got,
                "{}: resolved output mismatch\nwant:\n{expected}\ngot:\n{got}",
                case.name
            );
        }
    }
}

fn tempfile_tree(case: &Case) -> PathBuf {
    let base = std::env::temp_dir().join(format!(
        "skg-rust-resolution-{}-{:p}",
        case.name.replace('/', "_"),
        case
    ));
    let _ = std::fs::remove_dir_all(&base);
    for file in &case.files {
        let path = safe_path(&base, &file.path);
        assert!(
            file.path
                .starts_with(|c: char| c.is_ascii_alphanumeric() || c == '_'),
            "file paths must stay inside the tree: {}",
            file.path
        );
        std::fs::create_dir_all(path.parent().expect("path has a parent"))
            .expect("create fixture directory");
        std::fs::write(&path, &file.source).expect("write fixture file");
    }
    base
}
