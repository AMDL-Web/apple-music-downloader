package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type doctorCheck struct {
	Name     string
	OK       bool
	Required bool
	Hint     string
}

// doctor reports each check of what a command needs; a failed required
// check is exit 5, and the error's hint is that check's next command.
func TestDoctor(t *testing.T) {
	const remote = `{"version":"2.0.0","simulate":false,"tools":[{"name":"ffmpeg","available":true}],"startup":{"wrapper_address":"http://wrapper.test:12340"}}`
	for _, c := range []struct {
		name, system, wrapper string // wrapper "" fails with wrapper_unreachable
		wantExit              int
		wantErrHint           string // substring; "" for none
		check                 func(t *testing.T, checks map[string]doctorCheck, errHint string)
	}{
		{name: "a server elsewhere whose wrapper is down", system: remote, wantExit: exitUnavailable, wantErrHint: "http://wrapper.test:12340",
			check: func(t *testing.T, checks map[string]doctorCheck, errHint string) {
				if _, ok := checks["ffmpeg"]; ok {
					t.Errorf("a server elsewhere got a local ffmpeg check")
				}
				if h := checks["server"].Hint; h != errHint || strings.Contains(h, "config get wrapper.address") {
					t.Errorf("server hint %q, error hint %q: want the wrapper address it uses, as the error's hint", h, errHint)
				}
			}},
		{name: "a wrapper with no account", system: remote, wrapper: `{"ready":true,"logged_in":false,"regions":[]}`,
			wantExit: exitUnavailable, wantErrHint: "amdl wrapper login",
			check: func(t *testing.T, checks map[string]doctorCheck, _ string) {
				if _, ok := checks["simulate"]; ok {
					t.Errorf("a simulate check while simulate is off")
				}
			}},
		{name: "simulate mode warns but needs no wrapper",
			system:  `{"version":"2.0.0","simulate":true,"tools":[{"name":"ffmpeg","available":false}],"startup":{}}`,
			wrapper: `{"ready":false,"logged_in":false,"regions":[]}`, wantExit: 0,
			check: func(t *testing.T, checks map[string]doctorCheck, _ string) {
				s, ok := checks["simulate"]
				if !ok || s.OK || s.Required || !strings.Contains(s.Hint, "simulate.enabled=false") {
					t.Errorf("simulate check = %+v (present %v)", s, ok)
				}
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/system":
					fmt.Fprint(w, c.system)
				case r.URL.Path == "/api/v1/wrapper/status" && c.wrapper == "":
					http.Error(w, `{"error":{"code":"wrapper_unreachable","message":"down"}}`, http.StatusBadGateway)
				case r.URL.Path == "/api/v1/wrapper/status":
					fmt.Fprint(w, c.wrapper)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			r := run(t, srv, "", "doctor")
			var report struct {
				OK     bool
				Checks []doctorCheck
			}
			if err := json.Unmarshal([]byte(r.stdout), &report); err != nil || r.code != c.wantExit || report.OK != (c.wantExit == 0) {
				t.Fatalf("got %+v (%v), want exit %d", r, err, c.wantExit)
			}
			errHint := ""
			if c.wantExit != 0 {
				if code := errorCode(t, r.stderr); code != "checks_failed" {
					t.Errorf("error code %q", code)
				}
				errHint = errorHint(t, r.stderr)
			}
			if !strings.Contains(errHint, c.wantErrHint) {
				t.Errorf("error hint %q, want %q in it", errHint, c.wantErrHint)
			}
			checks := map[string]doctorCheck{}
			for _, ch := range report.Checks {
				checks[ch.Name] = ch
			}
			c.check(t, checks, errHint)
		})
	}
}
