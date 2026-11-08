package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newRunEnv prepares a config directory with a seeded config.yaml that points
// every data path into a temp dir and turns on simulate mode, the way
// `make dev` does.
func newRunEnv(t *testing.T, hooksYAML string) Options {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := Options{ConfigPath: filepath.Join(configDir, "config.yaml"), HooksPath: filepath.Join(configDir, "hooks.yaml")}
	if hooksYAML != "" {
		if err := os.WriteFile(files.HooksPath, []byte(hooksYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seed := fmt.Sprintf("server:\n  listen: \"127.0.0.1:0\"\ndatabase:\n  path: %q\nlogging:\n  console: false\ndownload:\n  downloads_dir: %q\n  temp_dir: %q\nsimulate:\n  enabled: true\n",
		filepath.Join(root, "db", "amdl.db"), filepath.Join(root, "downloads"), filepath.Join(root, "tmp"))
	if err := os.WriteFile(files.ConfigPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	return files
}

// amdl serve's start, clean stop, restart on the files it left and startup
// errors are end-to-end scripts: apps/amdl/testdata/script/serve*.txtar.

func TestConsoleURL(t *testing.T) {
	for addr, want := range map[string]string{
		"127.0.0.1:52011": "http://127.0.0.1:52011/console/",
		"[::]:18080":      "http://127.0.0.1:18080/console/",
		"0.0.0.0:8080":    "http://127.0.0.1:8080/console/",
		":8080":           "http://127.0.0.1:8080/console/",
		"[::1]:9":         "http://[::1]:9/console/",
		"nas.lan:80":      "http://nas.lan:80/console/",
	} {
		if got := consoleURL(addr); got != want {
			t.Errorf("consoleURL(%s) = %s, want %s", addr, got, want)
		}
	}
}

// startRun runs Run on a listener it opens on a free loopback port, with
// console as Options.Console, and waits until it answers /api/v1/health.
// Cancelling ctx stops it; Run's result arrives on done.
func startRun(t *testing.T, ctx context.Context, console http.Handler) (addr string, done <-chan error) {
	t.Helper()
	opts := newRunEnv(t, "")
	opts.Console = console
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr = listener.Addr().String()
	opts.Listener = listener

	result := make(chan error, 1)
	go func() { result <- Run(ctx, opts) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			return addr, result
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not come up on %s: %v", addr, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Run serves the caller's listener, and Options.Console under /console/.
func TestRunServesTheCallersListenerAndConsole(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("X-Console", r.URL.Path) })
	addr, done := startRun(t, ctx, stub)
	page, err := http.Get("http://" + addr + "/console/index.html")
	if err != nil {
		t.Fatal(err)
	}
	_ = page.Body.Close()
	if page.StatusCode != http.StatusOK || page.Header.Get("X-Console") != "/index.html" {
		t.Errorf("GET /console/index.html = %d reaching %q, want the console handler", page.StatusCode, page.Header.Get("X-Console"))
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("run did not return after its context ended")
	}
}

// An open SSE stream must not hold shutdown for the HTTP drain timeout: run
// registers api.Server.EndStreams with RegisterOnShutdown, so the stream ends
// when the drain starts.
func TestRunEndsOpenStreamsAtShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr, done := startRun(t, ctx, nil)

	// The stream gets a transport of its own. On the shared default transport it
	// races startRun's health probe for the probe's idle connection and can
	// dial a spare one that never carries a request; the server holds such a
	// new connection as active for five seconds, which stalls the drain.
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	base := "http://" + addr + "/api/v1"
	stream, err := client.Get(base + "/downloads/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK {
		t.Fatalf("feed status = %d, want an open stream", stream.StatusCode)
	}

	started := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("run did not return after its context ended")
	}
	if took := time.Since(started); took >= httpDrainTimeout {
		t.Fatalf("shutdown with an open stream took %v, want well under the %v drain timeout", took, httpDrainTimeout)
	}
	if _, err := io.ReadAll(stream.Body); err != nil {
		t.Fatalf("stream did not end cleanly: %v", err)
	}
}
