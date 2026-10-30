package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

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
	// A later start without settings keeps the file's.
	if err := (Backend{Dirs: d}).WriteConfig(); err != nil {
		t.Fatal(err)
	}
	if again, _ := config.Load(filepath.Join(d.ServerDir(), "config.yaml")); again.Wrapper.Address != "http://nas:30020" {
		t.Errorf("wrapper after a bare start = %q", again.Wrapper.Address)
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
