package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
)

// setListen points server.listen in the config at path to addr.
func setListen(t *testing.T, path, addr string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Replace(string(raw), `listen: "127.0.0.1:0"`, `listen: "`+addr+`"`, 1)
	if out == string(raw) {
		t.Fatalf("no server.listen to replace in %s", path)
	}
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
}

// freeAddr returns a loopback address nothing listens on right now.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func waitHealthy(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := http.Get(base + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not answer: %v", base, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// POST /system/restart stops Run with ErrRestart; running it again, as amdl
// serve does, applies a restart key saved before, on the same address, and
// repeated restarts leave no goroutines behind.
func TestRestartAppliesSavedRestartKeys(t *testing.T) {
	opts := newRunEnv(t, "")
	addr := freeAddr(t)
	setListen(t, opts.ConfigPath, addr)
	base := "http://" + addr
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 10 * time.Second}

	run := func() <-chan error {
		done := make(chan error, 1)
		go func() { done <- Run(context.Background(), opts) }()
		waitHealthy(t, base)
		return done
	}
	restart := func(done <-chan error) {
		t.Helper()
		resp, err := client.Post(base+"/api/v1/system/restart", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("POST /system/restart = %d", resp.StatusCode)
		}
		select {
		case err := <-done:
			if !errors.Is(err, ErrRestart) {
				t.Fatalf("Run() = %v, want ErrRestart", err)
			}
		case <-time.After(60 * time.Second):
			t.Fatal("Run did not return after the restart request")
		}
	}
	bufferSize := func() int {
		t.Helper()
		resp, err := client.Get(base + "/api/v1/system")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var s struct {
			Startup struct {
				LogBufferSize int `json:"log_buffer_size"`
			} `json:"startup"`
			PendingRestart []string `json:"pending_restart"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
			t.Fatal(err)
		}
		return s.Startup.LogBufferSize
	}

	done := run()
	before := runtime.NumGoroutine()
	req, _ := http.NewRequest(http.MethodPut, base+"/api/v1/config", strings.NewReader(`{"logging":{"buffer_size":321}}`))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || bufferSize() != config.Default().Logging.BufferSize {
		t.Fatalf("PUT = %d; the running buffer size must wait for the restart", resp.StatusCode)
	}
	restart(done)
	done = run()
	if got := bufferSize(); got != 321 {
		t.Fatalf("log_buffer_size after the restart = %d, want 321", got)
	}
	for range 2 {
		restart(done)
		done = run()
	}
	deadline := time.Now().Add(10 * time.Second)
	for runtime.NumGoroutine() > before+5 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before+5 {
		t.Fatalf("goroutines grew from %d to %d over three restarts", before, after)
	}
	// End the last run through the same path.
	restart(done)
}
