//! Standalone application-boundary test.
//!
//! This project consumes the published package surface the way an external
//! application would: a normal Cargo dependency, no repository-private
//! imports, no SKG CLI, and no bridge to the Go or Zig packages. It exercises
//! the source and file APIs, native composite types and defaults, custom
//! decode hooks, structured diagnostics with provenance, and the emitter's
//! rule that a resolved graph produces parseable canonical output without
//! active operations.

use std::path::Path;

use serde::{Deserialize, Serialize};

use skg::{
    emit, from_document_with, from_str_with, resolve_with, to_string, DecodeOptions, ErrorCode,
    FsLoader, Loader, MemoryLoader, NativeCode, ResolveOptions,
};

/// A custom-decoded type with a validation rule: the port must be nonzero.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
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

#[derive(Debug, PartialEq, Serialize)]
struct Worker {
    name: String,
    weight: u8,
}

impl<'de> Deserialize<'de> for Worker {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        #[derive(Deserialize)]
        struct Wire {
            name: String,
            weight: u8,
        }
        let wire = Wire::deserialize(deserializer)?;
        Ok(Worker {
            name: wire.name,
            weight: wire.weight,
        })
    }
}

#[derive(Debug, PartialEq, Serialize, Deserialize)]
struct Nested {
    #[allow(dead_code)]
    values: Vec<Vec<i64>>,
}

#[derive(Debug, PartialEq, Serialize, Deserialize)]
struct Config {
    service: Service,
    #[allow(dead_code)]
    features: std::collections::BTreeMap<String, bool>,
    workers: Vec<Option<Worker>>,
    #[allow(dead_code)]
    thresholds: Vec<Option<i16>>,
    #[allow(dead_code)]
    labels: std::collections::BTreeMap<String, u8>,
    remove_me: Option<String>,
    #[allow(dead_code)]
    nested: Nested,
}

#[derive(Debug, PartialEq, Serialize, Deserialize)]
struct Service {
    host: String,
    port: Port,
    #[allow(dead_code)]
    mode: String,
}

const BASE_SOURCE: &str = r#"service { host: "base" port: 80 mode: "safe" }
features { old: true keep: true }
workers [ { name: "alpha" weight: 1 } null ]
thresholds: [1, null, 3]
labels { "a/b~c": 7 }
remove_me: "gone"
extra: true
"#;

const MAIN_SOURCE: &str = r#"skg_version: "1.0"
import "base.skg"
service { host: "main" port: 8080 }
@replace features { new: true }
@delete remove_me
nested: { values: [[1, 2], [3]] }
"#;

fn write_tree(base: &Path) -> std::path::PathBuf {
    std::fs::create_dir_all(base).expect("create tree");
    std::fs::write(base.join("base.skg"), BASE_SOURCE).expect("write base");
    let main = base.join("main.skg");
    std::fs::write(&main, MAIN_SOURCE).expect("write main");
    main
}

fn assert_config(config: &Config) {
    assert_eq!(config.service.host, "main", "importer wins on scalars");
    assert_eq!(config.service.port, Port(8080));
    assert_eq!(
        config.service.mode, "safe",
        "unmentioned imported keys inherit"
    );
    assert_eq!(
        config.features.get("new"),
        Some(&true),
        "@replace discards inherited keys"
    );
    assert_eq!(config.features.len(), 1);
    assert_eq!(
        config.remove_me, None,
        "@delete removes the key before decoding"
    );
    assert_eq!(config.workers.len(), 2);
    assert_eq!(config.workers[0].as_ref().expect("alpha").name, "alpha");
    assert!(config.workers[1].is_none(), "null entries decode as None");
    assert_eq!(config.thresholds, vec![Some(1), None, Some(3)]);
    assert_eq!(
        config.labels.get("a/b~c"),
        Some(&7),
        "object keys are literal bytes"
    );
    assert_eq!(config.nested.values, vec![vec![1, 2], vec![3]]);
}

#[test]
fn public_consumer_workflow() {
    // Source API: records imports but never reads them.
    let parsed =
        skg::parse_source("import \"ghost.skg\"\nvalue: 1\n", "memory.skg").expect("source parse");
    assert_eq!(parsed.import_paths, vec!["ghost.skg".to_string()]);
    assert!(!parsed.imports_resolved);

    // File API: resolves imports, composes overlays, finalizes.
    let dir = std::env::temp_dir().join("skg-rust-consumer-workflow");
    let _ = std::fs::remove_dir_all(&dir);
    let main = write_tree(&dir);
    let resolved =
        resolve_with(main, &FsLoader, &ResolveOptions::rooted(&dir)).expect("file resolution");
    assert!(resolved.imports_resolved);

    // A resolved graph emits parseable canonical data with no operations.
    let canonical = emit(&resolved);
    assert!(!canonical.contains("import "), "{canonical}");
    assert!(
        !canonical.contains("@delete") && !canonical.contains("@replace"),
        "{canonical}"
    );
    skg::parse_bytes(canonical.as_bytes(), "canonical.skg").expect("canonical output parses");

    // Typed decode of the resolved graph, through the public API only.
    let config: Config =
        from_document_with(&resolved, &DecodeOptions::default()).expect("typed decode");
    assert_config(&config);

    // Typed encode round-trips the application model.
    let encoded = to_string(&config).expect("typed encode");
    let decoded: Config =
        from_str_with(&encoded, "roundtrip.skg", &DecodeOptions::default()).expect("re-decode");
    assert_eq!(decoded.service.host, "main");
    assert_eq!(decoded.workers[0].as_ref().expect("alpha").name, "alpha");

    let _ = std::fs::remove_dir_all(&dir);
}

#[test]
fn strict_diagnostics_keep_provenance() {
    #[derive(Debug, Deserialize)]
    struct Known {
        #[serde(rename = "value")]
        #[allow(dead_code)]
        value: u8,
    }
    let options = DecodeOptions {
        reject_unknown_fields: true,
        ..DecodeOptions::default()
    };
    let error = from_str_with::<Known>("value: 1 extra: true", "strict.skg", &options)
        .expect_err("unknown field rejected");
    assert_eq!(error.code, NativeCode::UnknownField);
    assert_eq!(error.field_path, "/extra");
    assert!(error.source.path.ends_with("strict.skg"));
    assert!(error.source.line > 0);

    // Custom-hook failures keep the custom classification.
    #[derive(Debug, Deserialize)]
    struct Root {
        #[serde(rename = "port")]
        #[allow(dead_code)]
        port: Port,
    }
    let error = from_str_with::<Root>("port: 0", "hook.skg", &DecodeOptions::default())
        .expect_err("zero port rejected");
    assert_eq!(error.code, NativeCode::CustomError);
    assert_eq!(error.field_path, "/port");
}

#[test]
fn custom_loader_is_a_supported_boundary() {
    let loader = MemoryLoader::new()
        .with_file("main.skg", "import \"base.skg\"\nhost: \"main\"\n")
        .with_file("base.skg", "host: \"base\"\nport: 80\n");
    let resolved =
        resolve_with("main.skg", &loader, &ResolveOptions::new()).expect("memory resolution");
    let text = emit(&resolved);
    assert_eq!(text, "host: \"main\"\nport: 80\n");
}

#[test]
fn parse_failures_carry_stable_codes() {
    let error = skg::from_str::<Config>("service: [").expect_err("parse failure");
    assert_eq!(error.code, NativeCode::ParseError);

    let error = skg::parse("@unknown value").expect_err("operation failure");
    assert_eq!(error.diagnostic.code, ErrorCode::UnknownOverlayOperation);
    // An @ in value position is a different code.
    let error = skg::parse("key: @unknown").expect_err("operation in value position");
    assert_eq!(error.diagnostic.code, ErrorCode::ExpectedValue);
}

/// Mirrors `Loader` implementability from outside the crate.
struct PrefixLoader;

impl Loader for PrefixLoader {
    fn canonicalize(&self, path: &Path) -> std::io::Result<std::path::PathBuf> {
        FsLoader.canonicalize(path)
    }
    fn read(&self, path: &Path) -> std::io::Result<Vec<u8>> {
        FsLoader.read(path)
    }
}

#[test]
fn loader_trait_is_implementable_downstream() {
    let loader = PrefixLoader;
    let bytes = loader.read(Path::new(
        &std::env::current_dir().expect("cwd").join("../../LICENSE"),
    ));
    drop(bytes);
}
