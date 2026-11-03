package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amdl/internal/local"
)

func TestVersionFlagsPrintTheVersionCommand(t *testing.T) {
	want := run(t, nil, "", "version")
	if want.code != 0 || !strings.Contains(want.stdout, `"client"`) {
		t.Fatalf("amdl version = %+v", want)
	}
	for _, flag := range []string{"--version", "-v", "-V"} {
		if r := run(t, nil, "", flag); r.code != 0 || r.stdout != want.stdout {
			t.Errorf("amdl %s = %+v, want amdl version's %q", flag, r, want.stdout)
		}
	}
	if r := run(t, nil, "", "--quiet", "--version"); r.code != 0 || r.stdout != want.stdout {
		t.Errorf("amdl --quiet --version = %+v", r)
	}
}

func TestACommandGroupListsItsCommands(t *testing.T) {
	for _, args := range [][]string{{"jobs"}, {"jobs", "--help"}} {
		r := run(t, nil, "", args...)
		var g struct {
			Group    string
			Commands []struct{ Command string }
		}
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &g) != nil || g.Group != "jobs" {
			t.Fatalf("amdl %v = %+v", args, r)
		}
		got := map[string]bool{}
		for _, c := range g.Commands {
			got[c.Command] = true
		}
		for _, want := range []string{"amdl jobs list", "amdl jobs get <job_id>", "amdl jobs retry <job_id>"} {
			if !got[want] {
				t.Errorf("amdl %v lacks %q", args, want)
			}
		}
		if got["amdl library list"] {
			t.Errorf("amdl %v lists another group's command", args)
		}
	}
	r := run(t, nil, "", "library", "sync")
	if r.code != 0 || !strings.Contains(r.stdout, "amdl library sync reset") || strings.Contains(r.stdout, "amdl library list") {
		t.Errorf("amdl library sync = %+v", r)
	}
}

func TestAnUnknownFlagIsNotACommandNamedEmpty(t *testing.T) {
	r := run(t, nil, "", "--bogus")
	if r.code != exitInput || strings.Contains(r.stderr, `command \"\"`) || !strings.Contains(r.stderr, "--bogus") {
		t.Errorf("got %+v", r)
	}
}

func TestUnknownCommandSuggestsTheNearSpelling(t *testing.T) {
	for args, want := range map[string]string{
		"jobs lst":   "amdl jobs list",
		"jobs gett":  "amdl jobs get",
		"wrapper lo": "amdl wrapper login",
	} {
		r := run(t, nil, "", strings.Fields(args)...)
		var e struct {
			Error struct {
				Details struct {
					DidYouMean []string `json:"did_you_mean"`
				}
			}
		}
		if r.code != exitInput || json.Unmarshal([]byte(r.stderr), &e) != nil || len(e.Error.Details.DidYouMean) == 0 ||
			e.Error.Details.DidYouMean[0] != want {
			t.Errorf("amdl %s: got %+v, want first suggestion %s", args, r, want)
		}
	}
	// A word no command is spelled like falls back to matching by meaning.
	if r := run(t, nil, "", "retry"); !strings.Contains(r.stderr, "amdl jobs retry") {
		t.Errorf("amdl retry: %+v", r)
	}
}

func TestCLISearchAnswersWhereTheFilesAre(t *testing.T) {
	r := run(t, nil, "", "cli", "search", "where did my files go")
	var hits []struct{ Command string }
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &hits) != nil || len(hits) == 0 || !strings.HasPrefix(hits[0].Command, "amdl jobs get") {
		t.Errorf("got %+v", r)
	}
}

func TestLogoutUsageNamesTheFlags(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	r := run(t, srv, "", "wrapper", "logout", "--yes")
	if r.code != exitInput || !strings.Contains(r.stderr, "exactly one of --username and --account-id") {
		t.Errorf("got %+v", r)
	}
}

func TestContextAddReportsReachability(t *testing.T) {
	r := run(t, nil, "", "context", "add", "nowhere", "http://127.0.0.1:1")
	var out struct {
		Name      string
		Current   bool
		Reachable *bool
	}
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &out) != nil || out.Name != "nowhere" || out.Current || out.Reachable == nil || *out.Reachable {
		t.Errorf("got %+v", r)
	}
}

// A current context on this machine that does not answer, such as a closed
// tunnel, gives way to this machine's running backend; one that answers, or
// one on another host, is kept.
func TestAStaleLocalContextGivesWayToThisMachinesBackend(t *testing.T) {
	isolate(t)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/wrapper/status" {
			_, _ = w.Write([]byte(`{"ready":true,"logged_in":true,"regions":["cn"],"login_supported":true,"token_set":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(backend.Close)
	dirs, err := local.DefaultDirs()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirs.ServerDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(local.Runtime{URL: backend.URL, PID: 4242, Version: "test", Started: time.Now()})
	if err := os.WriteFile(filepath.Join(dirs.ServerDir(), "runtime.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"context", "add", "tunnel", "http://127.0.0.1:1"}, {"context", "use", "tunnel"}} {
		if r := runLocal(t, args...); r.code != 0 {
			t.Fatalf("amdl %v = %+v", args, r)
		}
	}
	if r := runLocal(t, "wrapper", "status"); r.code != 0 || !strings.Contains(r.stdout, `"regions"`) {
		t.Fatalf("a stale local context: %+v", r)
	}

	// A context on another host is the user's choice: it is kept, even
	// while it does not answer, rather than talk to another server.
	for _, args := range [][]string{{"context", "add", "nas", "http://192.0.2.7:1"}, {"context", "use", "nas"}} {
		if r := runLocal(t, args...); r.code != 0 {
			t.Fatalf("amdl %v = %+v", args, r)
		}
	}
	e := &env{IO: IO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard, Getenv: func(string) string { return "" }}, ctx: context.Background()}
	if got, err := e.resolve(); err != nil || got.URL != "http://192.0.2.7:1" || got.Name != "nas" {
		t.Fatalf("a context on another host: %+v, %v", got, err)
	}
}
