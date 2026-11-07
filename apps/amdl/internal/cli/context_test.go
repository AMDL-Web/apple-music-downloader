package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amdl/internal/local"
)

// writeRuntime writes the runtime file a backend running on this machine
// (the desktop app's, or amdl serve --local) leaves, naming url.
func writeRuntime(t *testing.T, url string) {
	t.Helper()
	isolateHome(t)
	dirs, err := local.DefaultDirs()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirs.ServerDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(local.Runtime{URL: url, PID: 4242, Version: "test", Started: time.Now()})
	if err := os.WriteFile(filepath.Join(dirs.ServerDir(), "runtime.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A current context on this machine that does not answer, such as a closed
// tunnel, gives way to this machine's running backend; one on another host
// is the user's choice and is kept, even while it does not answer.
// (Contexts without a backend here: testdata/script/context.txtar.)
func TestAStaleLocalContextGivesWayToThisMachinesBackend(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/wrapper/status" {
			_, _ = w.Write([]byte(`{"ready":true,"logged_in":true,"regions":["cn"],"login_supported":true,"token_set":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(backend.Close)
	writeRuntime(t, backend.URL)
	for _, args := range [][]string{{"context", "add", "tunnel", "http://127.0.0.1:1"}, {"context", "use", "tunnel"}} {
		if r := run(t, nil, "", args...); r.code != 0 {
			t.Fatalf("amdl %v = %+v", args, r)
		}
	}
	if r := run(t, nil, "", "wrapper", "status"); r.code != 0 || !strings.Contains(r.stdout, `"regions"`) {
		t.Fatalf("a stale local context: %+v", r)
	}

	for _, args := range [][]string{{"context", "add", "nas", "http://192.0.2.7:1"}, {"context", "use", "nas"}} {
		if r := run(t, nil, "", args...); r.code != 0 {
			t.Fatalf("amdl %v = %+v", args, r)
		}
	}
	// cli setup names the server a command would use, without asking it.
	r := run(t, nil, "", "cli", "setup")
	var out struct {
		ClaudeCode string `json:"claude_code"`
	}
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &out) != nil || !strings.HasSuffix(out.ClaudeCode, " mcp --server http://192.0.2.7:1") {
		t.Fatalf("a context on another host: %+v", r)
	}
}
