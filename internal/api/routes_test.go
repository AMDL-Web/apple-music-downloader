package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"amdl/internal/config"
	"amdl/internal/console"

	"gopkg.in/yaml.v3"
)

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// specOperations returns every "METHOD /path" of openapi.yaml, with path
// parameters reduced to {} so a route's {id} matches the spec's {job_id}.
func specOperations(t *testing.T) []string {
	t.Helper()
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(openAPISpec, &spec); err != nil {
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
	for _, rt := range (&Server{}).routes() {
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
	if err := yaml.Unmarshal(openAPISpec, &spec); err != nil {
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
// (TestSettingsFieldsCoverRuntimeConfig in internal/console), so a runtime key
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
	if err := yaml.Unmarshal(openAPISpec, &spec); err != nil {
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

func TestUnknownAPIPathIsRouteNotFound(t *testing.T) {
	routes := (&Server{}).Routes()
	// The last two were served for other clients and are gone on purpose.
	for _, path := range []string{"/api/v1/nope", "/api/nope", "/api/v1/health/extra", "/api/v1/downloads/job1/nope", "/api/openapi.yaml", "/api/v1/developer-token"} {
		recorder := requestJSON(t, routes, http.MethodGet, path, "")
		assertErrorCode(t, recorder, http.StatusNotFound, "route_not_found")
	}
	if recorder := requestJSON(t, routes, http.MethodGet, "/docs", ""); recorder.Code != http.StatusNotFound {
		t.Fatalf("GET /docs = %d, want 404", recorder.Code)
	}
}

// A path that exists with other methods answers 405 with an Allow header that
// lists them (GET implies HEAD), in the error envelope rather than
// http.ServeMux's plain text.
func TestWrongMethodIsMethodNotAllowed(t *testing.T) {
	routes := (&Server{}).Routes()
	tests := []struct {
		method, path, allow string
	}{
		{http.MethodPost, "/api/v1/health", "GET, HEAD"},
		{http.MethodPut, "/api/v1/downloads", "GET, HEAD, POST"},
		{http.MethodPost, "/api/v1/downloads/job1", "DELETE, GET, HEAD"},
		{http.MethodDelete, "/api/v1/config", "GET, HEAD, PUT"},
		{http.MethodGet, "/api/v1/config/reload", "POST"},
		// "events" also matches /downloads/{job_id}, which a DELETE would reach.
		{http.MethodPut, "/api/v1/downloads/events", "DELETE, GET, HEAD"},
	}
	for _, tt := range tests {
		recorder := requestJSON(t, routes, tt.method, tt.path, "")
		assertErrorCode(t, recorder, http.StatusMethodNotAllowed, "method_not_allowed")
		if got := recorder.Header().Get("Allow"); got != tt.allow {
			t.Errorf("%s %s Allow = %q, want %q", tt.method, tt.path, got, tt.allow)
		}
	}
}

// The only client is the same-origin console: a cross-origin preflight gets
// the ordinary 405 and no response carries CORS headers.
func TestNoCrossOriginAccess(t *testing.T) {
	routes := (&Server{}).Routes()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/downloads", nil)
	req.Header.Set("Origin", "https://web.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	recorder := httptest.NewRecorder()
	routes.ServeHTTP(recorder, req)
	assertErrorCode(t, recorder, http.StatusMethodNotAllowed, "method_not_allowed")
	for name := range recorder.Header() {
		if strings.HasPrefix(strings.ToLower(name), "access-control-") {
			t.Errorf("preflight answered with %s", name)
		}
	}
}

func TestConsoleMount(t *testing.T) {
	routes := (&Server{}).Routes()

	recorder := requestJSON(t, routes, http.MethodGet, "/console/", "")
	if recorder.Code != http.StatusOK || !strings.Contains(strings.ToLower(recorder.Body.String()), "<!doctype html>") {
		t.Fatalf("GET /console/ = %d, want the console index", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Security-Policy"); got != console.ContentSecurityPolicy {
		t.Fatalf("GET /console/ CSP = %q, want the console policy", got)
	}

	recorder = requestJSON(t, routes, http.MethodGet, "/", "")
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/console/" {
		t.Fatalf("GET / = %d Location %q, want 302 to /console/", recorder.Code, recorder.Header().Get("Location"))
	}

	recorder = requestJSON(t, routes, http.MethodGet, "/api/v1/health", "")
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Security-Policy") != "" {
		t.Fatalf("GET /api/v1/health = %d CSP %q, want 200 without the console policy", recorder.Code, recorder.Header().Get("Content-Security-Policy"))
	}

	recorder = requestJSON(t, routes, http.MethodGet, "/api/v1/nope", "")
	assertErrorCode(t, recorder, http.StatusNotFound, "route_not_found")
}
