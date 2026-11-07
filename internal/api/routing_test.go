package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnknownAPIPathIsRouteNotFound(t *testing.T) {
	routes := newServer(t, Deps{}).Routes()
	// The last two were served for other clients and are gone on purpose.
	for _, path := range []string{"/api/v1/nope", "/api/nope", "/api/v1/health/extra", "/api/v1/downloads/job1/nope", "/api/openapi.yaml", "/api/v1/developer-token"} {
		recorder := requestJSON(t, routes, http.MethodGet, path, "")
		assertErrorCode(t, recorder, http.StatusNotFound, "route_not_found")
	}
	if recorder := requestJSON(t, routes, http.MethodGet, "/docs", ""); recorder.Code != http.StatusNotFound {
		t.Fatalf("GET /docs = %d, want 404", recorder.Code)
	}
}

// A GET route answers HEAD with the GET's status and headers and no body, as
// the Allow headers below promise.
func TestGetRoutesAnswerHead(t *testing.T) {
	ts := newHTTPServer(t, newServer(t, Deps{}).Routes())
	for _, path := range []string{"/api/v1/health", "/api/v1/downloads", "/api/v1/config"} {
		get := requestJSON(t, ts.Config.Handler, http.MethodGet, path, "")
		head, err := ts.Client().Head(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(head.Body)
		head.Body.Close()
		if head.StatusCode != get.Code || head.Header.Get("Content-Type") != get.Header().Get("Content-Type") || len(body) != 0 {
			t.Errorf("HEAD %s = %d %q with %d body bytes; GET = %d %q", path, head.StatusCode, head.Header.Get("Content-Type"), len(body), get.Code, get.Header().Get("Content-Type"))
		}
	}
}

// A path that exists with other methods answers 405 with an Allow header that
// lists them (GET implies HEAD), in the error envelope rather than
// http.ServeMux's plain text.
func TestWrongMethodIsMethodNotAllowed(t *testing.T) {
	routes := newServer(t, Deps{}).Routes()
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
	routes := newServer(t, Deps{}).Routes()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/downloads", nil)
	req.Header.Set("Origin", "https://web.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	recorder := serve(t, routes, req)
	assertErrorCode(t, recorder, http.StatusMethodNotAllowed, "method_not_allowed")
	for name := range recorder.Header() {
		if strings.HasPrefix(strings.ToLower(name), "access-control-") {
			t.Errorf("preflight answered with %s", name)
		}
	}
}

// The console handler is mounted with the /console prefix stripped, and the
// bare root redirects to it. The real handler is wired in by amdl serve (apps/amdl).
func TestConsoleMount(t *testing.T) {
	var gotPath string
	stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { gotPath = r.URL.Path })
	routes := newServer(t, Deps{Console: stub}).Routes()

	recorder := requestJSON(t, routes, http.MethodGet, "/console/js/app.js", "")
	if recorder.Code != http.StatusOK || gotPath != "/js/app.js" {
		t.Fatalf("GET /console/js/app.js = %d reaching %q, want the console handler at /js/app.js", recorder.Code, gotPath)
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

// Without a console the server is the API alone: / and /console/ are unknown.
func TestNoConsole(t *testing.T) {
	routes := newServer(t, Deps{}).Routes()
	for _, p := range []string{"/", "/console/"} {
		if recorder := requestJSON(t, routes, http.MethodGet, p, ""); recorder.Code != http.StatusNotFound {
			t.Errorf("GET %s without a console = %d, want 404", p, recorder.Code)
		}
	}
}

// A process missing a required service fails at startup instead of serving
// 503s from the routes that need it.
func TestNewRequiresEveryService(t *testing.T) {
	if _, err := New(Deps{}); err == nil || !strings.Contains(err.Error(), "Deps.Config") {
		t.Fatalf("New(Deps{}) error = %v, want Deps.Config named", err)
	}
	deps := fillDeps(t, Deps{})
	catalog := deps.Catalog
	deps.Catalog = nil
	if _, err := New(deps); err == nil || !strings.Contains(err.Error(), "Deps.Catalog") {
		t.Fatalf("New without Catalog error = %v, want Deps.Catalog named", err)
	}
	deps.Catalog = catalog
	if _, err := New(deps); err != nil {
		t.Fatalf("New with every service: %v", err)
	}
}
