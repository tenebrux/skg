//! Canonical emission: document → canonical SKG text.
//!
//! Output uses LF endings, two-space indentation, and one spelling per value.
//! Unresolved overlays keep operations and import statements; a document
//! whose imports were resolved emits standalone final data.

use crate::lexer::is_identifier;
use crate::model::{Document, Node, Value};
use crate::parser::is_directive;

/// Serialize a document back to canonical SKG text.
///
/// A composed overlay retains its `@delete` markers, `@replace` flags and
/// import statements. A resolved document (see the file API) emits standalone
/// final data: import statements are omitted because keeping them active
/// could recreate deleted values on the next load.
#[must_use]
pub fn emit(document: &Document) -> String {
    let mut out = String::new();

    // File-level leading comments come before the header.
    emit_comment_lines(&mut out, &document.leading_comments, 0);

    // Header order is normative: skg_version, imports, schema_version.
    if let Some(version) = &document.skg_version {
        out.push_str("skg_version: ");
        write_quoted(&mut out, version);
        out.push('\n');
    }

    if !document.imports_resolved && !document.import_paths.is_empty() {
        if document.import_paths.len() == 1 {
            out.push_str("import ");
            write_quoted(&mut out, &document.import_paths[0]);
            out.push('\n');
        } else {
            out.push_str("import [\n");
            for (i, path) in document.import_paths.iter().enumerate() {
                out.push_str("  ");
                write_quoted(&mut out, path);
                if i + 1 < document.import_paths.len() {
                    out.push(',');
                }
                out.push('\n');
            }
            out.push_str("]\n");
        }
    }

    if let Some(version) = &document.schema_version {
        out.push_str("schema_version: ");
        write_quoted(&mut out, version);
        out.push('\n');
    }

    let has_header = document.skg_version.is_some()
        || document.schema_version.is_some()
        || (!document.imports_resolved && !document.import_paths.is_empty());
    if has_header && !document.children.is_empty() {
        out.push('\n');
    }

    emit_nodes(&mut out, &document.children, 0);

    emit_comment_lines(&mut out, &document.trailing_comments, 0);
    out
}

fn emit_nodes(out: &mut String, nodes: &[Node], depth: usize) {
    for (i, node) in nodes.iter().enumerate() {
        match node {
            Node::Delete(delete) => {
                emit_comment_lines(out, &delete.leading_comments, depth);
                write_indent(out, depth);
                out.push_str("@delete ");
                write_key(out, &delete.key, depth);
                if let Some(trailing) = &delete.trailing_comment {
                    out.push(' ');
                    out.push_str(trailing);
                }
                out.push('\n');
            }
            Node::Field(field) => {
                emit_comment_lines(out, &field.leading_comments, depth);
                write_indent(out, depth);
                write_key(out, &field.key, depth);
                out.push_str(": ");
                emit_value(out, &field.value, depth);
                if let Some(trailing) = &field.trailing_comment {
                    out.push(' ');
                    out.push_str(trailing);
                }
                out.push('\n');
            }
            Node::Block(block) => {
                if i > 0 && depth == 0 {
                    out.push('\n');
                }
                emit_comment_lines(out, &block.leading_comments, depth);
                write_indent(out, depth);
                if block.replace {
                    out.push_str("@replace ");
                }
                write_key(out, &block.name, depth);
                out.push_str(" {\n");
                emit_nodes(out, &block.children, depth + 1);
                emit_comment_lines(out, &block.trailing_comments, depth + 1);
                write_indent(out, depth);
                out.push_str("}\n");
            }
            Node::BlockArray(block_array) => {
                if i > 0 && depth == 0 {
                    out.push('\n');
                }
                emit_comment_lines(out, &block_array.leading_comments, depth);
                write_indent(out, depth);
                write_key(out, &block_array.name, depth);
                out.push_str(" [\n");
                for item in &block_array.items {
                    write_indent(out, depth + 1);
                    emit_value(out, item, depth + 1);
                    out.push('\n');
                }
                emit_comment_lines(out, &block_array.trailing_comments, depth + 1);
                write_indent(out, depth);
                out.push_str("]\n");
            }
            Node::None => {}
        }
    }
}

fn emit_value(out: &mut String, value: &Value, depth: usize) {
    match value {
        Value::Int(n) => {
            out.push_str(&n.to_string());
        }
        Value::Float(f) => out.push_str(&format_float(*f)),
        Value::Bool(true) => out.push_str("true"),
        Value::Bool(false) => out.push_str("false"),
        Value::String(s) => {
            if s.contains('\n') && can_emit_multiline(s) {
                out.push_str("\"\"\"");
                out.push_str(s);
                out.push_str("\"\"\"");
            } else {
                out.push('"');
                write_escaped(out, s);
                out.push('"');
            }
        }
        Value::Null => out.push_str("null"),
        Value::Object(object) => {
            out.push_str("{\n");
            emit_nodes(out, &object.children, depth + 1);
            emit_comment_lines(out, &object.trailing_comments, depth + 1);
            write_indent(out, depth);
            out.push('}');
        }
        Value::Array(array) => {
            out.push('[');
            for (i, item) in array.items.iter().enumerate() {
                if i > 0 {
                    out.push_str(", ");
                }
                emit_value(out, item, depth + 1);
            }
            if !array.trailing_comments.is_empty() {
                out.push('\n');
                emit_comment_lines(out, &array.trailing_comments, depth + 1);
                write_indent(out, depth);
            }
            out.push(']');
        }
    }
}

/// Render a float as an SKG float literal.
///
/// The grammar has no exponent form, so the value is written as plain decimal
/// digits and always carries a fractional part; large or tiny magnitudes
/// therefore expand to long digit strings. Rust's shortest round-trip
/// formatting matches the Go and Zig emitters byte for byte.
///
/// NaN and infinity have no SKG literal. The encoder rejects them up front;
/// `emit` cannot report an error, so it degrades them to `null` rather than
/// writing output that will not parse.
fn format_float(f: f64) -> String {
    if !f.is_finite() {
        return "null".to_string();
    }
    let s = format!("{f}");
    if s.contains('.') {
        s
    } else {
        format!("{s}.0")
    }
}

/// Whether `s` survives a `"""..."""` round trip.
///
/// Multiline literals are taken verbatim with no escape processing, so an
/// embedded `"""` - or a trailing `"` that merges with the closing delimiter -
/// would end the literal early and the output would not re-parse. Such values
/// are emitted as an escaped double-quoted string instead.
fn can_emit_multiline(s: &str) -> bool {
    if s.contains("\"\"\"") {
        return false;
    }
    !s.ends_with('"')
}

fn emit_comment_lines(out: &mut String, comments: &[String], depth: usize) {
    for comment in comments {
        write_indent(out, depth);
        out.push_str(comment);
        out.push('\n');
    }
}

fn write_escaped(out: &mut String, s: &str) {
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\t' => out.push_str("\\t"),
            other => out.push(other),
        }
    }
}

fn write_indent(out: &mut String, depth: usize) {
    for _ in 0..depth {
        out.push_str("  ");
    }
}

fn write_quoted(out: &mut String, value: &str) {
    out.push('"');
    write_escaped(out, value);
    out.push('"');
}

/// Keep the familiar bare spelling wherever it is unambiguous. Reserved value
/// literals and header directives at the top level need quotes; `skg_version`
/// as a field key inside a block stays bare.
fn write_key(out: &mut String, key: &str, depth: usize) {
    if is_identifier(key) && (depth > 0 || !is_directive(key)) {
        out.push_str(key);
    } else {
        write_quoted(out, key);
    }
}
