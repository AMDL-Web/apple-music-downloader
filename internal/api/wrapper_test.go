package api

import (
	"errors"
	"net/http"
	"testing"

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
	want := `{"ready":true,"logged_in":true,"regions":["us"],"accounts":["user@example.com"],"login_supported":false}` + "\n"
	if recorder.Body.String() != want {
		t.Fatalf("body = %s, want %s", recorder.Body.String(), want)
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
