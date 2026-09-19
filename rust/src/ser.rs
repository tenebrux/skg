//! Serde encoding: native Rust values → canonical SKG text.
//!
//! Structs and maps become blocks, sequences become arrays or block arrays,
//! unit enum variants become string tags, newtype and struct enum variants
//! become the single-entry block form, and `None` becomes an explicit null.
//! Maps are emitted with keys sorted, so output is deterministic; struct
//! fields keep declaration order. The same `#[serde(rename)]` attributes that
//! steer decoding steer encoding.

use serde::ser::{self, Serialize};

use crate::emit::emit;
use crate::error::EncodeError;
use crate::model::{Array, Block, BlockArray, Document, Field, Node, ObjectBody, Value, ValueType};
use crate::parser::MAX_FILE_SIZE;

/// Encode `value` into canonical SKG text.
///
/// The top-level value must serialize as a struct or map; its fields and
/// entries become the document's top-level nodes. Encoding rejects non-finite
/// floats, integers outside the signed 64-bit range, tuple enum variants, and
/// maps with non-string keys.
///
/// # Errors
///
/// Returns [`EncodeError`] for values the SKG grammar cannot express.
pub fn to_string<T: Serialize>(value: &T) -> Result<String, EncodeError> {
    let document = to_document(value)?;
    let text = emit(&document);
    if text.len() > MAX_FILE_SIZE {
        return Err(EncodeError::InvalidValue(format!(
            "encoded document exceeds the {MAX_FILE_SIZE}-byte file cap"
        )));
    }
    Ok(text)
}

/// Encode `value` into a [`Document`] without rendering it.
pub fn to_document<T: Serialize>(value: &T) -> Result<Document, EncodeError> {
    let mut serializer = Serializer::default();
    value.serialize(&mut serializer)?;
    serializer.finish()
}

#[derive(Debug)]
enum Frame {
    Array {
        items: Vec<Value>,
    },
    Map {
        entries: Vec<(String, Value)>,
        pending_key: Option<String>,
        sort: bool,
    },
}

#[derive(Debug, Default)]
struct Serializer {
    stack: Vec<Frame>,
    root: Option<Value>,
    depth: usize,
    /// The next scalar is a map key, not a value: it must be a string and
    /// fills the pending key slot.
    key_slot: bool,
    /// Open struct-variant wrappers record their variant names and close in
    /// LIFO order, so nested variants resolve to the right names.
    pending_variants: Vec<String>,
}

impl Serializer {
    const MAX_DEPTH: usize = 128;

    fn attach(&mut self, value: Value) -> Result<(), EncodeError> {
        if self.key_slot {
            self.key_slot = false;
            let Value::String(key) = value else {
                return Err(EncodeError::Unsupported(
                    "map keys must be strings; SKG object keys are text".into(),
                ));
            };
            match self.stack.last_mut() {
                Some(Frame::Map { pending_key, .. }) => {
                    *pending_key = Some(key);
                    return Ok(());
                }
                _ => {
                    return Err(EncodeError::InvalidValue(
                        "map key serialized outside a map".into(),
                    ));
                }
            }
        }
        match self.stack.last_mut() {
            Some(Frame::Array { items }) => items.push(value),
            Some(Frame::Map {
                entries,
                pending_key,
                ..
            }) => {
                let Some(key) = pending_key.take() else {
                    return Err(EncodeError::InvalidValue(
                        "map value serialized before its key".into(),
                    ));
                };
                entries.push((key, value));
            }
            None => {
                if self.root.is_some() {
                    return Err(EncodeError::InvalidValue(
                        "only one top-level value can be encoded".into(),
                    ));
                }
                self.root = Some(value);
            }
        }
        Ok(())
    }

    fn enter(&mut self, frame: Frame) -> Result<(), EncodeError> {
        self.depth += 1;
        if self.depth > Self::MAX_DEPTH {
            return Err(EncodeError::Unsupported(format!(
                "nesting exceeds {} levels (possible cycle)",
                Self::MAX_DEPTH
            )));
        }
        self.stack.push(frame);
        Ok(())
    }

    fn leave(&mut self) -> Result<Value, EncodeError> {
        self.depth -= 1;
        let Some(frame) = self.stack.pop() else {
            return Err(EncodeError::InvalidValue(
                "container ended without a start".into(),
            ));
        };
        Ok(match frame {
            Frame::Array { items } => {
                check_homogeneous(&items)?;
                Value::Array(Array {
                    element_type: array_element_type(&items),
                    items,
                    trailing_comments: Vec::new(),
                })
            }
            Frame::Map {
                entries,
                pending_key,
                sort,
            } => {
                debug_assert!(pending_key.is_none(), "map ended with a key but no value");
                if sort {
                    let mut entries = entries;
                    entries.sort_by(|a, b| a.0.cmp(&b.0));
                    Value::Object(object_from(entries))
                } else {
                    Value::Object(object_from(entries))
                }
            }
        })
    }

    fn finish(mut self) -> Result<Document, EncodeError> {
        if !self.stack.is_empty() {
            return Err(EncodeError::InvalidValue(
                "encoding ended inside a container".into(),
            ));
        }
        match self.root.take() {
            Some(Value::Object(object)) => Ok(Document {
                path: String::new(),
                children: object.children,
                ..Document::default()
            }),
            Some(_) => Err(EncodeError::Unsupported(
                "the top-level value must serialize as a struct or map; SKG documents are named fields".into(),
            )),
            None => Err(EncodeError::InvalidValue("nothing was encoded".into())),
        }
    }

    /// Wrap the just-finished struct frame as a single-entry variant object
    /// and attach it: `{ Variant { fields } }`, which decodes symmetrically.
    fn finish_variant(&mut self) -> Result<(), EncodeError> {
        let inner = self.leave()?;
        let variant = self.pending_variants.pop().ok_or_else(|| {
            EncodeError::InvalidValue("struct variant ended without a variant name".into())
        })?;
        let entry = named_node(&variant, inner);
        self.attach(Value::Object(ObjectBody {
            children: vec![entry],
            trailing_comments: Vec::new(),
        }))
    }
}

fn object_from(entries: Vec<(String, Value)>) -> ObjectBody {
    ObjectBody {
        children: entries
            .into_iter()
            .map(|(key, value)| named_node(&key, value))
            .collect(),
        trailing_comments: Vec::new(),
    }
}

/// Reject heterogeneous arrays before emission: the parser would refuse the
/// output with `MIXED_ARRAY_TYPES`, so the encoder must refuse it first.
fn check_homogeneous(items: &[Value]) -> Result<(), EncodeError> {
    let mut expected: Option<ValueType> = None;
    for (index, item) in items.iter().enumerate() {
        let tag = item.value_type();
        if tag == ValueType::Null {
            continue;
        }
        match expected {
            Some(et) if et != tag => {
                return Err(EncodeError::InvalidValue(format!(
                    "array index {index} has type {} but the array holds {} values; \
                     SKG arrays must be homogeneous",
                    tag.as_str(),
                    et.as_str(),
                )));
            }
            Some(_) => {}
            None => expected = Some(tag),
        }
    }
    Ok(())
}

/// The element tag a completed array reports, mirroring the parser: the first
/// non-null element fixes it; an all-null array is `null`; an empty array
/// falls back to `string`.
fn array_element_type(items: &[Value]) -> ValueType {
    items
        .iter()
        .map(Value::value_type)
        .find(|t| *t != ValueType::Null)
        .unwrap_or(if items.is_empty() {
            ValueType::String
        } else {
            ValueType::Null
        })
}

/// Normalize a named value the way the parser does: objects become blocks and
/// object arrays become block arrays; everything else stays a field.
fn named_node(key: &str, value: Value) -> Node {
    match value {
        Value::Object(object) => Node::Block(Block {
            path: String::new(),
            replace: false,
            name: key.to_string(),
            children: object.children,
            line: 0,
            col: 0,
            leading_comments: Vec::new(),
            trailing_comments: Vec::new(),
        }),
        Value::Array(array) if array.element_type == ValueType::Object => {
            Node::BlockArray(BlockArray {
                path: String::new(),
                name: key.to_string(),
                items: array.items,
                line: 0,
                col: 0,
                leading_comments: Vec::new(),
                trailing_comments: Vec::new(),
            })
        }
        other => Node::Field(Field {
            path: String::new(),
            key: key.to_string(),
            value: other,
            line: 0,
            col: 0,
            leading_comments: Vec::new(),
            trailing_comment: None,
        }),
    }
}

impl serde::Serializer for &mut Serializer {
    type Ok = ();
    type Error = EncodeError;
    type SerializeSeq = Self;
    type SerializeTuple = Self;
    type SerializeTupleStruct = Self;
    type SerializeTupleVariant = Self;
    type SerializeMap = Self;
    type SerializeStruct = Self;
    type SerializeStructVariant = Self;

    fn serialize_bool(self, v: bool) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::Bool(v))
    }

    fn serialize_i8(self, v: i8) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::Int(i64::from(v)))
    }

    fn serialize_i16(self, v: i16) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::Int(i64::from(v)))
    }

    fn serialize_i32(self, v: i32) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::Int(i64::from(v)))
    }

    fn serialize_i64(self, v: i64) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::Int(v))
    }

    fn serialize_u8(self, v: u8) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::Int(i64::from(v)))
    }

    fn serialize_u16(self, v: u16) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::Int(i64::from(v)))
    }

    fn serialize_u32(self, v: u32) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::Int(i64::from(v)))
    }

    fn serialize_u64(self, v: u64) -> Result<Self::Ok, Self::Error> {
        let signed = i64::try_from(v).map_err(|_| {
            EncodeError::InvalidValue(format!(
                "unsigned integer {v} exceeds the SKG integer range"
            ))
        })?;
        self.attach(Value::Int(signed))
    }

    fn serialize_f32(self, v: f32) -> Result<Self::Ok, Self::Error> {
        self.serialize_f64(f64::from(v))
    }

    fn serialize_f64(self, v: f64) -> Result<Self::Ok, Self::Error> {
        if !v.is_finite() {
            return Err(EncodeError::InvalidValue(format!(
                "cannot encode non-finite float {v}; SKG has no literal for it"
            )));
        }
        self.attach(Value::Float(v))
    }

    fn serialize_char(self, v: char) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::String(v.to_string()))
    }

    fn serialize_str(self, v: &str) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::String(v.to_string()))
    }

    /// Byte slices encode as integer arrays, matching the Go encoder.
    fn serialize_bytes(self, v: &[u8]) -> Result<Self::Ok, Self::Error> {
        let items = v.iter().map(|&b| Value::Int(i64::from(b))).collect();
        self.attach(Value::Array(Array {
            element_type: ValueType::Int,
            items,
            trailing_comments: Vec::new(),
        }))
    }

    fn serialize_none(self) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::Null)
    }

    fn serialize_some<T: Serialize + ?Sized>(self, value: &T) -> Result<Self::Ok, Self::Error> {
        value.serialize(self)
    }

    fn serialize_unit(self) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::Null)
    }

    fn serialize_unit_struct(self, _name: &'static str) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::Null)
    }

    /// A unit enum variant encodes as its tag string: `mode: "local"`.
    fn serialize_unit_variant(
        self,
        _name: &'static str,
        _variant_index: u32,
        variant: &'static str,
    ) -> Result<Self::Ok, Self::Error> {
        self.attach(Value::String(variant.to_string()))
    }

    /// A newtype enum variant encodes as the single-entry block form, which
    /// decodes symmetrically: `Variant` holding the payload.
    fn serialize_newtype_variant<T: Serialize + ?Sized>(
        self,
        _name: &'static str,
        _variant_index: u32,
        variant: &'static str,
        value: &T,
    ) -> Result<Self::Ok, Self::Error> {
        // The wrapper itself is a nesting level, so a recursive payload
        // cannot bypass the depth bound through a fresh serializer.
        let mut scratch = Serializer {
            depth: self.depth + 1,
            ..Serializer::default()
        };
        if scratch.depth > Serializer::MAX_DEPTH {
            return Err(EncodeError::Unsupported(format!(
                "nesting exceeds {} levels (possible cycle)",
                Serializer::MAX_DEPTH
            )));
        }
        value.serialize(&mut scratch)?;
        let Some(payload) = scratch.root else {
            return Err(EncodeError::InvalidValue(
                "newtype variant payload did not encode".into(),
            ));
        };
        let entry = named_node(variant, payload);
        self.attach(Value::Object(ObjectBody {
            children: vec![entry],
            trailing_comments: Vec::new(),
        }))
    }

    fn serialize_tuple(self, len: usize) -> Result<Self::SerializeTuple, Self::Error> {
        self.serialize_seq(Some(len))
    }

    fn serialize_seq(self, len: Option<usize>) -> Result<Self::SerializeSeq, Self::Error> {
        self.enter(Frame::Array {
            items: Vec::with_capacity(len.unwrap_or(0)),
        })?;
        Ok(self)
    }

    fn serialize_tuple_struct(
        self,
        _name: &'static str,
        len: usize,
    ) -> Result<Self::SerializeTupleStruct, Self::Error> {
        self.serialize_seq(Some(len))
    }

    fn serialize_tuple_variant(
        self,
        _name: &'static str,
        _variant_index: u32,
        _variant: &'static str,
        _len: usize,
    ) -> Result<Self::SerializeTupleVariant, Self::Error> {
        Err(EncodeError::Unsupported(
            "tuple enum variants are not expressible in SKG; use a struct variant".into(),
        ))
    }

    fn serialize_map(self, _len: Option<usize>) -> Result<Self::SerializeMap, Self::Error> {
        self.enter(Frame::Map {
            entries: Vec::new(),
            pending_key: None,
            sort: true,
        })?;
        Ok(self)
    }

    fn serialize_struct(
        self,
        _name: &'static str,
        _len: usize,
    ) -> Result<Self::SerializeStruct, Self::Error> {
        self.enter(Frame::Map {
            entries: Vec::new(),
            pending_key: None,
            sort: false,
        })?;
        Ok(self)
    }

    fn serialize_struct_variant(
        self,
        _name: &'static str,
        _variant_index: u32,
        variant: &'static str,
        _len: usize,
    ) -> Result<Self::SerializeStructVariant, Self::Error> {
        self.enter(Frame::Map {
            entries: Vec::new(),
            pending_key: None,
            sort: false,
        })?;
        self.pending_variants.push(variant.to_string());
        Ok(self)
    }

    /// A newtype struct is transparent: only its payload is encoded.
    fn serialize_newtype_struct<T: Serialize + ?Sized>(
        self,
        _name: &'static str,
        value: &T,
    ) -> Result<Self::Ok, Self::Error> {
        value.serialize(self)
    }
}

impl ser::SerializeSeq for &mut Serializer {
    type Ok = ();
    type Error = EncodeError;

    fn serialize_element<T: Serialize + ?Sized>(&mut self, value: &T) -> Result<(), Self::Error> {
        value.serialize(&mut **self)
    }

    fn end(self) -> Result<Self::Ok, Self::Error> {
        let value = self.leave()?;
        self.attach(value)
    }
}

impl ser::SerializeTuple for &mut Serializer {
    type Ok = ();
    type Error = EncodeError;

    fn serialize_element<T: Serialize + ?Sized>(&mut self, value: &T) -> Result<(), Self::Error> {
        value.serialize(&mut **self)
    }

    fn end(self) -> Result<Self::Ok, Self::Error> {
        let value = self.leave()?;
        self.attach(value)
    }
}

impl ser::SerializeTupleStruct for &mut Serializer {
    type Ok = ();
    type Error = EncodeError;

    fn serialize_field<T: Serialize + ?Sized>(&mut self, value: &T) -> Result<(), Self::Error> {
        value.serialize(&mut **self)
    }

    fn end(self) -> Result<Self::Ok, Self::Error> {
        let value = self.leave()?;
        self.attach(value)
    }
}

impl ser::SerializeTupleVariant for &mut Serializer {
    type Ok = ();
    type Error = EncodeError;

    fn serialize_field<T: Serialize + ?Sized>(&mut self, _value: &T) -> Result<(), Self::Error> {
        Err(EncodeError::Unsupported(
            "tuple enum variants are not expressible in SKG; use a struct variant".into(),
        ))
    }

    fn end(self) -> Result<Self::Ok, Self::Error> {
        Err(EncodeError::Unsupported(
            "tuple enum variants are not expressible in SKG; use a struct variant".into(),
        ))
    }
}

impl ser::SerializeMap for &mut Serializer {
    type Ok = ();
    type Error = EncodeError;

    fn serialize_key<T: Serialize + ?Sized>(&mut self, key: &T) -> Result<(), Self::Error> {
        self.key_slot = true;
        let result = key.serialize(&mut **self);
        self.key_slot = false;
        result
    }

    fn serialize_value<T: Serialize + ?Sized>(&mut self, value: &T) -> Result<(), Self::Error> {
        value.serialize(&mut **self)
    }

    fn end(self) -> Result<Self::Ok, Self::Error> {
        let value = self.leave()?;
        self.attach(value)
    }
}

impl ser::SerializeStruct for &mut Serializer {
    type Ok = ();
    type Error = EncodeError;

    fn serialize_field<T: Serialize + ?Sized>(
        &mut self,
        key: &'static str,
        value: &T,
    ) -> Result<(), Self::Error> {
        match self.stack.last_mut() {
            Some(Frame::Map { pending_key, .. }) => *pending_key = Some(key.to_string()),
            _ => {
                return Err(EncodeError::InvalidValue(
                    "struct field serialized outside a struct".into(),
                ));
            }
        }
        value.serialize(&mut **self)
    }

    fn skip_field(&mut self, _key: &'static str) -> Result<(), Self::Error> {
        if let Some(Frame::Map { pending_key, .. }) = self.stack.last_mut() {
            *pending_key = None
        }
        Ok(())
    }

    fn end(self) -> Result<Self::Ok, Self::Error> {
        let value = self.leave()?;
        self.attach(value)
    }
}

impl ser::SerializeStructVariant for &mut Serializer {
    type Ok = ();
    type Error = EncodeError;

    fn serialize_field<T: Serialize + ?Sized>(
        &mut self,
        key: &'static str,
        value: &T,
    ) -> Result<(), Self::Error> {
        match self.stack.last_mut() {
            Some(Frame::Map { pending_key, .. }) => *pending_key = Some(key.to_string()),
            _ => {
                return Err(EncodeError::InvalidValue(
                    "variant field serialized outside a variant".into(),
                ));
            }
        }
        value.serialize(&mut **self)
    }

    fn skip_field(&mut self, _key: &'static str) -> Result<(), Self::Error> {
        if let Some(Frame::Map { pending_key, .. }) = self.stack.last_mut() {
            *pending_key = None;
        }
        Ok(())
    }

    fn end(self) -> Result<Self::Ok, Self::Error> {
        self.finish_variant()
    }
}
