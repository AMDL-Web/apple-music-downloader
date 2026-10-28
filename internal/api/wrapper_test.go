package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"amdl/internal/wrapper"
)

func TestWrapperStatusEndpoint(t *testing.T) {
	fake := &fakeWrapperService{statusResult: wrapper.Status{
		Ready: true, Status: true, Regions: []string{"us"}, ClientCount: 1,
		Accounts: []string{"user@example.com"}, AccountsSupported: true,
	}}
	server := &Server{wrapper: fake}
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/wrapper/status", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body wrapper.Status
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Ready || body.ClientCount != 1 || len(body.Regions) != 1 {
		t.Fatalf("unexpected body: %#v", body)
	}
	if !body.AccountsSupported || len(body.Accounts) != 1 || body.Accounts[0] != "user@example.com" {
		t.Fatalf("unexpected accounts support: %#v", body)
	}
}

func TestWrapperStatusEndpointReportsUnsupportedAccounts(t *testing.T) {
	fake := &fakeWrapperService{statusResult: wrapper.Status{Ready: true, Status: true, Regions: []string{"us"}, ClientCount: 1}}
	server := &Server{wrapper: fake}
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/wrapper/status", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body wrapper.Status
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.AccountsSupported {
		t.Fatalf("accounts_supported = true, want false: %#v", body)
	}
}

func TestWrapperStatusUnavailable(t *testing.T) {
	server := &Server{wrapper: &fakeWrapperService{statusErr: errors.New("offline")}}
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/wrapper/status", "")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestWrapperLoginResponses(t *testing.T) {
	tests := []struct {
		name   string
		result wrapper.LoginResult
		err    error
		status int
	}{
		{name: "logged in", result: wrapper.LoginResult{Status: wrapper.LoginStatusLoggedIn}, status: http.StatusOK},
		{name: "needs two step", result: wrapper.LoginResult{Status: wrapper.LoginStatusNeedsTwoStep, LoginID: "login-1"}, status: http.StatusAccepted},
		{name: "authentication failed", err: wrapper.ErrAuthenticationFailed, status: http.StatusUnauthorized},
		{name: "already logged in", err: wrapper.ErrAlreadyLoggedIn, status: http.StatusConflict},
		{name: "timeout", err: wrapper.ErrLoginTimeout, status: http.StatusGatewayTimeout},
		{name: "upstream", err: errors.New("connection refused"), status: http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeWrapperService{startResult: tt.result, startErr: tt.err}
			server := &Server{wrapper: fake}
			recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/wrapper/login", `{"username":" user ","password":" secret "}`)
			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d, body = %s", recorder.Code, tt.status, recorder.Body.String())
			}
			if fake.username != "user" || fake.password != "secret" {
				t.Fatalf("credentials not trimmed: %q %q", fake.username, fake.password)
			}
		})
	}
}

func TestWrapperLoginValidatesRequiredFields(t *testing.T) {
	server := &Server{wrapper: &fakeWrapperService{}}
	for _, body := range []string{`{"username":"","password":"secret"}`, `{"username":"user","password":" "}`, `{`} {
		recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/wrapper/login", body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d", body, recorder.Code)
		}
	}
}

func TestWrapperTwoStepResponses(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "success", status: http.StatusOK},
		{name: "failed", err: wrapper.ErrAuthenticationFailed, status: http.StatusUnauthorized},
		{name: "not found", err: wrapper.ErrLoginSessionNotFound, status: http.StatusNotFound},
		{name: "busy", err: wrapper.ErrLoginSessionBusy, status: http.StatusConflict},
		{name: "timeout", err: wrapper.ErrLoginTimeout, status: http.StatusGatewayTimeout},
		{name: "upstream", err: errors.New("stream failed"), status: http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeWrapperService{verifyResult: wrapper.LoginResult{Status: wrapper.LoginStatusLoggedIn}, verifyErr: tt.err}
			server := &Server{wrapper: fake}
			recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/wrapper/login/login-1/2fa", `{"two_step_code":" 123456 "}`)
			if recorder.Code != tt.status {
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
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestWrapperLogoutResponses(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "success", status: http.StatusOK},
		{name: "not found", err: wrapper.ErrAccountNotFound, status: http.StatusNotFound},
		{name: "upstream", err: errors.New("unavailable"), status: http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeWrapperService{logoutErr: tt.err}
			server := &Server{wrapper: fake}
			recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/wrapper/logout", `{"username":" user "}`)
			if recorder.Code != tt.status {
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
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
