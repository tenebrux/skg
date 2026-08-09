package skg

import (
	"fmt"
	"math"
	"reflect"
)

// Marshal encodes a Go struct into SKG text using `skg:"name"` struct tags.
func Marshal(v interface{}) ([]byte, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("skg: marshal source must be a struct, got %s", rv.Kind())
	}

	nodes, err := encodeStruct(rv)
	if err != nil {
		return nil, err
	}

	file := &File{Children: nodes}
	return Emit(file), nil
}

func encodeStruct(rv reflect.Value) ([]Node, error) {
	var nodes []Node

	for _, sf := range structFields(rv.Type()) {
		tag := sf.name
		fv, ok := fieldByIndexRO(rv, sf.index)
		if !ok {
			continue // promoted through a nil embedded pointer: nothing to encode
		}

		// Handle pointer fields
		if fv.Kind() == reflect.Ptr {
			if fv.IsNil() {
				nodes = append(nodes, Node{Field: &Field{Key: tag, Value: Value{Type: TypeNull}}})
				continue
			}
			fv = fv.Elem()
		}

		if fv.Kind() == reflect.Struct {
			children, err := encodeStruct(fv)
			if err != nil {
				return nil, fmt.Errorf("skg: block %q: %w", tag, err)
			}
			nodes = append(nodes, Node{Block: &Block{Name: tag, Children: children}})
			continue
		}

		if fv.Kind() == reflect.Map {
			children, err := encodeMap(fv)
			if err != nil {
				return nil, fmt.Errorf("skg: block %q: %w", tag, err)
			}
			nodes = append(nodes, Node{Block: &Block{Name: tag, Children: children}})
			continue
		}

		if fv.Kind() == reflect.Slice && fv.Type().Elem().Kind() == reflect.Struct {
			var items [][]Node
			for i := 0; i < fv.Len(); i++ {
				children, err := encodeStruct(fv.Index(i))
				if err != nil {
					return nil, fmt.Errorf("skg: block array %q index %d: %w", tag, i, err)
				}
				items = append(items, children)
			}
			nodes = append(nodes, Node{BlockArray: &BlockArray{Name: tag, Items: items}})
			continue
		}

		val, err := encodeValue(fv)
		if err != nil {
			return nil, fmt.Errorf("skg: field %q: %w", tag, err)
		}
		nodes = append(nodes, Node{Field: &Field{Key: tag, Value: val}})
	}

	return nodes, nil
}

func encodeMap(rv reflect.Value) ([]Node, error) {
	var nodes []Node
	iter := rv.MapRange()
	for iter.Next() {
		key := iter.Key().String()
		val := iter.Value()

		// Unwrap interface{}
		if val.Kind() == reflect.Interface {
			val = val.Elem()
		}

		if val.Kind() == reflect.Struct {
			children, err := encodeStruct(val)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", key, err)
			}
			nodes = append(nodes, Node{Block: &Block{Name: key, Children: children}})
			continue
		}

		if val.Kind() == reflect.Map {
			children, err := encodeMap(val)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", key, err)
			}
			nodes = append(nodes, Node{Block: &Block{Name: key, Children: children}})
			continue
		}

		v, err := encodeValue(val)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", key, err)
		}
		nodes = append(nodes, Node{Field: &Field{Key: key, Value: v}})
	}
	return nodes, nil
}

func encodeValue(rv reflect.Value) (Value, error) {
	// Unwrap interface{}
	if rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return Value{Type: TypeNull}, nil
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.String:
		return Value{Type: TypeString, Str: rv.String()}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return Value{Type: TypeInt, Int: rv.Int()}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := rv.Uint()
		if u > math.MaxInt64 {
			return Value{}, fmt.Errorf("uint value %d exceeds the SKG integer range", u)
		}
		return Value{Type: TypeInt, Int: int64(u)}, nil
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			// SKG has no literal for NaN or infinity, so there is no
			// representation that would parse back.
			return Value{}, fmt.Errorf("cannot encode non-finite float %v", f)
		}
		return Value{Type: TypeFloat, Float: f}, nil
	case reflect.Bool:
		return Value{Type: TypeBool, Bool: rv.Bool()}, nil
	case reflect.Slice:
		if rv.Len() == 0 {
			return Value{Type: TypeArray, Array: &Array{ElementType: TypeString}}, nil
		}
		items := make([]Value, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			v, err := encodeValue(rv.Index(i))
			if err != nil {
				return Value{}, fmt.Errorf("index %d: %w", i, err)
			}
			items[i] = v
		}
		return Value{Type: TypeArray, Array: &Array{ElementType: items[0].Type, Items: items}}, nil
	default:
		return Value{}, fmt.Errorf("unsupported type %s", rv.Kind())
	}
}
