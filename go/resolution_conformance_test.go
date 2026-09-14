package skg

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type resolutionContract struct {
	ContractVersion int                      `json:"contract_version"`
	LanguageVersion string                   `json:"language_version"`
	Cases           []resolutionContractCase `json:"cases"`
}

type resolutionContractCase struct {
	Name   string `json:"name"`
	Entry  string `json:"entry"`
	Root   string `json:"root"`
	Limits struct {
		Bytes     int64 `json:"bytes"`
		Files     int   `json:"files"`
		Nodes     int   `json:"nodes"`
		MergeWork int64 `json:"merge_work"`
	} `json:"limits"`
	Files []struct {
		Path   string  `json:"path"`
		Source *string `json:"source"`
	} `json:"files"`
	ExpectedFormatted *string    `json:"expected_formatted"`
	ExpectedCode      *ErrorCode `json:"expected_code"`
}

func TestResolutionConformance(t *testing.T) {
	const path = "../testdata/resolution/cases.json"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	validateResolutionContractJSON(t, data)

	var suite resolutionContract
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&suite); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("trailing JSON value")
	}
	if suite.ContractVersion != 1 || suite.LanguageVersion != "1.0" || len(suite.Cases) == 0 {
		t.Fatalf("unsupported or empty resolution contract: version=%d language=%q cases=%d", suite.ContractVersion, suite.LanguageVersion, len(suite.Cases))
	}

	codes := loadErrorCodes(t)
	names := make(map[string]bool, len(suite.Cases))
	for _, fixture := range suite.Cases {
		fixture := fixture
		if fixture.Name == "" || names[fixture.Name] {
			t.Fatalf("empty or duplicate resolution case name %q", fixture.Name)
		}
		if fixture.ExpectedCode != nil && !codes[string(*fixture.ExpectedCode)] {
			t.Fatalf("resolution case %q names unregistered error code %q", fixture.Name, *fixture.ExpectedCode)
		}
		names[fixture.Name] = true
		t.Run(fixture.Name, func(t *testing.T) { runResolutionContractCase(t, fixture) })
	}
	t.Logf("resolution contract v%d: %d cases, none skipped", suite.ContractVersion, len(suite.Cases))
}

func validateResolutionContractJSON(t *testing.T, data []byte) {
	t.Helper()
	root := nativeFixtureObject(t, data,
		[]string{"contract_version", "language_version", "cases"},
		[]string{"contract_version", "language_version", "cases"})
	var cases []json.RawMessage
	if err := json.Unmarshal(root["cases"], &cases); err != nil {
		t.Fatal(err)
	}
	for _, raw := range cases {
		obj := nativeFixtureObject(t, raw,
			[]string{"name", "entry", "root", "limits", "files", "expected_formatted", "expected_code"},
			[]string{"name", "entry", "limits", "files"})
		_, formatted := obj["expected_formatted"]
		_, code := obj["expected_code"]
		if formatted == code {
			t.Fatal("resolution case needs exactly one of expected_formatted and expected_code")
		}
		nativeFixtureObject(t, obj["limits"],
			[]string{"bytes", "files", "nodes", "merge_work"},
			[]string{"bytes", "files", "nodes", "merge_work"})
		var files []json.RawMessage
		if err := json.Unmarshal(obj["files"], &files); err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			nativeFixtureObject(t, file, []string{"path", "source"}, []string{"path", "source"})
		}
	}
}

func runResolutionContractCase(t *testing.T, fixture resolutionContractCase) {
	t.Helper()
	if fixture.Entry == "" || len(fixture.Files) == 0 || fixture.Limits.Bytes <= 0 || fixture.Limits.Files <= 0 || fixture.Limits.Nodes <= 0 || fixture.Limits.MergeWork <= 0 {
		t.Fatal("entry, files, and positive explicit limits are required")
	}
	base := t.TempDir()
	seen := make(map[string]bool, len(fixture.Files))
	for _, file := range fixture.Files {
		path := safeContractPath(t, base, file.Path)
		if file.Source == nil || seen[path] {
			t.Fatalf("file %q has null source or duplicate path", file.Path)
		}
		seen[path] = true
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(*file.Source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	options := ResolveOptions{
		MaxBytes: fixture.Limits.Bytes, MaxFiles: fixture.Limits.Files,
		MaxNodes: fixture.Limits.Nodes, MaxMergeWork: fixture.Limits.MergeWork,
	}
	if fixture.Root != "" {
		options.Root = safeContractPath(t, base, fixture.Root)
	}
	file, err := ParseFileWithOptions(safeContractPath(t, base, fixture.Entry), options)
	if fixture.ExpectedCode != nil {
		if err == nil {
			t.Fatalf("want %s, got success", *fixture.ExpectedCode)
		}
		var parseErr *ParseError
		if !errors.As(err, &parseErr) {
			t.Fatalf("want *ParseError with %s, got %T: %v", *fixture.ExpectedCode, err, err)
		}
		if parseErr.Diag.Code != *fixture.ExpectedCode {
			t.Fatalf("want %s, got %s: %v", *fixture.ExpectedCode, parseErr.Diag.Code, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if got := string(Emit(file)); got != *fixture.ExpectedFormatted {
		t.Fatalf("resolved output mismatch\nwant:\n%s\ngot:\n%s", *fixture.ExpectedFormatted, got)
	}
}

func safeContractPath(t *testing.T, base, slashPath string) string {
	t.Helper()
	path := filepath.Clean(filepath.FromSlash(slashPath))
	if slashPath == "" || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		t.Fatalf("unsafe resolution contract path %q", slashPath)
	}
	return filepath.Join(base, path)
}
