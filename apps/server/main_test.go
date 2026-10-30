package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amdl/apps/console"
	"amdl/internal/db"
)

// newRunEnv prepares a config directory with a seeded config.yaml that points
// every data path into a temp dir and turns on simulate mode, the way
// `make dev` does; the shipped example sits next to it as on a real install.
func newRunEnv(t *testing.T, hooksYAML string) configFiles {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	example, err := os.ReadFile(filepath.Join("..", "..", "configs", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.example.yaml"), example, 0o644); err != nil {
		t.Fatal(err)
	}
	files := configFiles{config: filepath.Join(configDir, "config.yaml"), hooks: filepath.Join(configDir, "hooks.yaml")}
	if hooksYAML != "" {
		if err := os.WriteFile(files.hooks, []byte(hooksYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seed := fmt.Sprintf("logging:\n  console: false\ndownload:\n  downloads_dir: %q\n  temp_dir: %q\nsimulate:\n  enabled: true\n",
		filepath.Join(root, "downloads"), filepath.Join(root, "tmp"))
	if err := os.WriteFile(files.config, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMDL_SERVER_LISTEN", "127.0.0.1:0")
	t.Setenv("AMDL_DATABASE_PATH", filepath.Join(root, "db", "amdl.db"))
	return files
}

func TestRunReturnsStartupErrors(t *testing.T) {
	t.Run("config without an example to bootstrap from", func(t *testing.T) {
		dir := t.TempDir()
		err := run(context.Background(), configFiles{config: filepath.Join(dir, "config.yaml"), hooks: filepath.Join(dir, "hooks.yaml")})
		if err == nil || !strings.Contains(err.Error(), "bootstrap config") {
			t.Fatalf("run() = %v, want a bootstrap config error", err)
		}
	})
	t.Run("invalid webhook url names the hook", func(t *testing.T) {
		files := newRunEnv(t, "enabled: true\nentries:\n  - name: refresh\n    type: webhook\n    events: [job_finished]\n    url: media-server.local/refresh\n")
		err := run(context.Background(), files)
		var logged loggedError
		if err == nil || !strings.Contains(err.Error(), `hooks.entries["refresh"]`) || !errors.As(err, &logged) {
			t.Fatalf("run() = %v, want a logged hooks config error naming the entry", err)
		}
	})
	t.Run("listen address in use", func(t *testing.T) {
		files := newRunEnv(t, "")
		busy, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer busy.Close()
		t.Setenv("AMDL_SERVER_LISTEN", busy.Addr().String())
		if err := run(context.Background(), files); err == nil || !strings.Contains(err.Error(), "listen on") {
			t.Fatalf("run() = %v, want a listen error", err)
		}
	})
}

// A full start in simulate mode stops cleanly when its context ends, and the
// database it closed opens again.
func TestRunStopsCleanlyWhenItsContextEnds(t *testing.T) {
	files := newRunEnv(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, files) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() = %v, want nil after a clean shutdown", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("run did not return after its context ended")
	}
	store, err := db.Open(os.Getenv("AMDL_DATABASE_PATH"))
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	_ = store.Close()
}

// startRun runs run on a free loopback port and waits until it answers
// /api/v1/health. Cancelling ctx stops it; run's result arrives on done.
func startRun(t *testing.T, ctx context.Context) (addr string, done <-chan error) {
	t.Helper()
	files := newRunEnv(t, "")
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr = probe.Addr().String()
	_ = probe.Close()
	t.Setenv("AMDL_SERVER_LISTEN", addr)

	result := make(chan error, 1)
	go func() { result <- run(ctx, files) }()

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

// The binary mounts the web console; the api package's own tests use a stub.
func TestRunServesTheConsole(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	addr, done := startRun(t, ctx)
	page, err := http.Get("http://" + addr + "/console/")
	if err != nil {
		t.Fatal(err)
	}
	_ = page.Body.Close()
	if page.StatusCode != http.StatusOK || page.Header.Get("Content-Security-Policy") != console.ContentSecurityPolicy {
		t.Errorf("GET /console/ = %d CSP %q, want the console", page.StatusCode, page.Header.Get("Content-Security-Policy"))
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() = %v, want nil", err)
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
	addr, done := startRun(t, ctx)

	base := "http://" + addr + "/api/v1"
	stream, err := http.Get(base + "/downloads/events")
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
			t.Fatalf("run() = %v, want nil", err)
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
