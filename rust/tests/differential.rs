//! Differential check against the Go and Zig reference implementations.
//!
//! `tools/differential.go` generates 1,000 valid V1 sources from a fixed seed,
//! canonicalizes them with Go, and requires the Zig formatter to agree byte
//! for byte. It can also dump the whole corpus as JSON (`-dump`), which is
//! committed at `tests/fixtures/differential.json`.
//!
//! This test closes the loop for Rust in two steps:
//!
//! 1. The generator is ported here; the regenerated sources must equal the
//!    dumped ones, so drift on either side fails loudly.
//! 2. Rust must parse and re-emit every case to the same canonical bytes as
//!    Go and Zig, and the canonical form must be a fixed point.

use serde::Deserialize;
use serde_json::Value as Json;

fn fixture_path() -> std::path::PathBuf {
    std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/differential.json")
}

#[derive(Debug, Deserialize)]
struct Corpus {
    seed: u64,
    count: usize,
    cases: Vec<Case>,
}

#[derive(Debug, Deserialize)]
struct Case {
    source: String,
    canonical: String,
}

/// The xorshift generator from tools/differential.go, bit for bit.
struct Random(u64);

impl Random {
    fn next(&mut self) -> u64 {
        let mut x = self.0;
        x ^= x << 13;
        x ^= x >> 7;
        x ^= x << 17;
        self.0 = x;
        x
    }
}

fn skg_string(value: &str) -> String {
    let mut out = String::with_capacity(value.len() + 2);
    out.push('"');
    for c in value.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\t' => out.push_str("\\t"),
            other => out.push(other),
        }
    }
    out.push('"');
    out
}

/// The source generator from tools/differential.go, bit for bit.
fn generate(index: usize, rng: &mut Random) -> String {
    let integer = (rng.next() % 2_000_001) as i64 - 1_000_000;
    let whole = rng.next() % 100_000;
    let fraction = rng.next() % 1_000_000;
    let float_sign = if rng.next() & 1 != 0 { "-" } else { "" };
    let text_values = [
        "plain",
        "quote \" and slash \\",
        "line one\nline two",
        "tab\tvalue",
        "éclair",
        "雪",
    ];
    let text = text_values[(rng.next() % text_values.len() as u64) as usize];

    let mut out = String::new();
    if index % 2 == 0 {
        out.push_str("skg_version: \"1.0\"\n");
    }
    if index % 5 == 0 {
        out.push_str("import [\"base.skg\", \"layer.skg\"]\n");
    }
    if index % 3 == 0 {
        out.push_str(&format!(
            "schema_version: {}\n",
            skg_string(&format!("1.{}.{}", index % 17, index % 29)),
        ));
    }
    out.push_str(&format!("seed_{index}: {integer}\n"));
    out.push_str(&format!(
        "{}: {float_sign}{whole}.{fraction:06}\n",
        skg_string(&format!("quoted key {index}/雪")),
    ));
    out.push_str(&format!("text: {}\n", skg_string(text)));
    out.push_str(&format!("enabled: {}\n", rng.next() & 1 == 0));
    match index % 8 {
        0 => {
            out.push_str(&format!(
                "nullable: [null, {}, null, {}]\n",
                index % 251,
                (index + 1) % 251,
            ));
            out.push_str("matrix: [[1, 2], [3, null], []]\n");
            out.push_str("nothing: [null, null]\n");
        }
        1 => {
            out.push_str(&format!(
                "items [ {{ id: {index} }} null {{ id: {} note: \"last\" }} ]\n",
                index + 1,
            ));
            out.push_str("empty_items [ ]\n");
        }
        2 => {
            out.push_str(&format!(
                "grid: [[{{ id: {index} }}, {{ id: {} nested: {{ ok: true }} }}], [null]]\n",
                index + 1,
            ));
            out.push_str("deep: [[[[1, 2]]], [[[3]]]]\n");
        }
        3 => {
            out.push_str(&format!(
                "settings {{ left: {index} nested {{ first: true }} }}\n"
            ));
            out.push_str(&format!(
                "settings {{ right: {} nested {{ second: false }} }}\n",
                index + 1,
            ));
            out.push_str("gone: true\n@delete gone\n");
        }
        4 => {
            out.push_str(&format!(
                "inline: {{ left: {index} nested: {{ values: [1, null, 3] }} }}\n",
            ));
            out.push_str("empty_value: []\nempty_object: {}\n");
        }
        5 => {
            out.push_str(&format!(
                "{}: {{ {}: {index} }}\n",
                skg_string("object/key~name"),
                skg_string("skg_version"),
            ));
            out.push_str(&format!(
                "{} {{ {}: true }}\n",
                skg_string("block key"),
                skg_string(""),
            ));
        }
        6 => {
            out.push_str(&format!("shape: {index}\nshape {{ nested: true }}\n"));
            out.push_str(&format!(
                "old {{ stale: true }}\n@replace old {{ fresh: {index} }}\nold {{ later: true }}\n",
            ));
        }
        7 => {
            out.push_str(
                "records: [{ name: \"a\" children: [{ id: 1 }, null] }, { name: \"b\" children: [] }]\n",
            );
            out.push_str(&format!("tail: {index}\n"));
        }
        _ => unreachable!("index % 8"),
    }
    out
}

#[test]
fn differential_corpus_matches_go_and_zig() {
    let raw = std::fs::read_to_string(fixture_path()).expect("differential fixture is readable");
    let corpus: Corpus = serde_json::from_str(&raw).expect("differential fixture is valid JSON");
    assert_eq!(
        corpus.seed, 0x0053_4b47_5631,
        "fixture seed is the shared one"
    );
    assert_eq!(
        corpus.cases.len(),
        corpus.count,
        "fixture case count matches"
    );
    assert!(
        corpus.count >= 1000,
        "the shared corpus runs at least 1,000 cases"
    );

    // The same RNG stream the Go tool uses: the generator starts at index 0
    // and each case draws in a fixed order, so regeneration must replay the
    // exact byte sequence.
    let mut rng = Random(corpus.seed);
    for (index, case) in corpus.cases.iter().enumerate() {
        let source = generate(index, &mut rng);
        assert_eq!(
            source, case.source,
            "case {index}: Rust generator diverged from the Go generator"
        );

        let document = skg::parse_bytes(source.as_bytes(), format!("generated-{index:04}.skg"))
            .unwrap_or_else(|e| {
                panic!("case {index}: Rust parser rejected a valid case: {e}\n{source}")
            });
        let canonical = skg::emit(&document);
        assert_eq!(
            canonical, case.canonical,
            "case {index}: Rust canonical output diverges from Go and Zig\nsource:\n{source}\nrust:\n{canonical}\ngo/zig:\n{}",
            case.canonical,
        );

        // The canonical form is a fixed point in Rust as well.
        let reparsed = skg::parse_bytes(canonical.as_bytes(), format!("canonical-{index:04}.skg"))
            .unwrap_or_else(|e| {
                panic!("case {index}: canonical form does not parse: {e}\n{canonical}")
            });
        assert_eq!(
            canonical,
            skg::emit(&reparsed),
            "case {index}: emit is not idempotent"
        );
    }
}

/// The dump stays in sync with the generator: this catches edits to the Go
/// generator or this port that would otherwise silently stale the fixture.
#[test]
fn corpus_count_is_pinned() {
    let raw = std::fs::read_to_string(fixture_path()).expect("differential fixture is readable");
    let json: Json = serde_json::from_str(&raw).expect("valid JSON");
    assert_eq!(
        json.get("count").and_then(Json::as_i64),
        Some(1000),
        "the committed corpus must cover the full 1,000-case run; regenerate with: (cd go && go run ../tools/differential.go -dump ../rust/tests/fixtures/differential.json)"
    );
}
