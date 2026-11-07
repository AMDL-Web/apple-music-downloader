package console

import (
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"amdl/internal/client"
)

// wantCSP is written out independently of ContentSecurityPolicy so a change
// to the policy has to be made in two places on purpose.
const wantCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' https: data:; " +
	"media-src 'self' https: blob:; connect-src 'self' https://mvod.itunes.apple.com; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// vendorFile is the vendored Preact+htm bundle; tools/repocheck pins its bytes.
const vendorFile = "vendor/htm-preact-standalone-3.1.1.mjs"

// mounted serves the console the way the API router mounts it.
func mounted() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /console/", http.StripPrefix("/console", Handler()))
	return mux
}

func get(t *testing.T, h http.Handler, method, target string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func assertSecurityHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	h := rec.Result().Header
	if got := h.Get("Content-Security-Policy"); got != wantCSP {
		t.Errorf("Content-Security-Policy = %q, want %q", got, wantCSP)
	}
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := h.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
}

func staticFS(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(embedded, "static")
	if err != nil {
		t.Fatalf("open embedded static: %v", err)
	}
	return sub
}

func readStatic(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(staticFS(t), name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func TestIndexServedWithSecurityHeaders(t *testing.T) {
	rec := get(t, mounted(), http.MethodGet, "/console/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /console/ = %d, want 200", rec.Code)
	}
	assertSecurityHeaders(t, rec)
	h := rec.Result().Header
	if got := h.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := h.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if etag := h.Get("ETag"); !regexp.MustCompile(`^"[0-9a-f]{32}"$`).MatchString(etag) {
		t.Errorf("ETag = %q, want a quoted strong hash", etag)
	}
	if !regexp.MustCompile(`<html lang="en"[ >]`).MatchString(rec.Body.String()) {
		t.Errorf("index.html does not declare lang=\"en\"")
	}
	// The default skin is static so the first frame, before main.js runs, has every skin token.
	if !strings.Contains(rec.Body.String(), `data-skin="music"`) {
		t.Errorf("index.html does not set the default data-skin=\"music\" on <html>")
	}
}

func TestIndexAtExplicitPath(t *testing.T) {
	a := get(t, mounted(), http.MethodGet, "/console/", nil)
	b := get(t, mounted(), http.MethodGet, "/console/index.html", nil)
	if b.Code != http.StatusOK || a.Body.String() != b.Body.String() {
		t.Fatalf("/console/index.html = %d, want the same page as /console/", b.Code)
	}
}

func TestConditionalGetReturns304(t *testing.T) {
	first := get(t, mounted(), http.MethodGet, "/console/css/base.css", nil)
	etag := first.Result().Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on first response")
	}
	rec := get(t, mounted(), http.MethodGet, "/console/css/base.css", map[string]string{"If-None-Match": etag})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match with the current ETag = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 carried a %d-byte body", rec.Body.Len())
	}
	assertSecurityHeaders(t, rec)

	stale := get(t, mounted(), http.MethodGet, "/console/css/base.css", map[string]string{"If-None-Match": `"0000"`})
	if stale.Code != http.StatusOK {
		t.Errorf("If-None-Match with another ETag = %d, want 200", stale.Code)
	}
}

func TestETagFollowsContent(t *testing.T) {
	a := get(t, mounted(), http.MethodGet, "/console/css/base.css", nil).Result().Header.Get("ETag")
	b := get(t, mounted(), http.MethodGet, "/console/css/tokens.css", nil).Result().Header.Get("ETag")
	if a == b {
		t.Errorf("two different files share ETag %s", a)
	}
}

func TestCacheControl(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/console/" + vendorFile, "public, max-age=31536000, immutable"},
		{"/console/js/main.js", "no-cache"},
		{"/console/css/tokens.css", "no-cache"},
		{"/console/icons.svg", "no-cache"},
	}
	for _, c := range cases {
		rec := get(t, mounted(), http.MethodGet, c.path, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d", c.path, rec.Code)
			continue
		}
		if got := rec.Result().Header.Get("Cache-Control"); got != c.want {
			t.Errorf("GET %s Cache-Control = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestContentTypes(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/console/js/main.js", "text/javascript; charset=utf-8"},
		{"/console/" + vendorFile, "text/javascript; charset=utf-8"},
		{"/console/icons.svg", "image/svg+xml"},
		{"/console/css/base.css", "text/css; charset=utf-8"},
	}
	for _, c := range cases {
		rec := get(t, mounted(), http.MethodGet, c.path, nil)
		if got := rec.Result().Header.Get("Content-Type"); got != c.want {
			t.Errorf("GET %s Content-Type = %q, want %q", c.path, got, c.want)
		}
		assertSecurityHeaders(t, rec)
	}
}

func TestMissingFilesAndDirectories(t *testing.T) {
	for _, p := range []string{"/nope.js", "/css/", "/css", "/js", "/../console.go", "/static/index.html", "/js/../../console.go"} {
		rec := get(t, Handler(), http.MethodGet, p, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
		assertSecurityHeaders(t, rec)
	}
}

func TestHeadAndMethods(t *testing.T) {
	head := get(t, Handler(), http.MethodHead, "/", nil)
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Errorf("HEAD / = %d with %d body bytes, want 200 and none", head.Code, head.Body.Len())
	}
	post := get(t, Handler(), http.MethodPost, "/", nil)
	if post.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST / = %d, want 405", post.Code)
	}
	assertSecurityHeaders(t, post)
}

func TestEmbeddedFilesHaveKnownTypes(t *testing.T) {
	err := fs.WalkDir(staticFS(t), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if _, ok := contentTypes[path.Ext(name)]; !ok {
			t.Errorf("embedded %s has no known content type; stray files (.DS_Store, editor backups) ship in the binary", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func jsFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(staticFS(t), "js", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if ext := path.Ext(name); ext == ".js" || ext == ".mjs" {
			out[name] = readStatic(t, name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 5 {
		t.Fatalf("found only %d JS files under static/js", len(out))
	}
	return out
}

// TestSegmentedKeepsItsWidthWhenPressed guards the cause of buttons that moved
// their neighbors: the pressed segment is semibold, so every label carries its
// semibold self hidden in the same grid cell, and pressing one changes no width.
// It was measured in Chrome as 1.6 px of shift on the queue tabs.
func TestSegmentedKeepsItsWidthWhenPressed(t *testing.T) {
	if js := readStatic(t, "js/ui/components.js"); !strings.Contains(js, `class="seg-label" data-label=${o.label}`) {
		t.Errorf("Segmented labels do not carry data-label, so the semibold width is not reserved")
	}
	css := readStatic(t, "css/controls.css")
	rule := regexp.MustCompile(`(?s)\.seg-label::after\s*\{[^}]*content:\s*attr\(data-label\);[^}]*font-weight:\s*var\(--w-semibold\);[^}]*\}`)
	if !rule.MatchString(css) || !regexp.MustCompile(`\.seg-label\s*\{\s*display:\s*inline-grid;`).MatchString(css) {
		t.Errorf("css/controls.css does not reserve the semibold width of a segment label")
	}
}

var (
	specPath    = regexp.MustCompile(`(?m)^  (/api/[^\s:]+):\s*$`)
	clientPath  = regexp.MustCompile(`/api/v1/(?:[A-Za-z0-9_\-./]|\$\{[^}]*\})*`)
	templateArg = regexp.MustCompile(`\$\{[^}]*\}`)
	specParam   = regexp.MustCompile(`\{[^}]+\}`)
)

// TestAPIPathsExistInSpec is the contract guard: every /api/v1 path the
// console calls must be a path in internal/api/openapi.yaml, so a renamed
// or removed route fails here before it fails in a browser.
func TestAPIPathsExistInSpec(t *testing.T) {
	spec, err := os.ReadFile("../../internal/api/openapi.yaml")
	if err != nil {
		t.Fatalf("read the API contract: %v", err)
	}
	known := map[string]bool{}
	for _, m := range specPath.FindAllStringSubmatch(string(spec), -1) {
		known[specParam.ReplaceAllString(m[1], "{param}")] = true
	}
	if len(known) < 10 {
		t.Fatalf("parsed only %d paths from openapi.yaml; the path pattern is broken", len(known))
	}
	used := map[string][]string{}
	for name, src := range jsFiles(t) {
		for _, m := range clientPath.FindAllString(src, -1) {
			p := strings.TrimRight(templateArg.ReplaceAllString(m, "{param}"), "/.")
			used[p] = append(used[p], name)
		}
	}
	if len(used) < 12 {
		t.Fatalf("found only %d distinct /api/v1 paths in static/js; the guard would pass vacuously", len(used))
	}
	paths := make([]string, 0, len(used))
	for p := range used {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if !known[p] {
			files := used[p]
			sort.Strings(files)
			t.Errorf("static/js calls %s (in %s), which is not a path in internal/api/openapi.yaml", p, strings.Join(files, ", "))
		}
	}
}

// requestCall is a call of the console's request(method, path, ...) helper.
var requestCall = regexp.MustCompile("request\\(\\s*'([A-Z]+)',\\s*['`](/api/v1/(?:[A-Za-z0-9_\\-./]|\\$\\{[^}]*\\})*)['`]")

// Every request(method, path) the console makes is an operation of
// internal/api/openapi.yaml with that method, so a route that moved to another
// method fails here, not with a 405 in a browser.
func TestAPICallsAreSpecOperations(t *testing.T) {
	raw, err := os.ReadFile("../../internal/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	ops := map[string]bool{}
	for p, item := range spec.Paths {
		for method := range item {
			ops[strings.ToUpper(method)+" "+specParam.ReplaceAllString(p, "{param}")] = true
		}
	}
	calls := 0
	for name, src := range jsFiles(t) {
		for _, m := range requestCall.FindAllStringSubmatch(src, -1) {
			calls++
			op := m[1] + " " + strings.TrimRight(templateArg.ReplaceAllString(m[2], "{param}"), "/.")
			if !ops[op] {
				t.Errorf("%s calls %s, which is not an operation of internal/api/openapi.yaml", name, op)
			}
		}
	}
	if calls < 30 {
		t.Fatalf("found only %d request() calls in static/js; the pattern is broken and the guard would pass vacuously", calls)
	}
}

var (
	settingsFieldKey = regexp.MustCompile(`\bkey:\s*'([a-z_]+\.[a-z0-9_]+)'`)
	notEditedBlock   = regexp.MustCompile(`(?s)export const NOT_EDITED = \{(.*?)\};`)
	notEditedEntry   = regexp.MustCompile(`'([a-z_]+\.[a-z0-9_]+)'\s*:\s*'([^']*)'`)
)

// TestSettingsFieldsCoverRuntimeConfig keeps the Settings page complete in
// both directions: every leaf of the RuntimeConfig schema in
// internal/api/openapi.yaml (what GET /api/v1/config returns and PUT accepts)
// has a field in static/js/lib/settings.js or an entry with a reason in its
// NOT_EDITED list, and no field or entry names a key the API does not return.
// A new runtime key therefore fails here until the page can edit it or says
// why it does not.
func TestSettingsFieldsCoverRuntimeConfig(t *testing.T) {
	spec, err := os.ReadFile("../../internal/api/openapi.yaml")
	if err != nil {
		t.Fatalf("read the API contract: %v", err)
	}
	type schema struct {
		Type       string            `yaml:"type"`
		Properties map[string]schema `yaml:"properties"`
	}
	var doc struct {
		Components struct {
			Schemas map[string]schema `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(spec, &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	runtime, ok := doc.Components.Schemas["RuntimeConfig"]
	if !ok {
		t.Fatal("openapi.yaml has no RuntimeConfig schema")
	}
	api := map[string]bool{}
	for section, body := range runtime.Properties {
		if len(body.Properties) == 0 {
			t.Errorf("RuntimeConfig.%s has no properties; the settings guard expects section.key leaves", section)
		}
		for name, leaf := range body.Properties {
			if leaf.Type == "object" {
				t.Errorf("RuntimeConfig.%s.%s is an object; lib/settings.js addresses two-level keys only", section, name)
				continue
			}
			api[section+"."+name] = true
		}
	}
	if len(api) < 30 {
		t.Fatalf("RuntimeConfig has only %d leaves; the schema walk is broken", len(api))
	}

	src := readStatic(t, "js/lib/settings.js")
	fields := map[string]bool{}
	for _, m := range settingsFieldKey.FindAllStringSubmatch(src, -1) {
		if fields[m[1]] {
			t.Errorf("lib/settings.js has two fields for %s", m[1])
		}
		fields[m[1]] = true
	}
	if len(fields) < 30 {
		t.Fatalf("found only %d field keys in lib/settings.js; the key pattern is broken", len(fields))
	}
	block := notEditedBlock.FindStringSubmatch(src)
	if block == nil {
		t.Fatal("lib/settings.js has no `export const NOT_EDITED = {...};` list")
	}
	excluded := map[string]string{}
	for _, m := range notEditedEntry.FindAllStringSubmatch(block[1], -1) {
		excluded[m[1]] = m[2]
	}

	for _, key := range slices.Sorted(maps.Keys(api)) {
		_, skip := excluded[key]
		switch {
		case !fields[key] && !skip:
			t.Errorf("GET /api/v1/config returns %s, but lib/settings.js has no field for it and NOT_EDITED does not say why", key)
		case fields[key] && skip:
			t.Errorf("%s has a field in lib/settings.js and is also listed in NOT_EDITED", key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		if !api[key] {
			t.Errorf("lib/settings.js has a field for %s, which RuntimeConfig in openapi.yaml does not have", key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(excluded)) {
		if !api[key] {
			t.Errorf("NOT_EDITED lists %s, which RuntimeConfig in openapi.yaml does not have", key)
		}
		if strings.TrimSpace(excluded[key]) == "" {
			t.Errorf("NOT_EDITED lists %s without a reason", key)
		}
	}
}

func TestLoginWaitMatchesTheGoClient(t *testing.T) {
	m := regexp.MustCompile(`const LOGIN_TIMEOUT_MS = (\d+);`).FindStringSubmatch(readStatic(t, "js/api.js"))
	if m == nil {
		t.Fatal("LOGIN_TIMEOUT_MS not found in api.js")
	}
	ms, _ := strconv.Atoi(m[1]) // the pattern only matches digits
	if got := time.Duration(ms) * time.Millisecond; got != client.LoginWait {
		t.Fatalf("LOGIN_TIMEOUT_MS is %v, client.LoginWait %v: a login to a wrapper amdl runs outlasts the shorter one", got, client.LoginWait)
	}
}
