package skg

// Cross-implementation conformance harness.
//
// Everything asserted here comes from the shared testdata/ tree, so the Go and
// Zig parsers are held to one description of correct behaviour. The rules this
// file enforces are specified in docs/conformance.md; a second implementation
// should be portable from that document alone.
//
// Three properties make it hard to conform by accident:
//
//  1. Fixtures are enumerated from disk. A new fixture runs without being
//     registered anywhere, so it cannot be added on one side only.
//  2. expected.json is strictly validated. An unknown or misspelled key is a
//     hard failure, never a silently skipped assertion.
//  3. Capabilities are declared in go/conformance.json. Declaring one obliges
//     this implementation to pass every fixture that needs it; not declaring
//     one skips those fixtures and reports the count unconditionally.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ─── Capabilities ───────────────────────────────────────────────────────────

const (
	capParse    = "parse"
	capEmit     = "emit"
	capImports  = "imports"
	capComments = "comments"
)

// knownCapabilities is closed: a manifest naming anything else fails the run.
var knownCapabilities = []string{capParse, capEmit, capImports, capComments}

type capabilityManifest struct {
	Implementation string            `json:"implementation"`
	Capabilities   map[string]bool   `json:"capabilities"`
	Notes          map[string]string `json:"notes"`
}

func (m capabilityManifest) has(c string) bool { return m.Capabilities[c] }

func loadManifest(t *testing.T) capabilityManifest {
	t.Helper()
	data, err := os.ReadFile("conformance.json")
	if err != nil {
		t.Fatalf("cannot read go/conformance.json: %v", err)
	}
	var m capabilityManifest
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("go/conformance.json: %v", err)
	}
	if m.Implementation == "" {
		t.Fatal("go/conformance.json: \"implementation\" is required")
	}
	for _, c := range knownCapabilities {
		if _, ok := m.Capabilities[c]; !ok {
			t.Fatalf("go/conformance.json: capability %q must be declared true or false", c)
		}
	}
	for name := range m.Capabilities {
		if !contains(knownCapabilities, name) {
			t.Fatalf("go/conformance.json: unknown capability %q (known: %v)", name, knownCapabilities)
		}
	}
	if !m.has(capParse) {
		t.Fatal("go/conformance.json: the \"parse\" capability is mandatory")
	}
	// An undeclared capability is allowed, but it must be a decision someone
	// wrote down - that is what keeps partial conformance honest rather than
	// quiet.
	for _, c := range knownCapabilities {
		if !m.has(c) && strings.TrimSpace(m.Notes[c]) == "" {
			t.Fatalf("go/conformance.json: capability %q is not declared and has no entry in \"notes\" explaining why", c)
		}
	}
	return m
}

// ─── Error-code registry ────────────────────────────────────────────────────

type errorCodeRegistry struct {
	Comment string `json:"comment"`
	Codes   []struct {
		Code    string `json:"code"`
		Summary string `json:"summary"`
	} `json:"codes"`
}

func loadErrorCodes(t *testing.T) map[string]bool {
	t.Helper()
	path := filepath.Join(testdataDir(), "error-codes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	var reg errorCodeRegistry
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reg); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(reg.Codes) == 0 {
		t.Fatalf("%s: registry is empty", path)
	}
	set := make(map[string]bool, len(reg.Codes))
	for _, c := range reg.Codes {
		if c.Code == "" {
			t.Fatalf("%s: entry with empty code", path)
		}
		if c.Summary == "" {
			t.Fatalf("%s: code %q has no summary", path, c.Code)
		}
		if set[c.Code] {
			t.Fatalf("%s: duplicate code %q", path, c.Code)
		}
		set[c.Code] = true
	}
	return set
}

// TestErrorCodeRegistryCoversParser fails if this parser can emit a code the
// shared registry does not list. Without it, a new diagnostic could be added in
// Go and asserted in a Go-only fixture that no other implementation can satisfy.
func TestErrorCodeRegistryCoversParser(t *testing.T) {
	registry := loadErrorCodes(t)
	for _, c := range ErrorCodes {
		if !registry[string(c)] {
			t.Errorf("ErrorCode %q is not in testdata/error-codes.json", c)
		}
	}
}

// ─── Fixture discovery ──────────────────────────────────────────────────────

func testdataDir() string {
	// testdata/ is one level up from go/.
	return filepath.Join("..", "testdata")
}

// fixture is one conformance case: either a flat `<name>.skg` parsed as bytes,
// or a `<name>/` directory whose `main.skg` is loaded through the file API so
// imports resolve.
type fixture struct {
	Name      string
	IsDir     bool
	EntryPath string // <name>.skg, or <name>/main.skg
	JSONPath  string // <name>.expected.json, or <name>/expected.json
	Formatted string // <name>.formatted.skg or <name>/formatted.skg; "" when absent
}

const (
	extSKG       = ".skg"
	extExpected  = ".expected.json"
	extFormatted = ".formatted.skg"
)

// discoverFixtures enumerates testdata/<subdir>. Nothing is hardcoded: dropping
// a fixture into the tree is all it takes to make both implementations run it.
func discoverFixtures(t *testing.T, subdir string) []fixture {
	t.Helper()
	dir := filepath.Join(testdataDir(), subdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read fixture dir %s: %v", dir, err)
	}

	var fixtures []fixture
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			f := fixture{
				Name:      name,
				IsDir:     true,
				EntryPath: filepath.Join(dir, name, "main.skg"),
				JSONPath:  filepath.Join(dir, name, "expected.json"),
			}
			if !fileExists(f.EntryPath) {
				t.Errorf("%s/%s: directory fixture has no main.skg", subdir, name)
				continue
			}
			if !fileExists(f.JSONPath) {
				t.Errorf("%s/%s: directory fixture has no expected.json", subdir, name)
				continue
			}
			if p := filepath.Join(dir, name, "formatted.skg"); fileExists(p) {
				f.Formatted = p
			}
			fixtures = append(fixtures, f)
			continue
		}

		switch {
		case strings.HasSuffix(name, extFormatted), strings.HasSuffix(name, extExpected):
			// Sidecar of some other fixture; validated with its owner.
			continue
		case strings.HasSuffix(name, extSKG):
			base := strings.TrimSuffix(name, extSKG)
			f := fixture{
				Name:      base,
				EntryPath: filepath.Join(dir, name),
				JSONPath:  filepath.Join(dir, base+extExpected),
			}
			if !fileExists(f.JSONPath) {
				t.Errorf("%s/%s: fixture has no %s%s", subdir, name, base, extExpected)
				continue
			}
			if p := filepath.Join(dir, base+extFormatted); fileExists(p) {
				f.Formatted = p
			}
			fixtures = append(fixtures, f)
		default:
			t.Errorf("%s/%s: unrecognised file in fixture tree (expected %s, %s or %s)",
				subdir, name, extSKG, extExpected, extFormatted)
		}
	}

	if len(fixtures) == 0 {
		t.Fatalf("no fixtures found in %s - the suite would pass vacuously", dir)
	}
	return fixtures
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// ─── Strict expected.json validation ────────────────────────────────────────
//
// A misspelled key such as "cod" used to mean the assertion silently did not
// happen. Validation walks the decoded JSON against a per-object allowlist and
// rejects anything unknown, so a typo is a hard failure instead.

type fixtureKind int

const (
	kindValid fixtureKind = iota
	kindInvalid
)

// schemaReport is what validation learns about a fixture beyond "it is legal":
// which optional assertions it carries, and therefore which capabilities the
// implementation needs in order to run it.
type schemaReport struct {
	assertsComments bool
}

var (
	rootValidKeys      = []string{"skg_version", "schema_version", "imports", "children", "leading_comments", "trailing_comments"}
	rootInvalidKeys    = []string{"error", "code", "line", "col"}
	fieldNodeKeys      = []string{"type", "key", "value", "leading_comments", "trailing_comment"}
	blockNodeKeys      = []string{"type", "name", "children", "leading_comments", "trailing_comments"}
	blockArrayNodeKeys = []string{"type", "name", "items", "leading_comments", "trailing_comments"}
	scalarValueKeys    = []string{"type", "data"}
	arrayValueKeys     = []string{"type", "data", "element_type"}

	valueTypes = []string{"string", "int", "float", "bool", "null", "array"}
)

type schemaError struct {
	path string
	msg  string
}

func (e *schemaError) Error() string { return e.path + ": " + e.msg }

func failAt(path, format string, args ...any) error {
	return &schemaError{path: path, msg: fmt.Sprintf(format, args...)}
}

func validateExpected(raw []byte, kind fixtureKind, codes map[string]bool) (schemaReport, error) {
	var rep schemaReport
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return rep, failAt("$", "not valid JSON: %v", err)
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return rep, failAt("$", "top level must be an object")
	}

	if kind == kindInvalid {
		if err := checkKeys("$", obj, rootInvalidKeys); err != nil {
			return rep, err
		}
		errFlag, ok := obj["error"]
		if !ok {
			return rep, failAt("$", "\"error\" is required and must be true")
		}
		if b, ok := errFlag.(bool); !ok || !b {
			return rep, failAt("$.error", "must be the literal true")
		}
		code, ok := obj["code"]
		if !ok {
			return rep, failAt("$", "\"code\" is required; message substrings are no longer accepted")
		}
		s, ok := code.(string)
		if !ok {
			return rep, failAt("$.code", "must be a string")
		}
		if !codes[s] {
			return rep, failAt("$.code", "%q is not in testdata/error-codes.json", s)
		}
		if s == string(CodeUnknown) {
			return rep, failAt("$.code", "UNKNOWN is a parser bug marker and may not be asserted")
		}
		for _, k := range []string{"line", "col"} {
			if v, ok := obj[k]; ok {
				if err := checkPositiveInt("$."+k, v); err != nil {
					return rep, err
				}
			}
		}
		return rep, nil
	}

	if err := checkKeys("$", obj, rootValidKeys); err != nil {
		return rep, err
	}
	for _, k := range []string{"skg_version", "schema_version"} {
		if v, ok := obj[k]; ok {
			if _, isStr := v.(string); !isStr && v != nil {
				return rep, failAt("$."+k, "must be a string or null")
			}
		}
	}
	if v, ok := obj["imports"]; ok {
		arr, ok := v.([]any)
		if !ok {
			return rep, failAt("$.imports", "must be an array of strings")
		}
		for i, item := range arr {
			if _, ok := item.(string); !ok {
				return rep, failAt(fmt.Sprintf("$.imports[%d]", i), "must be a string")
			}
		}
	}
	for _, k := range []string{"leading_comments", "trailing_comments"} {
		if v, ok := obj[k]; ok {
			rep.assertsComments = true
			if err := checkStringArray("$."+k, v); err != nil {
				return rep, err
			}
		}
	}
	if v, ok := obj["children"]; ok {
		if err := validateNodes("$.children", v, &rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

func validateNodes(path string, v any, rep *schemaReport) error {
	arr, ok := v.([]any)
	if !ok {
		return failAt(path, "must be an array of nodes")
	}
	for i, item := range arr {
		p := fmt.Sprintf("%s[%d]", path, i)
		obj, ok := item.(map[string]any)
		if !ok {
			return failAt(p, "must be an object")
		}
		typ, ok := obj["type"].(string)
		if !ok {
			return failAt(p, "\"type\" is required and must be one of field, block, block_array")
		}
		switch typ {
		case "field":
			if err := checkKeys(p, obj, fieldNodeKeys); err != nil {
				return err
			}
			if _, ok := obj["key"].(string); !ok {
				return failAt(p, "a field node requires a string \"key\"")
			}
			if v, ok := obj["value"]; ok {
				if err := validateValue(p+".value", v); err != nil {
					return err
				}
			}
			if v, ok := obj["leading_comments"]; ok {
				rep.assertsComments = true
				if err := checkStringArray(p+".leading_comments", v); err != nil {
					return err
				}
			}
			if v, ok := obj["trailing_comment"]; ok {
				rep.assertsComments = true
				if _, isStr := v.(string); !isStr && v != nil {
					return failAt(p+".trailing_comment", "must be a string or null")
				}
			}
		case "block", "block_array":
			allowed := blockNodeKeys
			if typ == "block_array" {
				allowed = blockArrayNodeKeys
			}
			if err := checkKeys(p, obj, allowed); err != nil {
				return err
			}
			if _, ok := obj["name"].(string); !ok {
				return failAt(p, "a %s node requires a string \"name\"", typ)
			}
			if typ == "block" {
				if v, ok := obj["children"]; ok {
					if err := validateNodes(p+".children", v, rep); err != nil {
						return err
					}
				}
			} else if v, ok := obj["items"]; ok {
				items, ok := v.([]any)
				if !ok {
					return failAt(p+".items", "must be an array of node arrays")
				}
				for j, it := range items {
					if err := validateNodes(fmt.Sprintf("%s.items[%d]", p, j), it, rep); err != nil {
						return err
					}
				}
			}
			for _, k := range []string{"leading_comments", "trailing_comments"} {
				if v, ok := obj[k]; ok {
					rep.assertsComments = true
					if err := checkStringArray(p+"."+k, v); err != nil {
						return err
					}
				}
			}
		default:
			return failAt(p+".type", "%q is not one of field, block, block_array", typ)
		}
	}
	return nil
}

func validateValue(path string, v any) error {
	obj, ok := v.(map[string]any)
	if !ok {
		return failAt(path, "must be an object")
	}
	typ, ok := obj["type"].(string)
	if !ok {
		return failAt(path, "\"type\" is required and must be one of %v", valueTypes)
	}
	if !contains(valueTypes, typ) {
		return failAt(path+".type", "%q is not one of %v", typ, valueTypes)
	}
	allowed := scalarValueKeys
	if typ == "array" {
		allowed = arrayValueKeys
	}
	if err := checkKeys(path, obj, allowed); err != nil {
		return err
	}
	data, hasData := obj["data"]
	if typ == "null" {
		if hasData {
			return failAt(path, "a null value carries no \"data\"")
		}
		return nil
	}
	if !hasData {
		return failAt(path, "\"data\" is required for a %s value", typ)
	}
	switch typ {
	case "string":
		if _, ok := data.(string); !ok {
			return failAt(path+".data", "must be a string")
		}
	case "int", "float":
		if _, ok := data.(float64); !ok {
			return failAt(path+".data", "must be a number")
		}
	case "bool":
		if _, ok := data.(bool); !ok {
			return failAt(path+".data", "must be a boolean")
		}
	case "array":
		et, ok := obj["element_type"]
		if !ok {
			return failAt(path, "\"element_type\" is required for an array value")
		}
		ets, ok := et.(string)
		if !ok || !contains(valueTypes, ets) {
			return failAt(path+".element_type", "must be one of %v", valueTypes)
		}
		items, ok := data.([]any)
		if !ok {
			return failAt(path+".data", "must be an array of value objects")
		}
		for i, item := range items {
			if err := validateValue(fmt.Sprintf("%s.data[%d]", path, i), item); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkKeys(path string, obj map[string]any, allowed []string) error {
	var unknown []string
	for k := range obj {
		if !contains(allowed, k) {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return failAt(path, "unknown key(s) %v (allowed: %v)", unknown, allowed)
	}
	return nil
}

func checkStringArray(path string, v any) error {
	arr, ok := v.([]any)
	if !ok {
		return failAt(path, "must be an array of strings")
	}
	for i, item := range arr {
		if _, ok := item.(string); !ok {
			return failAt(fmt.Sprintf("%s[%d]", path, i), "must be a string")
		}
	}
	return nil
}

func checkPositiveInt(path string, v any) error {
	f, ok := v.(float64)
	if !ok || f != math.Trunc(f) || f < 1 {
		return failAt(path, "must be a positive integer")
	}
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// ─── Typed views used for comparison ────────────────────────────────────────

type expectedFile struct {
	SKGVersion    *string        `json:"skg_version"`
	SchemaVersion *string        `json:"schema_version"`
	Imports       []string       `json:"imports"`
	Children      []expectedNode `json:"children"`
}

type expectedNode struct {
	Type     string           `json:"type"`
	Key      string           `json:"key"`
	Name     string           `json:"name"`
	Value    *expectedValue   `json:"value"`
	Children []expectedNode   `json:"children"`
	Items    [][]expectedNode `json:"items"`
}

type expectedValue struct {
	Type        string          `json:"type"`
	Data        json.RawMessage `json:"data"`
	ElementType string          `json:"element_type"`
}

type expectedError struct {
	Error bool   `json:"error"`
	Code  string `json:"code"`
	Line  *int   `json:"line"`
	Col   *int   `json:"col"`
}

// ─── Skip accounting ────────────────────────────────────────────────────────

// skipped counts fixtures not run because this implementation does not declare
// the capability they need. The totals are printed unconditionally by TestMain:
// honest partial conformance is allowed, quiet partial conformance is not.
var skipped = map[string][]string{}

func recordSkip(capability, fixtureName string) {
	skipped[capability] = append(skipped[capability], fixtureName)
}

func TestMain(m *testing.M) {
	code := m.Run()
	reportSkips(os.Stderr)
	os.Exit(code)
}

func reportSkips(w *os.File) {
	caps := make([]string, 0, len(skipped))
	for c := range skipped {
		caps = append(caps, c)
	}
	sort.Strings(caps)
	for _, c := range caps {
		names := skipped[c]
		sort.Strings(names)
		fmt.Fprintf(w, "CONFORMANCE: SKIPPED %d fixtures: capability %q not declared in go/conformance.json (%s)\n",
			len(names), c, strings.Join(names, ", "))
	}
	if len(caps) == 0 {
		fmt.Fprintf(w, "CONFORMANCE: all fixtures ran; no capability was skipped\n")
	}
}

// ─── Valid fixtures ─────────────────────────────────────────────────────────

func TestConformanceValid(t *testing.T) {
	manifest := loadManifest(t)
	codes := loadErrorCodes(t)

	for _, f := range discoverFixtures(t, "valid") {
		t.Run(f.Name, func(t *testing.T) {
			raw, err := os.ReadFile(f.JSONPath)
			if err != nil {
				t.Fatal(err)
			}
			report, err := validateExpected(raw, kindValid, codes)
			if err != nil {
				t.Fatalf("%s is not a valid expected.json: %v", f.JSONPath, err)
			}

			if missing := missingCapability(manifest, f, report); missing != "" {
				recordSkip(missing, "valid/"+f.Name)
				t.Skipf("capability %q not declared", missing)
			}

			var expected expectedFile
			if err := json.Unmarshal(raw, &expected); err != nil {
				t.Fatalf("bad expected JSON: %v", err)
			}

			file := loadFixture(t, f)

			compareOptionalString(t, "skg_version", expected.SKGVersion, file.SKGVersion)
			compareOptionalString(t, "schema_version", expected.SchemaVersion, file.SchemaVersion)

			if len(expected.Imports) != len(file.ImportPaths) {
				t.Errorf("imports: expected %d, got %d (%v)", len(expected.Imports), len(file.ImportPaths), file.ImportPaths)
			} else {
				for i, imp := range expected.Imports {
					if file.ImportPaths[i] != imp {
						t.Errorf("import[%d]: expected %q, got %q", i, imp, file.ImportPaths[i])
					}
				}
			}

			compareNodes(t, "", expected.Children, file.Children)

			if f.Formatted != "" {
				checkRoundTrip(t, f, file)
			}
		})
	}
}

// loadFixture applies the rule that separates the two parsing entry points:
// a directory fixture goes through the import-resolving file API, a flat
// fixture is parsed from bytes and must never touch the filesystem.
func loadFixture(t *testing.T, f fixture) *File {
	t.Helper()
	if f.IsDir {
		file, err := ParseFile(f.EntryPath)
		if err != nil {
			t.Fatalf("ParseFile(%s) failed: %v", f.EntryPath, err)
		}
		return file
	}
	data, err := os.ReadFile(f.EntryPath)
	if err != nil {
		t.Fatal(err)
	}
	file, err := ParseSource(data, f.Name+extSKG)
	if err != nil {
		t.Fatalf("ParseSource failed: %v", err)
	}
	return file
}

// checkRoundTrip pins the canonical text form: emitting the parsed fixture must
// reproduce the sidecar byte for byte, and re-emitting the sidecar must be a
// fixed point.
func checkRoundTrip(t *testing.T, f fixture, file *File) {
	t.Helper()
	want, err := os.ReadFile(f.Formatted)
	if err != nil {
		t.Fatal(err)
	}
	got := Emit(file)
	if string(got) != string(want) {
		t.Errorf("emit does not match %s\n--- want ---\n%s\n--- got ---\n%s",
			f.Formatted, want, got)
	}

	reparsed, err := ParseSource(want, filepath.Base(f.Formatted))
	if err != nil {
		t.Fatalf("formatted fixture does not parse: %v", err)
	}
	again := Emit(reparsed)
	if string(again) != string(want) {
		t.Errorf("emit is not idempotent for %s\n--- want ---\n%s\n--- got ---\n%s",
			f.Formatted, want, again)
	}
}

// missingCapability reports the first capability this fixture needs that the
// manifest does not declare, or "" when the fixture can run.
func missingCapability(m capabilityManifest, f fixture, rep schemaReport) string {
	if f.IsDir && !m.has(capImports) {
		return capImports
	}
	if f.Formatted != "" && !m.has(capEmit) {
		return capEmit
	}
	if rep.assertsComments && !m.has(capComments) {
		return capComments
	}
	return ""
}

func compareOptionalString(t *testing.T, label string, expected *string, actual *string) {
	t.Helper()
	switch {
	case expected == nil && actual != nil:
		t.Errorf("%s: expected nil, got %q", label, *actual)
	case expected != nil && actual == nil:
		t.Errorf("%s: expected %q, got nil", label, *expected)
	case expected != nil && *expected != *actual:
		t.Errorf("%s: expected %q, got %q", label, *expected, *actual)
	}
}

// ─── Invalid fixtures ───────────────────────────────────────────────────────

func TestConformanceInvalid(t *testing.T) {
	manifest := loadManifest(t)
	codes := loadErrorCodes(t)

	for _, f := range discoverFixtures(t, "invalid") {
		t.Run(f.Name, func(t *testing.T) {
			raw, err := os.ReadFile(f.JSONPath)
			if err != nil {
				t.Fatal(err)
			}
			report, err := validateExpected(raw, kindInvalid, codes)
			if err != nil {
				t.Fatalf("%s is not a valid expected.json: %v", f.JSONPath, err)
			}
			if missing := missingCapability(manifest, f, report); missing != "" {
				recordSkip(missing, "invalid/"+f.Name)
				t.Skipf("capability %q not declared", missing)
			}

			var expected expectedError
			if err := json.Unmarshal(raw, &expected); err != nil {
				t.Fatalf("bad expected JSON: %v", err)
			}

			var parseErr error
			if f.IsDir {
				_, parseErr = ParseFile(f.EntryPath)
			} else {
				data, readErr := os.ReadFile(f.EntryPath)
				if readErr != nil {
					t.Fatal(readErr)
				}
				_, parseErr = ParseSource(data, f.Name+extSKG)
			}
			if parseErr == nil {
				t.Fatal("expected parse error, got success")
			}
			pe, ok := parseErr.(*ParseError)
			if !ok {
				t.Fatalf("expected *ParseError, got %T: %v", parseErr, parseErr)
			}
			if string(pe.Diag.Code) != expected.Code {
				t.Errorf("code: expected %s, got %s (message: %s)", expected.Code, pe.Diag.Code, pe.Diag.Message)
			}
			if expected.Line != nil && pe.Diag.Line != *expected.Line {
				t.Errorf("line: expected %d, got %d", *expected.Line, pe.Diag.Line)
			}
			if expected.Col != nil && pe.Diag.Col != *expected.Col {
				t.Errorf("col: expected %d, got %d", *expected.Col, pe.Diag.Col)
			}
			if pe.Diag.Message == "" {
				t.Error("diagnostic has an empty human-readable message")
			}
		})
	}
}

// ─── AST comparison ─────────────────────────────────────────────────────────

func compareNodes(t *testing.T, path string, expected []expectedNode, actual []Node) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Errorf("%schildren count: expected %d, got %d", path, len(expected), len(actual))
		return
	}
	for i, en := range expected {
		an := actual[i]
		prefix := fmt.Sprintf("%s[%d].", path, i)

		switch en.Type {
		case "field":
			if an.Field == nil {
				t.Errorf("%sexpected field, got block", prefix)
				continue
			}
			if an.Field.Key != en.Key {
				t.Errorf("%skey: expected %q, got %q", prefix, en.Key, an.Field.Key)
			}
			if en.Value != nil {
				compareValue(t, prefix+"value.", *en.Value, an.Field.Value)
			}

		case "block":
			if an.Block == nil {
				t.Errorf("%sexpected block, got non-block", prefix)
				continue
			}
			if an.Block.Name != en.Name {
				t.Errorf("%sname: expected %q, got %q", prefix, en.Name, an.Block.Name)
			}
			compareNodes(t, prefix, en.Children, an.Block.Children)

		case "block_array":
			if an.BlockArray == nil {
				t.Errorf("%sexpected block_array, got non-block_array", prefix)
				continue
			}
			if an.BlockArray.Name != en.Name {
				t.Errorf("%sname: expected %q, got %q", prefix, en.Name, an.BlockArray.Name)
			}
			if len(en.Items) != len(an.BlockArray.Items) {
				t.Errorf("%sitems count: expected %d, got %d", prefix, len(en.Items), len(an.BlockArray.Items))
				continue
			}
			for j, item := range en.Items {
				itemPrefix := fmt.Sprintf("%sitems[%d].", prefix, j)
				compareNodes(t, itemPrefix, item, an.BlockArray.Items[j])
			}
		}
	}
}

func compareValue(t *testing.T, path string, expected expectedValue, actual Value) {
	t.Helper()

	var expectedType ValueType
	switch expected.Type {
	case "string":
		expectedType = TypeString
	case "int":
		expectedType = TypeInt
	case "float":
		expectedType = TypeFloat
	case "bool":
		expectedType = TypeBool
	case "null":
		expectedType = TypeNull
	case "array":
		expectedType = TypeArray
	default:
		t.Errorf("%sunknown expected type %q", path, expected.Type)
		return
	}

	if actual.Type != expectedType {
		t.Errorf("%stype: expected %v, got %v", path, expectedType, actual.Type)
		return
	}

	switch expected.Type {
	case "string":
		var s string
		if err := json.Unmarshal(expected.Data, &s); err != nil {
			t.Errorf("%scannot parse expected string data: %v", path, err)
			return
		}
		if actual.Str != s {
			t.Errorf("%svalue: expected %q, got %q", path, s, actual.Str)
		}

	case "int":
		var n float64 // JSON numbers are float64
		if err := json.Unmarshal(expected.Data, &n); err != nil {
			t.Errorf("%scannot parse expected int data: %v", path, err)
			return
		}
		if actual.Int != int64(n) {
			t.Errorf("%svalue: expected %d, got %d", path, int64(n), actual.Int)
		}

	case "float":
		var f float64
		if err := json.Unmarshal(expected.Data, &f); err != nil {
			t.Errorf("%scannot parse expected float data: %v", path, err)
			return
		}
		if math.Abs(actual.Float-f) > 1e-9 {
			t.Errorf("%svalue: expected %g, got %g", path, f, actual.Float)
		}

	case "bool":
		var b bool
		if err := json.Unmarshal(expected.Data, &b); err != nil {
			t.Errorf("%scannot parse expected bool data: %v", path, err)
			return
		}
		if actual.Bool != b {
			t.Errorf("%svalue: expected %v, got %v", path, b, actual.Bool)
		}

	case "null":
		// Nothing to compare.

	case "array":
		if actual.Array == nil {
			t.Errorf("%sexpected array data, got nil", path)
			return
		}
		var expectedElemType ValueType
		switch expected.ElementType {
		case "string":
			expectedElemType = TypeString
		case "int":
			expectedElemType = TypeInt
		case "float":
			expectedElemType = TypeFloat
		case "bool":
			expectedElemType = TypeBool
		case "array":
			expectedElemType = TypeArray
		case "null":
			expectedElemType = TypeNull
		}
		if actual.Array.ElementType != expectedElemType {
			t.Errorf("%selement_type: expected %v, got %v", path, expectedElemType, actual.Array.ElementType)
		}

		var items []expectedValue
		if err := json.Unmarshal(expected.Data, &items); err != nil {
			t.Errorf("%scannot parse expected array data: %v", path, err)
			return
		}
		if len(items) != len(actual.Array.Items) {
			t.Errorf("%sarray length: expected %d, got %d", path, len(items), len(actual.Array.Items))
			return
		}
		for i, item := range items {
			compareValue(t, fmt.Sprintf("%s[%d].", path, i), item, actual.Array.Items[i])
		}
	}
}

// ─── Self-tests for the harness ─────────────────────────────────────────────
//
// The validator is the thing standing between a typo and a silently missing
// assertion, so it gets tested too.

func TestExpectedSchemaRejectsUnknownKeys(t *testing.T) {
	codes := loadErrorCodes(t)
	cases := []struct {
		name string
		kind fixtureKind
		json string
	}{
		{"misspelled code key", kindInvalid, `{"error": true, "cod": "MIXED_ARRAY_TYPES"}`},
		{"missing code", kindInvalid, `{"error": true}`},
		{"unregistered code", kindInvalid, `{"error": true, "code": "NOT_A_REAL_CODE"}`},
		{"error false", kindInvalid, `{"error": false, "code": "MIXED_ARRAY_TYPES"}`},
		{"legacy message_contains", kindInvalid, `{"error": true, "message_contains": "mixed"}`},
		{"unknown root key", kindValid, `{"childrens": []}`},
		{"unknown node key", kindValid, `{"children": [{"type": "field", "key": "a", "keys": "b"}]}`},
		{"block key on field", kindValid, `{"children": [{"type": "field", "key": "a", "name": "a"}]}`},
		{"bad node type", kindValid, `{"children": [{"type": "feild", "key": "a"}]}`},
		{"value without data", kindValid, `{"children": [{"type": "field", "key": "a", "value": {"type": "int"}}]}`},
		{"array without element_type", kindValid, `{"children": [{"type": "field", "key": "a", "value": {"type": "array", "data": []}}]}`},
		{"null with data", kindValid, `{"children": [{"type": "field", "key": "a", "value": {"type": "null", "data": 1}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateExpected([]byte(tc.json), tc.kind, codes); err == nil {
				t.Errorf("expected validation to reject %s", tc.json)
			}
		})
	}
}

func TestExpectedSchemaDetectsCommentAssertions(t *testing.T) {
	codes := loadErrorCodes(t)
	withComments := []string{
		`{"leading_comments": ["# hi"]}`,
		`{"children": [{"type": "field", "key": "a", "trailing_comment": "# hi"}]}`,
		`{"children": [{"type": "block", "name": "b", "trailing_comments": ["# hi"]}]}`,
		`{"children": [{"type": "block", "name": "b", "children": [{"type": "field", "key": "a", "leading_comments": []}]}]}`,
	}
	for _, src := range withComments {
		rep, err := validateExpected([]byte(src), kindValid, codes)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", src, err)
		}
		if !rep.assertsComments {
			t.Errorf("%s: comment assertion not detected", src)
		}
	}

	rep, err := validateExpected([]byte(`{"children": [{"type": "field", "key": "a"}]}`), kindValid, codes)
	if err != nil {
		t.Fatal(err)
	}
	if rep.assertsComments {
		t.Error("comment assertion falsely detected")
	}
}
