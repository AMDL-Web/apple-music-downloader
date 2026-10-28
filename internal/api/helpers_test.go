package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/jobs"
	"amdl/internal/media"
	"amdl/internal/wrapper"
)

type fakeWrapperService struct {
	statusCalls  int
	statusResult wrapper.Status
	statusErr    error
	startResult  wrapper.LoginResult
	startErr     error
	verifyResult wrapper.LoginResult
	verifyErr    error
	logoutErr    error
	username     string
	password     string
	loginID      string
	twoStepCode  string
}

func (f *fakeWrapperService) Status(context.Context) (wrapper.Status, error) {
	f.statusCalls++
	return f.statusResult, f.statusErr
}

func (f *fakeWrapperService) StartLogin(_ context.Context, username, password string) (wrapper.LoginResult, error) {
	f.username, f.password = username, password
	return f.startResult, f.startErr
}

func (f *fakeWrapperService) SubmitTwoStepCode(_ context.Context, loginID, code string) (wrapper.LoginResult, error) {
	f.loginID, f.twoStepCode = loginID, code
	return f.verifyResult, f.verifyErr
}

func (f *fakeWrapperService) Logout(_ context.Context, username string) error {
	f.username = username
	return f.logoutErr
}

type fakeQualityService struct {
	result media.QualityResult
	err    error
	req    media.QualityRequest
}

func (f *fakeQualityService) QueryQuality(_ context.Context, req media.QualityRequest) (media.QualityResult, error) {
	f.req = req
	return f.result, f.err
}

func requestJSON(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

// stubProcessor parses "type|storefront|id" test URLs into a ValidationResult
// so createDownload can be exercised against a real jobs.Manager without
// hitting Apple Music.
type stubProcessor struct{}

func (stubProcessor) ValidateRequest(_ context.Context, url string) (jobs.ValidationResult, error) {
	parts := strings.SplitN(url, "|", 3)
	if len(parts) != 3 {
		return jobs.ValidationResult{}, &jobs.RequestError{Code: "invalid_url", Message: "malformed test url"}
	}
	return jobs.ValidationResult{Type: parts[0], Storefront: parts[1], ID: parts[2]}, nil
}

func (stubProcessor) ProcessJob(context.Context, domain.Job, jobs.Reporter) error { return nil }

func newTestServerWithManager(t *testing.T) *Server {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager := jobs.NewManager(store, events.NewHub(), stubProcessor{}, 1, slog.Default())
	return &Server{store: store, manager: manager}
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
