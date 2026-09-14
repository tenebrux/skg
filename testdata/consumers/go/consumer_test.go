package consumer_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tenebrux/skg/go"
)

type port uint16

func (p *port) DecodeSKG(ctx *skg.DecodeContext, value skg.Value) error {
	var decoded uint16
	if err := ctx.Decode(value, &decoded); err != nil {
		return err
	}
	*p = port(decoded)
	return nil
}

func (p *port) ValidateSKG(ctx *skg.DecodeContext) error {
	if *p == 0 {
		return ctx.Fail(skg.NativeCustomError, "port must be nonzero")
	}
	return nil
}

type worker struct {
	Name   string `skg:"name"`
	Weight uint8  `skg:"weight"`
}

type nested struct {
	Values [][]int64 `skg:"values"`
}

type service struct {
	Host string `skg:"host"`
	Port port   `skg:"port"`
	Mode string `skg:"mode"`
}

type config struct {
	Service    service          `skg:"service"`
	Features   map[string]bool  `skg:"features"`
	Workers    []*worker        `skg:"workers"`
	Thresholds []*int16         `skg:"thresholds"`
	Labels     map[string]uint8 `skg:"labels"`
	RemoveMe   *string          `skg:"remove_me"`
	Nested     nested           `skg:"nested"`
}

const baseSource = `service { host: "base" port: 80 mode: "safe" }
features { old: true keep: true }
workers [ { name: "alpha" weight: 1 } null ]
thresholds: [1, null, 3]
labels { "a/b~c": 7 }
remove_me: "gone"
extra: true
`

const mainSource = `skg_version: "1.0"
import "base.skg"
service { host: "main" port: 8080 }
@replace features { new: true }
@delete remove_me
nested: { values: [[1, 2], [3]] }
`

func writeGraph(t *testing.T, base, main string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.skg")
	mainPath := filepath.Join(dir, "main.skg")
	if err := os.WriteFile(basePath, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, mainPath
}

func TestPublicConsumerWorkflow(t *testing.T) {
	parsedSource, err := skg.ParseSource([]byte(`import "ghost.skg" value: 1`), "memory.skg")
	if err != nil {
		t.Fatal(err)
	}
	if len(parsedSource.ImportPaths) != 1 || parsedSource.ImportPaths[0] != "ghost.skg" || parsedSource.ImportsResolved {
		t.Fatalf("source parsing unexpectedly resolved imports: %#v", parsedSource)
	}

	dir, mainPath := writeGraph(t, baseSource, mainSource)
	parsedFile, err := skg.ParseFileWithOptions(mainPath, skg.ResolveOptions{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !parsedFile.ImportsResolved {
		t.Fatal("file API did not mark the import graph resolved")
	}
	canonical := skg.Emit(parsedFile)
	if strings.Contains(string(canonical), "import ") || strings.Contains(string(canonical), "@delete") || strings.Contains(string(canonical), "@replace") {
		t.Fatalf("resolved canonical output retained operations:\n%s", canonical)
	}
	if _, err := skg.ParseSource(canonical, "canonical.skg"); err != nil {
		t.Fatalf("canonical output does not parse: %v\n%s", err, canonical)
	}

	got, err := skg.DecodeFileWithOptions[config](mainPath, skg.DecodeOptions{}, skg.ResolveOptions{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	assertConfig(t, got)

	roundTrip, err := skg.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := skg.DecodeSource[config](roundTrip, "roundtrip.skg", skg.DecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertConfig(t, decoded)

	legacy := struct {
		Count int `skg:"count"`
	}{Count: 7}
	if err := skg.Unmarshal([]byte("count: 9"), &legacy); err != nil || legacy.Count != 9 {
		t.Fatalf("legacy compatibility API failed: %#v, %v", legacy, err)
	}
}

func assertConfig(t *testing.T, got config) {
	t.Helper()
	if got.Service.Host != "main" || got.Service.Port != 8080 || got.Service.Mode != "safe" {
		t.Fatalf("service overlay mismatch: %#v", got.Service)
	}
	if len(got.Features) != 1 || !got.Features["new"] {
		t.Fatalf("replace mismatch: %#v", got.Features)
	}
	if got.RemoveMe != nil {
		t.Fatalf("delete mismatch: %#v", got.RemoveMe)
	}
	if len(got.Workers) != 2 || got.Workers[0] == nil || got.Workers[0].Name != "alpha" || got.Workers[1] != nil {
		t.Fatalf("nullable object list mismatch: %#v", got.Workers)
	}
	if len(got.Thresholds) != 3 || got.Thresholds[0] == nil || *got.Thresholds[0] != 1 || got.Thresholds[1] != nil || got.Thresholds[2] == nil || *got.Thresholds[2] != 3 {
		t.Fatalf("nullable scalar list mismatch: %#v", got.Thresholds)
	}
	if got.Labels["a/b~c"] != 7 || len(got.Nested.Values) != 2 || got.Nested.Values[1][0] != 3 {
		t.Fatalf("map or nested array mismatch: %#v", got)
	}
}

func TestStrictDiagnosticsAndTransactionalInto(t *testing.T) {
	type known struct {
		Value uint8 `skg:"value"`
	}
	_, err := skg.DecodeSource[known]([]byte("value: 1 extra: true"), "strict.skg", skg.DecodeOptions{RejectUnknownFields: true})
	expectDecodeError(t, err, skg.NativeUnknownField, "/extra", "strict.skg")

	type defaults struct {
		Count uint8 `skg:"count"`
		Items []int `skg:"items"`
	}
	target := defaults{Count: 7, Items: []int{1}}
	err = skg.DecodeSourceInto([]byte("count: 256 items: [2]"), "bad.skg", &target, skg.DecodeOptions{AllowMissingFields: true})
	expectDecodeError(t, err, skg.NativeNumberOutOfRange, "/count", "bad.skg")
	if target.Count != 7 || len(target.Items) != 1 || target.Items[0] != 1 {
		t.Fatalf("failed transactional decode mutated target: %#v", target)
	}
	if err := skg.DecodeSourceInto([]byte("items: [2, 3]"), "good.skg", &target, skg.DecodeOptions{AllowMissingFields: true}); err != nil {
		t.Fatal(err)
	}
	if target.Count != 7 || len(target.Items) != 2 || target.Items[1] != 3 {
		t.Fatalf("defaults were not retained on successful Into: %#v", target)
	}

	_, err = skg.DecodeSource[struct {
		Port port `skg:"port"`
	}]([]byte("port: 0"), "hook.skg", skg.DecodeOptions{})
	expectDecodeError(t, err, skg.NativeCustomError, "/port", "hook.skg")
}

func TestImportedFailureRetainsProvenance(t *testing.T) {
	dir, mainPath := writeGraph(t, "service { port: 70000 }\n", "import \"base.skg\"\n")
	type badConfig struct {
		Service struct {
			Port port `skg:"port"`
		} `skg:"service"`
	}
	_, err := skg.DecodeFileWithOptions[badConfig](mainPath, skg.DecodeOptions{}, skg.ResolveOptions{Root: dir})
	expectDecodeError(t, err, skg.NativeNumberOutOfRange, "/service/port", "base.skg")
}

func expectDecodeError(t *testing.T, err error, code skg.NativeCode, fieldPath, sourceBase string) {
	t.Helper()
	var decoded *skg.DecodeError
	if !errors.As(err, &decoded) {
		t.Fatalf("expected DecodeError, got %T: %v", err, err)
	}
	if decoded.Code != code || decoded.FieldPath != fieldPath || filepath.Base(decoded.Source.Path) != sourceBase || decoded.Source.Line == 0 || decoded.Source.Col == 0 {
		t.Fatalf("unexpected diagnostic: %#v", decoded)
	}
}
