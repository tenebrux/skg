package skg

import (
	"encoding"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"reflect"
	"strconv"
	"strings"
)

// NativeCode classifies typed loading failures, independently of message wording.
type NativeCode string

const (
	NativeParseError       NativeCode = "parse_error"
	NativeTypeMismatch     NativeCode = "type_mismatch"
	NativeMissingField     NativeCode = "missing_field"
	NativeUnknownField     NativeCode = "unknown_field"
	NativeInvalidEnum      NativeCode = "invalid_enum"
	NativeNumberOutOfRange NativeCode = "number_out_of_range"
	NativeInexactNumber    NativeCode = "inexact_number"
	NativeUnsupportedType  NativeCode = "unsupported_type"
	NativeNestingTooDeep   NativeCode = "nesting_too_deep"
	NativeCustomError      NativeCode = "custom_error"
)

type SourceLocation struct {
	Path      string
	Line, Col int
}

// DecodeError retains the original cause for errors.Is/errors.As.
type DecodeError struct {
	Code      NativeCode
	FieldPath string // JSON Pointer, empty at the document root
	Source    SourceLocation
	Message   string
	Err       error
}

func (e *DecodeError) Error() string { return fmt.Sprintf("skg: field %s: %s", e.FieldPath, e.Message) }
func (e *DecodeError) Unwrap() error { return e.Err }

type DecodeOptions struct {
	RejectUnknownFields bool
	AllowLossyNumbers   bool
	// Permit absent nonnullable fields; useful with Into and native defaults.
	AllowMissingFields bool
}

type NativeDecoder interface {
	DecodeSKG(*DecodeContext, Value) error
}
type NativeValidator interface{ ValidateSKG(*DecodeContext) error }

// DecodeSource parses local operations without loading imports. Errors return zero T.
func DecodeSource[T any](data []byte, path string, options DecodeOptions) (T, error) {
	var out T
	file, err := ParseSource(data, path)
	if err == nil {
		err = decodeNativeFile(file, reflect.ValueOf(&out).Elem(), options)
	}
	if err != nil {
		var zero T
		return zero, nativeParseError(err, path)
	}
	return out, nil
}

// DecodeFile resolves imports and overlays before native conversion.
func DecodeFile[T any](path string, options DecodeOptions) (T, error) {
	var out T
	file, err := ParseFile(path)
	if err == nil {
		err = decodeNativeFile(file, reflect.ValueOf(&out).Elem(), options)
	}
	if err != nil {
		var zero T
		return zero, nativeParseError(err, path)
	}
	return out, nil
}

// DecodeSourceInto decodes transactionally from a copy of target's defaults.
func DecodeSourceInto(data []byte, path string, target any, options DecodeOptions) error {
	if err := checkUnmarshalTarget(target); err != nil {
		return err
	}
	file, err := ParseSource(data, path)
	if err != nil {
		return nativeParseError(err, path)
	}
	return decodeNativeInto(file, target, options)
}
func DecodeFileInto(path string, target any, options DecodeOptions) error {
	if err := checkUnmarshalTarget(target); err != nil {
		return err
	}
	file, err := ParseFile(path)
	if err != nil {
		return nativeParseError(err, path)
	}
	return decodeNativeInto(file, target, options)
}
func decodeNativeInto(file *File, target any, options DecodeOptions) error {
	original := reflect.ValueOf(target).Elem()
	staged, err := cloneDefault(original, 0)
	if err != nil {
		return err
	}
	if err = decodeNativeFile(file, staged, options); err != nil {
		return err
	}
	original.Set(staged)
	return nil
}
func nativeParseError(err error, path string) error {
	var native *DecodeError
	if errors.As(err, &native) {
		return err
	}
	location := SourceLocation{Path: path}
	var parse *ParseError
	if errors.As(err, &parse) {
		location = SourceLocation{parse.Diag.Path, parse.Diag.Line, parse.Diag.Col}
	}
	return &DecodeError{Code: NativeParseError, Source: location, Message: err.Error(), Err: err}
}
func decodeNativeFile(file *File, target reflect.Value, options DecodeOptions) error {
	nodes := file.Children
	if !file.ImportsResolved {
		nodes = MaterializeNodes(nodes)
	}
	ctx := DecodeContext{Options: options, Source: SourceLocation{Path: file.Path}}
	return ctx.convert(Value{Type: TypeObject, Object: nodes}, target)
}

// DecodeContext is scoped to the current value. Hooks can reuse conversion and
// report failures without inventing their own field-path or source conventions.
type DecodeContext struct {
	Options   DecodeOptions
	FieldPath string
	Source    SourceLocation
	depth     int
	legacy    bool
}

func (c *DecodeContext) Fail(code NativeCode, message string) error {
	return &DecodeError{Code: code, FieldPath: c.FieldPath, Source: c.Source, Message: message}
}
func (c *DecodeContext) Decode(value Value, target any) error {
	if err := checkUnmarshalTarget(target); err != nil {
		return err
	}
	return c.convert(value, reflect.ValueOf(target).Elem())
}
func (c *DecodeContext) DecodeChild(value Value, key string, source SourceLocation, target any) error {
	if err := checkUnmarshalTarget(target); err != nil {
		return err
	}
	child := c.child(key, source)
	return child.convert(value, reflect.ValueOf(target).Elem())
}
func (c *DecodeContext) child(key string, source SourceLocation) DecodeContext {
	next := *c
	next.FieldPath += "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
	next.Source = source
	return next
}
func (c *DecodeContext) hookError(err error) error {
	if err == nil {
		return nil
	}
	var d *DecodeError
	if errors.As(err, &d) {
		return err
	}
	return &DecodeError{Code: NativeCustomError, FieldPath: c.FieldPath, Source: c.Source, Message: err.Error(), Err: err}
}
func (c *DecodeContext) convert(value Value, target reflect.Value) (err error) {
	if c.depth >= 256 {
		return c.Fail(NativeNestingTooDeep, "native target nesting exceeds 256")
	}
	c.depth++
	defer func() { c.depth-- }()
	for n := 0; target.Kind() == reflect.Pointer; n++ {
		if n >= MaxNestingDepth {
			return c.Fail(NativeNestingTooDeep, "excessive pointer indirection")
		}
		if value.Type == TypeNull {
			target.SetZero()
			return nil
		}
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		target = target.Elem()
	}
	if !c.legacy && target.CanAddr() && target.Addr().CanInterface() {
		receiver := target.Addr().Interface()
		if validator, ok := receiver.(NativeValidator); ok {
			defer func() {
				if err == nil {
					err = c.hookError(validator.ValidateSKG(c))
				}
			}()
		}
		if decoder, ok := receiver.(NativeDecoder); ok {
			return c.hookError(decoder.DecodeSKG(c, value))
		}
		if decoder, ok := receiver.(encoding.TextUnmarshaler); ok && value.Type == TypeString {
			return c.hookError(decoder.UnmarshalText([]byte(value.Str)))
		}
	}
	if !c.legacy && target.Type() == reflect.TypeFor[Value]() {
		target.Set(reflect.ValueOf(value))
		return nil
	}
	if target.Kind() == reflect.Interface {
		if c.legacy && value.Type == TypeNull {
			target.SetZero()
			return nil
		}
		if target.NumMethod() != 0 {
			return c.Fail(NativeUnsupportedType, "cannot decode into non-empty interface")
		}
		if c.legacy {
			return assignInterface(target, valueToAny(value))
		}
		dynamic, err := c.dynamicValue(value)
		if err != nil {
			return err
		}
		return assignInterface(target, dynamic)
	}
	if value.Type == TypeNull {
		if c.legacy || isNilable(target.Kind()) {
			target.SetZero()
			return nil
		}
		return c.Fail(NativeTypeMismatch, "null requires a nullable native target")
	}
	switch value.Type {
	case TypeObject:
		return c.object(value.Object, target)
	case TypeArray:
		if target.Kind() != reflect.Slice && (c.legacy || target.Kind() != reflect.Array) {
			return c.Fail(NativeTypeMismatch, "expected a native slice or array")
		}
		var items []Value
		if value.Array != nil {
			items = value.Array.Items
		}
		if target.Kind() == reflect.Array && target.Len() != len(items) {
			return c.Fail(NativeTypeMismatch, "array length does not match the native array")
		}
		out := reflect.New(target.Type()).Elem()
		if target.Kind() == reflect.Slice {
			out = reflect.MakeSlice(target.Type(), len(items), len(items))
		}
		for i, item := range items {
			child := c.child(strconv.Itoa(i), c.Source)
			if err := child.convert(item, out.Index(i)); err != nil {
				return err
			}
		}
		target.Set(out)
		return nil
	case TypeString:
		if target.Kind() == reflect.String {
			target.SetString(value.Str)
			return nil
		}
		if !c.legacy && target.Kind() == reflect.Slice && target.Type().Elem().Kind() == reflect.Uint8 {
			out := reflect.MakeSlice(target.Type(), len(value.Str), len(value.Str))
			for i := 0; i < len(value.Str); i++ {
				out.Index(i).SetUint(uint64(value.Str[i]))
			}
			target.Set(out)
			return nil
		}
	case TypeBool:
		if target.Kind() == reflect.Bool {
			target.SetBool(value.Bool)
			return nil
		}
	case TypeInt:
		switch target.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if target.OverflowInt(value.Int) {
				return c.Fail(NativeNumberOutOfRange, "integer does not fit the native type")
			}
			target.SetInt(value.Int)
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			if value.Int < 0 || target.OverflowUint(uint64(value.Int)) {
				return c.Fail(NativeNumberOutOfRange, "integer does not fit the native type")
			}
			target.SetUint(uint64(value.Int))
			return nil
		case reflect.Float32, reflect.Float64:
			precision := 53
			if target.Kind() == reflect.Float32 {
				precision = 24
			}
			if !c.legacy && !c.Options.AllowLossyNumbers && !integerExact(value.Int, precision) {
				return c.Fail(NativeInexactNumber, "conversion would lose numeric precision")
			}
			if !c.legacy && target.Kind() == reflect.Float32 {
				// Convert directly: i64 -> f64 -> f32 can round twice.
				target.SetFloat(float64(float32(value.Int)))
			} else {
				target.SetFloat(float64(value.Int))
			}
			return nil
		}
	case TypeFloat:
		if target.Kind() == reflect.Float32 || target.Kind() == reflect.Float64 {
			if !isFinite(value.Float) || (c.legacy && target.OverflowFloat(value.Float)) || (target.Kind() == reflect.Float32 && !isFinite(float64(float32(value.Float)))) {
				return c.Fail(NativeNumberOutOfRange, "number does not fit the native float")
			}
			if !c.legacy && !c.Options.AllowLossyNumbers && target.Kind() == reflect.Float32 && float64(float32(value.Float)) != value.Float {
				return c.Fail(NativeInexactNumber, "conversion would lose numeric precision")
			}
			target.SetFloat(value.Float)
			return nil
		}
	}
	return c.Fail(NativeTypeMismatch, fmt.Sprintf("cannot assign %s to %s", value.Type, target.Kind()))
}

func (c *DecodeContext) dynamicValue(value Value) (any, error) {
	switch value.Type {
	case TypeArray:
		var items []Value
		if value.Array != nil {
			items = value.Array.Items
		}
		out := make([]any, len(items))
		for i, item := range items {
			child := c.child(strconv.Itoa(i), c.Source)
			if err := child.convert(item, reflect.ValueOf(&out[i]).Elem()); err != nil {
				return nil, err
			}
		}
		return out, nil
	case TypeObject:
		out := make(map[string]any, len(value.Object))
		for _, node := range value.Object {
			key, ok := nodeKey(node)
			if !ok || node.Delete != nil {
				continue
			}
			child := c.child(key, nodeLocation(node))
			var item any
			if err := child.convert(nodeValue(node), reflect.ValueOf(&item).Elem()); err != nil {
				return nil, err
			}
			out[key] = item
		}
		return out, nil
	case TypeNull, TypeString, TypeBool, TypeInt, TypeFloat:
		return valueToAny(value), nil
	default:
		return nil, c.Fail(NativeTypeMismatch, "invalid SKG value tag")
	}
}
func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
func integerExact(n int64, precision int) bool {
	magnitude := uint64(n)
	if n < 0 {
		magnitude = uint64(-(n + 1)) + 1
	}
	needed := bits.Len64(magnitude) - precision
	return needed <= 0 || bits.TrailingZeros64(magnitude) >= needed
}
func isNilable(kind reflect.Kind) bool {
	return kind == reflect.Pointer || kind == reflect.Map || kind == reflect.Slice || kind == reflect.Interface
}
func (c *DecodeContext) object(nodes []Node, target reflect.Value) error {
	if target.Kind() == reflect.Map {
		if target.Type().Key().Kind() != reflect.String {
			return c.Fail(NativeUnsupportedType, "map key must be string")
		}
		if target.Type().Elem().Kind() == reflect.Interface && target.Type().Elem().NumMethod() != 0 {
			return c.Fail(NativeUnsupportedType, "cannot decode into non-empty interface")
		}
		if !c.legacy || target.IsNil() {
			target.Set(reflect.MakeMap(target.Type()))
		}
		for _, node := range nodes {
			key, ok := nodeKey(node)
			if !ok || node.Delete != nil {
				continue
			}
			child := c.child(key, nodeLocation(node))
			item := reflect.New(target.Type().Elem()).Elem()
			if err := child.convert(nodeValue(node), item); err != nil {
				return err
			}
			target.SetMapIndex(reflect.ValueOf(key).Convert(target.Type().Key()), item)
		}
		return nil
	}
	if target.Kind() != reflect.Struct {
		return c.Fail(NativeTypeMismatch, "expected a native struct or string map")
	}
	if !c.legacy {
		var candidates []fieldCandidate
		collectFields(target.Type(), nil, 0, map[reflect.Type]bool{target.Type(): true}, &candidates)
		best := make(map[string]int)
		counts := make(map[string]int)
		for _, field := range candidates {
			depth, found := best[field.name]
			if !found || field.depth < depth {
				best[field.name], counts[field.name] = field.depth, 1
			} else if field.depth == depth {
				counts[field.name]++
			}
		}
		for _, field := range candidates {
			if counts[field.name] > 1 {
				return c.Fail(NativeUnsupportedType, "ambiguous native field mapping: "+field.name)
			}
		}
	}
	fields := structFields(target.Type())
	index := buildFieldMap(target.Type())
	seen := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		key, ok := nodeKey(node)
		if !ok || node.Delete != nil {
			continue
		}
		child := c.child(key, nodeLocation(node))
		path, known := index[key]
		if !known {
			if !c.legacy && c.Options.RejectUnknownFields {
				return child.Fail(NativeUnknownField, "unknown field")
			}
			continue
		}
		seen[key] = true
		field, err := fieldByIndex(target, path)
		if err != nil {
			return child.Fail(NativeUnsupportedType, err.Error())
		}
		if err := child.convert(nodeValue(node), field); err != nil {
			return err
		}
	}
	if !c.legacy && !c.Options.AllowMissingFields {
		for _, field := range fields {
			if seen[field.name] {
				continue
			}
			typ := target.Type()
			for _, i := range field.index {
				if typ.Kind() == reflect.Pointer {
					typ = typ.Elem()
				}
				typ = typ.Field(i).Type
			}
			if !isNilable(typ.Kind()) {
				child := c.child(field.name, c.Source)
				return child.Fail(NativeMissingField, "required native field is absent")
			}
		}
	}
	return nil
}
func nodeValue(n Node) Value {
	switch {
	case n.Field != nil:
		return n.Field.Value
	case n.Block != nil:
		return Value{Type: TypeObject, Object: n.Block.Children}
	case n.BlockArray != nil:
		return Value{Type: TypeArray, Array: &Array{ElementType: TypeObject, Items: n.BlockArray.Items}}
	default:
		return Value{Type: TypeNull}
	}
}
func nodeLocation(n Node) SourceLocation {
	switch {
	case n.Field != nil:
		return SourceLocation{n.Field.Path, n.Field.Line, n.Field.Col}
	case n.Block != nil:
		return SourceLocation{n.Block.Path, n.Block.Line, n.Block.Col}
	case n.BlockArray != nil:
		return SourceLocation{n.BlockArray.Path, n.BlockArray.Line, n.BlockArray.Col}
	case n.Delete != nil:
		return SourceLocation{n.Delete.Path, n.Delete.Line, n.Delete.Col}
	default:
		return SourceLocation{}
	}
}

// Default graphs are copied before conversion. Unsupported mutable private state
// and cycles are rejected rather than sharing aliases with the caller's target.
func cloneDefault(v reflect.Value, depth int) (reflect.Value, error) {
	if depth >= 256 {
		return reflect.Value{}, &DecodeError{Code: NativeNestingTooDeep, Message: "native defaults are too deep or cyclic"}
	}
	out := reflect.New(v.Type()).Elem()
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return out, nil
		}
		child, err := cloneDefault(v.Elem(), depth+1)
		if err != nil {
			return out, err
		}
		p := reflect.New(v.Type().Elem())
		p.Elem().Set(child)
		out.Set(p)
	case reflect.Interface:
		if v.IsNil() {
			return out, nil
		}
		child, err := cloneDefault(v.Elem(), depth+1)
		if err != nil {
			return out, err
		}
		out.Set(child)
	case reflect.Map:
		if v.IsNil() {
			return out, nil
		}
		if v.Type().Key().Kind() != reflect.String {
			return out, &DecodeError{Code: NativeUnsupportedType, Message: "default map keys must be strings"}
		}
		out.Set(reflect.MakeMapWithSize(v.Type(), v.Len()))
		iter := v.MapRange()
		for iter.Next() {
			child, err := cloneDefault(iter.Value(), depth+1)
			if err != nil {
				return out, err
			}
			out.SetMapIndex(iter.Key(), child)
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice {
			if v.IsNil() {
				return out, nil
			}
			out.Set(reflect.MakeSlice(v.Type(), v.Len(), v.Len()))
		}
		for i := 0; i < v.Len(); i++ {
			child, err := cloneDefault(v.Index(i), depth+1)
			if err != nil {
				return out, err
			}
			out.Index(i).Set(child)
		}
	case reflect.Struct:
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			if !out.Field(i).CanSet() {
				if !v.Field(i).IsZero() && mutableType(v.Field(i).Type()) {
					return out, &DecodeError{Code: NativeUnsupportedType, Message: "default contains mutable unexported state"}
				}
				continue
			}
			child, err := cloneDefault(v.Field(i), depth+1)
			if err != nil {
				return out, err
			}
			out.Field(i).Set(child)
		}
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		if !v.IsZero() {
			return out, &DecodeError{Code: NativeUnsupportedType, Message: "unsupported native default"}
		}
	default:
		out.Set(v)
	}
	return out, nil
}
func mutableType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if mutableType(t.Field(i).Type) {
				return true
			}
		}
	case reflect.Array:
		return mutableType(t.Elem())
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return true
	}
	return false
}
