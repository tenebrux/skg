package skg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"
)

type nativeFixture struct {
	Name    string  `json:"name"`
	Profile string  `json:"profile"`
	Source  *string `json:"source"`
	Options struct {
		RejectUnknownFields bool `json:"reject_unknown_fields"`
		AllowLossyNumbers   bool `json:"allow_lossy_numbers"`
	} `json:"options"`
	Expected json.RawMessage `json:"expected"`
	Error    *struct {
		Code      NativeCode `json:"code"`
		FieldPath *string    `json:"field_path"`
	} `json:"error"`
}
type nativeFixtureRecord struct {
	ID   uint16  `skg:"id"`
	Note *string `skg:"note"`
}
type nativeFixtureEnum string

func (e *nativeFixtureEnum) DecodeSKG(ctx *DecodeContext, value Value) error {
	var name string
	if err := ctx.Decode(value, &name); err != nil {
		return err
	}
	if name != "local" && name != "remote" {
		return ctx.Fail(NativeInvalidEnum, "unknown enum tag")
	}
	*e = nativeFixtureEnum(name)
	return nil
}

func TestNativeConformance(t *testing.T) {
	data, err := os.ReadFile("../testdata/native/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	// encoding/json accepts case-insensitive names and null for plain booleans.
	// Check the closed fixture objects explicitly so such mistakes fail the gate.
	root := nativeFixtureObject(t, data, []string{"version", "cases"}, []string{"version", "cases"})
	var rawCases []json.RawMessage
	if err := json.Unmarshal(root["cases"], &rawCases); err != nil {
		t.Fatal(err)
	}
	for _, raw := range rawCases {
		obj := nativeFixtureObject(t, raw, []string{"name", "profile", "source", "options", "expected", "error"}, []string{"name", "profile", "source"})
		_, hasExpected := obj["expected"]
		_, hasError := obj["error"]
		if hasExpected == hasError {
			t.Fatal("native fixture needs exactly one result")
		}
		if hasError {
			nativeFixtureObject(t, obj["error"], []string{"code", "field_path"}, []string{"code", "field_path"})
		}
		if options, ok := obj["options"]; ok {
			options := nativeFixtureObject(t, options, []string{"reject_unknown_fields", "allow_lossy_numbers"}, nil)
			for _, value := range options {
				if !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
					t.Fatal("native fixture option must be boolean")
				}
			}
		}
	}
	var suite struct {
		Version int             `json:"version"`
		Cases   []nativeFixture `json:"cases"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&suite); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("trailing JSON", err)
	}
	if suite.Version != 1 || len(suite.Cases) == 0 {
		t.Fatal("unsupported or empty native suite")
	}
	names := make(map[string]bool)
	for _, fixture := range suite.Cases {
		if fixture.Name == "" || names[fixture.Name] || fixture.Source == nil || (fixture.Expected != nil) == (fixture.Error != nil) || (fixture.Error != nil && (fixture.Error.Code == "" || fixture.Error.FieldPath == nil)) {
			t.Fatalf("malformed fixture %#v", fixture)
		}
		names[fixture.Name] = true
		t.Run(fixture.Name, func(t *testing.T) {
			switch fixture.Profile {
			case "bool":
				runNativeFixture[bool](t, fixture)
			case "u8", "default_u8":
				runNativeFixture[uint8](t, fixture)
			case "optional_u8":
				runNativeFixture[*uint8](t, fixture)
			case "i64":
				runNativeFixture[int64](t, fixture)
			case "f32":
				runNativeFixture[float32](t, fixture)
			case "f64":
				runNativeFixture[float64](t, fixture)
			case "string":
				runNativeFixture[string](t, fixture)
			case "bytes":
				runNativeFixture[[]byte](t, fixture)
			case "array2_u8":
				runNativeFixture[[2]uint8](t, fixture)
			case "record":
				runNativeFixture[nativeFixtureRecord](t, fixture)
			case "list_optional_records":
				runNativeFixture[[]*nativeFixtureRecord](t, fixture)
			case "map_optional_u8":
				runNativeFixture[map[string]*uint8](t, fixture)
			case "port":
				runNativeFixture[nativePort](t, fixture)
			case "enum":
				runNativeFixture[nativeFixtureEnum](t, fixture)
			default:
				t.Fatalf("unimplemented native profile %q", fixture.Profile)
			}
		})
	}
	t.Logf("native contract v%d: %d cases, none skipped", suite.Version, len(suite.Cases))
}

func nativeFixtureObject(t *testing.T, data []byte, allowed, required []string) map[string]json.RawMessage {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		t.Fatalf("expected a fixture object: %s (%v)", data, err)
	}
	for key := range obj {
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
			}
		}
		if !known {
			t.Fatalf("unknown fixture property %q", key)
		}
	}
	for _, key := range required {
		if _, ok := obj[key]; !ok {
			t.Fatalf("missing fixture property %q", key)
		}
	}
	return obj
}

func runNativeFixture[T any](t *testing.T, fixture nativeFixture) {
	t.Helper()
	type Target struct {
		Value T `skg:"value"`
	}
	options := DecodeOptions{RejectUnknownFields: fixture.Options.RejectUnknownFields, AllowLossyNumbers: fixture.Options.AllowLossyNumbers}
	var value Target
	var err error
	if fixture.Profile == "default_u8" {
		reflect.ValueOf(&value.Value).Elem().SetUint(7)
		options.AllowMissingFields = true
		err = DecodeSourceInto([]byte(*fixture.Source), fixture.Name+".skg", &value, options)
	} else {
		value, err = DecodeSource[Target]([]byte(*fixture.Source), fixture.Name+".skg", options)
	}
	if fixture.Error != nil {
		expectNativeError(t, err, fixture.Error.Code, *fixture.Error.FieldPath)
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var expected any
	decoder := json.NewDecoder(bytes.NewReader(fixture.Expected))
	decoder.UseNumber()
	if err := decoder.Decode(&expected); err != nil {
		t.Fatal(err)
	}
	if err := compareNativeJSON(reflect.ValueOf(&value.Value).Elem(), expected); err != nil {
		t.Fatal(err)
	}
}

func compareNativeJSON(actual reflect.Value, expected any) error {
	if expected == nil {
		if isNilable(actual.Kind()) && actual.IsNil() {
			return nil
		}
		return fmt.Errorf("expected null, got %v", actual)
	}
	for actual.Kind() == reflect.Pointer || actual.Kind() == reflect.Interface {
		if actual.IsNil() {
			return fmt.Errorf("unexpected null")
		}
		actual = actual.Elem()
	}
	switch expected := expected.(type) {
	case bool:
		if actual.Kind() == reflect.Bool && actual.Bool() == expected {
			return nil
		}
	case string:
		if actual.Kind() == reflect.String && actual.String() == expected {
			return nil
		}
	case json.Number:
		switch actual.Kind() {
		case reflect.Uint8, reflect.Uint16:
			n, err := expected.Int64()
			if err == nil && n >= 0 && actual.Uint() == uint64(n) {
				return nil
			}
		case reflect.Int64:
			n, err := expected.Int64()
			if err == nil && actual.Int() == n {
				return nil
			}
		case reflect.Float32, reflect.Float64:
			n, err := expected.Float64()
			if err == nil && actual.Float() == n {
				return nil
			}
		}
	case []any:
		if (actual.Kind() == reflect.Slice || actual.Kind() == reflect.Array) && actual.Len() == len(expected) {
			for i, item := range expected {
				if err := compareNativeJSON(actual.Index(i), item); err != nil {
					return fmt.Errorf("index %d: %w", i, err)
				}
			}
			return nil
		}
	case map[string]any:
		if actual.Kind() == reflect.Map && actual.Len() == len(expected) {
			for key, item := range expected {
				v := actual.MapIndex(reflect.ValueOf(key))
				if !v.IsValid() {
					return fmt.Errorf("missing key %q", key)
				}
				if err := compareNativeJSON(v, item); err != nil {
					return fmt.Errorf("key %q: %w", key, err)
				}
			}
			return nil
		}
		if actual.Kind() == reflect.Struct && len(structFields(actual.Type())) == len(expected) {
			for _, field := range structFields(actual.Type()) {
				item, ok := expected[field.name]
				if !ok {
					return fmt.Errorf("unexpected field %q", field.name)
				}
				if err := compareNativeJSON(actual.FieldByIndex(field.index), item); err != nil {
					return fmt.Errorf("field %q: %w", field.name, err)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("native value %v (%s) does not match %#v", actual, actual.Type(), expected)
}
