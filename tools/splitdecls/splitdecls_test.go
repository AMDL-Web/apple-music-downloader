package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sourceFile = `// Package demo is a fixture.
package demo

import (
	"fmt"
	"strings"
)

// limit caps things.
const limit = 3

// Free comment that belongs to the type below.

// Box holds a value.
type Box struct{ v string }

// Upper uppercases the value.
func (b *Box) Upper() string { return strings.ToUpper(b.v) }

func describe(b Box) string { return fmt.Sprintf("%s/%d", b.v, limit) }
`

const existingTarget = `package demo

import "fmt"

func hello() string { return fmt.Sprint("hi") }
`

func TestMoveIsByteExactAndVerifies(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("demo.go", sourceFile)
	write("existing.go", existingTarget)
	base, err := diskFiles(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	// Key base files by the same path the disk snapshot will use.
	err = move(dir, "demo.go", map[string][]string{
		"box.go":      {"Box", "Box.Upper"},
		"existing.go": {"describe"},
	})
	if err != nil {
		t.Fatal(err)
	}

	box := read(t, dir, "box.go")
	for _, want := range []string{
		"// Free comment that belongs to the type below.",
		"// Upper uppercases the value.\nfunc (b *Box) Upper() string { return strings.ToUpper(b.v) }",
		`"strings"`,
	} {
		if !strings.Contains(box, want) {
			t.Errorf("box.go missing %q:\n%s", want, box)
		}
	}
	if strings.Contains(box, `"fmt"`) {
		t.Errorf("box.go kept an unused import:\n%s", box)
	}
	existing := read(t, dir, "existing.go")
	if !strings.Contains(existing, "func hello()") || !strings.Contains(existing, "func describe(b Box)") {
		t.Errorf("existing.go lost or missed code:\n%s", existing)
	}
	demo := read(t, dir, "demo.go")
	if !strings.HasPrefix(demo, "// Package demo is a fixture.\npackage demo") {
		t.Errorf("demo.go lost its package doc:\n%s", demo)
	}
	if strings.Contains(demo, "import") {
		t.Errorf("demo.go kept imports it no longer uses:\n%s", demo)
	}

	head, err := diskFiles(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	problems, ndecls, _, err := compare(base, head)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 || ndecls != 5 {
		t.Fatalf("expected a clean 5-declaration comparison, got %d decls and %v", ndecls, problems)
	}
}

func TestCompareReportsEdits(t *testing.T) {
	base := map[string][]byte{"a.go": []byte("package a\n\n// keep\nfunc F() int { return 1 }\n")}
	head := map[string][]byte{"b.go": []byte("package a\n\n// keep\nfunc F() int { return 2 }\n\nfunc G() {}\n")}
	problems, _, _, err := compare(base, head)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(problems, "\n")
	if !strings.Contains(got, "changed: F") || !strings.Contains(got, "added: G") {
		t.Fatalf("unexpected problems: %v", problems)
	}
}

func TestMoveRejectsUnknownDeclaration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "demo.go"), []byte(sourceFile), 0o644); err != nil {
		t.Fatal(err)
	}
	err := move(dir, "demo.go", map[string][]string{"x.go": {"Missing"}})
	if err == nil || !strings.Contains(err.Error(), `"Missing" not found`) {
		t.Fatalf("expected a not-found error, got %v", err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
