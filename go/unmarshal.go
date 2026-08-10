package skg

import (
	"fmt"
	"reflect"
)

// Unmarshal parses SKG source bytes and decodes into a Go struct.
// The target must be a pointer to a struct. Fields are matched via `skg:"name"` tags.
//
// Like Parse, Unmarshal never touches the filesystem: `import` statements are
// recorded on the parsed file but not loaded. Use UnmarshalFile to decode a
// configuration whose imports should be resolved.
func Unmarshal(data []byte, v interface{}) error {
	if err := checkUnmarshalTarget(v); err != nil {
		return err
	}
	file, err := Parse(data)
	if err != nil {
		return err
	}
	return decodeNodes(file.Children, reflect.ValueOf(v))
}

// UnmarshalFile reads an SKG file from disk and decodes into a Go struct,
// resolving and merging its imports first. See ParseFile.
func UnmarshalFile(path string, v interface{}) error {
	if err := checkUnmarshalTarget(v); err != nil {
		return err
	}
	file, err := ParseFile(path)
	if err != nil {
		return err
	}
	return decodeNodes(file.Children, reflect.ValueOf(v))
}

// InvalidUnmarshalError describes a target that Unmarshal cannot decode into:
// anything that is not a non-nil pointer. It mirrors
// encoding/json.InvalidUnmarshalError, and exists because the alternative was a
// reflect panic out of the middle of the decoder - a caller mistake should come
// back as an error, not take the process down.
type InvalidUnmarshalError struct {
	Type reflect.Type
}

func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil {
		return "skg: Unmarshal(nil)"
	}
	if e.Type.Kind() != reflect.Pointer {
		return "skg: Unmarshal(non-pointer " + e.Type.String() + ")"
	}
	return "skg: Unmarshal(nil " + e.Type.String() + ")"
}

func checkUnmarshalTarget(v interface{}) error {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &InvalidUnmarshalError{Type: reflect.TypeOf(v)}
	}
	return nil
}

func decodeNodes(nodes []Node, target reflect.Value) error {
	if target.Kind() == reflect.Ptr {
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		target = target.Elem()
	}

	// Map target: block children become map entries.
	// Field keys/block names are map keys, values decode into the map value type.
	if target.Kind() == reflect.Map {
		return decodeMap(nodes, target)
	}

	if target.Kind() != reflect.Struct {
		return fmt.Errorf("skg: unmarshal target must be a struct or map, got %s", target.Kind())
	}

	fieldMap := buildFieldMap(target.Type())

	for _, node := range nodes {
		if node.Field != nil {
			idx, ok := fieldMap[node.Field.Key]
			if !ok {
				continue // extra fields ignored
			}
			fv, err := fieldByIndex(target, idx)
			if err != nil {
				return fmt.Errorf("skg: field %q: %w", node.Field.Key, err)
			}
			if err := decodeValue(node.Field.Value, fv); err != nil {
				return fmt.Errorf("skg: field %q: %w", node.Field.Key, err)
			}
		} else if node.Block != nil {
			idx, ok := fieldMap[node.Block.Name]
			if !ok {
				continue
			}
			fv, err := fieldByIndex(target, idx)
			if err != nil {
				return fmt.Errorf("skg: block %q: %w", node.Block.Name, err)
			}
			// Handle pointer-to-struct fields: allocate if nil, then decode into the pointee.
			if fv.Kind() == reflect.Ptr {
				if fv.IsNil() {
					fv.Set(reflect.New(fv.Type().Elem()))
				}
				if err := decodeNodes(node.Block.Children, fv); err != nil {
					return fmt.Errorf("skg: block %q: %w", node.Block.Name, err)
				}
			} else {
				if err := decodeNodes(node.Block.Children, fv.Addr()); err != nil {
					return fmt.Errorf("skg: block %q: %w", node.Block.Name, err)
				}
			}
		} else if node.BlockArray != nil {
			idx, ok := fieldMap[node.BlockArray.Name]
			if !ok {
				continue
			}
			fv, err := fieldByIndex(target, idx)
			if err != nil {
				return fmt.Errorf("skg: block array %q: %w", node.BlockArray.Name, err)
			}
			if err := decodeBlockArray(node.BlockArray, fv); err != nil {
				return fmt.Errorf("skg: block array %q: %w", node.BlockArray.Name, err)
			}
		}
	}
	return nil
}

func decodeMap(nodes []Node, target reflect.Value) error {
	if target.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("skg: map key must be string, got %s", target.Type().Key().Kind())
	}

	if target.IsNil() {
		target.Set(reflect.MakeMap(target.Type()))
	}

	valType := target.Type().Elem()
	isAny := valType.Kind() == reflect.Interface

	// The key kind is string, but the key *type* may be a named string type, and
	// SetMapIndex panics on an unconverted value.
	keyType := target.Type().Key()
	mapKey := func(s string) reflect.Value { return reflect.ValueOf(s).Convert(keyType) }

	for _, node := range nodes {
		if node.Field != nil {
			if isAny {
				target.SetMapIndex(mapKey(node.Field.Key), reflect.ValueOf(valueToAny(node.Field.Value)))
			} else {
				val := reflect.New(valType).Elem()
				if err := decodeValue(node.Field.Value, val); err != nil {
					return fmt.Errorf("skg: map key %q: %w", node.Field.Key, err)
				}
				target.SetMapIndex(mapKey(node.Field.Key), val)
			}
		} else if node.Block != nil {
			if isAny {
				// Decode block children into map[string]interface{}
				inner := reflect.MakeMap(reflect.TypeOf(map[string]interface{}{}))
				if err := decodeMap(node.Block.Children, inner); err != nil {
					return fmt.Errorf("skg: map key %q: %w", node.Block.Name, err)
				}
				target.SetMapIndex(mapKey(node.Block.Name), inner)
			} else {
				val := reflect.New(valType).Elem()
				if err := decodeNodes(node.Block.Children, val.Addr()); err != nil {
					return fmt.Errorf("skg: map key %q: %w", node.Block.Name, err)
				}
				target.SetMapIndex(mapKey(node.Block.Name), val)
			}
		} else if node.BlockArray != nil {
			// Without this branch a block array decoded into a map vanished
			// silently: the node matched none of the cases above and the key
			// simply never appeared in the result.
			if isAny {
				items := make([]interface{}, len(node.BlockArray.Items))
				for i, item := range node.BlockArray.Items {
					inner := reflect.MakeMap(reflect.TypeOf(map[string]interface{}{}))
					if err := decodeMap(item, inner); err != nil {
						return fmt.Errorf("skg: map key %q: index %d: %w", node.BlockArray.Name, i, err)
					}
					items[i] = inner.Interface()
				}
				target.SetMapIndex(mapKey(node.BlockArray.Name), reflect.ValueOf(items))
			} else {
				val := reflect.New(valType).Elem()
				if err := decodeBlockArray(node.BlockArray, val); err != nil {
					return fmt.Errorf("skg: map key %q: %w", node.BlockArray.Name, err)
				}
				target.SetMapIndex(mapKey(node.BlockArray.Name), val)
			}
		}
	}
	return nil
}

func decodeBlockArray(ba *BlockArray, target reflect.Value) error {
	if target.Kind() == reflect.Ptr {
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		target = target.Elem()
	}
	if target.Kind() != reflect.Slice {
		return fmt.Errorf("target must be a slice, got %s", target.Kind())
	}
	elemType := target.Type().Elem()
	slice := reflect.MakeSlice(target.Type(), len(ba.Items), len(ba.Items))
	for i, item := range ba.Items {
		elem := reflect.New(elemType)
		if err := decodeNodes(item, elem); err != nil {
			return fmt.Errorf("index %d: %w", i, err)
		}
		slice.Index(i).Set(elem.Elem())
	}
	target.Set(slice)
	return nil
}

func decodeValue(val Value, target reflect.Value) error {
	// Handle pointer types (nullable)
	if target.Kind() == reflect.Ptr {
		if val.Type == TypeNull {
			target.Set(reflect.Zero(target.Type()))
			return nil
		}
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		target = target.Elem()
	}

	// Handle interface{} / any - decode into native Go types
	if target.Kind() == reflect.Interface {
		target.Set(reflect.ValueOf(valueToAny(val)))
		return nil
	}

	switch val.Type {
	case TypeString:
		if target.Kind() != reflect.String {
			return fmt.Errorf("cannot assign string to %s", target.Kind())
		}
		target.SetString(val.Str)

	case TypeInt:
		switch target.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if target.OverflowInt(val.Int) {
				return fmt.Errorf("int %d overflows %s", val.Int, target.Kind())
			}
			target.SetInt(val.Int)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			if val.Int < 0 {
				return fmt.Errorf("cannot assign negative int %d to %s", val.Int, target.Kind())
			}
			if target.OverflowUint(uint64(val.Int)) {
				return fmt.Errorf("int %d overflows %s", val.Int, target.Kind())
			}
			target.SetUint(uint64(val.Int))
		case reflect.Float32, reflect.Float64:
			target.SetFloat(float64(val.Int))
		default:
			return fmt.Errorf("cannot assign int to %s", target.Kind())
		}

	case TypeFloat:
		switch target.Kind() {
		case reflect.Float32, reflect.Float64:
			if target.OverflowFloat(val.Float) {
				return fmt.Errorf("float %v overflows %s", val.Float, target.Kind())
			}
			target.SetFloat(val.Float)
		default:
			return fmt.Errorf("cannot assign float to %s", target.Kind())
		}

	case TypeBool:
		if target.Kind() != reflect.Bool {
			return fmt.Errorf("cannot assign bool to %s", target.Kind())
		}
		target.SetBool(val.Bool)

	case TypeNull:
		target.Set(reflect.Zero(target.Type()))

	case TypeArray:
		if target.Kind() != reflect.Slice {
			return fmt.Errorf("cannot assign array to %s", target.Kind())
		}
		if val.Array == nil {
			return nil
		}
		slice := reflect.MakeSlice(target.Type(), len(val.Array.Items), len(val.Array.Items))
		for i, item := range val.Array.Items {
			if err := decodeValue(item, slice.Index(i)); err != nil {
				return fmt.Errorf("index %d: %w", i, err)
			}
		}
		target.Set(slice)
	}
	return nil
}

// valueToAny converts an SKG Value into a native Go type for interface{} targets.
func valueToAny(val Value) interface{} {
	switch val.Type {
	case TypeString:
		return val.Str
	case TypeInt:
		return val.Int
	case TypeFloat:
		return val.Float
	case TypeBool:
		return val.Bool
	case TypeNull:
		return nil
	case TypeArray:
		if val.Array == nil {
			return []interface{}{}
		}
		items := make([]interface{}, len(val.Array.Items))
		for i, item := range val.Array.Items {
			items[i] = valueToAny(item)
		}
		return items
	}
	return nil
}

func buildFieldMap(t reflect.Type) map[string][]int {
	fields := structFields(t)
	m := make(map[string][]int, len(fields))
	for _, f := range fields {
		m[f.name] = f.index
	}
	return m
}

// structField describes one SKG-visible field of a struct: the name from its
// `skg` tag and the index path used to reach it. The path has more than one
// element for fields promoted out of an anonymous embedded struct.
type structField struct {
	name  string
	index []int
}

// structFields returns the SKG-visible fields of t in declaration order,
// promoting the tagged fields of anonymous embedded structs (and embedded
// pointers to structs) into the outer struct. A field declared on the outer
// struct shadows a promoted field of the same name, matching encoding/json's
// shallowest-wins rule.
func structFields(t reflect.Type) []structField {
	var cands []fieldCandidate
	collectFields(t, nil, 0, map[reflect.Type]bool{t: true}, &cands)

	// For each name keep the shallowest candidate; ties go to the first one.
	best := make(map[string]int, len(cands))
	for i, c := range cands {
		if j, ok := best[c.name]; ok && cands[j].depth <= c.depth {
			continue
		}
		best[c.name] = i
	}

	fields := make([]structField, 0, len(best))
	for i, c := range cands {
		if best[c.name] == i {
			fields = append(fields, structField{name: c.name, index: c.index})
		}
	}
	return fields
}

type fieldCandidate struct {
	name  string
	index []int
	depth int
}

// collectFields walks t and its anonymous embedded structs, appending one
// candidate per tagged field. visited breaks cycles created by self-embedding
// pointer types such as `type T struct { *T }`.
func collectFields(t reflect.Type, prefix []int, depth int, visited map[reflect.Type]bool, out *[]fieldCandidate) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("skg")
		index := append(append(make([]int, 0, len(prefix)+1), prefix...), i)

		if f.Anonymous && tag == "" {
			ft := f.Type
			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if !visited[ft] {
					visited[ft] = true
					collectFields(ft, index, depth+1, visited, out)
				}
				continue
			}
		}

		if tag == "" || tag == "-" || !f.IsExported() {
			continue
		}
		*out = append(*out, fieldCandidate{name: tag, index: index, depth: depth})
	}
}

// fieldByIndex resolves an index path from structFields for writing,
// allocating nil embedded pointers along the way.
func fieldByIndex(v reflect.Value, index []int) (reflect.Value, error) {
	for i, x := range index {
		if i > 0 && v.Kind() == reflect.Ptr {
			if v.IsNil() {
				if !v.CanSet() {
					return reflect.Value{}, fmt.Errorf("cannot allocate unexported embedded field of type %s", v.Type())
				}
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(x)
	}
	return v, nil
}

// fieldByIndexRO resolves an index path from structFields for reading. It
// reports false when the path crosses a nil embedded pointer, meaning the
// promoted field is absent and there is nothing to encode.
func fieldByIndexRO(v reflect.Value, index []int) (reflect.Value, bool) {
	for i, x := range index {
		if i > 0 && v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		v = v.Field(x)
	}
	return v, true
}
