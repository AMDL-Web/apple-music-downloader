package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amdl/internal/config"
	"amdl/internal/usertoken"
	"amdl/internal/wrapper"
)

func TestWrapperStatusEndpoint(t *testing.T) {
	fake := &fakeWrapperService{statusResult: wrapper.Status{
		Ready: true, LoggedIn: true, Regions: []string{"us"},
		Accounts: []string{"user@example.com"},
	}}
	server := &Server{wrapper: fake}
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/wrapper/status", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	want := `{"ready":true,"logged_in":true,"regions":["us"],"accounts":["user@example.com"],"login_supported":false,"media_user_token":""}` + "\n"
	if recorder.Body.String() != want {
		t.Fatalf("body = %s, want %s", recorder.Body.String(), want)
	}
}

// GET /wrapper/status carries the wrapper's token masked as GET /config masks
// the configured one, never the value, and "" when amdl cannot read one.
func TestWrapperStatusMasksWrapperToken(t *testing.T) {
	token := strings.Repeat("A", 196) + "wxyz"
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, usertoken.FileName), []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withDataDir := config.Default()
	withDataDir.Wrapper.DataDir = dataDir
	withDataDir.Catalog.MediaUserToken = strings.Repeat("C", 200)
	for _, tt := range []struct {
		name   string
		cfg    config.Config
		tokens *usertoken.Provider
		want   string
	}{
		{"managed wrapper token", config.Default(), usertoken.New(func() (string, error) { return token, nil }), "...wxyz"},
		{"data_dir token", withDataDir, usertoken.New(nil), "...wxyz"},
		{"no wrapper token, config token only", withDataDir, usertoken.New(func() (string, error) { return "", nil }), ""},
		{"unreadable wrapper file", config.Default(), usertoken.New(func() (string, error) { return "", errors.New("io") }), ""},
		{"invalid wrapper file", config.Default(), usertoken.New(func() (string, error) { return "not a token", nil }), ""},
		{"no provider", withDataDir, nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := &Server{cfg: config.NewStore(tt.cfg), tokens: tt.tokens, wrapper: &fakeWrapperService{statusResult: wrapper.Status{Ready: true}}}
			recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/wrapper/status", "")
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			body := recorder.Body.String()
			if strings.Contains(body, strings.Repeat("A", 16)) || strings.Contains(body, strings.Repeat("C", 16)) {
				t.Fatal("the response carries a token value")
			}
			var got map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if v, ok := got["media_user_token"]; !ok || v != tt.want {
				t.Fatalf("media_user_token = %v (present %v), want %q", v, ok, tt.want)
			}
		})
	}
}

func TestWrapperStatusUnavailable(t *testing.T) {
	server := &Server{wrapper: &fakeWrapperService{statusErr: errors.New("offline")}}
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/wrapper/status", "")
	assertErrorCode(t, recorder, http.StatusServiceUnavailable, "wrapper_unavailable")
}

func TestWrapperLoginResponses(t *testing.T) {
	tests := []struct {
		name   string
		result wrapper.LoginResult
		err    error
		status int
		code   ErrorCode
	}{
		{name: "logged in", result: wrapper.LoginResult{Status: wrapper.LoginStatusLoggedIn}, status: http.StatusOK},
		{name: "needs two step", result: wrapper.LoginResult{Status: wrapper.LoginStatusNeedsTwoStep, LoginID: "login-1"}, status: http.StatusAccepted},
		{name: "authentication failed", err: wrapper.ErrAuthenticationFailed, status: http.StatusUnauthorized, code: "authentication_failed"},
		{name: "already logged in", err: wrapper.ErrAlreadyLoggedIn, status: http.StatusConflict, code: "already_logged_in"},
		{name: "login pending", err: wrapper.ErrLoginPending, status: http.StatusConflict, code: "login_pending"},
		{name: "timeout", err: wrapper.ErrLoginTimeout, status: http.StatusGatewayTimeout, code: "upstream_timeout"},
		{name: "unsupported", err: wrapper.ErrLoginUnsupported, status: http.StatusNotImplemented, code: "login_unsupported"},
		{name: "upstream", err: errors.New("connection refused"), status: http.StatusBadGateway, code: "upstream_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeWrapperService{startResult: tt.result, startErr: tt.err}
			server := &Server{wrapper: fake}
			recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/wrapper/login", `{"username":" user ","password":" p@ss "}`)
			if tt.code != "" {
				assertErrorCode(t, recorder, tt.status, tt.code)
			} else if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d, body = %s", recorder.Code, tt.status, recorder.Body.String())
			}
			// The username is trimmed; the password reaches the wrapper as
			// typed, because spaces can be part of it.
			if fake.username != "user" || fake.password != " p@ss " {
				t.Fatalf("credentials forwarded as %q %q, want %q %q", fake.username, fake.password, "user", " p@ss ")
			}
		})
	}
}

func TestWrapperLoginValidatesRequiredFields(t *testing.T) {
	tests := []struct {
		body string
		code ErrorCode
	}{
		{body: `{"username":"","password":"secret"}`, code: "invalid_request"},
		{body: `{"username":"user","password":" "}`, code: "invalid_request"},
		{body: `{"username":"user","password":""}`, code: "invalid_request"},
		{body: `{`, code: "invalid_json"},
	}
	for _, tt := range tests {
		fake := &fakeWrapperService{}
		recorder := requestJSON(t, (&Server{wrapper: fake}).Routes(), http.MethodPost, "/api/v1/wrapper/login", tt.body)
		assertErrorCode(t, recorder, http.StatusBadRequest, tt.code)
		if fake.username != "" || fake.password != "" {
			t.Fatalf("body %s reached the wrapper", tt.body)
		}
	}
}

func TestWrapperTwoStepResponses(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   ErrorCode
	}{
		{name: "success", status: http.StatusOK},
		{name: "failed", err: wrapper.ErrAuthenticationFailed, status: http.StatusUnauthorized, code: "authentication_failed"},
		{name: "not found", err: wrapper.ErrLoginSessionNotFound, status: http.StatusNotFound, code: "login_not_found"},
		{name: "busy", err: wrapper.ErrLoginSessionBusy, status: http.StatusConflict, code: "login_pending"},
		{name: "timeout", err: wrapper.ErrLoginTimeout, status: http.StatusGatewayTimeout, code: "upstream_timeout"},
		{name: "upstream", err: errors.New("stream failed"), status: http.StatusBadGateway, code: "upstream_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeWrapperService{verifyResult: wrapper.LoginResult{Status: wrapper.LoginStatusLoggedIn}, verifyErr: tt.err}
			server := &Server{wrapper: fake}
			recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/wrapper/login/login-1/2fa", `{"two_step_code":" 123456 "}`)
			if tt.code != "" {
				assertErrorCode(t, recorder, tt.status, tt.code)
			} else if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d, body = %s", recorder.Code, tt.status, recorder.Body.String())
			}
			if fake.loginID != "login-1" || fake.twoStepCode != "123456" {
				t.Fatalf("unexpected verification input: %q %q", fake.loginID, fake.twoStepCode)
			}
		})
	}
}

func TestWrapperTwoStepValidatesCode(t *testing.T) {
	server := &Server{wrapper: &fakeWrapperService{}}
	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/wrapper/login/login-1/2fa", `{"two_step_code":" "}`)
	assertErrorCode(t, recorder, http.StatusBadRequest, "invalid_request")
}

func TestWrapperLogoutResponses(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   ErrorCode
	}{
		{name: "success", status: http.StatusOK},
		{name: "not found", err: wrapper.ErrAccountNotFound, status: http.StatusNotFound, code: "account_not_found"},
		{name: "unsupported", err: wrapper.ErrLoginUnsupported, status: http.StatusNotImplemented, code: "login_unsupported"},
		{name: "upstream", err: errors.New("unavailable"), status: http.StatusBadGateway, code: "upstream_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeWrapperService{logoutErr: tt.err}
			server := &Server{wrapper: fake}
			recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/wrapper/logout", `{"username":" user "}`)
			if tt.code != "" {
				assertErrorCode(t, recorder, tt.status, tt.code)
			} else if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.status)
			}
			if fake.username != "user" {
				t.Fatalf("username = %q", fake.username)
			}
		})
	}
}

func TestWrapperLogoutValidatesUsername(t *testing.T) {
	server := &Server{wrapper: &fakeWrapperService{}}
	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/wrapper/logout", `{"username":" "}`)
	assertErrorCode(t, recorder, http.StatusBadRequest, "invalid_request")
}
