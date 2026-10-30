package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"amdl/apps/console"
)

func TestOptionsReadTheEnvironment(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	if opts := options(getenv); opts.ConfigPath != "configs/config.yaml" || opts.HooksPath != "configs/hooks.yaml" {
		t.Fatalf("default paths = %q, %q", opts.ConfigPath, opts.HooksPath)
	}
	env["AMDL_CONFIG"], env["AMDL_HOOKS_CONFIG"] = "/app/configs/config.yaml", "/app/configs/hooks.yaml"
	if opts := options(getenv); opts.ConfigPath != "/app/configs/config.yaml" || opts.HooksPath != "/app/configs/hooks.yaml" {
		t.Fatalf("paths from the environment = %q, %q", opts.ConfigPath, opts.HooksPath)
	}
}

// The binary mounts the real console; internal/server's tests use a stub.
func TestOptionsMountTheConsole(t *testing.T) {
	recorder := httptest.NewRecorder()
	options(func(string) string { return "" }).Console.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Security-Policy") != console.ContentSecurityPolicy {
		t.Fatalf("console index = %d CSP %q, want the console", recorder.Code, recorder.Header().Get("Content-Security-Policy"))
	}
}
