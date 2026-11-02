package jobs

import (
	"context"
	"testing"
	"time"

	"amdl/internal/domain"
)

func TestQueueStats(t *testing.T) {
	if got := (*Manager)(nil).QueueStats(); got != (QueueStats{}) {
		t.Fatalf("nil manager QueueStats() = %+v, want zero", got)
	}

	// The manager is never started, so admitted and recovered jobs stay queued.
	ctx := context.Background()
	m := newTestManager(t)
	now := time.Now().UTC()
	for _, id := range []string{"a", "b", "c"} {
		job := domain.Job{ID: "job_recovered_" + id, Input: "album|cn|" + id, Type: "album", Storefront: "cn",
			CanonicalKey: "album:cn:" + id, Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now}
		if err := m.store.CreateJob(ctx, job); err != nil {
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

	want := QueueStats{Workers: 1, Active: 0, QueueDepth: 2, QueueCapacity: 256, RecoveryBacklog: 3}
	if got := m.QueueStats(); got != want {
		t.Fatalf("QueueStats() = %+v, want %+v", got, want)
	}
	claim, ok, err := m.claimNext(ctx)
	if err != nil || !ok {
		t.Fatalf("claimNext = (%v, %v), want a job", ok, err)
	}
	defer claim.cancel(nil)
	if claim.job.ID != "job_recovered_a" {
		t.Fatalf("claimed %s, want the first recovered job", claim.job.ID)
	}
	if got := m.QueueStats().RecoveryBacklog; got != 2 {
		t.Fatalf("RecoveryBacklog after one claim = %d, want 2", got)
	}
}
