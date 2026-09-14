package skg

import (
	"fmt"
	"math"
	"reflect"
	"sort"
)

// Marshal encodes a Go struct into SKG text using `skg:"name"` struct tags.
// Cycles, excessive nesting and values outside the SKG grammar return errors.
func Marshal(v interface{}) ([]byte, error) {
	rv, err := unwrapValue(reflect.ValueOf(v))
	if err != nil {
		return nil, err
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("skg: marshal source must be a struct, got %s", rv.Kind())
	}
	nodes, err := encodeStruct(rv, 0)
	if err != nil {
		return nil, err
	}
	data := Emit(&File{Children: nodes})
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("skg: marshaled file exceeds %d bytes", MaxFileSize)
	}
	return data, nil
}

// Bound pointer/interface chains separately from syntactic nesting. A pointer
// to an interface can point back to itself without introducing a block.
func unwrapValue(rv reflect.Value) (reflect.Value, error) {
	for i := 0; rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface; i++ {
		if i >= MaxNestingDepth {
			return reflect.Value{}, fmt.Errorf("skg: excessive pointer indirection (possible cycle)")
		}
		if rv.IsNil() {
			return reflect.Value{}, nil
		}
		rv = rv.Elem()
	}
	return rv, nil
}

func checkEncodeDepth(depth int) error {
	if depth > MaxNestingDepth {
		return fmt.Errorf("skg: marshal nesting exceeds %d (possible cycle)", MaxNestingDepth)
	}
	return nil
}

func encodeStruct(rv reflect.Value, depth int) ([]Node, error) {
	if err := checkEncodeDepth(depth); err != nil {
		return nil, err
	}
	var nodes []Node
	for _, sf := range structFields(rv.Type()) {
		fv, ok := fieldByIndexRO(rv, sf.index)
		if !ok {
			continue
		}
		node, err := encodeNode(sf.name, fv, depth)
		if err != nil {
			return nil, fmt.Errorf("skg: field %q: %w", sf.name, err)
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func encodeMap(rv reflect.Value, depth int) ([]Node, error) {
	if err := checkEncodeDepth(depth); err != nil {
		return nil, err
	}
	if rv.Type().Key().Kind() != reflect.String {
		return nil, fmt.Errorf("map key must be a string, got %s", rv.Type().Key().Kind())
	}
	keys := rv.MapKeys()
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	nodes := make([]Node, 0, len(keys))
	for _, key := range keys {
		node, err := encodeNode(key.String(), rv.MapIndex(key), depth)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", key.String(), err)
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func encodeNode(key string, rv reflect.Value, depth int) (Node, error) {
	rv, err := unwrapValue(rv)
	if err != nil {
		return Node{}, err
	}
	switch rv.Kind() {
	case reflect.Struct, reflect.Map:
		children, err := encodeChildren(rv, depth+1)
		if err != nil {
			return Node{}, err
		}
		return Node{Block: &Block{Name: key, Children: children}}, nil
	case reflect.Slice:
		// Empty typed collections still retain their block-array shape.
		elem := rv.Type().Elem()
		for indirection := 0; elem.Kind() == reflect.Ptr; indirection++ {
			if indirection >= MaxNestingDepth {
				return Node{}, fmt.Errorf("skg: excessive element pointer indirection")
			}
			elem = elem.Elem()
		}
		blocks := elem.Kind() == reflect.Struct || elem.Kind() == reflect.Map
		if !blocks && rv.Len() > 0 {
			first, err := unwrapValue(rv.Index(0))
			if err != nil {
				return Node{}, err
			}
			blocks = first.Kind() == reflect.Struct || first.Kind() == reflect.Map
		}
		if blocks {
			if err := checkEncodeDepth(depth + 1); err != nil {
				return Node{}, err
			}
			items := make([][]Node, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				item, err := unwrapValue(rv.Index(i))
				if err != nil {
					return Node{}, err
				}
				items[i], err = encodeChildren(item, depth+2)
				if err != nil {
					return Node{}, fmt.Errorf("index %d: %w", i, err)
				}
			}
			return Node{BlockArray: &BlockArray{Name: key, Items: items}}, nil
		}
	}
	value, err := encodeValue(rv, depth)
	if err != nil {
		return Node{}, err
	}
	return Node{Field: &Field{Key: key, Value: value}}, nil
}

func encodeChildren(rv reflect.Value, depth int) ([]Node, error) {
	switch rv.Kind() {
	case reflect.Struct:
		return encodeStruct(rv, depth)
	case reflect.Map:
		return encodeMap(rv, depth)
	default:
		return nil, fmt.Errorf("block array entries must be structs or maps, got %s", rv.Kind())
	}
}

func encodeValue(rv reflect.Value, depth int) (Value, error) {
	rv, err := unwrapValue(rv)
	if err != nil {
		return Value{}, err
	}
	switch rv.Kind() {
	case reflect.Invalid:
		return Value{Type: TypeNull}, nil
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
			return Value{}, fmt.Errorf("cannot encode non-finite float %v", f)
		}
		return Value{Type: TypeFloat, Float: f}, nil
	case reflect.Bool:
		return Value{Type: TypeBool, Bool: rv.Bool()}, nil
	case reflect.Slice:
		if err := checkEncodeDepth(depth + 1); err != nil {
			return Value{}, err
		}
		items := make([]Value, rv.Len())
		kind := TypeString
		for i := 0; i < rv.Len(); i++ {
			v, err := encodeValue(rv.Index(i), depth+1)
			if err != nil {
				return Value{}, fmt.Errorf("index %d: %w", i, err)
			}
			if i > 0 && kind != TypeNull && v.Type != TypeNull && v.Type != kind {
				return Value{}, fmt.Errorf("index %d: mixed array types %s and %s", i, kind, v.Type)
			}
			if i == 0 || kind == TypeNull {
				kind = v.Type
			}
			items[i] = v
		}
		return Value{Type: TypeArray, Array: &Array{ElementType: kind, Items: items}}, nil
	default:
		return Value{}, fmt.Errorf("unsupported type %s", rv.Kind())
	}
}
