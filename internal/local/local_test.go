package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
)

func tempDirs(t *testing.T) Dirs {
	root := t.TempDir()
	return Dirs{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Music: filepath.Join(root, "music")}
}

func TestWriteConfig(t *testing.T) {
	d := tempDirs(t)
	if err := (Backend{Dirs: d}).WriteConfig(); !errors.Is(err, ErrNoWrapper) {
		t.Fatalf("a first start without a wrapper: %v", err)
	}
	if err := (Backend{Dirs: d, Wrapper: "nas:30020"}).WriteConfig(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(d.ServerDir(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Wrapper.Address != "http://nas:30020" || cfg.Download.DownloadsDir != filepath.Join(d.Music, "AMDL") ||
		cfg.Database.Path != filepath.Join(d.ServerDir(), "db", "amdl.db") || !filepath.IsAbs(cfg.Download.TempDir) {
		t.Errorf("first config = wrapper %q, downloads %q, db %q, temp %q", cfg.Wrapper.Address, cfg.Download.DownloadsDir, cfg.Database.Path, cfg.Download.TempDir)
	}
	// A new install numbers library tracks across discs, as the example does;
	// {TrackNumber} restarts on every disc and collides.
	if !strings.Contains(cfg.Download.LibraryPathFormat, "{SongNumber") {
		t.Errorf("first config library layout = %q", cfg.Download.LibraryPathFormat)
	}
	// A later start without settings keeps the file's.
	if err := (Backend{Dirs: d}).WriteConfig(); err != nil {
		t.Fatal(err)
	}
	if again, _ := config.Load(filepath.Join(d.ServerDir(), "config.yaml")); again.Wrapper.Address != "http://nas:30020" {
		t.Errorf("wrapper after a bare start = %q", again.Wrapper.Address)
	}
}

// A config.yaml an older version wrote still starts the backend, without
// the keys this version removed, and the log names them.
func TestWriteConfigDropsRemovedKeys(t *testing.T) {
	d := tempDirs(t)
	if err := (Backend{Dirs: d, Wrapper: "nas:30020"}).WriteConfig(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(d.ServerDir(), "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(string(raw), "download:\n", "download:\n    memory_mode: low\n", 1)
	if stale == string(raw) {
		t.Fatal("no download section to add to")
	}
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	if err := (Backend{Dirs: d, LogOutput: &log}).WriteConfig(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Wrapper.Address != "http://nas:30020" || !strings.Contains(log.String(), "download.memory_mode") {
		t.Errorf("wrapper %q, log %q", cfg.Wrapper.Address, log.String())
	}
}

func TestStartFindStop(t *testing.T) {
	d := tempDirs(t)
	if _, ok := Find(context.Background(), d); ok {
		t.Fatal("nothing runs yet")
	}
	r, err := Backend{Dirs: d, Wrapper: "http://127.0.0.1:9"}.Start()
	if err != nil {
		t.Fatal(err)
	}
	if u, ok := Find(context.Background(), d); !ok || u != r.Base {
		t.Errorf("Find = %q, %v; want %s", u, ok, r.Base)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d.ServerDir(), "runtime.json")); !os.IsNotExist(err) {
		t.Errorf("runtime file after Stop: %v", err)
	}
	if _, ok := Find(context.Background(), d); ok {
		t.Error("a stopped backend is not found")
	}
}

func TestOneBackendPerDataDirectory(t *testing.T) {
	d := tempDirs(t)
	b := Backend{Dirs: d, Wrapper: "http://127.0.0.1:9"}
	first, err := b.Start()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Start(); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second Start: %v, want ErrBusy", err)
	}
	// Serve waits for the lock, then takes over.
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- b.Serve(ctx, "127.0.0.1:0") }()
	time.Sleep(2 * lockPoll)
	if u, _ := Find(context.Background(), d); u != first.Base {
		t.Fatalf("while the first runs, Find = %q", u)
	}
	if err := first.Stop(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(healthDeadline)
	for {
		if u, ok := Find(context.Background(), d); ok && u != first.Base {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Serve did not take over")
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	if _, ok := Find(context.Background(), d); ok {
		t.Error("the runtime file outlived Serve")
	}
}
