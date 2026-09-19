//! Overlay composition: merges one node list onto another.
//!
//! Used by the resolver to apply imports before the importing file, and by the
//! parser to deduplicate repeated keys within one file. Load-order rule: later
//! values overwrite earlier values; block children merge recursively by name.
//!
//! Composition keeps `@delete` markers and replacement flags so a composed
//! overlay can be composed again without resurrecting earlier values. Call
//! [`materialize_nodes`] once after the final merge to obtain final data.

use std::collections::HashMap;

use crate::model::{Array, Block, Node, ObjectBody, Value};

/// Compose `overlay` onto an empty base. Repeated keys merge under the shared
/// rules; the result retains deletion and replacement markers.
#[must_use]
pub fn merge_nodes(nodes: Vec<Node>) -> Vec<Node> {
    merge_nodes_budget(Vec::new(), nodes, &mut None).expect("unbounded merge cannot fail")
}

/// Compose `overlay` onto `base` without a work budget.
#[must_use]
pub fn merge_overlay(base: Vec<Node>, overlay: Vec<Node>) -> Vec<Node> {
    merge_nodes_budget(base, overlay, &mut None).expect("unbounded merge cannot fail")
}

/// The resolver's bounded form. A work unit is one node slot scanned while
/// composing a node list; recursive block merges are charged independently.
/// Returns `Err(())` when the remaining budget runs out.
pub(crate) fn merge_nodes_budget(
    base: Vec<Node>,
    overlay: Vec<Node>,
    remaining: &mut Option<u64>,
) -> Result<Vec<Node>, ()> {
    if let Some(budget) = remaining.as_mut() {
        let cost = base.len() as u64 + overlay.len() as u64;
        if cost > *budget {
            return Err(());
        }
        *budget -= cost;
    }
    if overlay.is_empty() {
        return Ok(base);
    }

    let mut result: Vec<Node> = Vec::with_capacity(base.len() + overlay.len());
    let mut index: HashMap<String, usize> = HashMap::with_capacity(base.len() + overlay.len());

    for node in base {
        // Nodes with no key carry no data and are dropped.
        let Some(key) = node.key() else { continue };
        index.insert(key.to_string(), result.len());
        result.push(node);
    }

    for overlay_node in overlay {
        let Some(key) = overlay_node.key().map(str::to_string) else {
            continue; // empty node carries no data
        };
        let Some(&position) = index.get(&key) else {
            index.insert(key, result.len());
            result.push(overlay_node);
            continue;
        };
        // Take the base slot by value so the recursive merge can consume both
        // sides without borrowing `result` across the assignment.
        let base_node = std::mem::replace(&mut result[position], Node::None);
        match (overlay_node, base_node) {
            // Only a block onto a non-replacing block merges recursively.
            (Node::Block(overlay_block), Node::Block(base_block)) if !overlay_block.replace => {
                let children =
                    merge_nodes_budget(base_block.children, overlay_block.children, remaining)?;
                result[position] = Node::Block(Block {
                    path: base_block.path,
                    replace: base_block.replace,
                    name: base_block.name,
                    children,
                    line: base_block.line,
                    col: base_block.col,
                    leading_comments: concat_comments(
                        &base_block.leading_comments,
                        &overlay_block.leading_comments,
                    ),
                    trailing_comments: concat_comments(
                        &base_block.trailing_comments,
                        &overlay_block.trailing_comments,
                    ),
                });
            }
            // Everything else - scalar over block, block over scalar, delete,
            // block array, replacing block - replaces the base wholesale. An
            // ordinary block that lands on a non-block is a replacement
            // barrier too: a scalar, null, or deletion followed by an object
            // must not resurrect the base's children.
            (overlay, _) => {
                let mut replacement = overlay;
                if let Node::Block(block) = &mut replacement {
                    if !block.replace {
                        block.replace = true;
                    }
                }
                result[position] = replacement;
            }
        }
    }

    Ok(result)
}

/// Preserve each source comment exactly once when cached imports meet again.
///
/// Compare source identity, not text: identical comments at different source
/// locations must all survive. Cached parse results share their comment
/// strings, so pointer identity identifies the same source comment.
fn concat_comments(a: &[String], b: &[String]) -> Vec<String> {
    if b.is_empty() {
        return a.to_vec();
    }
    if a.is_empty() {
        return b.to_vec();
    }
    let mut seen: Vec<(&str, usize)> = Vec::new();
    let mut out = Vec::with_capacity(a.len() + b.len());
    for comments in [a, b] {
        for comment in comments {
            let identity = (comment.as_str(), comment.len());
            if seen.contains(&identity) {
                continue;
            }
            seen.push(identity);
            out.push(comment.clone());
        }
    }
    out
}

/// Finish a composed overlay against an empty base: deletes disappear and
/// replacement flags clear, including inside nested values. The input tree is
/// never mutated; a finalized tree is data, not reusable operation history.
#[must_use]
pub fn materialize_nodes(nodes: &[Node]) -> Vec<Node> {
    let mut out = Vec::with_capacity(nodes.len());
    for node in nodes {
        match node {
            Node::Delete(_) | Node::None => continue,
            Node::Field(field) => {
                let mut copy = field.clone();
                copy.value = materialize_value(&field.value);
                out.push(Node::Field(copy));
            }
            Node::Block(block) => {
                let mut copy = block.clone();
                copy.replace = false;
                copy.children = materialize_nodes(&block.children);
                out.push(Node::Block(copy));
            }
            Node::BlockArray(block_array) => {
                let mut copy = block_array.clone();
                copy.items = block_array.items.iter().map(materialize_value).collect();
                out.push(Node::BlockArray(copy));
            }
        }
    }
    out
}

fn materialize_value(value: &Value) -> Value {
    match value {
        Value::Object(ObjectBody {
            children,
            trailing_comments,
        }) => Value::Object(ObjectBody {
            children: materialize_nodes(children),
            trailing_comments: trailing_comments.clone(),
        }),
        Value::Array(array) => Value::Array(Array {
            element_type: array.element_type,
            items: array.items.iter().map(materialize_value).collect(),
            trailing_comments: array.trailing_comments.clone(),
        }),
        other => other.clone(),
    }
}
