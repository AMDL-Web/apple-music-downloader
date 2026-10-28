package db

import (
	"context"
	"path/filepath"
	"testing"

	"amdl/internal/domain"
)

func TestListEventsAfterLimitPagesWithoutSkipping(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	job := domain.Job{ID: "job-page", Input: "song|us|1", Type: "song", Status: domain.JobQueued}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := store.AddEvent(ctx, domain.Event{JobID: job.ID, Type: "item_progress"}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ListEventsAfterLimit(ctx, job.ID, 0, 2)
	if err != nil || len(first) != 2 {
		t.Fatalf("first page = (%+v, %v), want 2 events", first, err)
	}
	second, err := store.ListEventsAfterLimit(ctx, job.ID, first[1].ID, 2)
	if err != nil || len(second) != 1 || second[0].ID <= first[1].ID {
		t.Fatalf("second page = (%+v, %v), want final event after cursor", second, err)
	}
}

func TestListMilestoneEventsAfterFiltersAndOrders(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, job := range []domain.Job{
		{ID: "job1", Input: "song|us|1", Type: "song", CanonicalKey: "song:us:1", Status: domain.JobRunning},
		{ID: "job2", Input: "song|us|2", Type: "song", CanonicalKey: "song:us:2", Status: domain.JobRunning},
	} {
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	// Mix milestone and non-milestone events across two jobs.
	for _, ev := range []domain.Event{
		{JobID: "job1", Type: "job_started"},    // milestone
		{JobID: "job1", Type: "codec_selected"}, // not a milestone
		{JobID: "job2", Type: "job_queued"},     // milestone
		{JobID: "job1", Type: "item_completed"}, // milestone
	} {
		if _, err := store.AddEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	got, err := store.ListMilestoneEventsAfter(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantTypes := []string{"job_started", "job_queued", "item_completed"}
	if len(got) != len(wantTypes) {
		t.Fatalf("got %d milestone events, want %d: %+v", len(got), len(wantTypes), got)
	}
	for i, ty := range wantTypes {
		if got[i].Type != ty {
			t.Fatalf("milestone[%d].Type = %s, want %s (codec_selected must be filtered out, order by id)", i, got[i].Type, ty)
		}
	}

	// The global cursor is the max id across all jobs.
	last, err := store.LatestGlobalEventID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if last != got[len(got)-1].ID {
		t.Fatalf("LatestGlobalEventID = %d, want %d", last, got[len(got)-1].ID)
	}

	// Resuming from that cursor yields nothing new.
	after, err := store.ListMilestoneEventsAfter(ctx, last)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("ListMilestoneEventsAfter(last) = %+v, want empty", after)
	}
}
