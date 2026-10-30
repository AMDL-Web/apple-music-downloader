package app

import (
	"context"
	"path/filepath"
	"testing"

	"amdl/internal/local"
)

func TestStandaloneAttachesOrRunsTheOneBackend(t *testing.T) {
	root := t.TempDir()
	d := local.Dirs{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Music: filepath.Join(root, "music")}
	b := local.Backend{Dirs: d, Wrapper: "http://127.0.0.1:9"}

	// Another program, such as amdl service, runs the backend: the app uses it.
	service, err := b.Start()
	if err != nil {
		t.Fatal(err)
	}
	own, shared, err := startOrAttach(b)
	if err != nil || own != nil || shared.URL != service.Base {
		t.Fatalf("with a backend running: own %v, shared %q, %v; want to attach to %s", own, shared.URL, err, service.Base)
	}
	if err := service.Stop(); err != nil {
		t.Fatal(err)
	}

	// Nothing runs: the app runs its own, which other clients find.
	own, _, err = startOrAttach(b)
	if err != nil || own == nil {
		t.Fatalf("with nothing running: own %v, %v", own, err)
	}
	defer func() {
		if err := own.Stop(); err != nil {
			t.Error(err)
		}
		own.log.Close()
	}()
	if u, ok := local.Find(context.Background(), d); !ok || u != own.Base {
		t.Errorf("another client finds %q, %v; want %s", u, ok, own.Base)
	}
}
