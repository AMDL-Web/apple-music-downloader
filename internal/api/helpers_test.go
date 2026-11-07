package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"amdl/internal/accounts"
	"amdl/internal/api/openapitest"
	"amdl/internal/applelibrary"
	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/events"
	"amdl/internal/jobs"
)

func requestJSON(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	return serve(t, handler, req)
}

// contract is openapi.yaml, which every response a test records is checked
// against: the operation, the status and the body's schema.
var contract = openapitest.MustLoad(OpenAPI)

// serve runs req through handler and checks the response against the spec.
func serve(t *testing.T, handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	contract.Check(t, req, recorder)
	return recorder
}

// newHTTPServer serves handler on a real local listener, checking every
// response against the spec.
func newHTTPServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(contract.Middleware(t, handler))
	t.Cleanup(ts.Close)
	return ts
}

// openStore opens a job store in a temporary directory, closed when the test
// ends.
func openStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// quietLogger drops every record; tests that check logs pass their own.
var quietLogger = slog.New(slog.DiscardHandler)

// newServer builds a Server through New, filling each dependency deps leaves
// nil (see fillDeps).
func newServer(t *testing.T, deps Deps) *Server {
	t.Helper()
	server, err := New(fillDeps(t, deps))
	if err != nil {
		t.Fatal(err)
	}
	return server
}

// fillDeps fills each required dependency deps leaves nil: the default
// config, a store in a temp dir, a hub, a manager over stubProcessor, and the
// fakes of fakes_test.go. Logs are dropped unless deps.Logger is set. A test
// that must reach a dependency the server builds (the manager, say) fills
// deps first and keeps its own reference.
func fillDeps(t *testing.T, deps Deps) Deps {
	t.Helper()
	if deps.Config == nil {
		deps.Config = config.NewStore(config.Default())
	}
	if deps.Store == nil {
		deps.Store = openStore(t)
	}
	if deps.Hub == nil {
		deps.Hub = events.NewHub()
	}
	if deps.Logger == nil {
		deps.Logger = quietLogger
	}
	if deps.Manager == nil {
		deps.Manager = jobs.NewManager(deps.Store, deps.Hub, stubProcessor{}, 1, quietLogger)
	}
	if deps.Wrapper == nil {
		deps.Wrapper = &fakeWrapperService{}
	}
	if deps.Quality == nil {
		deps.Quality = &fakeQualityService{}
	}
	if deps.LibrarySync == nil {
		deps.LibrarySync = &fakeLibrarySync{}
	}
	if deps.Catalog == nil {
		deps.Catalog = &fakeCatalog{t: t}
	}
	if deps.Accounts == nil {
		deps.Accounts = accounts.New(deps.Store, deps.Wrapper, deps.Tokens, deps.Config.Get, quietLogger)
	}
	if deps.AppleLibrary == nil {
		deps.AppleLibrary = applelibrary.NewSet(deps.Config, deps.Tokens, nil, deps.Store, deps.Accounts, quietLogger)
	}
	return deps
}

// assertErrorCode checks a response against the error envelope
// {"error": {"code", "message", "details"?}}: the status, the code, a
// non-empty message, and the JSON content type. It returns the details.
func assertErrorCode(t *testing.T, recorder *httptest.ResponseRecorder, status int, code ErrorCode) json.RawMessage {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, status, recorder.Body.String())
	}
	if ct := recorder.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q, want JSON (body %s)", ct, recorder.Body.String())
	}
	var body struct {
		Error *struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Error == nil {
		t.Fatalf("body %s is not the error envelope: %v", recorder.Body.String(), err)
	}
	if body.Error.Code != string(code) || body.Error.Message == "" {
		t.Fatalf("error = %+v, want code %q and a message", body.Error, code)
	}
	return body.Error.Details
}
