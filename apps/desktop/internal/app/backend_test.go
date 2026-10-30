package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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
		if err := own.log.Close(); err != nil {
			t.Error(err)
		}
	}()
	if u, ok := local.Find(context.Background(), d); !ok || u != own.Base {
		t.Errorf("another client finds %q, %v; want %s", u, ok, own.Base)
	}
}

func TestFollowsARestartingServiceBeforeTakingOver(t *testing.T) {
	const url = "http://127.0.0.1:52011"
	start := time.Date(2026, 10, 30, 21, 0, 0, 0, time.UTC)
	var gone time.Time
	if replaceShared(local.Runtime{URL: url}, true, url, &gone, start) {
		t.Fatal("left a backend that answers")
	}
	// The service stops; launchd and systemd start it again 10 s later.
	for _, after := range []time.Duration{0, 3 * time.Second, 12 * time.Second} {
		if replaceShared(local.Runtime{}, false, url, &gone, start.Add(after)) {
			t.Fatalf("took the data directory %s after the service stopped, inside its restart delay", after)
		}
	}
	if replaceShared(local.Runtime{URL: url}, true, url, &gone, start.Add(13*time.Second)) || !gone.IsZero() {
		t.Fatal("did not follow the service once it answered again")
	}
	// Stopped for good: after the grace the app runs the backend itself.
	replaceShared(local.Runtime{}, false, url, &gone, start.Add(time.Minute))
	if !replaceShared(local.Runtime{}, false, url, &gone, start.Add(time.Minute+takeoverGrace)) {
		t.Fatal("never took over from a service that stays stopped")
	}
	// Another backend took its place: the app attaches at once.
	gone = time.Time{}
	if !replaceShared(local.Runtime{URL: "http://127.0.0.1:52012"}, true, url, &gone, start) {
		t.Fatal("kept following a backend that was replaced")
	}
}
