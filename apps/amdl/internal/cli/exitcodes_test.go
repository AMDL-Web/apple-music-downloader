package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An API error becomes its error object on stderr, nothing on stdout, and
// the exit status of its class (SKILL.md's table); some carry a hint naming
// what to do. A closed port is server_unreachable, exit 5 (the end-to-end
// scripts cover it).
func TestAPIErrorsMapToExitStatuses(t *testing.T) {
	for _, c := range []struct {
		status   int
		body     string
		wantCode string
		wantExit int
		wantHint string
	}{
		{404, `{"error":{"code":"job_not_found","message":"job job_x not found"}}`, "job_not_found", exitNotFound, ""},
		// A link the catalog does not have is not found, not a retry later.
		{404, `{"error":{"code":"catalog_not_found","message":"read Apple Music catalog: Apple Music has no album 1 in storefront us (404)"}}`,
			"catalog_not_found", exitNotFound, ""},
		{400, `{"error":{"code":"invalid_request","message":"bad"}}`, "invalid_request", exitInput, ""},
		{422, `{"error":{"code":"no_urls_accepted","message":"none"}}`, "no_urls_accepted", exitInput, ""},
		{409, `{"error":{"code":"job_active","message":"running"}}`, "job_active", exitConflict, ""},
		{503, `{"error":{"code":"queue_full","message":"full"}}`, "queue_full", exitUnavailable, ""},
		{401, `{"error":{"code":"authentication_failed","message":"no"}}`, "authentication_failed", exitAuth, ""},
		{500, `{"error":{"code":"store_error","message":"disk"}}`, "store_error", exitInternal, ""},
		// A gateway that refuses the wrapper key: exit 6 and how to set it.
		{503, `{"error":{"code":"wrapper_unauthorized","message":"wrapper /status: the wrapper refused the key (HTTP 401); check wrapper.token"}}`,
			"wrapper_unauthorized", exitAuth, "amdl config set wrapper.token=<key>"},
		{502, `{"error":{"code":"wrapper_unavailable","message":"down"}}`, "wrapper_unavailable", exitUnavailable, "amdl system restart"},
		{400, `{"error":{"code":"config_key_not_mutable","message":"file only"}}`, "config_key_not_mutable", exitInput, "edit the file at startup.config_path"},
		{400, `{"error":{"code":"config_key_not_mutable","message":"file only","details":{"reason":"managed_wrapper"}}}`,
			"config_key_not_mutable", exitInput, "amdl wrapper uninstall"},
		// An answer without a code is classed by its status.
		{502, ``, "http_502", exitUnavailable, ""},
		{418, ``, "http_418", exitInput, ""},
		{404, `{"error":{"code":"something_new","message":"?"}}`, "something_new", exitInput, ""},
	} {
		t.Run(fmt.Sprintf("%d %s", c.status, c.wantCode), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				fmt.Fprint(w, c.body)
			}))
			defer srv.Close()
			r := run(t, srv, "", "jobs", "get", "job_x")
			e := errorOf(t, r.stderr)
			if r.code != c.wantExit || e.Error.Code != c.wantCode || r.stdout != "" {
				t.Errorf("exit %d, code %q, stdout %q; want exit %d, code %q", r.code, e.Error.Code, r.stdout, c.wantExit, c.wantCode)
			}
			if !strings.Contains(e.Error.Hint, c.wantHint) || (c.wantHint == "" && e.Error.Hint != "") {
				t.Errorf("hint %q, want %q", e.Error.Hint, c.wantHint)
			}
		})
	}
}
