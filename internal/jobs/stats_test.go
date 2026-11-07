package jobs

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
)

func TestQueueStats(t *testing.T) {
	if got := (*Manager)(nil).QueueStats(); got != (QueueStats{}) {
		t.Fatalf("nil manager QueueStats() = %+v, want zero", got)
	}

	ctx := context.Background()
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	p := newGateProcessor()
	m := NewManager(store, events.NewHub(), p, 1, slog.Default())
	now := time.Now().UTC()
	for _, id := range []string{"a", "b", "c"} {
		job := domain.Job{ID: "job_recovered_" + id, Input: "album|cn|" + id, Type: "album", Storefront: "cn",
			CanonicalKey: "album:cn:" + id, Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now}
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	if recovered, err := m.RecoverUnfinished(ctx); err != nil || recovered != 3 {
		t.Fatalf("RecoverUnfinished = (%d, %v), want (3, nil)", recovered, err)
	}
	resp := m.SubmitBatch(ctx, []string{"station|us|1", "station|us|2"}, nil)
	if resp.Accepted != 2 {
		t.Fatalf("SubmitBatch accepted %d, want 2: %+v", resp.Accepted, resp.Results)
	}

	// Not started yet: admitted and recovered jobs all wait.
	want := QueueStats{Workers: 1, Active: 0, QueueDepth: 2, QueueCapacity: 256, RecoveryBacklog: 3}
	if got := m.QueueStats(); got != want {
		t.Fatalf("QueueStats() = %+v, want %+v", got, want)
	}

	// The one worker takes the first recovered job and holds it.
	m.Start(ctx)
	defer shutdown(t, m)
	if got := waitEntered(t, p); got != "album|cn|a" {
		t.Fatalf("first job run = %s, want the first recovered job", got)
	}
	want.Active, want.RecoveryBacklog = 1, 2
	if got := m.QueueStats(); got != want {
		t.Fatalf("QueueStats() with one job running = %+v, want %+v", got, want)
	}
}
