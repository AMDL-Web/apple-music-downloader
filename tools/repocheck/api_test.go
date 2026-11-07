package repocheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"amdl/internal/domain"
)

// The checks that hold the API's documents (docs/api.md, the enums of
// internal/api/openapi.yaml) to the Go source that produces what they list.
// The parity checks between the spec and the server's own code stay in
// internal/api.

// The Events section of docs/api.md lists every event type.
func TestDocsListEveryEventType(t *testing.T) {
	var want []string
	for _, ty := range domain.EventTypes {
		want = append(want, string(ty))
	}
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "api.md"))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(doc), "\n## Events")
	if start < 0 {
		t.Fatal("docs/api.md has no Events section")
	}
	events := string(doc[start+1:])
	if end := strings.Index(events, "\n## "); end >= 0 {
		events = events[:end]
	}
	for _, ty := range want {
		if !strings.Contains(events, "`"+ty+"`") {
			t.Errorf("docs/api.md Events section does not list `%s`", ty)
		}
	}
	for _, gone := range []string{"codec_retry", "codec_exhausted", "codec_recovered"} {
		if strings.Contains(events, "`"+gone+"`") {
			t.Errorf("docs/api.md Events section still lists never-emitted `%s`", gone)
		}
	}
}

var requestErrorCode = regexp.MustCompile(`(?s)RequestError\{[^}]*?Code:\s*"([a-z_]+)"`)

// The SubmitErrorCode enum lists exactly the per-URL codes the server
// produces: the job manager's domain.SubmitCode* constants (the untyped string
// constants of internal/domain/submit.go) and every code the request
// validator in internal/media puts in a jobs.RequestError.
func TestSubmitErrorCodesMatchProducers(t *testing.T) {
	produced := untypedStringConsts(t, filepath.Join("..", "..", "internal", "domain", "submit.go"))
	sources, err := filepath.Glob(filepath.Join("..", "..", "internal", "media", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range requestErrorCode.FindAllSubmatch(raw, -1) {
			produced = append(produced, string(match[1]))
		}
	}
	slices.Sort(produced)
	produced = slices.Compact(produced)
	want := openAPIEnum(t, "SubmitErrorCode")
	if !slices.Equal(produced, want) {
		t.Fatalf("produced submit codes = %v\nopenapi.yaml SubmitErrorCode enum = %v", produced, want)
	}
}

// openAPIEnum returns components.schemas.<name>.enum of
// internal/api/openapi.yaml, sorted.
func openAPIEnum(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Enum []string `yaml:"enum"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	enum := slices.Clone(spec.Components.Schemas[name].Enum)
	if len(enum) == 0 {
		t.Fatalf("openapi.yaml has no %s enum", name)
	}
	slices.Sort(enum)
	return enum
}

// untypedStringConsts parses a Go file and returns the values of its untyped
// string constants, sorted.
func untypedStringConsts(t *testing.T, file string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			if vs.Type != nil {
				continue
			}
			for _, value := range vs.Values {
				lit, ok := value.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				unquoted, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				values = append(values, unquoted)
			}
		}
	}
	slices.Sort(values)
	return values
}
