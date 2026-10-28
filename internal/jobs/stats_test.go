package jobs

import (
	"context"
	"testing"
)

func TestQueueStats(t *testing.T) {
	if got := (*Manager)(nil).QueueStats(); got != (QueueStats{}) {
		t.Fatalf("nil manager QueueStats() = %+v, want zero", got)
	}

	// The manager is never started, so admitted and recovered jobs stay queued.
	m := newTestManager(t)
	resp := m.SubmitBatch(context.Background(), []string{"album|us|1", "album|us|2"}, nil)
	if resp.Accepted != 2 {
		t.Fatalf("SubmitBatch accepted %d, want 2: %+v", resp.Accepted, resp.Results)
	}
	m.enqueueRecovered("job_recovered_a")
	m.enqueueRecovered("job_recovered_b")
	m.enqueueRecovered("job_recovered_c")

	want := QueueStats{Workers: 1, Active: 0, QueueDepth: 2, QueueCapacity: 256, RecoveryBacklog: 3}
	if got := m.QueueStats(); got != want {
		t.Fatalf("QueueStats() = %+v, want %+v", got, want)
	}
	if _, ok := m.popRecovered(); !ok {
		t.Fatal("popRecovered found nothing")
	}
	if got := m.QueueStats().RecoveryBacklog; got != 2 {
		t.Fatalf("RecoveryBacklog after one dequeue = %d, want 2", got)
	}
}
