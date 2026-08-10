package skg

import (
	"os"
	"path/filepath"
	"testing"
)

// The files in examples/ are what a reader meets first - they are quoted in the
// README and in docs/spec.md. Nothing used to parse them, so a grammar change
// could leave the documentation illustrating a language the parser no longer
// accepts. This test runs them through the import-resolving file API, which is
// how a consumer would actually load them.
//
// It deliberately does not assert their formatting: the emitter has no
// blank-line trivia, so `skg fmt` closes up the spacing that makes the examples
// readable. See docs/conformance.md "Known gaps".
func TestExamplesParse(t *testing.T) {
	dir := filepath.Join("..", "examples")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", dir, err)
	}

	found := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".skg" {
			continue
		}
		found++
		t.Run(e.Name(), func(t *testing.T) {
			if _, err := ParseFile(filepath.Join(dir, e.Name())); err != nil {
				t.Errorf("%s does not parse: %v", e.Name(), err)
			}
		})
	}
	if found == 0 {
		t.Fatal("no examples found - the test would pass vacuously")
	}
}
