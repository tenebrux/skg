//! Shared-corpus conformance runner for valid, invalid, and emit fixtures.
//!
//! Everything asserted here comes from the shared `testdata/` tree, so the
//! Rust parser is held to the same description of correct behaviour as the Go
//! and Zig parsers. The rules enforced here are specified in
//! `docs/conformance.md`; a new implementation should be portable from that
//! document alone.
//!
//! Like the reference runners, this file:
//!
//! 1. enumerates fixtures from disk (nothing is hardcoded);
//! 2. strictly validates every `expected.json` against the per-object
//!    allowlist - an unknown or misspelled key is a hard failure;
//! 3. enforces the capability rules in `rust/conformance.json`, including a
//!    loud summary of any skipped fixture.

use std::collections::{BTreeMap, BTreeSet, HashMap};
use std::fmt::Write as _;
use std::path::{Path, PathBuf};

use serde::Deserialize as _;
use serde_json::Value as Json;

use skg::{
    emit, is_absolute_import_path, parse_bytes, resolve_with, Diagnostic, Document, ErrorCode,
    FsLoader, Node, ParseError, ResolveOptions, Value, ValueType,
};

const CONTRACT_VERSION: i64 = 1;
const LANGUAGE_VERSION: &str = "1.0";
const KNOWN_CAPABILITIES: [&str; 5] = ["parse", "emit", "imports", "native", "comments"];
const MANDATORY_CAPABILITIES: [&str; 3] = ["parse", "imports", "native"];

fn repo_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("manifest dir has a parent")
        .to_path_buf()
}

fn testdata_dir() -> PathBuf {
    repo_root().join("testdata")
}

// ─── Capability manifest ────────────────────────────────────────────────────

fn load_manifest() -> BTreeMap<String, bool> {
    let raw =
        std::fs::read_to_string(Path::new(env!("CARGO_MANIFEST_DIR")).join("conformance.json"))
            .expect("rust/conformance.json is readable");
    let json: Json = serde_json::from_str(&raw).expect("rust/conformance.json is valid JSON");
    let object = json.as_object().expect("manifest is an object");
    let contract_version = object
        .get("contract_version")
        .and_then(Json::as_i64)
        .expect("contract_version is an integer");
    let language_version = object
        .get("language_version")
        .and_then(Json::as_str)
        .expect("language_version is a string");
    let implementation = object
        .get("implementation")
        .and_then(Json::as_str)
        .expect("implementation is a string");
    assert_eq!(
        contract_version, CONTRACT_VERSION,
        "contract_version must be {CONTRACT_VERSION}"
    );
    assert_eq!(
        language_version, LANGUAGE_VERSION,
        "language_version must be {LANGUAGE_VERSION}"
    );
    assert!(!implementation.is_empty(), "implementation is required");

    let capabilities = object
        .get("capabilities")
        .expect("capabilities is required");
    let mut manifest = BTreeMap::new();
    for name in KNOWN_CAPABILITIES {
        let declared = capabilities
            .get(name)
            .and_then(Json::as_bool)
            .unwrap_or_else(|| panic!("capability {name} must be declared true or false"));
        manifest.insert(name.to_string(), declared);
    }
    let unknown: Vec<_> = capabilities
        .as_object()
        .expect("capabilities is an object")
        .keys()
        .filter(|k| !KNOWN_CAPABILITIES.contains(&k.as_str()))
        .collect();
    assert!(unknown.is_empty(), "unknown capabilities: {unknown:?}");
    for name in MANDATORY_CAPABILITIES {
        assert!(manifest[name], "core capability {name} is mandatory");
    }
    manifest
}

fn capability_for(
    manifest: &BTreeMap<String, bool>,
    is_dir: bool,
    has_formatted: bool,
    asserts_comments: bool,
) -> Option<&'static str> {
    if is_dir && !manifest["imports"] {
        return Some("imports");
    }
    if has_formatted && !manifest["emit"] {
        return Some("emit");
    }
    if asserts_comments && !manifest["comments"] {
        return Some("comments");
    }
    None
}

// ─── Error-code registry ────────────────────────────────────────────────────

fn load_error_codes() -> BTreeSet<String> {
    let raw = std::fs::read_to_string(testdata_dir().join("error-codes.json"))
        .expect("testdata/error-codes.json is readable");
    let json: Json = serde_json::from_str(&raw).expect("registry is valid JSON");
    let codes = json
        .get("codes")
        .and_then(Json::as_array)
        .expect("codes is an array");
    assert!(!codes.is_empty(), "registry is empty");
    let mut set = BTreeSet::new();
    for entry in codes {
        let code = entry
            .get("code")
            .and_then(Json::as_str)
            .expect("code is a string");
        let summary = entry
            .get("summary")
            .and_then(Json::as_str)
            .expect("summary is a string");
        assert!(!code.is_empty(), "entry with empty code");
        assert!(!summary.is_empty(), "code {code} has no summary");
        assert!(set.insert(code.to_string()), "duplicate code {code}");
    }
    // Every code this implementation knows about must be registered.
    for code in ErrorCode::all() {
        assert!(
            set.contains(code.as_str()),
            "ErrorCode {} is not in testdata/error-codes.json",
            code.as_str()
        );
    }
    set
}

// ─── Fixture discovery ──────────────────────────────────────────────────────

#[derive(Debug)]
struct Fixture {
    name: String,
    is_dir: bool,
    entry: PathBuf,
    expected: PathBuf,
    formatted: Option<PathBuf>,
}

fn discover_fixtures(subdir: &str) -> Vec<Fixture> {
    let dir = testdata_dir().join(subdir);
    let entries = std::fs::read_dir(&dir).unwrap_or_else(|e| panic!("read {dir:?}: {e}"));
    let mut names: Vec<_> = entries
        .map(|e| {
            e.expect("directory entry")
                .file_name()
                .to_string_lossy()
                .into_owned()
        })
        .collect();
    names.sort();

    let mut fixtures = Vec::new();
    for name in names {
        let path = dir.join(&name);
        if path.is_dir() {
            let entry = path.join("main.skg");
            let expected = path.join("expected.json");
            assert!(
                entry.is_file(),
                "{subdir}/{name}: directory fixture has no main.skg"
            );
            assert!(
                expected.is_file(),
                "{subdir}/{name}: directory fixture has no expected.json"
            );
            let formatted = path.join("formatted.skg");
            fixtures.push(Fixture {
                name,
                is_dir: true,
                entry,
                expected,
                formatted: formatted.is_file().then_some(formatted),
            });
            continue;
        }
        if name.ends_with(".formatted.skg") || name.ends_with(".expected.json") {
            // Sidecar of another fixture; validated with its owner.
            continue;
        }
        let Some(base) = name.strip_suffix(".skg") else {
            panic!("{subdir}/{name}: unrecognised file in fixture tree");
        };
        let expected = dir.join(format!("{base}.expected.json"));
        assert!(
            expected.is_file(),
            "{subdir}/{name}: fixture has no {base}.expected.json"
        );
        let formatted = dir.join(format!("{base}.formatted.skg"));
        fixtures.push(Fixture {
            name: base.to_string(),
            is_dir: false,
            entry: path,
            expected,
            formatted: formatted.is_file().then_some(formatted),
        });
    }
    assert!(
        !fixtures.is_empty(),
        "no fixtures found in {dir:?} - the suite would pass vacuously"
    );
    fixtures
}

// ─── Strict expected.json validation ────────────────────────────────────────

const ROOT_VALID_KEYS: [&str; 6] = [
    "skg_version",
    "schema_version",
    "imports",
    "children",
    "leading_comments",
    "trailing_comments",
];
const ROOT_INVALID_KEYS: [&str; 4] = ["error", "code", "line", "col"];
const DELETE_NODE_KEYS: [&str; 4] = ["type", "key", "leading_comments", "trailing_comment"];
const FIELD_NODE_KEYS: [&str; 5] = [
    "type",
    "key",
    "value",
    "leading_comments",
    "trailing_comment",
];
const BLOCK_NODE_KEYS: [&str; 6] = [
    "type",
    "name",
    "children",
    "replace",
    "leading_comments",
    "trailing_comments",
];
const BLOCK_ARRAY_NODE_KEYS: [&str; 5] = [
    "type",
    "name",
    "items",
    "leading_comments",
    "trailing_comments",
];
const SCALAR_VALUE_KEYS: [&str; 2] = ["type", "data"];
const ARRAY_VALUE_KEYS: [&str; 3] = ["type", "data", "element_type"];
const VALUE_TYPES: [&str; 7] = ["string", "int", "float", "bool", "null", "array", "object"];

#[derive(Default)]
struct SchemaReport {
    asserts_comments: bool,
}

fn fail_at(path: impl AsRef<str>, message: impl std::fmt::Display) -> String {
    format!("{}: {message}", path.as_ref())
}

fn check_keys(
    object: &serde_json::Map<String, Json>,
    allowed: &[&str],
    path: &str,
) -> Result<(), String> {
    let mut unknown: Vec<&String> = object
        .keys()
        .filter(|k| !allowed.contains(&k.as_str()))
        .collect();
    unknown.sort();
    if unknown.is_empty() {
        return Ok(());
    }
    let list: Vec<_> = unknown.iter().map(|s| s.as_str()).collect();
    Err(fail_at(
        path,
        format!("unknown key(s) {list:?} (allowed: {allowed:?})"),
    ))
}

fn check_string_array(value: &Json, path: &str) -> Result<(), String> {
    let array = value
        .as_array()
        .ok_or_else(|| fail_at(path, "must be an array of strings"))?;
    for (i, item) in array.iter().enumerate() {
        if !item.is_string() {
            return Err(fail_at(format!("{path}[{i}]"), "must be a string"));
        }
    }
    Ok(())
}

fn check_positive_int(value: &Json, path: &str) -> Result<(), String> {
    let number = value
        .as_f64()
        .ok_or_else(|| fail_at(path, "must be a positive integer"))?;
    if !number.is_finite() || number.fract() != 0.0 || number < 1.0 {
        return Err(fail_at(path, "must be a positive integer"));
    }
    Ok(())
}

/// Parse fixture JSON with serde_json's 128-level recursion guard disabled:
/// deep-nesting fixtures legitimately exceed it. The walker's own allowlist
/// still bounds the decoded shape.
fn parse_fixture_json(raw: &str) -> Json {
    let mut deserializer = serde_json::Deserializer::from_str(raw);
    deserializer.disable_recursion_limit();
    let json = Json::deserialize(&mut deserializer)
        .unwrap_or_else(|e| panic!("fixture JSON is not valid: {e}"));
    deserializer
        .end()
        .unwrap_or_else(|e| panic!("fixture JSON has trailing data: {e}"));
    json
}

fn validate_expected(
    raw: &str,
    invalid: bool,
    codes: &BTreeSet<String>,
) -> Result<SchemaReport, String> {
    let mut report = SchemaReport::default();
    // Deep-nesting fixtures legitimately exceed serde_json's default 128-level
    // recursion guard, so the limit is disabled; this walker's own allowlist
    // still bounds the shape.
    let mut deserializer = serde_json::Deserializer::from_str(raw);
    deserializer.disable_recursion_limit();
    let json = Json::deserialize(&mut deserializer)
        .map_err(|e| fail_at("$", format!("not valid JSON: {e}")))?;
    deserializer
        .end()
        .map_err(|e| fail_at("$", format!("trailing JSON: {e}")))?;
    let object = json
        .as_object()
        .ok_or_else(|| fail_at("$", "top level must be an object"))?;

    if invalid {
        check_keys(object, &ROOT_INVALID_KEYS, "$")?;
        match object.get("error") {
            Some(Json::Bool(true)) => {}
            Some(_) => return Err(fail_at("$.error", "must be the literal true")),
            None => return Err(fail_at("$", "\"error\" is required and must be true")),
        }
        let code = object
            .get("code")
            .and_then(Json::as_str)
            .ok_or_else(|| fail_at("$.code", "must be a string"))?;
        if !codes.contains(code) {
            return Err(fail_at(
                "$.code",
                format!("{code:?} is not in testdata/error-codes.json"),
            ));
        }
        if code == "UNKNOWN" {
            return Err(fail_at(
                "$.code",
                "UNKNOWN is a parser bug marker and may not be asserted",
            ));
        }
        for key in ["line", "col"] {
            if let Some(value) = object.get(key) {
                check_positive_int(value, &format!("$.{key}"))?;
            }
        }
        return Ok(report);
    }

    check_keys(object, &ROOT_VALID_KEYS, "$")?;
    for key in ["skg_version", "schema_version"] {
        if let Some(value) = object.get(key) {
            if !value.is_string() && !value.is_null() {
                return Err(fail_at(format!("$.{key}"), "must be a string or null"));
            }
        }
    }
    if let Some(imports) = object.get("imports") {
        let array = imports
            .as_array()
            .ok_or_else(|| fail_at("$.imports", "must be an array of strings"))?;
        for (i, item) in array.iter().enumerate() {
            if !item.is_string() {
                return Err(fail_at(format!("$.imports[{i}]"), "must be a string"));
            }
        }
    }
    for key in ["leading_comments", "trailing_comments"] {
        if let Some(value) = object.get(key) {
            report.asserts_comments = true;
            check_string_array(value, &format!("$.{key}"))?;
        }
    }
    if let Some(children) = object.get("children") {
        validate_nodes(children, "$.children", &mut report)?;
    }
    Ok(report)
}

fn validate_nodes(value: &Json, path: &str, report: &mut SchemaReport) -> Result<(), String> {
    let array = value
        .as_array()
        .ok_or_else(|| fail_at(path, "must be an array of nodes"))?;
    for (i, item) in array.iter().enumerate() {
        let node_path = format!("{path}[{i}]");
        let object = item
            .as_object()
            .ok_or_else(|| fail_at(&node_path, "must be an object"))?;
        let node_type = object.get("type").and_then(Json::as_str).ok_or_else(|| {
            fail_at(
                &node_path,
                "\"type\" is required and must be one of field, block, block_array, delete",
            )
        })?;
        match node_type {
            "field" | "delete" => {
                let allowed: &[&str] = if node_type == "delete" {
                    &DELETE_NODE_KEYS
                } else {
                    &FIELD_NODE_KEYS
                };
                check_keys(object, allowed, &node_path)?;
                if !object.get("key").map(Json::is_string).unwrap_or(false) {
                    return Err(fail_at(
                        &node_path,
                        "a field node requires a string \"key\"",
                    ));
                }
                if let Some(value) = object.get("value") {
                    validate_value(value, &format!("{node_path}.value"), report)?;
                }
                if let Some(value) = object.get("leading_comments") {
                    report.asserts_comments = true;
                    check_string_array(value, &format!("{node_path}.leading_comments"))?;
                }
                if let Some(value) = object.get("trailing_comment") {
                    report.asserts_comments = true;
                    if !value.is_string() && !value.is_null() {
                        return Err(fail_at(
                            format!("{node_path}.trailing_comment"),
                            "must be a string or null",
                        ));
                    }
                }
            }
            "block" | "block_array" => {
                let allowed: &[&str] = if node_type == "block" {
                    &BLOCK_NODE_KEYS
                } else {
                    &BLOCK_ARRAY_NODE_KEYS
                };
                check_keys(object, allowed, &node_path)?;
                if !object.get("name").map(Json::is_string).unwrap_or(false) {
                    return Err(fail_at(
                        &node_path,
                        format!("a {node_type} node requires a string \"name\""),
                    ));
                }
                if node_type == "block" {
                    if let Some(replace) = object.get("replace") {
                        if !replace.is_boolean() {
                            return Err(fail_at(
                                format!("{node_path}.replace"),
                                "must be a boolean",
                            ));
                        }
                    }
                    if let Some(children) = object.get("children") {
                        validate_nodes(children, &format!("{node_path}.children"), report)?;
                    }
                } else if let Some(items) = object.get("items") {
                    let items = items.as_array().ok_or_else(|| {
                        fail_at(
                            format!("{node_path}.items"),
                            "must be an array of node arrays or null entries",
                        )
                    })?;
                    for (j, item) in items.iter().enumerate() {
                        if item.is_null() {
                            continue;
                        }
                        validate_nodes(item, &format!("{node_path}.items[{j}]"), report)?;
                    }
                }
                for key in ["leading_comments", "trailing_comments"] {
                    if let Some(value) = object.get(key) {
                        report.asserts_comments = true;
                        check_string_array(value, &format!("{node_path}.{key}"))?;
                    }
                }
            }
            other => {
                return Err(fail_at(
                    format!("{node_path}.type"),
                    format!("{other:?} is not one of field, block, block_array, delete"),
                ));
            }
        }
    }
    Ok(())
}

fn validate_value(value: &Json, path: &str, report: &mut SchemaReport) -> Result<(), String> {
    let object = value
        .as_object()
        .ok_or_else(|| fail_at(path, "must be an object"))?;
    let value_type = object.get("type").and_then(Json::as_str).ok_or_else(|| {
        fail_at(
            path,
            format!("\"type\" is required and must be one of {VALUE_TYPES:?}"),
        )
    })?;
    if !VALUE_TYPES.contains(&value_type) {
        return Err(fail_at(
            format!("{path}.type"),
            format!("{value_type:?} is not one of {VALUE_TYPES:?}"),
        ));
    }
    let allowed: &[&str] = if value_type == "array" {
        &ARRAY_VALUE_KEYS
    } else {
        &SCALAR_VALUE_KEYS
    };
    check_keys(object, allowed, path)?;
    let data = object.get("data");
    if value_type == "null" {
        if data.is_some() {
            return Err(fail_at(path, "a null value carries no \"data\""));
        }
        return Ok(());
    }
    let data = data.ok_or_else(|| {
        fail_at(
            path,
            format!("\"data\" is required for a {value_type} value"),
        )
    })?;
    match value_type {
        "string" => {
            if !data.is_string() {
                return Err(fail_at(format!("{path}.data"), "must be a string"));
            }
        }
        "int" | "float" => {
            if !data.is_number() {
                return Err(fail_at(format!("{path}.data"), "must be a number"));
            }
        }
        "bool" => {
            if !data.is_boolean() {
                return Err(fail_at(format!("{path}.data"), "must be a boolean"));
            }
        }
        "object" => validate_nodes(data, &format!("{path}.data"), report)?,
        "array" => {
            let element_type = object
                .get("element_type")
                .and_then(Json::as_str)
                .ok_or_else(|| fail_at(path, "\"element_type\" is required for an array value"))?;
            if !VALUE_TYPES.contains(&element_type) {
                return Err(fail_at(
                    format!("{path}.element_type"),
                    format!("{element_type:?} is not one of {VALUE_TYPES:?}"),
                ));
            }
            let items = data.as_array().ok_or_else(|| {
                fail_at(format!("{path}.data"), "must be an array of value objects")
            })?;
            for (i, item) in items.iter().enumerate() {
                validate_value(item, &format!("{path}.data[{i}]"), report)?;
            }
        }
        other => unreachable!("value type {other} checked above"),
    }
    Ok(())
}

// ─── AST comparison ─────────────────────────────────────────────────────────

fn compare_optional_string(label: &str, expected: Option<&str>, actual: Option<&str>) {
    assert_eq!(
        expected, actual,
        "{label}: expected {expected:?}, got {actual:?}"
    );
}

fn compare_comments(label: &str, expected: Option<&Json>, actual: &[skg::Comment]) {
    let Some(expected) = expected else { return };
    let wanted: Vec<&str> = expected
        .as_array()
        .expect("comments are an array")
        .iter()
        .map(|c| c.as_str().expect("comments are strings"))
        .collect();
    let got: Vec<&str> = actual.iter().map(|c| c.text.as_str()).collect();
    assert_eq!(wanted, got, "{label}: comment mismatch");
}

fn compare_trailing_comment(label: &str, expected: Option<&Json>, actual: Option<&skg::Comment>) {
    let Some(expected) = expected else { return };
    let wanted = expected.as_str();
    let got = actual.map(|c| c.text.as_str());
    assert_eq!(wanted, got, "{label}: trailing comment mismatch");
}

fn compare_children(label: &str, expected: &[Json], actual: &[Node]) {
    assert_eq!(
        expected.len(),
        actual.len(),
        "{label}children count: expected {}, got {}",
        expected.len(),
        actual.len()
    );
    for (i, (expected, actual)) in expected.iter().zip(actual).enumerate() {
        let prefix = format!("{label}[{i}].");
        let object = expected.as_object().expect("nodes are objects");
        let node_type = object
            .get("type")
            .and_then(Json::as_str)
            .expect("node type");
        match node_type {
            "delete" => {
                let Node::Delete(delete) = actual else {
                    panic!("{prefix}expected delete, got {actual:?}");
                };
                assert_eq!(
                    object.get("key").and_then(Json::as_str),
                    Some(delete.key.as_str()),
                    "{prefix}delete key"
                );
                compare_comments(
                    &prefix.to_string(),
                    object.get("leading_comments"),
                    &delete.leading_comments,
                );
                compare_trailing_comment(
                    &prefix,
                    object.get("trailing_comment"),
                    delete.trailing_comment.as_ref(),
                );
            }
            "field" => {
                let Node::Field(field) = actual else {
                    panic!("{prefix}expected field, got {actual:?}");
                };
                assert_eq!(
                    object.get("key").and_then(Json::as_str),
                    Some(field.key.as_str()),
                    "{prefix}key"
                );
                if let Some(value) = object.get("value") {
                    compare_value(&format!("{prefix}value."), value, &field.value);
                }
                compare_comments(
                    &prefix.to_string(),
                    object.get("leading_comments"),
                    &field.leading_comments,
                );
                compare_trailing_comment(
                    &prefix,
                    object.get("trailing_comment"),
                    field.trailing_comment.as_ref(),
                );
            }
            "block" => {
                let Node::Block(block) = actual else {
                    panic!("{prefix}expected block, got {actual:?}");
                };
                if let Some(replace) = object.get("replace").and_then(Json::as_bool) {
                    assert_eq!(replace, block.replace, "{prefix}replace");
                }
                assert_eq!(
                    object.get("name").and_then(Json::as_str),
                    Some(block.name.as_str()),
                    "{prefix}name"
                );
                compare_comments(
                    &prefix.to_string(),
                    object.get("leading_comments"),
                    &block.leading_comments,
                );
                compare_comments(
                    &prefix.to_string(),
                    object.get("trailing_comments"),
                    &block.trailing_comments,
                );
                let children = object
                    .get("children")
                    .and_then(Json::as_array)
                    .map(Vec::as_slice)
                    .unwrap_or(&[]);
                compare_children(&prefix, children, &block.children);
            }
            "block_array" => {
                let Node::BlockArray(block_array) = actual else {
                    panic!("{prefix}expected block_array, got {actual:?}");
                };
                assert_eq!(
                    object.get("name").and_then(Json::as_str),
                    Some(block_array.name.as_str()),
                    "{prefix}name"
                );
                compare_comments(
                    &prefix.to_string(),
                    object.get("leading_comments"),
                    &block_array.leading_comments,
                );
                compare_comments(
                    &prefix.to_string(),
                    object.get("trailing_comments"),
                    &block_array.trailing_comments,
                );
                let items = object
                    .get("items")
                    .and_then(Json::as_array)
                    .expect("block_array items");
                assert_eq!(items.len(), block_array.items.len(), "{prefix}items count");
                for (j, (item, actual)) in items.iter().zip(&block_array.items).enumerate() {
                    let item_prefix = format!("{prefix}items[{j}].");
                    if item.is_null() {
                        assert_eq!(
                            ValueType::Null,
                            actual.value_type(),
                            "{item_prefix}expected null"
                        );
                    } else {
                        let children = item.as_array().expect("object entry is a node array");
                        let Value::Object(object) = actual else {
                            panic!(
                                "{item_prefix}expected object, got {}",
                                actual.value_type().as_str()
                            );
                        };
                        compare_children(&item_prefix, children, &object.children);
                    }
                }
            }
            other => panic!("{prefix}unknown expected node type {other:?}"),
        }
    }
}

fn compare_value(label: &str, expected: &Json, actual: &Value) {
    let object = expected.as_object().expect("values are objects");
    let expected_type = object
        .get("type")
        .and_then(Json::as_str)
        .expect("value type");
    let data = object.get("data");

    let matches_type = match expected_type {
        "string" => actual.value_type() == ValueType::String,
        "int" => actual.value_type() == ValueType::Int,
        "float" => actual.value_type() == ValueType::Float,
        "bool" => actual.value_type() == ValueType::Bool,
        "null" => actual.value_type() == ValueType::Null,
        "array" => actual.value_type() == ValueType::Array,
        "object" => actual.value_type() == ValueType::Object,
        other => panic!("{label}unknown expected type {other:?}"),
    };
    assert!(
        matches_type,
        "{label}type: expected {expected_type}, got {}",
        actual.value_type().as_str()
    );

    match expected_type {
        "string" => {
            let Value::String(s) = actual else {
                unreachable!()
            };
            assert_eq!(
                data.and_then(Json::as_str),
                Some(s.as_str()),
                "{label}value"
            );
        }
        // Ints are compared as 64-bit integers, never through a float.
        "int" => {
            let Value::Int(n) = actual else {
                unreachable!()
            };
            let wanted = data
                .and_then(Json::as_i64)
                .unwrap_or_else(|| panic!("{label}int data is not an i64: {data:?}"));
            assert_eq!(wanted, *n, "{label}value");
        }
        // Floats are compared bit for bit against the parsed binary64.
        "float" => {
            let Value::Float(f) = actual else {
                unreachable!()
            };
            let wanted = data.and_then(Json::as_f64).expect("float data");
            assert_eq!(
                wanted.to_bits(),
                f.to_bits(),
                "{label}value: expected {wanted}, got {f}"
            );
        }
        "bool" => {
            let Value::Bool(b) = actual else {
                unreachable!()
            };
            assert_eq!(data.and_then(Json::as_bool), Some(*b), "{label}value");
        }
        "null" => {}
        "object" => {
            let Value::Object(object_value) = actual else {
                unreachable!()
            };
            compare_children(
                label,
                data.and_then(Json::as_array)
                    .map(Vec::as_slice)
                    .unwrap_or(&[]),
                &object_value.children,
            );
        }
        "array" => {
            let Value::Array(array) = actual else {
                unreachable!()
            };
            let expected_element = object
                .get("element_type")
                .and_then(Json::as_str)
                .expect("element_type");
            assert_eq!(
                expected_element,
                array.element_type.as_str(),
                "{label}element_type"
            );
            let items = data.and_then(Json::as_array).expect("array data");
            assert_eq!(items.len(), array.items.len(), "{label}array length");
            for (i, (item, actual)) in items.iter().zip(&array.items).enumerate() {
                compare_value(&format!("{label}[{i}]."), item, actual);
            }
        }
        _ => unreachable!("type checked above"),
    }
}

// ─── Fixture execution ──────────────────────────────────────────────────────

fn load_fixture(fixture: &Fixture) -> Document {
    if fixture.is_dir {
        resolve_with(&fixture.entry, &FsLoader, &ResolveOptions::new())
            .unwrap_or_else(|e| panic!("resolve({:?}) failed: {e}", fixture.entry))
    } else {
        let source = std::fs::read(&fixture.entry).expect("fixture is readable");
        parse_bytes(&source, format!("{}.skg", fixture.name))
            .unwrap_or_else(|e| panic!("parse_bytes({}) failed: {e}", fixture.name))
    }
}

fn check_round_trip(fixture: &Fixture, document: &Document) {
    let formatted = fixture.formatted.as_ref().expect("formatted sidecar");
    let want = std::fs::read_to_string(formatted).expect("formatted fixture is readable");
    let got = emit(document);
    assert_eq!(
        want,
        got,
        "emit does not match {}\n--- want ---\n{want}\n--- got ---\n{got}",
        formatted.display()
    );

    // Emitting is idempotent: the formatted form is a fixed point.
    let reparsed = parse_bytes(want.as_bytes(), "formatted.skg")
        .unwrap_or_else(|e| panic!("formatted fixture does not parse: {e}"));
    assert_eq!(
        want,
        emit(&reparsed),
        "emit is not idempotent for {}",
        formatted.display()
    );
}

#[test]
fn conformance_valid() {
    let manifest = load_manifest();
    let codes = load_error_codes();
    let mut skipped = Vec::new();

    for fixture in discover_fixtures("valid") {
        let raw = std::fs::read_to_string(&fixture.expected).expect("expected.json is readable");
        let report = validate_expected(&raw, false, &codes).unwrap_or_else(|e| {
            panic!(
                "{} is not a valid expected.json: {e}",
                fixture.expected.display()
            )
        });
        let json = parse_fixture_json(&raw);
        let object = json.as_object().expect("expected.json is an object");

        if let Some(capability) = capability_for(
            &manifest,
            fixture.is_dir,
            fixture.formatted.is_some(),
            report.asserts_comments,
        ) {
            skipped.push((capability, format!("valid/{}", fixture.name)));
            eprintln!(
                "SKIP {} (capability {capability:?} not declared)",
                fixture.name
            );
            continue;
        }

        let document = load_fixture(&fixture);

        compare_optional_string(
            "skg_version",
            object.get("skg_version").and_then(Json::as_str),
            document.skg_version.as_deref(),
        );
        compare_optional_string(
            "schema_version",
            object.get("schema_version").and_then(Json::as_str),
            document.schema_version.as_deref(),
        );

        if let Some(imports) = object.get("imports").and_then(Json::as_array) {
            let wanted: Vec<&str> = imports
                .iter()
                .map(|v| v.as_str().expect("strings"))
                .collect();
            let got: Vec<&str> = document.import_paths.iter().map(String::as_str).collect();
            assert_eq!(wanted, got, "imports: expected {wanted:?}, got {got:?}");
        } else {
            assert!(
                document.import_paths.is_empty(),
                "imports unexpectedly present: {:?}",
                document.import_paths
            );
        }

        let children = object
            .get("children")
            .and_then(Json::as_array)
            .map(Vec::as_slice)
            .unwrap_or(&[]);
        compare_children("", children, &document.children);

        compare_comments(
            "file leading",
            object.get("leading_comments"),
            &document.leading_comments,
        );
        compare_comments(
            "file trailing",
            object.get("trailing_comments"),
            &document.trailing_comments,
        );

        if fixture.formatted.is_some() {
            check_round_trip(&fixture, &document);
        }
    }

    assert!(
        skipped.is_empty(),
        "conformance: skipped {skipped:?} - rust/conformance.json declares every capability, so none may skip"
    );
    eprintln!("CONFORMANCE: all valid fixtures ran; no capability was skipped");
}

fn run_invalid_fixture(
    fixture: &Fixture,
    expected_code: &str,
    expected_line: Option<u32>,
    expected_col: Option<u32>,
) {
    let name = &fixture.name;
    let error: ParseError = if fixture.is_dir {
        match resolve_with(&fixture.entry, &FsLoader, &ResolveOptions::new()) {
            Ok(_) => panic!("{name}: expected parse error, got success"),
            Err(skg::ResolveError::Parse(diagnostic)) => ParseError { diagnostic },
            Err(skg::ResolveError::Resolution(diagnostic)) => ParseError { diagnostic },
        }
    } else {
        let source = std::fs::read(&fixture.entry).expect("fixture is readable");
        match parse_bytes(&source, format!("{name}.skg")) {
            Ok(_) => panic!("{name}: expected parse error, got success"),
            Err(error) => error,
        }
    };
    let diagnostic: &Diagnostic = &error.diagnostic;
    assert_eq!(
        expected_code,
        diagnostic.code.as_str(),
        "code: expected {expected_code}, got {} (message: {})",
        diagnostic.code.as_str(),
        diagnostic.message
    );
    if let Some(line) = expected_line {
        assert_eq!(
            line, diagnostic.line,
            "line: expected {line}, got {}",
            diagnostic.line
        );
    }
    if let Some(col) = expected_col {
        assert_eq!(
            col, diagnostic.col,
            "col: expected {col}, got {}",
            diagnostic.col
        );
    }
    assert!(
        !diagnostic.message.is_empty(),
        "diagnostic has an empty human-readable message"
    );
}

#[test]
fn conformance_invalid() {
    let manifest = load_manifest();
    let codes = load_error_codes();
    let mut skipped = Vec::new();

    for fixture in discover_fixtures("invalid") {
        let raw = std::fs::read_to_string(&fixture.expected).expect("expected.json is readable");
        let report = validate_expected(&raw, true, &codes).unwrap_or_else(|e| {
            panic!(
                "{} is not a valid expected.json: {e}",
                fixture.expected.display()
            )
        });
        if let Some(capability) = capability_for(
            &manifest,
            fixture.is_dir,
            fixture.formatted.is_some(),
            report.asserts_comments,
        ) {
            skipped.push((capability, format!("invalid/{}", fixture.name)));
            continue;
        }
        let json = parse_fixture_json(&raw);
        let object = json.as_object().expect("expected.json is an object");
        let code = object.get("code").and_then(Json::as_str).expect("code");
        let line = object.get("line").and_then(Json::as_u64).map(|v| v as u32);
        let col = object.get("col").and_then(Json::as_u64).map(|v| v as u32);
        run_invalid_fixture(&fixture, code, line, col);
    }

    assert!(skipped.is_empty(), "conformance: skipped {skipped:?}");
    eprintln!("CONFORMANCE: all invalid fixtures ran; no capability was skipped");
}

#[test]
fn absolute_import_paths_rejected_at_parse_time() {
    // Windows spellings are rejected on every platform.
    for path in [
        "/etc/theme.skg",
        "\\theme.skg",
        "C:\\theme.skg",
        "c:theme.skg",
    ] {
        assert!(is_absolute_import_path(path), "{path} must be absolute");
    }
    for path in ["theme.skg", "./theme.skg", "../theme/theme.skg", ""] {
        assert!(
            !is_absolute_import_path(path),
            "{path} must not be absolute"
        );
    }
}

#[test]
fn byte_api_never_resolves_imports() {
    // flat-import-not-merged pins this through expected.json too; this keeps
    // the boundary covered even if that fixture disappears.
    let source = b"import \"definitely-missing.skg\"\nvalue: 1\n";
    let document =
        parse_bytes(source, "boundary.skg").expect("parses without touching the filesystem");
    assert_eq!(
        document.import_paths,
        vec!["definitely-missing.skg".to_string()]
    );
    assert!(!document.imports_resolved);
}

/// The strict validator catches the classic silent-assertion failures.
#[test]
fn expected_schema_rejects_unknown_keys() {
    let codes = load_error_codes();
    let cases: Vec<(&str, bool, &str)> = vec![
        (
            "misspelled code key",
            true,
            r#"{"error": true, "cod": "MIXED_ARRAY_TYPES"}"#,
        ),
        ("missing code", true, r#"{"error": true}"#),
        (
            "unregistered code",
            true,
            r#"{"error": true, "code": "NOT_A_REAL_CODE"}"#,
        ),
        (
            "error false",
            true,
            r#"{"error": false, "code": "MIXED_ARRAY_TYPES"}"#,
        ),
        (
            "legacy message_contains",
            true,
            r#"{"error": true, "message_contains": "mixed"}"#,
        ),
        ("unknown root key", false, r#"{"childrens": []}"#),
        (
            "unknown node key",
            false,
            r#"{"children": [{"type": "field", "key": "a", "keys": "b"}]}"#,
        ),
        (
            "block key on field",
            false,
            r#"{"children": [{"type": "field", "key": "a", "name": "a"}]}"#,
        ),
        (
            "bad node type",
            false,
            r#"{"children": [{"type": "feild", "key": "a"}]}"#,
        ),
        (
            "value without data",
            false,
            r#"{"children": [{"type": "field", "key": "a", "value": {"type": "int"}}]}"#,
        ),
        (
            "array without element_type",
            false,
            r#"{"children": [{"type": "field", "key": "a", "value": {"type": "array", "data": []}}]}"#,
        ),
        (
            "null with data",
            false,
            r#"{"children": [{"type": "field", "key": "a", "value": {"type": "null", "data": 1}}]}"#,
        ),
    ];
    for (name, invalid, json) in cases {
        assert!(
            validate_expected(json, invalid, &codes).is_err(),
            "expected validation to reject {name}: {json}"
        );
    }
}

/// Comment-bearing fixtures are detected structurally, so a package that does
/// not declare `comments` cannot silently skip them.
#[test]
fn expected_schema_detects_comment_assertions() {
    let codes = load_error_codes();
    let with_comments = [
        r##"{"children":[{"type":"field","key":"a","value":{"type":"array","element_type":"object","data":[{"type":"object","data":[{"type":"field","key":"b","leading_comments":["# nested"]}]}]}}]}"##,
        r##"{"leading_comments": ["# hi"]}"##,
        r##"{"children": [{"type": "field", "key": "a", "trailing_comment": "# hi"}]}"##,
        r##"{"children": [{"type": "block", "name": "b", "trailing_comments": ["# hi"]}]}"##,
        r##"{"children": [{"type": "block", "name": "b", "children": [{"type": "field", "key": "a", "leading_comments": []}]}]}"##,
    ];
    for json in with_comments {
        let report = validate_expected(json, false, &codes).expect("valid fixture schema");
        assert!(
            report.asserts_comments,
            "comment assertion not detected: {json}"
        );
    }
    let report = validate_expected(
        r#"{"children": [{"type": "field", "key": "a"}]}"#,
        false,
        &codes,
    )
    .expect("valid fixture schema");
    assert!(
        !report.asserts_comments,
        "comment assertion falsely detected"
    );
}

/// Parse - emit - parse preserves the model for every valid fixture, and
/// canonical emission is idempotent, even where no sidecar exists.
#[test]
fn valid_fixtures_round_trip_without_sidecars() {
    for fixture in discover_fixtures("valid") {
        let document = load_fixture(&fixture);
        let once = emit(&document);
        let reparsed = parse_bytes(once.as_bytes(), "roundtrip.skg").unwrap_or_else(|e| {
            panic!(
                "{}: canonical form does not parse: {e}\n{once}",
                fixture.name
            )
        });
        assert_eq!(
            once,
            emit(&reparsed),
            "{}: emit is not idempotent",
            fixture.name
        );
    }
}

// Keep the unused-import linter honest about the HashMap import used by the
// fixture index below when the corpus gains fixture families.
#[allow(dead_code)]
type Unused<T> = HashMap<String, T>;

// Silence the unused-writer lint in the skip summary helper.
#[allow(dead_code)]
fn summarize_skips(skips: &[(String, String)]) -> String {
    let mut out = String::new();
    for (capability, name) in skips {
        let _ = writeln!(
            out,
            "CONFORMANCE: SKIPPED {name}: capability {capability:?} not declared"
        );
    }
    out
}
