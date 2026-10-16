//! The Rust implementation's parity harness. It speaks the protocol described
//! in tools/parity/README.md: read a JSON case file, run every case through the
//! public Rust API, and write one JSON result per line.

use std::collections::HashMap;
use std::io::Write;
use std::path::Path;

use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value as Json};

use skg::{DecodeOptions, ResolveOptions};

#[derive(Deserialize)]
struct CaseFile {
    mode: String,
    cases: Vec<Case>,
}

#[derive(Deserialize, Default)]
#[serde(default)]
struct Case {
    id: String,
    path: String,
    entry: String,
    root: String,
    max_bytes: u64,
    max_files: usize,
    max_nodes: usize,
    max_merge_work: u64,
    profile: String,
    source: String,
    reject_unknown_fields: bool,
    allow_lossy_numbers: bool,
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 3 {
        eprintln!("usage: skg-parity <cases.json> <results.jsonl>");
        std::process::exit(2);
    }
    let data = std::fs::read(&args[1]).expect("read case file");
    let cases: CaseFile = serde_json::from_slice(&data).expect("parse case file");
    let mut out = std::io::BufWriter::new(std::fs::File::create(&args[2]).expect("create results"));
    for case in &cases.cases {
        let mut result = match cases.mode.as_str() {
            "parse" => {
                let source = std::fs::read(&case.path).expect("read source");
                let name = basename(&case.path);
                match skg::parse_bytes(&source, name.clone()) {
                    Ok(document) => json!({"ok": true, "emit": skg::emit(&document)}),
                    Err(error) => failure(&error.diagnostic),
                }
            }
            "resolve" => {
                let mut options = match case.root.as_str() {
                    "" => ResolveOptions::new(),
                    root => ResolveOptions::rooted(root),
                };
                if case.max_bytes != 0 {
                    options.max_bytes = case.max_bytes;
                }
                if case.max_files != 0 {
                    options.max_files = case.max_files;
                }
                if case.max_nodes != 0 {
                    options.max_nodes = case.max_nodes;
                }
                if case.max_merge_work != 0 {
                    options.max_merge_work = case.max_merge_work;
                }
                match skg::resolve(&case.entry, &options) {
                    Ok(document) => json!({"ok": true, "emit": skg::emit(&document)}),
                    Err(error) => failure(error.diagnostic()),
                }
            }
            "native" => native(case),
            other => panic!("unknown mode {other:?}"),
        };
        result["id"] = json!(case.id);
        serde_json::to_writer(&mut out, &result).expect("write result");
        out.write_all(b"\n").expect("write result");
    }
    out.flush().expect("flush results");
}

fn basename(path: &str) -> String {
    Path::new(path)
        .file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_default()
}

fn failure(diagnostic: &skg::Diagnostic) -> Json {
    json!({
        "ok": false,
        "code": diagnostic.code.as_str(),
        "file": basename(&diagnostic.path),
        "line": diagnostic.line,
        "col": diagnostic.col,
    })
}

// The shared native profiles, bound exactly as rust/tests/native_conformance.rs
// binds them.

#[derive(Serialize)]
struct Port(u16);

impl<'de> Deserialize<'de> for Port {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let inner = u16::deserialize(deserializer)?;
        if inner == 0 {
            return Err(serde::de::Error::custom("port must be nonzero"));
        }
        Ok(Port(inner))
    }
}

#[derive(Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
enum Mode {
    Local,
    Remote,
}

#[derive(Default, Serialize)]
struct Bytes(Option<Vec<u8>>);

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

#[derive(Serialize, Deserialize)]
struct Record {
    id: u16,
    note: Option<String>,
}

#[derive(Serialize, Deserialize)]
struct Target<T> {
    value: T,
}

#[derive(Serialize, Deserialize)]
struct OptionalTarget<T> {
    #[serde(default)]
    value: Option<T>,
}

fn default_seven() -> u8 {
    7
}

#[derive(Serialize, Deserialize)]
struct DefaultTarget {
    #[serde(default = "default_seven")]
    value: u8,
}

fn native(case: &Case) -> Json {
    let mut options = DecodeOptions::new();
    options.reject_unknown_fields = case.reject_unknown_fields;
    options.allow_lossy_numbers = case.allow_lossy_numbers;
    let source = case.source.as_bytes();
    match case.profile.as_str() {
        "bool" => decode::<Target<bool>>(source, &options),
        "u8" => decode::<Target<u8>>(source, &options),
        "optional_u8" => decode::<OptionalTarget<u8>>(source, &options),
        "default_u8" => decode::<DefaultTarget>(source, &options),
        "i64" => decode::<Target<i64>>(source, &options),
        "f32" => decode::<Target<f32>>(source, &options),
        "f64" => decode::<Target<f64>>(source, &options),
        "string" => decode::<Target<String>>(source, &options),
        "bytes" => decode::<OptionalTarget<Bytes>>(source, &options),
        "array2_u8" => decode::<Target<[u8; 2]>>(source, &options),
        "record" => decode::<Target<Record>>(source, &options),
        "list_optional_records" => decode::<OptionalTarget<Vec<Option<Record>>>>(source, &options),
        "map_optional_u8" => {
            decode::<OptionalTarget<HashMap<String, Option<u8>>>>(source, &options)
        }
        "port" => decode::<Target<Port>>(source, &options),
        "enum" => decode::<Target<Mode>>(source, &options),
        other => panic!("unknown native profile {other:?}"),
    }
}

fn decode<T: DeserializeOwned + Serialize>(source: &[u8], options: &DecodeOptions) -> Json {
    match skg::from_bytes_with::<T>(source, "case.skg", options) {
        Ok(target) => {
            // serde_json renders f32 through binary64, as the Go and Zig
            // harnesses widen before printing.
            let mut encoded = serde_json::to_value(&target).expect("re-encode native value");
            json!({"ok": true, "value": encoded["value"].take()})
        }
        Err(error) => json!({
            "ok": false,
            "code": error.code.as_str(),
            "field_path": error.field_path,
        }),
    }
}
