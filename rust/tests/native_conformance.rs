//! Shared native-conversion corpus runner (`testdata/native/cases.json`).
//!
//! Each profile binds one Rust type through Serde derives, exactly as the Go
//! and Zig runners bind their native types. Every case runs; the native
//! contract has no capability skips.

use std::collections::BTreeSet;

use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};
use serde_json::Value as Json;

use skg::{from_bytes_with, DecodeError, DecodeOptions};

fn repo_root() -> std::path::PathBuf {
    std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("manifest dir has a parent")
        .to_path_buf()
}

/// `port` profile: custom decoder into u16 with application validation.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
pub struct Port(u16);

impl<'de> Deserialize<'de> for Port {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let inner = u16::deserialize(deserializer)?;
        if inner == 0 {
            // Raised through the custom-decode hook; keeps the custom_error
            // classification, while nested range errors stay
            // number_out_of_range.
            return Err(serde::de::Error::custom("port must be nonzero"));
        }
        Ok(Port(inner))
    }
}

/// `enum` profile: a string tag of exactly `local` or `remote`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Mode {
    Local,
    Remote,
}

/// `bytes` profile: a nullable byte list that also accepts strings.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize)]
pub struct Bytes(pub Option<Vec<u8>>);

impl<'de> Deserialize<'de> for Bytes {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct Visitor;
        impl<'de> serde::de::Visitor<'de> for Visitor {
            type Value = Bytes;
            fn expecting(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
                f.write_str("a string or an array of bytes")
            }
            fn visit_str<E: serde::de::Error>(self, v: &str) -> Result<Bytes, E> {
                Ok(Bytes(Some(v.as_bytes().to_vec())))
            }
            fn visit_seq<A: serde::de::SeqAccess<'de>>(
                self,
                mut seq: A,
            ) -> Result<Bytes, A::Error> {
                let mut out = Vec::new();
                while let Some(byte) = seq.next_element::<u8>()? {
                    out.push(byte);
                }
                Ok(Bytes(Some(out)))
            }
            fn visit_none<E>(self) -> Result<Bytes, E> {
                Ok(Bytes(None))
            }
            fn visit_unit<E>(self) -> Result<Bytes, E> {
                Ok(Bytes(None))
            }
        }
        deserializer.deserialize_any(Visitor)
    }
}

/// `record` profile: required u16 `id`, nullable string `note`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Record {
    pub id: u16,
    pub note: Option<String>,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename = "value")]
struct Target<T> {
    value: T,
}

#[derive(Debug, Deserialize, Serialize)]
struct DefaultTarget {
    #[serde(rename = "value", default = "default_seven")]
    value: u8,
}

fn default_seven() -> u8 {
    7
}

#[derive(Debug, Default, Deserialize, Serialize)]
#[serde(rename = "value")]
struct OptionalTarget<T> {
    #[serde(default)]
    value: Option<T>,
}

/// A typed conversion failure named by a fixture.
#[derive(Debug)]
struct FixtureError {
    code: String,
    field_path: String,
}

/// One parsed native case, with strict per-key validation matching the Go and
/// Zig runners: unknown keys, missing required keys, non-boolean options, and
/// missing outcomes are hard failures, never silently skipped assertions.
struct Fixture {
    name: String,
    profile: String,
    source: String,
    options: Option<DecodeOptions>,
    /// Present only when the fixture declares an outcome; the inner option
    /// carries the value, which may itself be null.
    expected: Option<Option<Json>>,
    error: Option<FixtureError>,
}

const CASE_KEYS: [&str; 6] = ["name", "profile", "source", "options", "expected", "error"];
const ERROR_KEYS: [&str; 2] = ["code", "field_path"];
const OPTION_KEYS: [&str; 2] = ["reject_unknown_fields", "allow_lossy_numbers"];

fn parse_case(raw: &Json) -> Fixture {
    let object = raw.as_object().expect("each native case is an object");
    for key in object.keys() {
        assert!(
            CASE_KEYS.contains(&key.as_str()),
            "unknown fixture property {key:?}"
        );
    }
    for key in ["name", "profile", "source"] {
        assert!(object.contains_key(key), "missing fixture property {key:?}");
    }
    let name = object["name"]
        .as_str()
        .expect("name is a string")
        .to_string();
    let profile = object["profile"]
        .as_str()
        .expect("profile is a string")
        .to_string();
    let source = object["source"]
        .as_str()
        .expect("source is a string")
        .to_string();

    let options = object.get("options").map(|options| {
        let object = options.as_object().expect("options is an object");
        for key in object.keys() {
            assert!(
                OPTION_KEYS.contains(&key.as_str()),
                "unknown option {key:?}"
            );
        }
        let flag = |name: &str| {
            object
                .get(name)
                .map(|v| v.as_bool().expect("option is boolean"))
        };
        DecodeOptions {
            reject_unknown_fields: flag("reject_unknown_fields").unwrap_or(false),
            allow_lossy_numbers: flag("allow_lossy_numbers").unwrap_or(false),
        }
    });

    // An explicit `expected: null` expects the null value; the outer Option
    // records whether the key was present at all.
    let expected = object.get("expected").map(|value| {
        if value.is_null() {
            None
        } else {
            Some(value.clone())
        }
    });

    let error = object.get("error").map(|error| {
        let object = error.as_object().expect("error is an object");
        for key in object.keys() {
            assert!(
                ERROR_KEYS.contains(&key.as_str()),
                "unknown error property {key:?}"
            );
        }
        for key in ERROR_KEYS {
            assert!(object.contains_key(key), "missing error property {key:?}");
        }
        FixtureError {
            code: object["code"]
                .as_str()
                .expect("code is a string")
                .to_string(),
            field_path: object["field_path"]
                .as_str()
                .expect("field_path is a string")
                .to_string(),
        }
    });

    Fixture {
        name,
        profile,
        source,
        options,
        expected,
        error,
    }
}

fn run_case(case: &Fixture) -> Result<Json, DecodeError> {
    match case.profile.as_str() {
        "bool" => decode::<Target<bool>>(case),
        "u8" => decode::<Target<u8>>(case),
        "optional_u8" => decode::<OptionalTarget<u8>>(case),
        "default_u8" => decode::<DefaultTarget>(case),
        "i64" => decode::<Target<i64>>(case),
        "f32" => decode::<Target<f32>>(case),
        "f64" => decode::<Target<f64>>(case),
        "string" => decode::<Target<String>>(case),
        "bytes" => decode::<OptionalTarget<Bytes>>(case),
        "array2_u8" => decode::<Target<[u8; 2]>>(case),
        "record" => decode::<Target<Record>>(case),
        "list_optional_records" => decode::<OptionalTarget<Vec<Option<Record>>>>(case),
        "map_optional_u8" => {
            decode::<OptionalTarget<std::collections::HashMap<String, Option<u8>>>>(case)
        }
        "port" => decode::<Target<Port>>(case),
        "enum" => decode::<Target<Mode>>(case),
        other => panic!("unimplemented native profile {other:?}"),
    }
}

fn decode<T: DeserializeOwned + Serialize>(case: &Fixture) -> Result<Json, DecodeError> {
    let options = case.options.clone().unwrap_or_default();
    let target: T = from_bytes_with(
        case.source.as_bytes(),
        format!("{}.skg", case.name),
        &options,
    )?;
    // The root record has one field named `value`; the fixture's expected
    // value describes that field.
    let encoded = serde_json::to_value(&target)
        .map_err(|e| panic!("{}: re-encoding failed: {e}", case.name))?;
    Ok(encoded
        .get("value")
        .cloned()
        .expect("root record carries a value field"))
}

const NULL: Json = Json::Null;

/// Structural comparison mirroring the Go runner: integers exactly, floats as
/// binary64, strings byte for byte, ordered lists, keyed records.
fn json_matches(expected: &Json, actual: &Json, path: &str) {
    match (expected, actual) {
        (Json::Null, Json::Null) => {}
        (Json::Bool(a), Json::Bool(b)) => assert_eq!(a, b, "{path}: bool"),
        (Json::String(a), Json::String(b)) => assert_eq!(a, b, "{path}: string"),
        (Json::Number(a), Json::Number(b)) => {
            if let (Some(a), Some(b)) = (a.as_i64(), b.as_i64()) {
                assert_eq!(a, b, "{path}: integer");
            } else if let (Some(a), Some(b)) = (a.as_u64(), b.as_u64()) {
                assert_eq!(a, b, "{path}: integer");
            } else {
                let a = a.as_f64().unwrap_or_else(|| panic!("{path}: not a float"));
                let b = b.as_f64().unwrap_or_else(|| panic!("{path}: not a float"));
                assert_eq!(a.to_bits(), b.to_bits(), "{path}: float {a} vs {b}");
            }
        }
        (Json::Array(a), Json::Array(b)) => {
            assert_eq!(a.len(), b.len(), "{path}: array length");
            for (i, (a, b)) in a.iter().zip(b).enumerate() {
                json_matches(a, b, &format!("{path}/{i}"));
            }
        }
        (Json::Object(a), Json::Object(b)) => {
            assert_eq!(a.len(), b.len(), "{path}: object size");
            for (key, value) in a {
                let actual = b
                    .get(key)
                    .unwrap_or_else(|| panic!("{path}: missing key {key:?}"));
                json_matches(value, actual, &format!("{path}/{key}"));
            }
        }
        _ => panic!("{path}: expected {expected}, got {actual}"),
    }
}

#[test]
fn native_conformance() {
    let raw = std::fs::read_to_string(repo_root().join("testdata/native/cases.json"))
        .expect("testdata/native/cases.json is readable");
    let suite: Json = serde_json::from_str(&raw).expect("native suite is valid JSON");
    let object = suite.as_object().expect("native suite is an object");
    assert_eq!(
        object.get("contract_version").and_then(Json::as_i64),
        Some(1),
        "native contract_version must be 1"
    );
    assert_eq!(
        object.get("language_version").and_then(Json::as_str),
        Some("1.0"),
        "native language_version must be 1.0"
    );
    for key in object.keys() {
        assert!(
            matches!(
                key.as_str(),
                "contract_version" | "language_version" | "cases"
            ),
            "unknown suite key {key:?}"
        );
    }

    let raw_cases = object
        .get("cases")
        .and_then(Json::as_array)
        .expect("cases is an array");
    assert!(!raw_cases.is_empty(), "native suite is empty");
    let cases: Vec<Fixture> = raw_cases.iter().map(parse_case).collect();

    let mut seen = BTreeSet::new();
    for case in &cases {
        assert!(!case.name.is_empty(), "case name is empty");
        assert!(
            seen.insert(&case.name),
            "duplicate native case {}",
            case.name
        );
        assert!(
            case.expected.is_some() != case.error.is_some(),
            "{}: exactly one of expected or error",
            case.name
        );
        if let Some(error) = &case.error {
            assert!(!error.code.is_empty(), "{}: empty error code", case.name);
        }
    }

    for case in &cases {
        let result = run_case(case);
        match (&case.error, result) {
            (Some(expected), Err(error)) => {
                assert_eq!(
                    expected.code,
                    error.code.as_str(),
                    "{}: code {} does not match {} (message: {})",
                    case.name,
                    expected.code,
                    error.code,
                    error.message
                );
                assert_eq!(
                    expected.field_path, error.field_path,
                    "{}: field_path",
                    case.name
                );
            }
            (Some(expected), Ok(actual)) => {
                panic!(
                    "{}: expected error {}, got {}",
                    case.name, expected.code, actual
                )
            }
            (None, Ok(actual)) => {
                let expected = case
                    .expected
                    .as_ref()
                    .and_then(Option::as_ref)
                    .unwrap_or(&NULL);
                json_matches(expected, &actual, &case.name);
            }
            (None, Err(error)) => {
                panic!("{}: expected a value, got error {}", case.name, error)
            }
        }
    }
    eprintln!(
        "native contract v1 for SKG 1.0: {} cases, none skipped",
        cases.len()
    );
}
