package temaridiff

import (
	"bytes"
	"os"
	"testing"

	"amdl/tools/temaridiff/internal/transpile"
)

// TestRoundsGenUpToDate fails when the Temari version in go.mod no longer
// translates to the committed rounds_gen.go, for example after a dependency
// bump changed upstream's rounds_gen.rs or a hand-ported helper.
func TestRoundsGenUpToDate(t *testing.T) {
	src, version, err := transpile.Source()
	if err != nil {
		t.Fatal(err)
	}
	want, err := transpile.File(src, version)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../internal/fairplay/rounds_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("internal/fairplay/rounds_gen.go is not the translation of Temari %s; from tools/temaridiff run: go run ./cmd/transpile -o ../../internal/fairplay/rounds_gen.go", version)
	}
}
