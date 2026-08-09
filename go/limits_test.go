package skg

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nestedArrays(n int) []byte {
	return []byte("x: " + strings.Repeat("[", n) + strings.Repeat("]", n))
}

func nestedBlocks(n int) []byte {
	return []byte(strings.Repeat("a { ", n) + strings.Repeat("} ", n))
}

func nestedBlockArrays(n int) []byte {
	// Each level contributes two nesting levels: the '[' and the item's '{'.
	return []byte(strings.Repeat("a [ { ", n) + strings.Repeat("} ] ", n))
}

func TestNestingDepthLimitAtBoundary(t *testing.T) {
	cases := []struct {
		name string
		src  []byte
	}{
		{"arrays", nestedArrays(MaxNestingDepth)},
		{"blocks", nestedBlocks(MaxNestingDepth)},
		{"block_arrays", nestedBlockArrays(MaxNestingDepth / 2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.src); err != nil {
				t.Fatalf("depth %d should be accepted, got %v", MaxNestingDepth, err)
			}
		})
	}
}

// Regression: unbounded recursion used to end in a fatal stack overflow that
// recover() cannot catch, so a consumer had no way to defend itself.
func TestNestingDepthLimitExceeded(t *testing.T) {
	cases := []struct {
		name string
		src  []byte
	}{
		{"arrays", nestedArrays(MaxNestingDepth + 1)},
		{"blocks", nestedBlocks(MaxNestingDepth + 1)},
		{"block_arrays", nestedBlockArrays(MaxNestingDepth/2 + 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Parse(tc.src)
			if err == nil {
				t.Fatalf("expected nesting depth error, got file %v", f)
			}
			pe, ok := err.(*ParseError)
			if !ok {
				t.Fatalf("expected *ParseError, got %T: %v", err, err)
			}
			if !strings.Contains(pe.Diag.Message, "nesting too deep") {
				t.Errorf("unexpected message: %q", pe.Diag.Message)
			}
			if pe.Diag.Line < 1 || pe.Diag.Col < 1 {
				t.Errorf("expected a source position, got %d:%d", pe.Diag.Line, pe.Diag.Col)
			}
		})
	}
}

func TestFileSizeLimitParseSource(t *testing.T) {
	atLimit := bytes.Repeat([]byte("#"), MaxFileSize)
	if _, err := Parse(atLimit); err != nil {
		t.Fatalf("input of exactly MaxFileSize should parse, got %v", err)
	}

	oversized := bytes.Repeat([]byte("#"), MaxFileSize+1)
	_, err := Parse(oversized)
	if err == nil {
		t.Fatal("expected oversized input to be rejected")
	}
	if !strings.Contains(err.Error(), "file too large") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFileSizeLimitParseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.skg")
	if err := os.WriteFile(path, bytes.Repeat([]byte("#"), MaxFileSize+1), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected oversized file to be rejected")
	}
	if !strings.Contains(err.Error(), "file too large") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSKGVersionValidation(t *testing.T) {
	cases := []struct {
		version string
		wantMsg string // empty means the version must be accepted
	}{
		{"1.0", ""},
		{"0.9", ""},
		{"9.9", "newer than this parser supports"},
		{"1.1", "newer than this parser supports"},
		{"2.0", "newer than this parser supports"},
		{"abc", "malformed skg_version"},
		{"1", "malformed skg_version"},
		{"1.0.0", "malformed skg_version"},
		{"", "malformed skg_version"},
		{"-1.0", "malformed skg_version"},
		{"1.x", "malformed skg_version"},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			src := []byte("skg_version: \"" + tc.version + "\"\nname: \"x\"\n")
			f, err := Parse(src)
			if tc.wantMsg == "" {
				if err != nil {
					t.Fatalf("expected %q to be accepted, got %v", tc.version, err)
				}
				if f.SKGVersion == nil || *f.SKGVersion != tc.version {
					t.Fatalf("expected recorded skg_version %q, got %v", tc.version, f.SKGVersion)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected %q to be rejected", tc.version)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("expected message containing %q, got %v", tc.wantMsg, err)
			}
		})
	}
}

// schema_version is recorded but deliberately not interpreted (docs/spec.md:
// "validation is the consuming application's responsibility"), so any string
// is accepted.
func TestSchemaVersionNotValidated(t *testing.T) {
	f, err := Parse([]byte("schema_version: \"9.9.9-not-a-version\"\nname: \"x\"\n"))
	if err != nil {
		t.Fatalf("schema_version should not be validated, got %v", err)
	}
	if f.SchemaVersion == nil || *f.SchemaVersion != "9.9.9-not-a-version" {
		t.Fatalf("unexpected schema_version: %v", f.SchemaVersion)
	}
}
