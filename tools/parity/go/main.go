// Command go is the Go implementation's parity harness. It speaks the
// protocol described in tools/parity/README.md: read a JSON case file, run
// every case through the public Go API, and write one JSON result per line.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"

	skg "github.com/tenebrux/skg/go"
)

type caseFile struct {
	Mode  string `json:"mode"`
	Cases []struct {
		ID                  string `json:"id"`
		Path                string `json:"path"`
		Entry               string `json:"entry"`
		Root                string `json:"root"`
		MaxBytes            int64  `json:"max_bytes"`
		MaxFiles            int    `json:"max_files"`
		MaxNodes            int    `json:"max_nodes"`
		MaxMergeWork        int64  `json:"max_merge_work"`
		Profile             string `json:"profile"`
		Source              string `json:"source"`
		RejectUnknownFields bool   `json:"reject_unknown_fields"`
		AllowLossyNumbers   bool   `json:"allow_lossy_numbers"`
	} `json:"cases"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go <cases.json> <results.jsonl>")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	check(err)
	var cases caseFile
	check(json.Unmarshal(data, &cases))
	out, err := os.Create(os.Args[2])
	check(err)
	w := bufio.NewWriter(out)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, c := range cases.Cases {
		result := map[string]any{"id": c.ID}
		switch cases.Mode {
		case "parse":
			src, err := os.ReadFile(c.Path)
			check(err)
			file, err := skg.ParseSource(src, filepath.Base(c.Path))
			parseResult(result, file, err)
		case "resolve":
			var file *skg.File
			if c.Root == "" && c.MaxBytes == 0 && c.MaxFiles == 0 && c.MaxNodes == 0 && c.MaxMergeWork == 0 {
				file, err = skg.ParseFile(c.Entry)
			} else {
				file, err = skg.ParseFileWithOptions(c.Entry, skg.ResolveOptions{
					Root: c.Root, MaxBytes: c.MaxBytes, MaxFiles: c.MaxFiles,
					MaxNodes: c.MaxNodes, MaxMergeWork: c.MaxMergeWork,
				})
			}
			parseResult(result, file, err)
		case "native":
			nativeResult(result, c.Profile, c.Source, skg.DecodeOptions{
				RejectUnknownFields: c.RejectUnknownFields,
				AllowLossyNumbers:   c.AllowLossyNumbers,
			})
		default:
			check(fmt.Errorf("unknown mode %q", cases.Mode))
		}
		check(enc.Encode(result))
	}
	check(w.Flush())
	check(out.Close())
}

func parseResult(result map[string]any, file *skg.File, err error) {
	if err == nil {
		result["ok"] = true
		result["emit"] = string(skg.Emit(file))
		return
	}
	var pe *skg.ParseError
	if !errors.As(err, &pe) {
		// The entry file itself is missing: os errors have no diagnostic.
		result["ok"], result["code"] = false, "IMPORT_NOT_FOUND"
		result["file"], result["line"], result["col"] = "", 0, 0
		return
	}
	result["ok"], result["code"] = false, string(pe.Diag.Code)
	result["file"], result["line"], result["col"] = filepath.Base(pe.Diag.Path), pe.Diag.Line, pe.Diag.Col
}

// The shared native profiles, bound exactly as go/native_conformance_test.go
// binds them.
type record struct {
	ID   uint16  `skg:"id"`
	Note *string `skg:"note"`
}

type mode string

func (e *mode) DecodeSKG(ctx *skg.DecodeContext, value skg.Value) error {
	var name string
	if err := ctx.Decode(value, &name); err != nil {
		return err
	}
	if name != "local" && name != "remote" {
		return ctx.Fail(skg.NativeInvalidEnum, "unknown enum tag")
	}
	*e = mode(name)
	return nil
}

type port uint16

func (p *port) DecodeSKG(ctx *skg.DecodeContext, value skg.Value) error {
	var n uint16
	if err := ctx.Decode(value, &n); err != nil {
		return err
	}
	*p = port(n)
	return nil
}

func (p *port) ValidateSKG(ctx *skg.DecodeContext) error {
	if *p == 0 {
		return ctx.Fail(skg.NativeCustomError, "port must be nonzero")
	}
	return nil
}

func nativeResult(result map[string]any, profile, source string, options skg.DecodeOptions) {
	var value any
	var err error
	switch profile {
	case "bool":
		value, err = decode[bool](source, options)
	case "u8":
		value, err = decode[uint8](source, options)
	case "default_u8":
		var target struct {
			Value uint8 `skg:"value"`
		}
		target.Value = 7
		options.AllowMissingFields = true
		err = skg.DecodeSourceInto([]byte(source), "case.skg", &target, options)
		value = target.Value
	case "optional_u8":
		value, err = decode[*uint8](source, options)
	case "i64":
		value, err = decode[int64](source, options)
	case "f32":
		value, err = decode[float32](source, options)
	case "f64":
		value, err = decode[float64](source, options)
	case "string":
		value, err = decode[string](source, options)
	case "bytes":
		value, err = decode[[]byte](source, options)
	case "array2_u8":
		value, err = decode[[2]uint8](source, options)
	case "record":
		value, err = decode[record](source, options)
	case "list_optional_records":
		value, err = decode[[]*record](source, options)
	case "map_optional_u8":
		value, err = decode[map[string]*uint8](source, options)
	case "port":
		value, err = decode[port](source, options)
	case "enum":
		value, err = decode[mode](source, options)
	default:
		check(fmt.Errorf("unknown native profile %q", profile))
	}
	if err != nil {
		var de *skg.DecodeError
		if !errors.As(err, &de) {
			check(err)
		}
		result["ok"], result["code"], result["field_path"] = false, string(de.Code), de.FieldPath
		return
	}
	result["ok"], result["value"] = true, toJSON(reflect.ValueOf(value))
}

func decode[T any](source string, options skg.DecodeOptions) (T, error) {
	type target struct {
		Value T `skg:"value"`
	}
	out, err := skg.DecodeSource[target]([]byte(source), "case.skg", options)
	return out.Value, err
}

// toJSON renders a decoded value in the shared structural form: byte slices
// as integer arrays, nil collections as null, floats widened to binary64.
func toJSON(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return toJSON(v.Elem())
	case reflect.Slice:
		if v.IsNil() {
			return nil
		}
		fallthrough
	case reflect.Array:
		out := make([]any, v.Len())
		for i := range out {
			out[i] = toJSON(v.Index(i))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		out := map[string]any{}
		for _, k := range v.MapKeys() {
			out[k.String()] = toJSON(v.MapIndex(k))
		}
		return out
	case reflect.Struct:
		return map[string]any{"id": toJSON(v.Field(0)), "note": toJSON(v.Field(1))}
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return json.Number(strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return json.Number(strconv.FormatUint(v.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		return json.Number(strconv.FormatFloat(v.Float(), 'g', -1, 64))
	case reflect.String:
		return v.String()
	}
	panic("unsupported native value kind " + v.Kind().String())
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "skg parity (go):", err)
		os.Exit(1)
	}
}
