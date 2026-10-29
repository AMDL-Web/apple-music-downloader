package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"amdl/internal/buildinfo"
	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/events"
	"amdl/internal/jobs"
	"amdl/internal/logging"
)

func TestSystemInfo(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := signingEnabledConfig()
	cfg.Simulate.Enabled = true
	cfg.Catalog.MotionArtworkEnabled = false
	cfg.Catalog.DefaultStorefront = "jp"
	cfg.Download.MaxParallelDownloads, cfg.Download.MaxParallelDecrypts, cfg.Download.MaxParallelWrapperRequests = 4, 5, 6
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.TempDir = filepath.Join(t.TempDir(), "missing")
	cfg.Tools.FFmpeg = "amdl-test-tool-that-does-not-exist"

	hub := events.NewHub()
	// Never started, so the admitted job stays in the queue.
	manager := jobs.NewManager(store, hub, stubProcessor{}, 2, slog.Default())
	if resp := manager.SubmitBatch(context.Background(), []string{"album|us|1"}, nil); resp.Accepted != 1 {
		t.Fatalf("submit: %+v", resp.Results)
	}
	server := newServer(t, Deps{Config: config.NewStore(cfg), Store: store, Hub: hub, Manager: manager})
	server.now = func() time.Time { return buildinfo.StartedAt.Add(90*time.Second + 600*time.Millisecond) }

	rec := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/system", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{"88KBJL3CKU", "2VTXNMR2GL", "AuthKey"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("body exposes signing configuration %q: %s", secret, rec.Body.String())
		}
	}
	var got struct {
		Version              string         `json:"version"`
		Commit               *string        `json:"commit"`
		BuildDate            *string        `json:"build_date"`
		GoVersion            string         `json:"go_version"`
		OS                   string         `json:"os"`
		Arch                 string         `json:"arch"`
		StartedAt            time.Time      `json:"started_at"`
		UptimeSeconds        int64          `json:"uptime_seconds"`
		Simulate             bool           `json:"simulate"`
		SigningMode          string         `json:"signing_mode"`
		MotionArtworkEnabled *bool          `json:"motion_artwork_enabled"`
		DefaultStorefront    *string        `json:"default_storefront"`
		RawJobs              map[string]int `json:"jobs"`
		RawPools             map[string]int `json:"pools"`
		Tools                []struct {
			Name      string `json:"name"`
			Available bool   `json:"available"`
			Path      string `json:"path"`
			Error     string `json:"error"`
		} `json:"tools"`
		Storage []struct {
			Name       string  `json:"name"`
			Path       string  `json:"path"`
			FreeBytes  *uint64 `json:"free_bytes"`
			TotalBytes *uint64 `json:"total_bytes"`
			Error      string  `json:"error"`
		} `json:"storage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "dev" || got.Commit == nil || got.BuildDate == nil {
		t.Errorf("build = %q commit %v date %v, want dev with commit and build_date present", got.Version, got.Commit, got.BuildDate)
	}
	if got.GoVersion != runtime.Version() || got.OS != runtime.GOOS || got.Arch != runtime.GOARCH {
		t.Errorf("runtime = %s %s/%s", got.GoVersion, got.OS, got.Arch)
	}
	if !got.StartedAt.Equal(buildinfo.StartedAt) || got.UptimeSeconds != 90 {
		t.Errorf("started_at = %v, uptime = %d; want %v and 90", got.StartedAt, got.UptimeSeconds, buildinfo.StartedAt)
	}
	if !got.Simulate || got.SigningMode != "local" || got.MotionArtworkEnabled == nil || *got.MotionArtworkEnabled {
		t.Errorf("simulate %v, signing_mode %q, motion %v; want true, local, false", got.Simulate, got.SigningMode, got.MotionArtworkEnabled)
	}
	if got.DefaultStorefront == nil || *got.DefaultStorefront != "jp" {
		t.Errorf("default_storefront = %v, want jp", got.DefaultStorefront)
	}
	wantJobs := map[string]int{"workers": 2, "active": 0, "queue_depth": 1, "queue_capacity": 256, "recovery_backlog": 0}
	if !equalCounts(got.RawJobs, wantJobs) {
		t.Errorf("jobs = %v, want %v", got.RawJobs, wantJobs)
	}
	wantPools := map[string]int{"downloads": 4, "decrypts": 5, "wrapper_requests": 6}
	if !equalCounts(got.RawPools, wantPools) {
		t.Errorf("pools = %v, want %v", got.RawPools, wantPools)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "ffmpeg" || got.Tools[0].Available || got.Tools[0].Error == "" {
		t.Errorf("tools = %+v, want ffmpeg missing with an error", got.Tools)
	}
	if len(got.Storage) != 2 || got.Storage[0].Name != "downloads_dir" || got.Storage[1].Name != "temp_dir" {
		t.Fatalf("storage = %+v, want downloads_dir and temp_dir", got.Storage)
	}
	downloads, temp := got.Storage[0], got.Storage[1]
	if downloads.Path != cfg.Download.DownloadsDir || temp.Path != cfg.Download.TempDir {
		t.Errorf("storage paths = %q, %q", downloads.Path, temp.Path)
	}
	switch runtime.GOOS {
	case "linux", "darwin", "freebsd":
		if downloads.Error != "" || downloads.FreeBytes == nil || downloads.TotalBytes == nil ||
			*downloads.TotalBytes == 0 || *downloads.FreeBytes > *downloads.TotalBytes {
			t.Errorf("downloads_dir = %+v, want free <= total, both set", downloads)
		}
	default:
		if downloads.Error == "" || downloads.FreeBytes != nil {
			t.Errorf("downloads_dir = %+v, want the unsupported error", downloads)
		}
	}
	if temp.Error == "" || temp.FreeBytes != nil || temp.TotalBytes != nil {
		t.Errorf("missing temp_dir = %+v, want an error and no byte counts", temp)
	}
}

func TestSystemInfoLegacySigning(t *testing.T) {
	server := newTestServerWithManager(t)
	server.cfg = config.NewStore(config.Default())
	rec := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/system", "")
	var body struct {
		SigningMode string `json:"signing_mode"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.SigningMode != "legacy" {
		t.Fatalf("status %d, body %s; want signing_mode legacy", rec.Code, rec.Body.String())
	}
}

// A pool size <= 0 runs as 1 (limits.NewSemaphore), so /system reports 1.
func TestSystemInfoReportsEffectivePoolSizes(t *testing.T) {
	server := newTestServerWithManager(t)
	cfg := config.Default()
	cfg.Download.MaxParallelDownloads, cfg.Download.MaxParallelDecrypts, cfg.Download.MaxParallelWrapperRequests = 0, -3, 2
	server.cfg = config.NewStore(cfg)
	rec := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/system", "")
	var body struct {
		Pools map[string]int `json:"pools"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
	}
	if want := map[string]int{"downloads": 1, "decrypts": 1, "wrapper_requests": 2}; !equalCounts(body.Pools, want) {
		t.Fatalf("pools = %v, want %v", body.Pools, want)
	}
}

func equalCounts(got, want map[string]int) bool {
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if n, ok := got[k]; !ok || n != v {
			return false
		}
	}
	return true
}

// With logging.buffer_size 0 the log store keeps no history, so both log
// routes report the feature as unavailable instead of an always-empty page.
func TestLogRoutesWithoutRetention(t *testing.T) {
	cfg := config.Default()
	cfg.Logging.Console = false
	cfg.Logging.BufferSize = 0
	system, err := logging.New(cfg.Logging)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = system.Close() })
	server := newServer(t, Deps{Config: config.NewStore(cfg), Logger: system.Logger, Logging: system})
	for _, path := range []string{"/api/v1/logs", "/api/v1/logs/stream"} {
		rec := requestJSON(t, server.Routes(), http.MethodGet, path, "")
		assertErrorCode(t, rec, http.StatusServiceUnavailable, CodeFeatureUnavailable)
		if !strings.Contains(rec.Body.String(), "logging.buffer_size") {
			t.Errorf("%s: message does not name logging.buffer_size: %s", path, rec.Body.String())
		}
	}
}
