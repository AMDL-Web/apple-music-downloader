package api

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

	"amdl/internal/config"
	"amdl/internal/domain"

	"gopkg.in/yaml.v3"
)

// The parity checks between openapi.yaml and the code: the route table, the
// error codes, the event types and the runtime config keys. Response bodies
// are checked against the spec by every handler test (requestJSON, serve and
// newHTTPServer in helpers_test.go).

// The spec is OpenAPI 3.1, whose schemas are JSON Schema 2020-12: the dialect
// the openapitest checker implements.
func TestSpecIsOpenAPI31(t *testing.T) {
	var spec struct {
		OpenAPI string `yaml:"openapi"`
		Info    struct {
			Version string `yaml:"version"`
		} `yaml:"info"`
	}
	if err := yaml.Unmarshal(OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.OpenAPI != "3.1.0" || spec.Info.Version == "" {
		t.Fatalf("openapi %q, info.version %q; want 3.1.0 and a contract version", spec.OpenAPI, spec.Info.Version)
	}
}

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// specOperations returns every "METHOD /path" of openapi.yaml, with path
// parameters reduced to {} so a route's {id} matches the spec's {job_id}.
func specOperations(t *testing.T) []string {
	t.Helper()
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	var ops []string
	for path, item := range spec.Paths {
		for method := range item {
			switch method {
			case "get", "put", "post", "delete", "patch", "head", "options":
				ops = append(ops, strings.ToUpper(method)+" "+pathParam.ReplaceAllString(path, "{}"))
			}
		}
	}
	slices.Sort(ops)
	return ops
}

// The route table and openapi.yaml describe the same operations, in both
// directions. Routes outside /api/v1/ (the console) are not API operations.
func TestRoutesMatchSpec(t *testing.T) {
	var table []string
	for _, rt := range newServer(t, Deps{}).routes() {
		if strings.HasPrefix(rt.pattern, "/api/v1/") {
			table = append(table, rt.method+" "+pathParam.ReplaceAllString(rt.pattern, "{}"))
		}
	}
	slices.Sort(table)
	spec := specOperations(t)
	for _, op := range table {
		if _, found := slices.BinarySearch(spec, op); !found {
			t.Errorf("route %s is not in openapi.yaml", op)
		}
	}
	for _, op := range spec {
		if _, found := slices.BinarySearch(table, op); !found {
			t.Errorf("openapi.yaml operation %s has no route", op)
		}
	}
}

// specEnum returns components.schemas.<name>.enum of openapi.yaml.
func specEnum(t *testing.T, name string) []string {
	t.Helper()
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Enum []string `yaml:"enum"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	enum := slices.Clone(spec.Components.Schemas[name].Enum)
	if len(enum) == 0 {
		t.Fatalf("openapi.yaml has no %s enum", name)
	}
	slices.Sort(enum)
	return enum
}

// typedStringConsts parses a Go file and returns the values of the string
// constants declared with the named type, or of the untyped ones when
// typeName is empty.
func typedStringConsts(t *testing.T, file, typeName string) []string {
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
			if typeName == "" {
				if vs.Type != nil {
					continue
				}
			} else if ident, ok := vs.Type.(*ast.Ident); !ok || ident.Name != typeName {
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

// The ErrorCode constants in errors.go and the ErrorCode enum in openapi.yaml
// are the same set, so a client mirroring the spec can switch on every code
// the server sends.
func TestErrorCodesMatchSpec(t *testing.T) {
	got := typedStringConsts(t, "errors.go", "ErrorCode")
	want := specEnum(t, "ErrorCode")
	if !slices.Equal(got, want) {
		t.Fatalf("errors.go ErrorCode constants = %v\nopenapi.yaml ErrorCode enum = %v", got, want)
	}
}

var requestErrorCode = regexp.MustCompile(`(?s)RequestError\{[^}]*?Code:\s*"([a-z_]+)"`)

// The SubmitErrorCode enum lists exactly the per-URL codes the server
// produces: the job manager's domain.SubmitCode* constants and every code the
// request validator in internal/media puts in a jobs.RequestError.
func TestSubmitErrorCodesMatchProducers(t *testing.T) {
	produced := typedStringConsts(t, filepath.Join("..", "domain", "submit.go"), "")
	sources, err := filepath.Glob(filepath.Join("..", "media", "*.go"))
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
	want := specEnum(t, "SubmitErrorCode")
	if !slices.Equal(produced, want) {
		t.Fatalf("produced submit codes = %v\nopenapi.yaml SubmitErrorCode enum = %v", produced, want)
	}
}

// The RuntimeConfig schema in openapi.yaml has a property for exactly the
// runtime config keys, which GET /api/v1/config returns (config.MutableView,
// derived from the config package's runtime classification). The console's
// Settings page is checked against this schema
// (TestSettingsFieldsCoverRuntimeConfig in apps/console), so a runtime key
// missing here would silently get no setting.
func TestRuntimeConfigSchemaMatchesRuntimeKeys(t *testing.T) {
	type schema struct {
		Properties map[string]schema `yaml:"properties"`
	}
	var spec struct {
		Components struct {
			Schemas map[string]schema `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	var documented []string
	for section, body := range spec.Components.Schemas["RuntimeConfig"].Properties {
		for leaf := range body.Properties {
			documented = append(documented, section+"."+leaf)
		}
	}
	view, err := config.MutableView(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	var runtime []string
	for section, body := range view {
		leaves, ok := body.(map[string]any)
		if !ok {
			t.Fatalf("runtime config section %s is %T, want a map of keys", section, body)
		}
		for leaf := range leaves {
			runtime = append(runtime, section+"."+leaf)
		}
	}
	if len(documented) < 30 || len(runtime) < 30 {
		t.Fatalf("found %d RuntimeConfig properties and %d runtime keys; one of the walks is broken", len(documented), len(runtime))
	}
	slices.Sort(documented)
	slices.Sort(runtime)
	for _, key := range runtime {
		if _, found := slices.BinarySearch(documented, key); !found {
			t.Errorf("runtime config key %s has no RuntimeConfig property in openapi.yaml", key)
		}
	}
	for _, key := range documented {
		if _, found := slices.BinarySearch(runtime, key); !found {
			t.Errorf("openapi.yaml RuntimeConfig property %s is not a runtime config key", key)
		}
	}
}

// RuntimeConfig marks exactly the restart keys with x-applies: restart, so a
// client can tell which saved values wait for a restart before it saves.
func TestRuntimeConfigMarksRestartKeys(t *testing.T) {
	type property struct {
		Applies    string              `yaml:"x-applies"`
		Properties map[string]property `yaml:"properties"`
	}
	var spec struct {
		Components struct {
			Schemas map[string]property `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	marked := 0
	for section, body := range spec.Components.Schemas["RuntimeConfig"].Properties {
		for leaf, prop := range body.Properties {
			key := section + "." + leaf
			if want := config.AppliesOnRestart(key); (prop.Applies == "restart") != want {
				t.Errorf("%s: x-applies %q, restart key %v", key, prop.Applies, want)
			}
			if prop.Applies == "restart" {
				marked++
			}
		}
	}
	if marked != 10 {
		t.Fatalf("%d keys marked x-applies: restart, want 10", marked)
	}
}

// The EventType enum in openapi.yaml and the domain constants list the same
// event types, so a client mirroring the spec never misses one the backend
// emits and never handles one it cannot.
func TestEventTypeEnumMatchesDomain(t *testing.T) {
	var spec struct {
		Components struct {
			Schemas struct {
				EventType struct {
					Enum []string `yaml:"enum"`
				} `yaml:"EventType"`
				Event struct {
					Properties struct {
						Payload struct {
							Type string `yaml:"type"`
						} `yaml:"payload"`
					} `yaml:"properties"`
				} `yaml:"Event"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, ty := range domain.EventTypes {
		want = append(want, string(ty))
	}
	got := spec.Components.Schemas.EventType.Enum
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("openapi EventType enum = %v\ndomain.EventTypes      = %v", got, want)
	}
	if spec.Components.Schemas.Event.Properties.Payload.Type != "object" {
		t.Fatalf("Event.payload type = %q, want object", spec.Components.Schemas.Event.Properties.Payload.Type)
	}
}

// The Events section of docs/api.md lists every event type. This polices the
// docs, not the server; it belongs in tools/repocheck.
func TestDocsListEveryEventType(t *testing.T) {
	var want []string
	for _, ty := range domain.EventTypes {
		want = append(want, string(ty))
	}
	doc, err := os.ReadFile("../../docs/api.md")
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
