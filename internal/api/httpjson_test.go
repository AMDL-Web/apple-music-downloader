package api

import (
	"net/http"
	"strings"
	"testing"

	"amdl/internal/config"
)

func TestJSONEndpointsRejectTrailingValues(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "config", method: http.MethodPut, path: "/api/v1/config", body: `{}`},
		{name: "wrapper login", method: http.MethodPost, path: "/api/v1/wrapper/login", body: `{"username":"user","password":"secret"}`},
		{name: "wrapper two step", method: http.MethodPost, path: "/api/v1/wrapper/login/login-1/2fa", body: `{"two_step_code":"123456"}`},
		{name: "wrapper logout", method: http.MethodPost, path: "/api/v1/wrapper/logout", body: `{"username":"user"}`},
		{name: "quality", method: http.MethodPost, path: "/api/v1/quality", body: `{"url":"song|us|1"}`},
		{name: "downloads", method: http.MethodPost, path: "/api/v1/downloads", body: `{"urls":["song|us|1"]}`},
	}
	for _, tc := range tests {
		for _, suffix := range []struct {
			name  string
			value string
		}{
			{name: "second JSON value", value: "\n{}"},
			{name: "trailing garbage", value: " trailing"},
		} {
			t.Run(tc.name+"/"+suffix.name, func(t *testing.T) {
				server := &Server{
					cfg:     config.NewStore(config.Default()),
					wrapper: &fakeWrapperService{},
					quality: &fakeQualityService{},
				}
				recorder := requestJSON(t, server.Routes(), tc.method, tc.path, tc.body+suffix.value)
				if recorder.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
				}
			})
		}
	}
}

func TestJSONEndpointsRejectOversizedBodies(t *testing.T) {
	padding := strings.Repeat("x", int(maxJSONBodyBytes))
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "config", method: http.MethodPut, path: "/api/v1/config", body: `{"logging":{"level":"` + padding + `"}}`},
		{name: "wrapper login", method: http.MethodPost, path: "/api/v1/wrapper/login", body: `{"username":"` + padding + `","password":"secret"}`},
		{name: "wrapper two step", method: http.MethodPost, path: "/api/v1/wrapper/login/login-1/2fa", body: `{"two_step_code":"` + padding + `"}`},
		{name: "wrapper logout", method: http.MethodPost, path: "/api/v1/wrapper/logout", body: `{"username":"` + padding + `"}`},
		{name: "quality", method: http.MethodPost, path: "/api/v1/quality", body: `{"url":"` + padding + `"}`},
		{name: "downloads", method: http.MethodPost, path: "/api/v1/downloads", body: `{"urls":["` + padding + `"]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := &Server{
				cfg:     config.NewStore(config.Default()),
				wrapper: &fakeWrapperService{},
				quality: &fakeQualityService{},
			}
			recorder := requestJSON(t, server.Routes(), tc.method, tc.path, tc.body)
			if recorder.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
			}
		})
	}
}

func TestJSONBodyKnownOversizeWinsOverEarlySyntaxError(t *testing.T) {
	server := &Server{quality: &fakeQualityService{}}
	body := "!" + strings.Repeat("x", int(maxJSONBodyBytes))
	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/quality", body)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}
