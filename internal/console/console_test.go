package console

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// wantCSP is written out independently of ContentSecurityPolicy so a change
// to the policy has to be made in two places on purpose.
const wantCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' https: data:; " +
	"media-src 'self' https: blob:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// vendorFile and vendorSHA256 pin the vendored Preact+htm bundle; see AGENTS.md.
const (
	vendorFile   = "vendor/htm-preact-standalone-3.1.1.mjs"
	vendorSHA256 = "72284e8e9079c87817145df1110f74e8a2aa040b2fc384922e18dfcb46fc1fd7"
)

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
	if !strings.Contains(rec.Body.String(), `<html lang="en">`) {
		t.Errorf("index.html does not declare lang=\"en\"")
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
	first := get(t, mounted(), http.MethodGet, "/console/css/app.css", nil)
	etag := first.Result().Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on first response")
	}
	rec := get(t, mounted(), http.MethodGet, "/console/css/app.css", map[string]string{"If-None-Match": etag})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match with the current ETag = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 carried a %d-byte body", rec.Body.Len())
	}
	assertSecurityHeaders(t, rec)

	stale := get(t, mounted(), http.MethodGet, "/console/css/app.css", map[string]string{"If-None-Match": `"0000"`})
	if stale.Code != http.StatusOK {
		t.Errorf("If-None-Match with another ETag = %d, want 200", stale.Code)
	}
}

func TestETagFollowsContent(t *testing.T) {
	a := get(t, mounted(), http.MethodGet, "/console/css/app.css", nil).Result().Header.Get("ETag")
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
		{"/console/css/app.css", "text/css; charset=utf-8"},
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

func TestVendorChecksum(t *testing.T) {
	sum := sha256.Sum256([]byte(readStatic(t, vendorFile)))
	if got := hex.EncodeToString(sum[:]); got != vendorSHA256 {
		t.Fatalf("%s sha256 = %s, want %s; the vendored file must stay byte-identical to upstream (see AGENTS.md)", vendorFile, got, vendorSHA256)
	}
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

var (
	attrURL    = regexp.MustCompile(`\b(?:src|href)="([^"]*)"`)
	importSpec = regexp.MustCompile(`(?m)(?:^\s*import\s+(?:[^'"]*?\s+from\s+)?|^\s*export\s+[^'"]*?\s+from\s+|\bimport\s*\(\s*)['"]([^'"]+)['"]`)
)

// TestIndexReferencesResolve checks that every relative src/href in
// index.html names an embedded file, so the page works under /console/.
func TestIndexReferencesResolve(t *testing.T) {
	files := staticFS(t)
	matches := attrURL.FindAllStringSubmatch(readStatic(t, "index.html"), -1)
	checked := 0
	for _, m := range matches {
		ref := m[1]
		if ref == "" || strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "../") || strings.Contains(ref, "://") || strings.HasPrefix(ref, "#") {
			continue
		}
		if strings.HasPrefix(ref, "/") {
			t.Errorf("index.html references %q with an absolute path; use a relative one so the console works under any prefix", ref)
			continue
		}
		if _, err := fs.Stat(files, ref); err != nil {
			t.Errorf("index.html references %q, which is not embedded", ref)
		}
		checked++
	}
	if checked < 3 {
		t.Fatalf("checked only %d references in index.html; the attribute pattern is broken", checked)
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

// TestModuleImportsResolve checks that every import in static/js is a
// relative specifier naming an embedded file: there is no bundler and no
// import map, so a bare or broken specifier fails in the browser.
func TestModuleImportsResolve(t *testing.T) {
	files := staticFS(t)
	total := 0
	for name, src := range jsFiles(t) {
		for _, m := range importSpec.FindAllStringSubmatch(src, -1) {
			spec := m[1]
			total++
			if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
				t.Errorf("%s imports %q; only relative specifiers resolve without a bundler", name, spec)
				continue
			}
			target := path.Clean(path.Join(path.Dir(name), spec))
			if _, err := fs.Stat(files, target); err != nil {
				t.Errorf("%s imports %q, which resolves to %s and is not embedded", name, spec, target)
			}
		}
	}
	if total < 20 {
		t.Fatalf("found only %d imports; the import pattern is broken", total)
	}
}

var (
	colorLiteral    = regexp.MustCompile(`(?i)#[0-9a-f]{3}(?:[0-9a-f]{1,5})?\b|\b(?:rgba?|hsla?|hwb|oklch|oklab|lab|lch)\(`)
	transitionAll   = regexp.MustCompile(`(?i)transition(?:-property)?\s*:\s*all\b`)
	fontSizeDecl    = regexp.MustCompile(`font-size:\s*([^;]+);`)
	innerHTMLWrites = regexp.MustCompile(`\.(?:inner|outer)HTML\s*=|insertAdjacentHTML|dangerouslySetInnerHTML|document\.write`)
)

func hasCJK(s string) (rune, bool) {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return r, true
		}
	}
	return 0, false
}

// designViolations returns what the design guard rejects in one file:
// color literals outside tokens.css, "transition: all", CJK characters in
// UI copy, raw HTML writes, and font sizes that bypass the type scale.
func designViolations(name, text string) []string {
	var out []string
	if name != "css/tokens.css" {
		for _, m := range colorLiteral.FindAllString(text, -1) {
			out = append(out, "color literal "+m+" (colors live in css/tokens.css)")
		}
	}
	if m := transitionAll.FindString(text); m != "" {
		out = append(out, `"`+m+`" (name the properties that transition)`)
	}
	if r, ok := hasCJK(text); ok {
		out = append(out, "CJK character "+string(r)+" (UI copy is English; Apple data arrives at runtime)")
	}
	if path.Ext(name) == ".js" {
		if m := innerHTMLWrites.FindString(text); m != "" {
			out = append(out, m+" (render through htm, which escapes data)")
		}
	}
	if name == "css/app.css" {
		for _, m := range fontSizeDecl.FindAllStringSubmatch(text, -1) {
			if !strings.HasPrefix(strings.TrimSpace(m[1]), "var(--fs-") {
				out = append(out, "font-size: "+m[1]+" (use a --fs-* token)")
			}
		}
	}
	return out
}

func TestDesignGuard(t *testing.T) {
	scanned := 0
	err := fs.WalkDir(staticFS(t), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(name, vendorDir) {
			return err
		}
		scanned++
		for _, v := range designViolations(name, readStatic(t, name)) {
			t.Errorf("static/%s: %s", name, v)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 10 {
		t.Fatalf("design guard scanned only %d files", scanned)
	}
}

func TestDesignGuardCatchesViolations(t *testing.T) {
	cases := []struct {
		name, file, text string
	}{
		{"hex color", "css/app.css", "a { color: #e35e38; }"},
		{"short hex", "js/x.js", "const c = '#fff';"},
		{"rgb", "css/app.css", "a { color: rgb(0 0 0); }"},
		{"oklch", "css/app.css", "a { color: oklch(0.5 0.1 20); }"},
		{"transition all", "css/app.css", "a { transition: all 200ms; }"},
		{"cjk", "js/x.js", "const label = '下载';"},
		{"innerHTML", "js/x.js", "el.innerHTML = name;"},
		{"raw font size", "css/app.css", "a { font-size: 13px; }"},
	}
	for _, c := range cases {
		if len(designViolations(c.file, c.text)) == 0 {
			t.Errorf("%s: guard accepted %q", c.name, c.text)
		}
	}
	clean := []struct{ file, text string }{
		{"css/tokens.css", "--x: light-dark(#ffffff, #111113);"},
		{"js/x.js", "location.hash = '#/queue'; const bg = `#${hex}`;"},
		{"css/app.css", "a { font-size: var(--fs-12); transition: opacity 120ms; }"},
	}
	for _, c := range clean {
		if v := designViolations(c.file, c.text); len(v) != 0 {
			t.Errorf("guard rejected clean %s %q: %v", c.file, c.text, v)
		}
	}
}

func TestIndexHasNoInlineCode(t *testing.T) {
	index := readStatic(t, "index.html")
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`<script(?:\s[^>]*)?>\s*[^<\s]`),
		regexp.MustCompile(`<style`),
		regexp.MustCompile(`\sstyle="`),
		regexp.MustCompile(`\son[a-z]+="`),
	} {
		if m := re.FindString(index); m != "" {
			t.Errorf("index.html contains %q; the CSP blocks inline scripts, styles and handlers", m)
		}
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
	spec, err := os.ReadFile("../api/openapi.yaml")
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

var (
	componentTag  = regexp.MustCompile(`"component",\s*"([a-z_]+)"`)
	logComponents = regexp.MustCompile(`export const LOG_COMPONENTS = \[([^\]]*)\]`)
	quotedWord    = regexp.MustCompile(`'([a-z_]+)'`)
)

// The Logs page offers a component filter; a tag the backend logs with but the
// list lacks can only be reached by editing the URL.
func TestLogComponentsMatchBackend(t *testing.T) {
	backend := map[string]bool{}
	for _, root := range []string{"../../cmd", "../../internal"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, m := range componentTag.FindAllStringSubmatch(string(src), -1) {
				backend[m[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(backend) < 5 {
		t.Fatalf("found only %d component tags in the Go code; the pattern is broken", len(backend))
	}
	src, err := fs.ReadFile(embedded, "static/js/lib/route.js")
	if err != nil {
		t.Fatal(err)
	}
	list := logComponents.FindStringSubmatch(string(src))
	if list == nil {
		t.Fatal("LOG_COMPONENTS not found in lib/route.js")
	}
	console := map[string]bool{}
	for _, m := range quotedWord.FindAllStringSubmatch(list[1], -1) {
		console[m[1]] = true
	}
	for _, name := range slices.Sorted(maps.Keys(backend)) {
		if !console[name] {
			t.Errorf("the backend logs component %q, but LOG_COMPONENTS in lib/route.js lacks it", name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(console)) {
		if !backend[name] {
			t.Errorf("LOG_COMPONENTS lists %q, which no Go code logs as a component", name)
		}
	}
}
