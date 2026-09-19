//! The public document model.
//!
//! The representation preserves every distinction V1 requires: field, block,
//! block array and delete nodes; the seven value types; array element tags;
//! replacement flags and delete markers on composed overlays; and comment
//! trivia. All strings are owned, so a [`Document`] is valid independently of
//! the input buffer.

use crate::error::Position;

/// The type tag of a [`Value`].
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default)]
pub enum ValueType {
    #[default]
    String,
    Int,
    Float,
    Bool,
    Null,
    Array,
    Object,
}

impl ValueType {
    /// The registry spelling used by shared fixtures and diagnostics.
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            ValueType::String => "string",
            ValueType::Int => "int",
            ValueType::Float => "float",
            ValueType::Bool => "bool",
            ValueType::Null => "null",
            ValueType::Array => "array",
            ValueType::Object => "object",
        }
    }
}

/// Where a comment was written.
///
/// An origin has three parts: the labeled path of the file, a digest of that
/// file's exact source bytes, and the comment's sequence number within the
/// parse. Two comments from one parse never share an origin; one file seen
/// twice through an import graph does, because the resolver caches the one
/// parse; and two independent parses share an origin only when both the
/// labeled path and the source bytes are identical, in which case the
/// comments are indistinguishable. Merge deduplication compares origins;
/// equal comment text alone never means "the same comment".
#[derive(Debug, Clone, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub struct CommentOrigin {
    /// The labeled path of the file the comment was written in.
    pub path: String,
    /// Digest of the file's exact source bytes, distinguishing parses that
    /// share a labeled path (two independent `parse` calls, for instance).
    pub source: u64,
    /// The comment's sequence number within that parse.
    pub sequence: u64,
}

/// One comment attached to a node as trivia: its text plus where it was
/// written.
///
/// Provenance is per comment because merges append comments from different
/// sources onto one node. `None` marks a programmatically built comment,
/// which merges never deduplicate.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct Comment {
    /// The comment text, including the leading `#` and excluding the
    /// trailing newline.
    pub text: String,
    pub origin: Option<CommentOrigin>,
}

impl Comment {
    /// A comment with no source provenance.
    #[must_use]
    pub fn new(text: impl Into<String>) -> Self {
        Comment {
            text: text.into(),
            origin: None,
        }
    }
}

impl From<&str> for Comment {
    fn from(text: &str) -> Self {
        Comment::new(text)
    }
}

impl From<String> for Comment {
    fn from(text: String) -> Self {
        Comment { text, origin: None }
    }
}

/// A typed array: all non-null elements share the outer element tag, checked
/// one level deep by the parser. Null entries preserve their positions.
#[derive(Debug, Clone, PartialEq, Default)]
pub struct Array {
    /// The non-null element tag; [`ValueType::String`] for an empty array and
    /// [`ValueType::Null`] for an all-null array.
    pub element_type: ValueType,
    pub items: Vec<Value>,
    /// Comments between the last element and the closing `]`.
    pub trailing_comments: Vec<Comment>,
}

/// An anonymous object: the value form of a block. Objects may appear at any
/// value position, including inside arrays.
#[derive(Debug, Clone, PartialEq, Default)]
pub struct ObjectBody {
    pub children: Vec<Node>,
    /// Comments between the last child and the closing `}`.
    pub trailing_comments: Vec<Comment>,
}

/// A scalar, array, or object value.
#[derive(Debug, Clone, PartialEq, Default)]
pub enum Value {
    Int(i64),
    Float(f64),
    Bool(bool),
    /// Unescaped string content, without surrounding quotes.
    String(String),
    /// An explicit present null. Distinct from a deleted or absent key.
    #[default]
    Null,
    Array(Array),
    Object(ObjectBody),
}

impl Value {
    /// The value's type tag.
    #[must_use]
    pub fn value_type(&self) -> ValueType {
        match self {
            Value::Int(_) => ValueType::Int,
            Value::Float(_) => ValueType::Float,
            Value::Bool(_) => ValueType::Bool,
            Value::String(_) => ValueType::String,
            Value::Null => ValueType::Null,
            Value::Array(_) => ValueType::Array,
            Value::Object(_) => ValueType::Object,
        }
    }
}

/// `key: value`
#[derive(Debug, Clone, PartialEq, Default)]
pub struct Field {
    /// Source provenance; empty for programmatically built values.
    pub path: String,
    pub key: String,
    pub value: Value,
    pub line: u32,
    pub col: u32,
    pub leading_comments: Vec<Comment>,
    /// One comment on the same line as the value, if any.
    pub trailing_comment: Option<Comment>,
}

/// A named scope: `name { children }`
#[derive(Debug, Clone, PartialEq, Default)]
pub struct Block {
    pub path: String,
    /// `@replace`: do not inherit children from an earlier value. Cleared by
    /// materialization.
    pub replace: bool,
    pub name: String,
    pub children: Vec<Node>,
    pub line: u32,
    pub col: u32,
    pub leading_comments: Vec<Comment>,
    /// Comments between the last child and the closing `}`.
    pub trailing_comments: Vec<Comment>,
}

/// A named list of object and null entries: `name [ { ... } null ]`
#[derive(Debug, Clone, PartialEq, Default)]
pub struct BlockArray {
    pub path: String,
    pub name: String,
    /// Each item is an object or null value.
    pub items: Vec<Value>,
    pub line: u32,
    pub col: u32,
    pub leading_comments: Vec<Comment>,
    /// Comments between the last entry and the closing `]`.
    pub trailing_comments: Vec<Comment>,
}

/// Records `@delete key` until the overlay is materialized.
#[derive(Debug, Clone, PartialEq, Default)]
pub struct Delete {
    pub path: String,
    pub key: String,
    pub line: u32,
    pub col: u32,
    pub leading_comments: Vec<Comment>,
    pub trailing_comment: Option<Comment>,
}

/// A field, block, block array, or deletion operation.
#[derive(Debug, Clone, PartialEq, Default)]
pub enum Node {
    Delete(Delete),
    Field(Field),
    Block(Block),
    BlockArray(BlockArray),
    /// Nodes with no variant set carry no data and are dropped by merges.
    /// The parser never produces this.
    #[default]
    None,
}

impl Node {
    /// The merge key of this node: the field key or the block/block-array
    /// name. `None` for a node with no variant set.
    #[must_use]
    pub fn key(&self) -> Option<&str> {
        match self {
            Node::Delete(delete) => Some(&delete.key),
            Node::Field(field) => Some(&field.key),
            Node::Block(block) => Some(&block.name),
            Node::BlockArray(block_array) => Some(&block_array.name),
            Node::None => None,
        }
    }

    /// The source position recorded on this node, if any.
    #[must_use]
    pub fn position(&self) -> Option<Position> {
        let (line, col) = match self {
            Node::Delete(delete) => (delete.line, delete.col),
            Node::Field(field) => (field.line, field.col),
            Node::Block(block) => (block.line, block.col),
            Node::BlockArray(block_array) => (block_array.line, block_array.col),
            Node::None => return None,
        };
        if line == 0 && col == 0 {
            None
        } else {
            Some(Position { line, col })
        }
    }

    /// The source provenance recorded on this node.
    #[must_use]
    pub fn path(&self) -> &str {
        match self {
            Node::Delete(delete) => &delete.path,
            Node::Field(field) => &field.path,
            Node::Block(block) => &block.path,
            Node::BlockArray(block_array) => &block_array.path,
            Node::None => "",
        }
    }

    /// The value carried by this node: the field value, the block children as
    /// an object, or the block-array entries as an object-typed array.
    #[must_use]
    pub fn value(&self) -> Option<Value> {
        match self {
            Node::Field(field) => Some(field.value.clone()),
            Node::Block(block) => Some(Value::Object(ObjectBody {
                children: block.children.clone(),
                trailing_comments: block.trailing_comments.clone(),
            })),
            Node::BlockArray(block_array) => Some(Value::Array(Array {
                element_type: ValueType::Object,
                items: block_array.items.clone(),
                trailing_comments: block_array.trailing_comments.clone(),
            })),
            Node::Delete(_) | Node::None => None,
        }
    }
}

/// The parsed representation of a single `.skg` file.
#[derive(Debug, Clone, PartialEq, Default)]
pub struct Document {
    /// Source provenance; empty for programmatically built documents.
    pub path: String,
    pub skg_version: Option<String>,
    pub schema_version: Option<String>,
    /// Raw import paths as written, in declaration order.
    pub import_paths: Vec<String>,
    /// Source position of each entry in [`Document::import_paths`], same
    /// length and order. Resolution failures are reported here.
    pub import_positions: Vec<Position>,
    pub children: Vec<Node>,
    /// File-loading APIs set this after applying all imports and operations;
    /// emission then omits the import statements.
    pub imports_resolved: bool,
    /// Comments before the first declaration.
    pub leading_comments: Vec<Comment>,
    /// Comments after the last node.
    pub trailing_comments: Vec<Comment>,
}

impl Document {
    /// An empty document with the given provenance path.
    #[must_use]
    pub fn new(path: impl Into<String>) -> Self {
        Document {
            path: path.into(),
            ..Document::default()
        }
    }
}
