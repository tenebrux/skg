//! Rust-specific behaviour tests: typed round trips, Serde options, error
//! provenance, loader boundaries, and deterministic encoding.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

use skg::{
    emit, from_document_with, from_str, from_str_with, parse, resolve_with, to_string,
    DecodeOptions, FsLoader, MemoryLoader, NativeCode, ResolveError, ResolveOptions, Value,
    ValueType,
};

// ─── Dynamic documents ──────────────────────────────────────────────────────

#[test]
fn parse_emit_parse_preserves_the_model() {
    let source = r#"
skg_version: "1.0"
import "./theme.skg"

schema_version: "2.0.0"

theme {
  accent: "green"
  nested { deep: [1, null, 3] }
}

panels [
  { position: "top" }
  null
]
"#;
    let document = parse(source).expect("parses");
    let once = emit(&document);
    let reparsed = parse(&once).expect("canonical form parses");
    assert_eq!(once, emit(&reparsed), "emit is idempotent");
    // The overlay keeps operations and imports for formatting.
    assert_eq!(document.import_paths, vec!["./theme.skg".to_string()]);
    assert!(!document.imports_resolved);
}

#[test]
fn values_preserve_type_distinctions() {
    let document = parse(
        "int: 42\nfloat: 13.0\nbool: true\nnothing: null\nstring: \"x\"\nempty: []\nempty_object: {}\n",
    )
    .expect("parses");
    let children = document.children.clone();
    let mut fields = children.into_iter().map(|node| {
        let skg::Node::Field(field) = node else {
            panic!("field")
        };
        (field.key, field.value)
    });
    assert_eq!(fields.next().unwrap().1, Value::Int(42));
    assert_eq!(fields.next().unwrap().1, Value::Float(13.0));
    assert_eq!(fields.next().unwrap().1, Value::Bool(true));
    let (key, value) = fields.next().unwrap();
    assert_eq!(key, "nothing");
    assert_eq!(value, Value::Null);
    let (key, value) = fields.next().unwrap();
    assert_eq!(key, "string");
    assert_eq!(value, Value::String("x".into()));
    let (_, Value::Array(array)) = fields.next().unwrap() else {
        panic!("array")
    };
    assert_eq!(array.element_type, ValueType::String);
    assert!(array.items.is_empty());
    // Empty braces normalize to a block.
    assert!(matches!(
        document.children.last(),
        Some(skg::Node::Block(block)) if block.name == "empty_object"
    ));
}

// ─── Typed decoding ─────────────────────────────────────────────────────────

#[derive(Debug, Deserialize, Serialize, PartialEq)]
struct Service {
    host: String,
    port: u16,
    #[serde(default)]
    tags: Vec<String>,
    note: Option<String>,
}

#[derive(Debug, Deserialize, Serialize, PartialEq)]
struct Config {
    service: Service,
    #[serde(default)]
    features: BTreeMap<String, bool>,
    thresholds: Vec<Option<i16>>,
}

#[test]
fn typed_round_trip_preserves_null_and_nested_collections() {
    let source = r#"
service { host: "main" port: 8080 tags: ["edge"] note: null }
features { cache: true }
thresholds: [1, null, 3]
"#;
    let config: Config = from_str(source).expect("decodes");
    assert_eq!(config.service.host, "main");
    assert_eq!(config.service.port, 8080);
    assert_eq!(config.service.tags, vec!["edge".to_string()]);
    assert_eq!(config.service.note, None);
    assert_eq!(config.features.get("cache"), Some(&true));
    assert_eq!(config.thresholds, vec![Some(1), None, Some(3)]);

    let encoded = to_string(&config).expect("encodes");
    let decoded: Config = from_str(&encoded).expect("re-decodes");
    assert_eq!(decoded, config);
    // Deterministic: re-encoding is a fixed point.
    assert_eq!(encoded, to_string(&decoded).unwrap());
}

#[test]
fn absent_null_and_empty_are_distinct() {
    #[derive(Debug, Deserialize, Serialize)]
    #[serde(rename_all = "lowercase")]
    struct Shape {
        #[serde(default)]
        value: Option<Inner>,
    }
    #[derive(Debug, Deserialize, Serialize, PartialEq)]
    struct Inner {
        n: u8,
    }

    // Absent: outer default applies.
    let absent: Shape = from_str("").expect("absent");
    assert_eq!(absent.value, None);

    // Null: present, but no value.
    let null: Shape = from_str("value: null").expect("null");
    assert_eq!(null.value, None);

    // Empty object: present and constructed.
    let empty: Shape = from_str("value { n: 0 }").expect("empty");
    assert_eq!(empty.value.as_ref().expect("present").n, 0);

    // Explicit null is not a default: a non-optional target fails.
    let error = from_str::<Target>("value: null").unwrap_err();
    assert_eq!(error.code, NativeCode::TypeMismatch);

    #[derive(Debug, Deserialize)]
    struct Target {
        #[serde(rename = "value")]
        #[allow(dead_code)]
        value: u8,
    }
}

#[test]
fn missing_required_field_fails_with_pointer_and_provenance() {
    #[derive(Debug, Deserialize)]
    struct Target {
        #[serde(rename = "value")]
        #[allow(dead_code)]
        value: Inner,
    }
    #[derive(Debug, Deserialize)]
    struct Inner {
        #[allow(dead_code)]
        port: u16,
    }
    let error = from_str_with::<Target>(
        "value { host: \"x\" }",
        "doc.skg",
        &DecodeOptions::default(),
    )
    .unwrap_err();
    assert_eq!(error.code, NativeCode::MissingField);
    assert_eq!(error.field_path, "/value/port");
    assert_eq!(error.source.path, "doc.skg");
}

#[test]
fn unknown_fields_ignored_or_rejected() {
    #[derive(Debug, Deserialize)]
    #[serde(rename = "value")]
    struct Target {
        #[allow(dead_code)]
        value: u8,
    }
    let default = from_str::<Target>("value: 2 extra: true").expect("ignored by default");
    assert_eq!(default.value, 2);

    let options = DecodeOptions {
        reject_unknown_fields: true,
        ..DecodeOptions::default()
    };
    let error =
        from_str_with::<Target>("value: 2 extra: true", "strict.skg", &options).unwrap_err();
    assert_eq!(error.code, NativeCode::UnknownField);
    assert_eq!(error.field_path, "/extra");
}

#[test]
fn strict_numbers() {
    #[derive(Debug, Deserialize)]
    #[serde(rename = "value")]
    struct U8 {
        #[allow(dead_code)]
        value: u8,
    }
    let error = from_str::<U8>("value: 256").unwrap_err();
    assert_eq!(error.code, NativeCode::NumberOutOfRange);
    assert_eq!(error.field_path, "/value");
    let error = from_str::<U8>("value: 1.0").unwrap_err();
    assert_eq!(error.code, NativeCode::TypeMismatch);

    #[derive(Debug, Deserialize)]
    #[serde(rename = "value")]
    struct F32 {
        #[allow(dead_code)]
        value: f32,
    }
    let error = from_str::<F32>("value: 0.1").unwrap_err();
    assert_eq!(error.code, NativeCode::InexactNumber);
    let lossy = DecodeOptions {
        allow_lossy_numbers: true,
        ..DecodeOptions::default()
    };
    let value = from_str_with::<F32>("value: 0.1", "f.skg", &lossy).expect("lossy allowed");
    assert_eq!(format!("{}", value.value), "0.1");
}

#[test]
fn enum_string_tags() {
    #[derive(Debug, Deserialize, Serialize, PartialEq)]
    #[serde(rename_all = "lowercase")]
    enum Mode {
        Local,
        Remote,
    }
    #[derive(Debug, Deserialize, Serialize)]
    struct Target {
        mode: Mode,
    }
    let decoded: Target = from_str("mode: \"remote\"").expect("decodes");
    assert_eq!(decoded.mode, Mode::Remote);
    let error = from_str::<Target>("mode: \"REMOTE\"").unwrap_err();
    assert_eq!(error.code, NativeCode::InvalidEnum);
    let error = from_str::<Target>("mode: 1").unwrap_err();
    assert_eq!(error.code, NativeCode::TypeMismatch);
}

#[test]
fn parse_failures_surface_as_parse_error_with_location() {
    let error =
        from_str_with::<Config>("service: [", "broken.skg", &DecodeOptions::default()).unwrap_err();
    assert_eq!(error.code, NativeCode::ParseError);
    assert_eq!(error.field_path, "");
    assert_eq!(error.source.path, "broken.skg");
    assert_eq!(error.source.line, 1);
}

// ─── Encoding rules ─────────────────────────────────────────────────────────

#[test]
fn encoding_rejects_values_the_grammar_cannot_express() {
    assert!(to_string(&f64::NAN).is_err());
    assert!(to_string(&f64::INFINITY).is_err());
    assert!(to_string(&u64::MAX).is_err());
    assert!(to_string(&42_u32).is_err(), "scalars are not documents");
}

#[test]
fn map_keys_are_sorted_and_struct_fields_keep_order() {
    let mut map = std::collections::HashMap::new();
    map.insert("zeta", 1);
    map.insert("alpha", 2);
    let encoded = to_string(&map).expect("encodes map");
    assert_eq!(
        encoded, "alpha: 2\nzeta: 1\n",
        "map keys sorted for determinism"
    );

    #[derive(Serialize)]
    struct Ordered {
        zeta: u8,
        alpha: u8,
    }
    let encoded = to_string(&Ordered { zeta: 1, alpha: 2 }).expect("encodes struct");
    assert_eq!(
        encoded, "zeta: 1\nalpha: 2\n",
        "struct fields keep declaration order"
    );
}

#[test]
fn reserved_names_and_punctuation_quote_as_keys() {
    let mut map: BTreeMap<&str, serde_json::Value> = BTreeMap::new();
    map.insert("skg_version", serde_json::json!("data, not a header"));
    map.insert("a.b", serde_json::json!(1));
    map.insert("true", serde_json::json!(2));
    let encoded = to_string(&map).expect("encodes");
    assert_eq!(
        encoded,
        "\"a.b\": 1\n\"skg_version\": \"data, not a header\"\n\"true\": 2\n"
    );
    let reparsed = parse(&encoded).expect("re-parses");
    assert!(
        reparsed.skg_version.is_none(),
        "quoted names are data, not directives"
    );
}

// ─── Resolution boundaries ──────────────────────────────────────────────────

/// A unique per-test temporary directory under the system temp dir.
fn scratch(label: &str) -> std::path::PathBuf {
    use std::sync::atomic::{AtomicUsize, Ordering};
    static NEXT: AtomicUsize = AtomicUsize::new(0);
    let dir = std::env::temp_dir().join(format!(
        "skg-rust-api-{}-{}",
        label,
        NEXT.fetch_add(1, Ordering::SeqCst)
    ));
    let _ = std::fs::remove_dir_all(&dir);
    std::fs::create_dir_all(&dir).expect("create scratch directory");
    dir
}

fn write(dir: &std::path::Path, name: &str, contents: &str) -> std::path::PathBuf {
    let path = dir.join(name);
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).expect("mkdir");
    }
    std::fs::write(&path, contents).expect("write");
    path
}

#[test]
fn resolve_applies_imports_and_overlays_then_materializes() {
    let dir = scratch("api");
    write(
        &dir,
        "base.skg",
        "service { host: \"base\" port: 80 }\nremove_me: \"gone\"\nlegacy: true\n",
    );
    let main = write(
        &dir,
        "main.skg",
        "import \"base.skg\"\nservice { host: \"main\" }\n@delete remove_me\n@replace legacy { fresh: 1 }\n",
    );

    let options = ResolveOptions::rooted(&dir);
    let document = resolve_with(&main, &FsLoader, &options).expect("resolves");
    assert!(document.imports_resolved);
    // Delete markers and replacement flags are gone; emit writes final data.
    let text = emit(&document);
    assert!(
        !text.contains("import "),
        "resolved output keeps no imports: {text}"
    );
    assert!(
        !text.contains("@delete") && !text.contains("@replace"),
        "{text}"
    );
    assert!(!text.contains("remove_me"), "{text}");

    // Typed decode of the resolved graph.
    #[derive(Debug, Deserialize)]
    struct Root {
        service: Service,
        legacy: Legacy,
    }
    #[derive(Debug, Deserialize)]
    struct Legacy {
        fresh: u8,
    }
    let root: Root = from_document_with(&document, &DecodeOptions::default()).expect("decodes");
    assert_eq!(root.service.host, "main", "importer wins");
    assert_eq!(root.service.port, 80, "unmentioned keys inherit");
    assert_eq!(
        root.legacy.fresh, 1,
        "replacement discards inherited children"
    );
}

#[derive(Debug, Deserialize)]
struct Svc {
    #[allow(dead_code)]
    host: String,
    #[allow(dead_code)]
    port: u16,
}

#[test]
fn typed_failures_on_imported_files_keep_provenance() {
    let dir = scratch("api");
    write(&dir, "base.skg", "service { host: \"base\" port: 99999 }\n");
    let main = write(&dir, "main.skg", "import \"base.skg\"\n");

    let document =
        resolve_with(&main, &FsLoader, &ResolveOptions::rooted(&dir)).expect("resolution succeeds");
    #[derive(Debug, Deserialize)]
    struct Root {
        #[allow(dead_code)]
        service: Svc,
    }
    let error = from_document_with::<Root>(&document, &DecodeOptions::default()).unwrap_err();
    assert_eq!(error.code, NativeCode::NumberOutOfRange);
    assert_eq!(error.field_path, "/service/port");
    assert!(
        error.source.path.ends_with("base.skg"),
        "provenance follows the imported file: {}",
        error.source.path
    );
    assert!(error.source.line > 0, "imported source location survives");
}

#[test]
fn resolution_failures_report_the_import_statement() {
    let dir = scratch("api");
    let main = write(&dir, "main.skg", "import \"ghost.skg\"\nvalue: 1\n");

    let error = resolve_with(&main, &FsLoader, &ResolveOptions::rooted(&dir)).unwrap_err();
    let ResolveError::Resolution(diagnostic) = error else {
        panic!("resolution diagnostic");
    };
    assert_eq!(diagnostic.code, skg::ErrorCode::ImportNotFound);
    assert_eq!(diagnostic.line, 1, "reported at the import statement");
    assert_eq!(diagnostic.col, 8, "column of the path token");
}

#[test]
fn cycles_and_diamonds() {
    let dir = scratch("api");
    write(&dir, "a.skg", "import \"b.skg\"\na: 1\n");
    write(&dir, "b.skg", "import \"a.skg\"\nb: 1\n");
    let error =
        resolve_with(dir.join("a.skg"), &FsLoader, &ResolveOptions::rooted(&dir)).unwrap_err();
    assert_eq!(error.code(), skg::ErrorCode::CircularImport);

    // A diamond is not a cycle, and memoization keeps it linear.
    write(
        &dir,
        "top.skg",
        "import \"left.skg\"\nimport \"right.skg\"\n",
    );
    write(&dir, "left.skg", "import \"shared.skg\"\nleft: 1\n");
    write(&dir, "right.skg", "import \"shared.skg\"\nright: 1\n");
    write(&dir, "shared.skg", "shared: 1\n");
    let document = resolve_with(
        dir.join("top.skg"),
        &FsLoader,
        &ResolveOptions::rooted(&dir),
    )
    .expect("diamond resolves");
    let text = emit(&document);
    assert!(text.contains("shared: 1"), "{text}");
}

#[test]
fn custom_loader_serves_an_in_memory_store() {
    #[derive(Debug, Deserialize)]
    struct Root {
        #[allow(dead_code)]
        host: String,
        #[allow(dead_code)]
        port: u16,
    }
    let loader = MemoryLoader::new()
        .with_file("main.skg", "import \"base.skg\"\nhost: \"main\"\n")
        .with_file("base.skg", "port: 8080\n");
    let document =
        resolve_with("main.skg", &loader, &ResolveOptions::new()).expect("resolves in memory");
    let root: Root = from_document_with(&document, &DecodeOptions::default()).expect("decodes");
    assert_eq!(root.port, 8080);
}

// ─── Limits ─────────────────────────────────────────────────────────────────

#[test]
fn parser_enforces_nesting_and_size_limits() {
    let deep = format!(
        "{}{}",
        "a {\n".repeat(skg::MAX_NESTING_DEPTH + 1),
        "}".repeat(skg::MAX_NESTING_DEPTH + 1)
    );
    let error = parse(&deep).unwrap_err();
    assert_eq!(error.diagnostic.code, skg::ErrorCode::NestingTooDeep);

    let oversized = vec![b'a'; skg::MAX_FILE_SIZE + 1];
    let error = skg::parse_bytes(&oversized, "big.skg").unwrap_err();
    assert_eq!(error.diagnostic.code, skg::ErrorCode::FileTooLarge);
}

#[test]
fn aggregate_limits_are_enforceable_per_call() {
    let dir = scratch("api");
    write(&dir, "base.skg", "child: 2\n");
    let main = write(&dir, "main.skg", "import \"base.skg\"\nroot: 1\n");

    let mut options = ResolveOptions::rooted(&dir);
    options.max_files = 1;
    let error = resolve_with(&main, &FsLoader, &options).unwrap_err();
    assert_eq!(error.code(), skg::ErrorCode::ResolutionFileLimit);
}

// ─── Review regressions ─────────────────────────────────────────────────────

#[test]
fn sibling_blocks_do_not_exhaust_the_nesting_budget() {
    // The depth counter must release when a block closes; 200 shallow
    // siblings are not 200 levels of nesting.
    let source: String = (0..200)
        .map(|i| format!("block{i} {{ value: {i} }}\n"))
        .collect();
    let document = parse(&source).expect("sibling blocks parse");
    assert_eq!(document.children.len(), 200);
    // Siblings inside a block count one level, regardless of quantity.
    let mut inner = String::from("outer {\n");
    inner.push_str(
        &(0..200)
            .map(|i| format!("  key{i}: {i}\n"))
            .collect::<String>(),
    );
    inner.push_str("}\n");
    let document = parse(&inner).expect("many sibling fields parse");
    assert_eq!(document.children.len(), 1);
}

#[test]
fn encoder_rejects_heterogeneous_sequences() {
    let mixed: Vec<serde_json::Value> = vec![serde_json::json!(1), serde_json::json!("text")];
    // A heterogeneous array would emit text the parser rejects with
    // MIXED_ARRAY_TYPES, so encoding must refuse it up front.
    let error = to_string(&mixed).expect_err("mixed array rejected");
    assert!(matches!(error, skg::EncodeError::InvalidValue(_)));

    // Homogeneous arrays with nulls keep working.
    #[derive(Serialize)]
    struct WithNullable {
        v: Vec<Option<i32>>,
    }
    assert_eq!(
        to_string(&WithNullable {
            v: vec![Some(1), None, Some(2)]
        })
        .expect("nullable array encodes"),
        "v: [1, null, 2]\n"
    );
}

#[test]
fn nested_struct_variants_keep_their_names() {
    #[derive(Debug, Serialize, Deserialize, PartialEq)]
    enum Outer {
        Holds(Inner),
    }
    #[derive(Debug, Serialize, Deserialize, PartialEq)]
    enum Inner {
        Wrapped { deep: u8 },
    }

    let value = Outer::Holds(Inner::Wrapped { deep: 7 });
    let encoded = to_string(&value).expect("nested variants encode");
    assert_eq!(encoded, "Holds {\n  Wrapped {\n    deep: 7\n  }\n}\n");
    let decoded: Outer = from_str(&encoded).expect("nested variants decode");
    assert_eq!(decoded, value);
}

#[test]
fn recursive_newtype_variants_stop_at_the_depth_bound() {
    #[derive(Serialize)]
    enum Rec {
        V(Box<Rec>),
        Leaf(u8),
    }
    fn chain(depth: usize) -> Rec {
        let mut value = Rec::Leaf(0);
        for _ in 0..depth {
            value = Rec::V(Box::new(value));
        }
        value
    }
    assert!(
        to_string(&chain(100)).is_ok(),
        "depth inside the bound encodes"
    );
    let error = to_string(&chain(500)).expect_err("deep recursion rejected");
    assert!(matches!(error, skg::EncodeError::Unsupported(_)));
}

#[test]
fn crlf_comments_normalize_on_emit() {
    // The comment text excludes the carriage return: canonical output is
    // LF-normalized end to end.
    let document = parse("name: \"x\" # trailing\r\n# last\r\n").expect("parses");
    let text = emit(&document);
    assert!(!text.contains('\r'), "emit normalizes CRLF: {text:?}");
    assert_eq!(text, "name: \"x\" # trailing\n# last\n");
}

// ─── Review round 2 regressions ─────────────────────────────────────────────

#[test]
fn unit_enum_variants_reject_nonempty_bodies() {
    #[derive(Debug, Deserialize, PartialEq)]
    enum Mode {
        Local,
    }
    #[derive(Debug, Deserialize)]
    struct Target {
        #[serde(rename = "mode")]
        mode: Mode,
    }

    // The string tag form.
    let decoded: Target = from_str("mode: \"Local\"").expect("string tag decodes");
    assert_eq!(decoded.mode, Mode::Local);
    // An empty block body is a valid spelling of a unit variant.
    let decoded: Target = from_str("mode { Local {} }").expect("empty body decodes");
    assert_eq!(decoded.mode, Mode::Local);
    // A nonempty body is a configuration mistake, not a payload to discard.
    let error = from_str::<Target>("mode { Local { typo: true } }").expect_err("payload rejected");
    assert_eq!(error.code, NativeCode::TypeMismatch);
    assert_eq!(error.field_path, "/mode/Local");
}

#[test]
fn memory_loader_supports_rooted_resolution() {
    let loader = MemoryLoader::new()
        .with_file("cfg/main.skg", "import \"base.skg\"\nhost: \"main\"\n")
        .with_file("cfg/base.skg", "port: 80\n");
    let document = resolve_with("cfg/main.skg", &loader, &ResolveOptions::rooted("cfg"))
        .expect("rooted in-memory resolution");
    // The base's key holds the first slot; the importer's value wins.
    assert_eq!(emit(&document), "port: 80\nhost: \"main\"\n");

    // A root with no stored file beneath it is simply not found, exactly
    // like a missing root on the filesystem loader.
    let error = resolve_with(
        "cfg/main.skg",
        &loader,
        &ResolveOptions::rooted("elsewhere"),
    )
    .expect_err("missing root");
    assert_eq!(error.code(), skg::ErrorCode::ImportNotFound);
}

#[test]
fn identical_comment_text_from_separate_locations_survives_merge() {
    let source = "# same\nservice { a: 1 }\n# same\nservice { b: 2 }\n";
    let document = parse(source).expect("parses");
    let text = emit(&document);
    assert_eq!(
        text.matches("# same").count(),
        2,
        "two source comments with equal text must both survive: {text}"
    );
}

#[test]
fn shared_comments_are_emitted_once_across_a_diamond() {
    let dir = scratch("diamond-comments");
    // The comment must attach to the block: file-leading comments are not
    // part of the children a merge propagates.
    write(
        &dir,
        "shared.skg",
        "other: true\n# shared comment\nsvc { port: 80 }\n",
    );
    write(&dir, "left.skg", "import \"shared.skg\"\nleft: 1\n");
    write(&dir, "right.skg", "import \"shared.skg\"\nright: 1\n");
    write(
        &dir,
        "top.skg",
        "import \"left.skg\"\nimport \"right.skg\"\nmain: 1\n",
    );
    let document = resolve_with(
        dir.join("top.skg"),
        &FsLoader,
        &ResolveOptions::rooted(&dir),
    )
    .expect("diamond resolves");
    let text = emit(&document);
    assert_eq!(
        text.matches("# shared comment").count(),
        1,
        "a cached import's comment must not repeat: {text}"
    );
}

#[test]
fn deeply_nested_enum_payloads_hit_the_native_depth_bound() {
    // 300 nested `Choice { ... }` blocks, deeper than the native limit of
    // 256. Parsed files stop at the syntax limit long before this; the
    // programmatic document path must still stop at the native bound.
    let mut children: Vec<skg::Node> = Vec::new();
    for _ in 0..300 {
        children = vec![skg::Node::Block(skg::Block {
            name: "Choice".into(),
            children,
            ..skg::Block::default()
        })];
    }
    let document = skg::Document {
        children,
        ..skg::Document::default()
    };

    #[derive(Debug, Deserialize)]
    #[allow(dead_code)]
    enum Rec {
        Choice(Box<Rec>),
        Leaf,
    }

    let error = from_document_with::<Rec>(&document, &DecodeOptions::default())
        .expect_err("depth bound reached");
    assert_eq!(error.code, NativeCode::NestingTooDeep);
}
