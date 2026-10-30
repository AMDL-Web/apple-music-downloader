package db

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"amdl/internal/domain"
)

func TestJobMarksFindsQueuedRunningAndCompletedJobsByID(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	created := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	finished := created.Add(time.Hour)
	for _, job := range []domain.Job{
		{ID: "done", Type: "album", CanonicalKey: "album:us:100", Status: domain.JobCompleted, FinishedAt: &finished},
		{ID: "other-storefront", Type: "album", CanonicalKey: "album:jp:100", Status: domain.JobRunning},
		{ID: "legacy-key", Type: "song", CanonicalKey: "song:us:200:0123456789ab", Status: domain.JobQueued},
		{ID: "failed", Type: "album", CanonicalKey: "album:us:100", Status: domain.JobFailed},
		{ID: "longer-id", Type: "album", CanonicalKey: "album:us:1000", Status: domain.JobCompleted},
		{ID: "playlist", Type: "playlist", CanonicalKey: "playlist:us:100", Status: domain.JobCompleted},
		{ID: "unrelated", Type: "album", CanonicalKey: "album:us:300", Status: domain.JobCompleted},
	} {
		job.Input, job.Storefront, job.CreatedAt, job.UpdatedAt = "input", "us", created, created
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}

	marks, err := store.JobMarks(ctx, []string{"album", "song"}, []string{"100", "200"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range marks {
		ids = append(ids, m.ID)
		if m.ID == "done" && (m.Status != domain.JobCompleted || m.FinishedAt == nil || !m.FinishedAt.Equal(finished) || !m.CreatedAt.Equal(created) || m.CanonicalKey != "album:us:100") {
			t.Fatalf("completed mark = %+v", m)
		}
	}
	slices.Sort(ids)
	// A failed job is no mark, and other types stay out. "1000" contains
	// ":100": the filter is coarse on purpose and the caller matches exactly.
	if want := []string{"done", "legacy-key", "longer-id", "other-storefront"}; !slices.Equal(ids, want) {
		t.Fatalf("marked jobs = %v, want %v", ids, want)
	}

	if marks, err := store.JobMarks(ctx, []string{"album"}, nil); err != nil || marks != nil {
		t.Fatalf("no ids = %v, %v", marks, err)
	}
}
