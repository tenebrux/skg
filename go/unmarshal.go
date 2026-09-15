package skg

import (
	"fmt"
	"reflect"
	"sync"
)

// Unmarshal parses and materializes SKG source bytes, then decodes native values.
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
	return decodeNodes(MaterializeNodes(file.Children), reflect.ValueOf(v))
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

// UnmarshalFileWithOptions is the legacy decoder paired with explicit file
// resolution policy. New code should generally prefer DecodeFileWithOptions.
func UnmarshalFileWithOptions(path string, v interface{}, options ResolveOptions) error {
	if err := checkUnmarshalTarget(v); err != nil {
		return err
	}
	file, err := ParseFileWithOptions(path, options)
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

// Compatibility entry points use the same engine without opting into strict
// presence/null/precision checks, hooks, or transactional target replacement.
func decodeNodes(nodes []Node, target reflect.Value) error {
	ctx := DecodeContext{legacy: true}
	return ctx.convert(Value{Type: TypeObject, Object: nodes}, target)
}

// A nil reflect.Value means delete in SetMapIndex, and panics in Set. Always
// represent SKG null as a typed zero, and reject incompatible interfaces.
func assignInterface(target reflect.Value, value any) error {
	if value == nil {
		target.SetZero()
		return nil
	}
	rv := reflect.ValueOf(value)
	if !rv.Type().AssignableTo(target.Type()) {
		return fmt.Errorf("cannot assign %s to %s", rv.Type(), target.Type())
	}
	target.Set(rv)
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
	case TypeObject:
		out := make(map[string]any, len(val.Object))
		for _, node := range val.Object {
			switch {
			case node.Field != nil:
				out[node.Field.Key] = valueToAny(node.Field.Value)
			case node.Block != nil:
				out[node.Block.Name] = valueToAny(Value{Type: TypeObject, Object: node.Block.Children})
			case node.BlockArray != nil:
				out[node.BlockArray.Name] = valueToAny(Value{Type: TypeArray, Array: &Array{Items: node.BlockArray.Items}})
			}
		}
		return out
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

// structField describes one SKG-visible field of a struct: the name from its
// `skg` tag and the index path used to reach it. The path has more than one
// element for fields promoted out of an anonymous embedded struct.
type structField struct {
	name  string
	index []int
}

// structMetadata caches the SKG-visible fields of a native struct together with
// the lookup and ambiguity information used by strict decoding.
type structMetadata struct {
	fields    []structField
	index     map[string][]int
	ambiguous string
}

var structMetadataCache sync.Map // map[reflect.Type]*structMetadata

func cachedStructMetadata(t reflect.Type) *structMetadata {
	if cached, ok := structMetadataCache.Load(t); ok {
		return cached.(*structMetadata)
	}
	computed := computeStructMetadata(t)
	actual, _ := structMetadataCache.LoadOrStore(t, computed)
	return actual.(*structMetadata)
}

func structFields(t reflect.Type) []structField {
	return cachedStructMetadata(t).fields
}

// computeStructMetadata returns the SKG-visible fields of t in declaration
// order, promoting the tagged fields of anonymous embedded structs (and
// embedded pointers to structs) into the outer struct. A field declared on the
// outer struct shadows a promoted field of the same name, matching
// encoding/json's shallowest-wins rule.
func computeStructMetadata(t reflect.Type) *structMetadata {
	var cands []fieldCandidate
	collectFields(t, nil, 0, map[reflect.Type]bool{t: true}, &cands)

	// For each name keep the shallowest candidate; ties go to the first one.
	best := make(map[string]int, len(cands))
	counts := make(map[string]int, len(cands))
	for i, c := range cands {
		if j, ok := best[c.name]; ok {
			if cands[j].depth < c.depth {
				continue
			}
			if cands[j].depth == c.depth {
				counts[c.name]++
				continue
			}
		}
		best[c.name] = i
		counts[c.name] = 1
	}

	fields := make([]structField, 0, len(best))
	index := make(map[string][]int, len(best))
	ambiguous := ""
	for i, c := range cands {
		if best[c.name] == i {
			field := structField{name: c.name, index: c.index}
			fields = append(fields, field)
			index[c.name] = c.index
			if ambiguous == "" && counts[c.name] > 1 {
				ambiguous = c.name
			}
		}
	}
	return &structMetadata{fields: fields, index: index, ambiguous: ambiguous}
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
