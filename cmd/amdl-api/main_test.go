package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/logging"
)

func TestRecoveryTempOverrideRejectsLegacyPathEscape(t *testing.T) {
	root := t.TempDir()
	base := config.Default()
	base.Download.DownloadsDir = filepath.Join(root, "downloads")
	base.Download.TempDir = filepath.Join(root, "temp")

	inside := filepath.Join(base.Download.TempDir, "job-1")
	job := domain.Job{Overrides: &config.DownloadOverrides{TempDir: &inside}}
	got, ok, err := recoveryTempOverride(base, job)
	if err != nil || !ok || got != inside {
		t.Fatalf("inside override = (%q, %v, %v), want accepted", got, ok, err)
	}

	escape := filepath.Join(root, "outside")
	job.Overrides.TempDir = &escape
	if _, _, err := recoveryTempOverride(base, job); err == nil {
		t.Fatal("legacy temp_dir escape was accepted for startup cleanup")
	}
}

// newRunEnv prepares a config directory bootstrapped from the shipped example
// and points every data path into a temp dir, the way `make dev` does.
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
	t.Setenv("AMDL_SERVER_LISTEN", "127.0.0.1:0")
	t.Setenv("AMDL_SIMULATE_ENABLED", "true")
	t.Setenv("AMDL_DATABASE_PATH", filepath.Join(root, "db", "amdl.db"))
	t.Setenv("AMDL_DOWNLOAD_DOWNLOADS_DIR", filepath.Join(root, "downloads"))
	t.Setenv("AMDL_DOWNLOAD_TEMP_DIR", filepath.Join(root, "tmp"))
	t.Setenv("AMDL_LOGGING_FILE_ENABLED", "false")
	t.Setenv("AMDL_LOGGING_CONSOLE", "false")
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

// SIGHUP re-reads the config file and applies its logging level; an invalid
// file keeps the previous snapshot.
func TestReloadConfigAppliesLoggingLevelAndKeepsSnapshotOnError(t *testing.T) {
	files := newRunEnv(t, "")
	if _, err := config.EnsureFile(files.config); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(files.config)
	if err != nil {
		t.Fatal(err)
	}
	logSystem, err := logging.New(cfg.Logging)
	if err != nil {
		t.Fatal(err)
	}
	defer logSystem.Close()
	cfgStore := config.NewFileStore(cfg, files.config)
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	raw, err := os.ReadFile(files.config)
	if err != nil {
		t.Fatal(err)
	}

	debug := strings.Replace(string(raw), `level: "info"`, `level: "debug"`, 1)
	if debug == string(raw) {
		t.Fatal(`config example has no level: "info" line to edit`)
	}
	if err := os.WriteFile(files.config, []byte(debug), 0o600); err != nil {
		t.Fatal(err)
	}
	reloadConfig(cfgStore, logSystem, logger)
	if got := cfgStore.Get().Logging.Level; got != "debug" {
		t.Fatalf("reloaded logging level = %q, want debug", got)
	}
	if !logSystem.Logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("logger does not emit debug after the reload")
	}
	if !strings.Contains(logs.String(), "config reloaded on SIGHUP") {
		t.Fatalf("reload log = %q, want a reload line", logs.String())
	}

	if err := os.WriteFile(files.config, []byte("logging: [not, a, map]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloadConfig(cfgStore, logSystem, logger)
	if got := cfgStore.Get().Logging.Level; got != "debug" {
		t.Fatalf("logging level after an invalid reload = %q, want the previous debug", got)
	}
	if !strings.Contains(logs.String(), "keeping the previous config") {
		t.Fatalf("reload log = %q, want the reload error", logs.String())
	}
}
