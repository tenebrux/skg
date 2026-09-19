//! Serde decoding: materialized SKG values → native Rust types.
//!
//! The target type provides the schema through `#[derive(Deserialize)]`;
//! there is no separate schema language. Supported Serde attributes:
//!
//! - `#[serde(rename = "...")]` and `#[serde(rename_all = "...")]` for fields
//!   and enum variants;
//! - `#[serde(default)]` and `#[serde(default = "path")]` for absent fields;
//! - `#[serde(skip)]` and `#[serde(skip_serializing_if = "...")]`;
//! - `#[serde(flatten)]` for embedded structures;
//! - `#[serde(deny_unknown_fields)`, plus the runtime
//!   [`DecodeOptions::reject_unknown_fields`];
//! - `#[serde(from = "...")]`, `#[serde(try_from = "...")]`, and manual
//!   `Deserialize` implementations as custom-decoder hooks.
//!
//! Null and absence are distinct. An absent field either has a Serde default
//! or fails with `missing_field`; explicit `null` requires `Option` (or a
//! custom decoder) and never falls back to a default.

use serde::de::{
    DeserializeOwned, DeserializeSeed, EnumAccess, MapAccess, SeqAccess, VariantAccess, Visitor,
};
use serde::forward_to_deserialize_any;
use std::rc::Rc;

use crate::error::{DecodeError, NativeCode, SourceLocation};
use crate::merge::materialize_nodes;
use crate::model::{Node, ObjectBody, Value};
use crate::parser::parse_bytes;

/// Bound on native conversion recursion, independent of the parser's syntax
/// depth limit. Mirrors the Go and Zig typed loaders.
pub const MAX_NATIVE_NESTING_DEPTH: usize = 256;

/// Runtime conversion policy for typed decoding.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct DecodeOptions {
    /// Fail on input fields the target type does not name. Default ignores
    /// them.
    pub reject_unknown_fields: bool,
    /// Permit float narrowing and integer-to-float conversion that rounds.
    /// Overflow to infinity remains an error.
    pub allow_lossy_numbers: bool,
}

impl DecodeOptions {
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

/// Decode a parsed document into `T`, applying its local overlay first.
pub fn from_document<T: DeserializeOwned>(
    document: &crate::model::Document,
) -> Result<T, DecodeError> {
    from_document_with(document, &DecodeOptions::default())
}

/// [`from_document`] with explicit conversion options.
pub fn from_document_with<T: DeserializeOwned>(
    document: &crate::model::Document,
    options: &DecodeOptions,
) -> Result<T, DecodeError> {
    let children = materialize_nodes(&document.children);
    let root = Value::Object(ObjectBody {
        children,
        trailing_comments: Vec::new(),
    });
    let context = Rc::new(Context {
        options: options.clone(),
        source: SourceLocation::new(document.path.clone(), 0, 0),
    });
    let deserializer = ValueDeserializer {
        value: view_value(&root),
        context,
        segments: Vec::new(),
        depth: 0,
    };
    T::deserialize(deserializer).map_err(convert_error)
}

/// Parse `source` and decode the local document into `T`.
///
/// Imports are recorded but never read; use the resolver before decoding when
/// imports should load.
pub fn from_str<T: DeserializeOwned>(source: &str) -> Result<T, DecodeError> {
    from_bytes_with(source.as_bytes(), "<string>", &DecodeOptions::default())
}

/// [`from_str`] with a diagnostic path and conversion options.
pub fn from_str_with<T: DeserializeOwned>(
    source: &str,
    path: impl Into<String>,
    options: &DecodeOptions,
) -> Result<T, DecodeError> {
    from_bytes_with(source.as_bytes(), path, options)
}

/// [`from_str`] over raw bytes.
pub fn from_bytes<T: DeserializeOwned>(source: &[u8]) -> Result<T, DecodeError> {
    from_bytes_with(source, "<string>", &DecodeOptions::default())
}

/// [`from_str`] over raw bytes, with a diagnostic path and options.
pub fn from_bytes_with<T: DeserializeOwned>(
    source: &[u8],
    path: impl Into<String>,
    options: &DecodeOptions,
) -> Result<T, DecodeError> {
    match parse_bytes(source, path) {
        Ok(document) => from_document_with(&document, options),
        Err(error) => Err(DecodeError {
            code: NativeCode::ParseError,
            field_path: String::new(),
            source: SourceLocation::new(
                error.diagnostic.path,
                error.diagnostic.line,
                error.diagnostic.col,
            ),
            message: error.diagnostic.message,
        }),
    }
}

fn convert_error(error: Error) -> DecodeError {
    DecodeError {
        code: error.code(),
        field_path: error.path.unwrap_or_default(),
        source: error.source.unwrap_or_default(),
        message: error.message,
    }
}

/// Internal conversion error. `path` and `source` are filled in as the error
/// climbs past the named node that owns the failing value; `None` at the top
/// means the document root.
#[derive(Debug)]
pub(crate) struct Error {
    kind: ErrorKind,
    path: Option<String>,
    source: Option<SourceLocation>,
    message: String,
}

#[derive(Debug)]
enum ErrorKind {
    TypeMismatch,
    MissingField(String),
    UnknownField,
    InvalidEnum,
    NumberOutOfRange,
    InexactNumber,
    UnsupportedType,
    NestingTooDeep,
    Custom,
}

impl Error {
    fn new(kind: ErrorKind, message: impl Into<String>) -> Self {
        Error {
            kind,
            path: None,
            source: None,
            message: message.into(),
        }
    }

    fn code(&self) -> NativeCode {
        match self.kind {
            ErrorKind::TypeMismatch => NativeCode::TypeMismatch,
            ErrorKind::MissingField(_) => NativeCode::MissingField,
            ErrorKind::UnknownField => NativeCode::UnknownField,
            ErrorKind::InvalidEnum => NativeCode::InvalidEnum,
            ErrorKind::NumberOutOfRange => NativeCode::NumberOutOfRange,
            ErrorKind::InexactNumber => NativeCode::InexactNumber,
            ErrorKind::UnsupportedType => NativeCode::UnsupportedType,
            ErrorKind::NestingTooDeep => NativeCode::NestingTooDeep,
            ErrorKind::Custom => NativeCode::CustomError,
        }
    }

    fn at(mut self, path: String, source: SourceLocation) -> Self {
        if self.path.is_none() {
            self.path = Some(path);
        }
        if self.source.is_none() {
            self.source = Some(source);
        }
        self
    }
}

impl serde::de::Error for Error {
    fn custom<T: std::fmt::Display>(message: T) -> Self {
        Error::new(ErrorKind::Custom, message.to_string())
    }

    fn missing_field(field: &'static str) -> Self {
        Error::new(
            ErrorKind::MissingField(field.to_string()),
            format!("missing field `{field}`"),
        )
    }

    fn unknown_field(field: &str, expected: &'static [&'static str]) -> Self {
        Error::new(
            ErrorKind::UnknownField,
            format!("unknown field `{field}`{}", expected_list(expected)),
        )
    }

    fn unknown_variant(variant: &str, expected: &'static [&'static str]) -> Self {
        Error::new(
            ErrorKind::InvalidEnum,
            format!("unknown enum tag `{variant}`{}", expected_list(expected)),
        )
    }
}

fn expected_list(expected: &[&str]) -> String {
    match expected {
        [] => String::new(),
        [one] => format!(", expected `{one}`"),
        [first, second] => format!(", expected `{first}` or `{second}`"),
        many => {
            let list = many
                .iter()
                .map(|e| format!("`{e}`"))
                .collect::<Vec<_>>()
                .join(", ");
            format!(", expected one of {list}")
        }
    }
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for Error {}

/// A JSON Pointer segment.
#[derive(Debug, Clone)]
enum Segment {
    Key(String),
    Index(usize),
}

fn render_path(segments: &[Segment]) -> String {
    let mut out = String::new();
    for segment in segments {
        match segment {
            Segment::Key(key) => {
                out.push('/');
                for c in key.chars() {
                    match c {
                        '~' => out.push_str("~0"),
                        '/' => out.push_str("~1"),
                        other => out.push(other),
                    }
                }
            }
            Segment::Index(index) => {
                out.push('/');
                out.push_str(&index.to_string());
            }
        }
    }
    out
}

fn escape_key(key: &str) -> String {
    let mut out = String::with_capacity(key.len());
    for c in key.chars() {
        match c {
            '~' => out.push_str("~0"),
            '/' => out.push_str("~1"),
            other => out.push(other),
        }
    }
    out
}

/// Shared decode state threaded through nested deserializers.
struct Context {
    options: DecodeOptions,
    source: SourceLocation,
}

/// The runtime value a deserializer decodes from. Objects are node lists, so
/// named provenance survives into diagnostics.
#[derive(Clone, Copy)]
enum Val<'de> {
    Int(i64),
    Float(f64),
    Bool(bool),
    Str(&'de str),
    Null,
    Array(&'de [Value]),
    Object(&'de [Node]),
}

fn view_value(value: &Value) -> Val<'_> {
    match value {
        Value::Int(n) => Val::Int(*n),
        Value::Float(f) => Val::Float(*f),
        Value::Bool(b) => Val::Bool(*b),
        Value::String(s) => Val::Str(s.as_str()),
        Value::Null => Val::Null,
        Value::Array(array) => Val::Array(&array.items),
        Value::Object(object) => Val::Object(&object.children),
    }
}

/// The value a named node carries. Delete markers are gone by the time
/// decoding starts; the arm exists to keep the match exhaustive.
fn view_node(node: &Node) -> Option<Val<'_>> {
    match node {
        Node::Field(field) => Some(view_value(&field.value)),
        Node::Block(block) => Some(Val::Object(&block.children)),
        Node::BlockArray(block_array) => Some(Val::Array(&block_array.items)),
        Node::Delete(_) | Node::None => None,
    }
}

fn node_location(node: &Node) -> SourceLocation {
    match node {
        Node::Field(field) => SourceLocation::new(field.path.clone(), field.line, field.col),
        Node::Block(block) => SourceLocation::new(block.path.clone(), block.line, block.col),
        Node::BlockArray(block_array) => {
            SourceLocation::new(block_array.path.clone(), block_array.line, block_array.col)
        }
        Node::Delete(delete) => SourceLocation::new(delete.path.clone(), delete.line, delete.col),
        Node::None => SourceLocation::default(),
    }
}

struct ValueDeserializer<'de> {
    value: Val<'de>,
    context: Rc<Context>,
    segments: Vec<Segment>,
    depth: usize,
}

impl<'de> ValueDeserializer<'de> {
    /// Swap out the value so views borrowed from it survive `&mut self` use.
    fn take(&mut self) -> Val<'de> {
        std::mem::replace(&mut self.value, Val::Null)
    }

    fn path(&self) -> String {
        render_path(&self.segments)
    }

    fn type_error(&self, message: impl Into<String>) -> Error {
        Error::new(ErrorKind::TypeMismatch, message).at(self.path(), self.context.source.clone())
    }

    fn range_error(&self, message: impl Into<String>) -> Error {
        Error::new(ErrorKind::NumberOutOfRange, message)
            .at(self.path(), self.context.source.clone())
    }

    fn inexact_error(&self, message: impl Into<String>) -> Error {
        Error::new(ErrorKind::InexactNumber, message).at(self.path(), self.context.source.clone())
    }

    fn child(
        &self,
        value: Val<'de>,
        segment: Segment,
        source: SourceLocation,
    ) -> Result<ValueDeserializer<'de>, Error> {
        if self.depth >= MAX_NATIVE_NESTING_DEPTH {
            return Err(Error::new(
                ErrorKind::NestingTooDeep,
                format!("native target nesting exceeds {MAX_NATIVE_NESTING_DEPTH}"),
            )
            .at(self.path(), self.context.source.clone()));
        }
        let mut segments = self.segments.clone();
        segments.push(segment);
        Ok(ValueDeserializer {
            value,
            context: Rc::new(Context {
                options: self.context.options.clone(),
                source,
            }),
            segments,
            depth: self.depth + 1,
        })
    }

    fn number_mismatch(&self) -> Error {
        self.type_error(number_expectation(self.value))
    }
}

fn number_expectation(value: Val<'_>) -> String {
    match value {
        Val::Int(_) => "expected a number".to_string(),
        Val::Float(_) => "expected an integer, found a float".to_string(),
        Val::Str(s) => format!("expected a number, found the string `{s}`"),
        Val::Bool(_) => "expected a number, found a boolean".to_string(),
        Val::Null => "expected a number, found null".to_string(),
        Val::Array(_) => "expected a number, found an array".to_string(),
        Val::Object(_) => "expected a number, found an object".to_string(),
    }
}

/// Exactness of i64 → binary float with `precision` mantissa bits: the
/// magnitude fits, or its low zero bits absorb the rounding.
fn integer_exact(n: i64, precision: u32) -> bool {
    let magnitude = n.unsigned_abs();
    let needed = (64 - magnitude.leading_zeros() as i32) - precision as i32;
    needed <= 0 || magnitude.trailing_zeros() as i32 >= needed
}

macro_rules! deserialize_signed {
    ($method:ident, $visit:ident, $t:ty) => {
        fn $method<V>(self, visitor: V) -> Result<V::Value, Error>
        where
            V: Visitor<'de>,
        {
            match self.value {
                Val::Int(n) => {
                    if n < <$t>::MIN as i64 || n > <$t>::MAX as i64 {
                        return Err(
                            self.range_error(format!("integer {n} does not fit the target type"))
                        );
                    }
                    visitor.$visit(n as $t)
                }
                _ => Err(self.number_mismatch()),
            }
        }
    };
}

macro_rules! deserialize_unsigned {
    ($method:ident, $visit:ident, $t:ty) => {
        fn $method<V>(self, visitor: V) -> Result<V::Value, Error>
        where
            V: Visitor<'de>,
        {
            match self.value {
                Val::Int(n) => {
                    if n < 0 || n > <$t>::MAX as i64 {
                        return Err(
                            self.range_error(format!("integer {n} does not fit the target type"))
                        );
                    }
                    visitor.$visit(n as $t)
                }
                _ => Err(self.number_mismatch()),
            }
        }
    };
}

impl<'de> serde::Deserializer<'de> for ValueDeserializer<'de> {
    type Error = Error;

    fn deserialize_any<V>(mut self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.take() {
            Val::Int(n) => visitor.visit_i64(n),
            Val::Float(f) => visitor.visit_f64(f),
            Val::Bool(b) => visitor.visit_bool(b),
            Val::Str(s) => visitor.visit_borrowed_str(s),
            Val::Null => visitor.visit_unit(),
            Val::Array(items) => visitor.visit_seq(SeqDeserializer::new(&mut self, items)),
            Val::Object(children) => visitor.visit_map(MapDeserializer::new(&mut self, children)),
        }
    }

    fn deserialize_bool<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.value {
            Val::Bool(b) => visitor.visit_bool(b),
            _ => Err(self.type_error("expected a boolean")),
        }
    }

    deserialize_signed!(deserialize_i8, visit_i8, i8);
    deserialize_signed!(deserialize_i16, visit_i16, i16);
    deserialize_signed!(deserialize_i32, visit_i32, i32);
    deserialize_signed!(deserialize_i64, visit_i64, i64);
    deserialize_unsigned!(deserialize_u8, visit_u8, u8);
    deserialize_unsigned!(deserialize_u16, visit_u16, u16);
    deserialize_unsigned!(deserialize_u32, visit_u32, u32);

    fn deserialize_u64<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.value {
            Val::Int(n) => {
                if n < 0 {
                    return Err(
                        self.range_error(format!("integer {n} does not fit the target type"))
                    );
                }
                visitor.visit_u64(n as u64)
            }
            _ => Err(self.number_mismatch()),
        }
    }

    fn deserialize_f32<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.value {
            Val::Float(f) => {
                let narrowed = f as f32;
                if !narrowed.is_finite() {
                    return Err(self.range_error("number is out of range for a 32-bit float"));
                }
                if !self.context.options.allow_lossy_numbers && narrowed as f64 != f {
                    return Err(
                        self.inexact_error("conversion to a 32-bit float would lose precision")
                    );
                }
                visitor.visit_f32(narrowed)
            }
            Val::Int(n) => {
                // Convert directly: i64 -> f64 -> f32 can round twice.
                let narrowed = n as f32;
                if !narrowed.is_finite() {
                    return Err(self.range_error("integer is out of range for a 32-bit float"));
                }
                if !self.context.options.allow_lossy_numbers && !integer_exact(n, 24) {
                    return Err(
                        self.inexact_error("conversion to a 32-bit float would lose precision")
                    );
                }
                visitor.visit_f32(narrowed)
            }
            _ => Err(self.number_mismatch()),
        }
    }

    fn deserialize_f64<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.value {
            Val::Float(f) => visitor.visit_f64(f),
            Val::Int(n) => {
                if !self.context.options.allow_lossy_numbers && !integer_exact(n, 53) {
                    return Err(
                        self.inexact_error("conversion to a 64-bit float would lose precision")
                    );
                }
                visitor.visit_f64(n as f64)
            }
            _ => Err(self.number_mismatch()),
        }
    }

    fn deserialize_char<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.value {
            Val::Str(s) => {
                let mut chars = s.chars();
                match (chars.next(), chars.next()) {
                    (Some(c), None) => visitor.visit_char(c),
                    _ => Err(self.type_error("expected a single-character string")),
                }
            }
            _ => Err(self.type_error("expected a single-character string")),
        }
    }

    fn deserialize_str<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.value {
            Val::Str(s) => visitor.visit_borrowed_str(s),
            _ => Err(self.type_error("expected a string")),
        }
    }

    fn deserialize_string<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        self.deserialize_str(visitor)
    }

    fn deserialize_bytes<V>(mut self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.take() {
            Val::Str(s) => visitor.visit_bytes(s.as_bytes()),
            Val::Array(items) => visitor.visit_seq(SeqDeserializer::new(&mut self, items)),
            _ => Err(self.type_error("expected a string or array of bytes")),
        }
    }

    fn deserialize_byte_buf<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        self.deserialize_bytes(visitor)
    }

    fn deserialize_option<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.value {
            Val::Null => visitor.visit_none(),
            _ => visitor.visit_some(self),
        }
    }

    fn deserialize_unit<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.value {
            Val::Null => visitor.visit_unit(),
            _ => Err(self.type_error("expected null")),
        }
    }

    fn deserialize_unit_struct<V>(self, _name: &'static str, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        self.deserialize_unit(visitor)
    }

    fn deserialize_newtype_struct<V>(
        self,
        _name: &'static str,
        visitor: V,
    ) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        visitor.visit_newtype_struct(self)
    }

    fn deserialize_seq<V>(mut self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.take() {
            Val::Array(items) => visitor.visit_seq(SeqDeserializer::new(&mut self, items)),
            _ => Err(self.type_error("expected an array")),
        }
    }

    fn deserialize_tuple<V>(mut self, len: usize, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.take() {
            Val::Array(items) => {
                if items.len() != len {
                    return Err(self.type_error(format!(
                        "expected an array of {len} elements, found {}",
                        items.len()
                    )));
                }
                visitor.visit_seq(SeqDeserializer::new(&mut self, items))
            }
            _ => Err(self.type_error("expected an array")),
        }
    }

    fn deserialize_tuple_struct<V>(
        self,
        _name: &'static str,
        len: usize,
        visitor: V,
    ) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        self.deserialize_tuple(len, visitor)
    }

    fn deserialize_map<V>(mut self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.take() {
            Val::Object(children) => visitor.visit_map(MapDeserializer::new(&mut self, children)),
            _ => Err(self.type_error("expected an object")),
        }
    }

    fn deserialize_struct<V>(
        mut self,
        _name: &'static str,
        _fields: &'static [&'static str],
        visitor: V,
    ) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        let entry_path = self.path();
        let entry_source = self.context.source.clone();
        match self.take() {
            Val::Object(children) => visitor
                .visit_map(MapDeserializer::new(&mut self, children))
                .map_err(|error| fill_struct_error(error, &entry_path, &entry_source)),
            _ => Err(self.type_error("expected an object")),
        }
    }

    fn deserialize_enum<V>(
        self,
        _name: &'static str,
        _variants: &'static [&'static str],
        visitor: V,
    ) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.value {
            Val::Str(tag) => visitor.visit_enum(StrEnumAccess { tag }),
            Val::Object(children) => {
                let mut entries = children
                    .iter()
                    .filter(|node| !matches!(node, Node::Delete(_) | Node::None));
                let Some(first) = entries.next() else {
                    return Err(
                        self.type_error("expected an enum tag string or a single-entry object")
                    );
                };
                if entries.next().is_some() {
                    return Err(
                        self.type_error("expected an enum tag string or a single-entry object")
                    );
                }
                let Some(tag) = first.key() else {
                    return Err(self.type_error("expected an enum tag string"));
                };
                let Some(inner) = view_node(first) else {
                    return Err(self.type_error("expected an enum tag string"));
                };
                visitor.visit_enum(ObjectEnumAccess {
                    tag: tag.to_string(),
                    inner,
                    context: self.context.clone(),
                    segments: self.segments,
                    depth: self.depth,
                })
            }
            _ => Err(self.type_error("expected an enum tag string or a single-entry object")),
        }
    }

    fn deserialize_identifier<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        match self.value {
            Val::Str(s) => visitor.visit_borrowed_str(s),
            _ => Err(self.type_error("expected an identifier string")),
        }
    }

    fn deserialize_ignored_any<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        // Derived struct visitors reach here only for fields they do not
        // recognize, so this is where unknown-field rejection lives.
        if self.context.options.reject_unknown_fields {
            if let Some(Segment::Key(key)) = self.segments.last() {
                return Err(
                    Error::new(ErrorKind::UnknownField, format!("unknown field `{key}`"))
                        .at(self.path(), self.context.source.clone()),
                );
            }
        }
        visitor.visit_unit()
    }
}

/// Fill in the pointer and source of an error leaving a struct's scope.
///
/// `missing_field` is raised by the derived visitor without location context;
/// the missing field's name completes the containing scope's pointer, which is
/// what makes `/value/id` reachable for a nested record.
fn fill_struct_error(mut error: Error, path: &str, source: &SourceLocation) -> Error {
    if error.path.is_none() {
        let suffix = match &error.kind {
            ErrorKind::MissingField(field) => format!("/{}", escape_key(field)),
            _ => String::new(),
        };
        error.path = Some(format!("{path}{suffix}"));
    }
    if error.source.is_none() {
        error.source = Some(source.clone());
    }
    error
}

struct SeqDeserializer<'a, 'de> {
    de: &'a mut ValueDeserializer<'de>,
    items: &'de [Value],
    index: usize,
}

impl<'a, 'de> SeqDeserializer<'a, 'de> {
    fn new(de: &'a mut ValueDeserializer<'de>, items: &'de [Value]) -> Self {
        SeqDeserializer {
            de,
            items,
            index: 0,
        }
    }
}

impl<'de> SeqAccess<'de> for SeqDeserializer<'_, 'de> {
    type Error = Error;

    fn next_element_seed<T>(&mut self, seed: T) -> Result<Option<T::Value>, Error>
    where
        T: DeserializeSeed<'de>,
    {
        if self.index >= self.items.len() {
            return Ok(None);
        }
        let item = &self.items[self.index];
        let index = self.index;
        self.index += 1;
        // An array index appears in the field path, but its location is the
        // containing named value, matching the Go and Zig loaders.
        let mut child_segments = self.de.segments.clone();
        child_segments.push(Segment::Index(index));
        let child_path = render_path(&child_segments);
        let child = self.de.child(
            view_value(item),
            Segment::Index(index),
            self.de.context.source.clone(),
        )?;
        seed.deserialize(child)
            .map(Some)
            .map_err(|error| error.at(child_path, self.de.context.source.clone()))
    }
}

struct MapDeserializer<'a, 'de> {
    de: &'a mut ValueDeserializer<'de>,
    children: &'de [Node],
    index: usize,
}

impl<'a, 'de> MapDeserializer<'a, 'de> {
    fn new(de: &'a mut ValueDeserializer<'de>, children: &'de [Node]) -> Self {
        MapDeserializer {
            de,
            children,
            index: 0,
        }
    }
}

impl<'de> MapAccess<'de> for MapDeserializer<'_, 'de> {
    type Error = Error;

    fn next_key_seed<K>(&mut self, seed: K) -> Result<Option<K::Value>, Error>
    where
        K: DeserializeSeed<'de>,
    {
        while self.index < self.children.len() {
            let node = &self.children[self.index];
            if matches!(node, Node::Delete(_) | Node::None) {
                self.index += 1;
                continue;
            }
            return seed
                .deserialize(StrDeserializer {
                    key: node.key().unwrap_or_default(),
                })
                .map(Some);
        }
        Ok(None)
    }

    fn next_value_seed<V>(&mut self, seed: V) -> Result<V::Value, Error>
    where
        V: DeserializeSeed<'de>,
    {
        let node = &self.children[self.index];
        self.index += 1;
        let key = node.key().unwrap_or_default().to_string();
        let mut child_segments = self.de.segments.clone();
        child_segments.push(Segment::Key(key.clone()));
        let child_path = render_path(&child_segments);
        let source = node_location(node);
        let Some(value) = view_node(node) else {
            return Err(self
                .de
                .type_error("unexpected delete marker after materialization"));
        };
        let child = self
            .de
            .child(value, Segment::Key(key.clone()), source.clone())?;
        seed.deserialize(child)
            .map_err(|error| error.at(child_path, source))
    }
}

/// Deserializer for object keys, struct field names, and enum variant tags.
struct StrDeserializer<'a> {
    key: &'a str,
}

impl<'de> serde::Deserializer<'de> for StrDeserializer<'_> {
    type Error = Error;

    fn deserialize_any<V>(self, visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        visitor.visit_str(self.key)
    }

    forward_to_deserialize_any! {
        bool f32 f64 char str string bytes byte_buf option unit unit_struct
        newtype_struct seq tuple tuple_struct map struct enum identifier
        ignored_any i8 i16 i32 i64 u8 u16 u32 u64
    }
}

/// Enum access for a plain string tag: `mode: "local"`.
struct StrEnumAccess<'de> {
    tag: &'de str,
}

impl<'de> EnumAccess<'de> for StrEnumAccess<'de> {
    type Error = Error;
    type Variant = UnitOnly;

    fn variant_seed<V>(self, seed: V) -> Result<(V::Value, Self::Variant), Error>
    where
        V: DeserializeSeed<'de>,
    {
        let tag = seed.deserialize(StrDeserializer { key: self.tag })?;
        Ok((tag, UnitOnly))
    }
}

/// Variant access that only unit variants can use; a string tag carries no
/// payload.
struct UnitOnly;

impl<'de> VariantAccess<'de> for UnitOnly {
    type Error = Error;

    fn unit_variant(self) -> Result<(), Error> {
        Ok(())
    }

    fn newtype_variant_seed<T>(self, _seed: T) -> Result<T::Value, Error>
    where
        T: DeserializeSeed<'de>,
    {
        Err(Error::new(
            ErrorKind::TypeMismatch,
            "enum tag string carries no payload; use the block form for payload variants",
        ))
    }

    fn tuple_variant<V>(self, _len: usize, _visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        Err(Error::new(
            ErrorKind::TypeMismatch,
            "enum tag string carries no payload",
        ))
    }

    fn struct_variant<V>(
        self,
        _fields: &'static [&'static str],
        _visitor: V,
    ) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        Err(Error::new(
            ErrorKind::TypeMismatch,
            "enum tag string carries no payload",
        ))
    }
}

/// Enum access for the block form: `service { host: "main" }` decodes as
/// variant `service` with the single entry's value as payload.
struct ObjectEnumAccess<'de> {
    tag: String,
    inner: Val<'de>,
    context: Rc<Context>,
    segments: Vec<Segment>,
    depth: usize,
}

impl<'de> EnumAccess<'de> for ObjectEnumAccess<'de> {
    type Error = Error;
    type Variant = Self;

    fn variant_seed<V>(self, seed: V) -> Result<(V::Value, Self::Variant), Error>
    where
        V: DeserializeSeed<'de>,
    {
        let tag = seed.deserialize(StrDeserializer { key: &self.tag })?;
        Ok((tag, self))
    }
}

impl<'de> VariantAccess<'de> for ObjectEnumAccess<'de> {
    type Error = Error;

    fn unit_variant(self) -> Result<(), Error> {
        // `Tag { }` decodes a unit variant. A nonempty body is a mistake the
        // caller should hear about, not a payload to discard silently.
        if let Val::Object(children) = self.inner {
            let has_entries = children
                .iter()
                .any(|node| !matches!(node, Node::Delete(_) | Node::None));
            if has_entries {
                let path = render_path(&self.segments) + "/" + &escape_key(&self.tag);
                return Err(Error::new(
                    ErrorKind::TypeMismatch,
                    format!("unit enum variant `{}` must have an empty body", self.tag),
                )
                .at(path, self.context.source.clone()));
            }
        }
        Ok(())
    }

    fn newtype_variant_seed<T>(self, seed: T) -> Result<T::Value, Error>
    where
        T: DeserializeSeed<'de>,
    {
        seed.deserialize(self.payload_deserializer()?)
    }

    fn tuple_variant<V>(self, _len: usize, _visitor: V) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        Err(Error::new(
            ErrorKind::UnsupportedType,
            "tuple enum variants are not supported in SKG",
        ))
    }

    fn struct_variant<V>(
        self,
        _fields: &'static [&'static str],
        visitor: V,
    ) -> Result<V::Value, Error>
    where
        V: Visitor<'de>,
    {
        serde::Deserializer::deserialize_struct(self.payload_deserializer()?, "", &[], visitor)
    }
}

impl<'de> ObjectEnumAccess<'de> {
    /// The payload decodes one level under the variant's own name, so field
    /// pointers read `/service/port` for `service { port: 1 }`.
    fn payload_deserializer(&self) -> Result<ValueDeserializer<'de>, Error> {
        if self.depth >= MAX_NATIVE_NESTING_DEPTH {
            return Err(Error::new(
                ErrorKind::NestingTooDeep,
                format!("native target nesting exceeds {MAX_NATIVE_NESTING_DEPTH}"),
            )
            .at(render_path(&self.segments), self.context.source.clone()));
        }
        let mut segments = self.segments.clone();
        segments.push(Segment::Key(self.tag.clone()));
        Ok(ValueDeserializer {
            value: self.inner,
            context: self.context.clone(),
            segments,
            depth: self.depth + 1,
        })
    }
}
