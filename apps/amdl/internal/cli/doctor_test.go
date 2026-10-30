package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// doctorServer answers /system with a startup wrapper address; the wrapper
// status call fails unless wrapperOK, then reports a wrapper with no account.
func doctorServer(t *testing.T, wrapperOK bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system":
			fmt.Fprint(w, `{"version":"2.0.0","simulate":false,"tools":[{"name":"ffmpeg","available":true}],
				"startup":{"wrapper_address":"http://wrapper.test:12340"}}`)
		case "/api/v1/wrapper/status":
			if !wrapperOK {
				http.Error(w, `{"error":{"code":"wrapper_unreachable","message":"down"}}`, http.StatusBadGateway)
				return
			}
			fmt.Fprint(w, `{"ready":true,"logged_in":false,"regions":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDoctorSkipsFFmpegForARemoteServerAndNamesTheWrapper(t *testing.T) {
	r := run(t, doctorServer(t, false), "", "doctor")
	if r.code != exitUnavailable || errorCode(t, r.stderr) != "checks_failed" {
		t.Fatalf("got %+v", r)
	}
	var report struct {
		Checks []struct{ Name, Hint string } `json:"checks"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &report); err != nil {
		t.Fatal(err)
	}
	var hint string
	for _, c := range report.Checks {
		if c.Name == "ffmpeg" {
			t.Errorf("a server elsewhere got a local ffmpeg check")
		}
		if c.Name == "server" {
			hint = c.Hint
		}
	}
	if !strings.Contains(hint, "http://wrapper.test:12340") || strings.Contains(hint, "config get wrapper.address") {
		t.Errorf("server hint = %q", hint)
	}
	var e struct {
		Error struct{ Hint string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stderr), &e); err != nil || e.Error.Hint != hint {
		t.Errorf("error hint = %q, want the failed check's %q", e.Error.Hint, hint)
	}
}

func TestDoctorErrorHintCarriesTheNextCommand(t *testing.T) {
	r := run(t, doctorServer(t, true), "", "doctor")
	if r.code != exitUnavailable {
		t.Fatalf("got %+v", r)
	}
	var e struct {
		Error struct{ Hint string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stderr), &e); err != nil || !strings.Contains(e.Error.Hint, "amdl wrapper login") {
		t.Errorf("error = %s", r.stderr)
	}
}
