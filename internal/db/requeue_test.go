package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/domain"
)

func requeueStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func addKeyedJob(t *testing.T, store *Store, id, key string, status domain.JobStatus, created time.Time, overrides *config.DownloadOverrides) {
	t.Helper()
	started := created.Add(time.Second)
	job := domain.Job{ID: id, Input: id, Type: "album", Storefront: "us", CanonicalKey: key, Status: status, Overrides: overrides,
		Error: "boom", StartedAt: &started, FinishedAt: &started, CreatedAt: created, UpdatedAt: created}
	if err := store.CreateJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
}

func TestFindLatestFinishedJobByKeyPicksNewestFinishedRow(t *testing.T) {
	store := requeueStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	addKeyedJob(t, store, "old", "album:us:1", domain.JobFailed, base, nil)
	addKeyedJob(t, store, "new", "album:us:1", domain.JobCompleted, base.Add(2*time.Minute), nil)
	addKeyedJob(t, store, "mid", "album:us:1", domain.JobCancelled, base.Add(time.Minute), nil)
	addKeyedJob(t, store, "active", "album:us:1", domain.JobQueued, base.Add(3*time.Minute), nil)
	addKeyedJob(t, store, "other", "album:us:2", domain.JobCompleted, base.Add(4*time.Minute), nil)

	job, found, err := store.FindLatestFinishedJobByKey(ctx, "album:us:1")
	if err != nil || !found || job.ID != "new" {
		t.Fatalf("latest = %q, %v, %v, want new", job.ID, found, err)
	}
	if _, found, err := store.FindLatestFinishedJobByKey(ctx, "album:us:3"); err != nil || found {
		t.Fatalf("missing key found = %v, %v, want false", found, err)
	}
}

func TestRequeueJobResetsTheRunAndWritesOnlyWhatItIsTold(t *testing.T) {
	store := requeueStore(t)
	ctx := context.Background()
	created := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	force := true
	forced := &config.DownloadOverrides{ForceOverwrite: &force}
	addKeyedJob(t, store, "keep", "album:us:1", domain.JobFailed, created, forced)
	addKeyedJob(t, store, "replace", "album:us:2", domain.JobCompleted, created, forced)

	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := store.RequeueJob(ctx, "keep", RequeueParams{QueueOrder: 7, Now: now}); err != nil {
		t.Fatal(err)
	}
	kept, err := store.GetJob(ctx, "keep")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Status != domain.JobQueued || kept.Error != "" || kept.StartedAt != nil || kept.FinishedAt != nil ||
		kept.QueueOrder == nil || *kept.QueueOrder != 7 || !kept.UpdatedAt.Equal(now) {
		t.Fatalf("requeued = %+v, want a queued fresh run at order 7", kept)
	}
	if !kept.CreatedAt.Equal(created) || kept.Overrides == nil || !*kept.Overrides.ForceOverwrite {
		t.Fatalf("requeued = %+v, want created_at and overrides kept", kept)
	}

	if err := store.RequeueJob(ctx, "replace", RequeueParams{QueueOrder: 8, ReplaceOverrides: true, CreatedAt: &now, Now: now}); err != nil {
		t.Fatal(err)
	}
	replaced, err := store.GetJob(ctx, "replace")
	if err != nil {
		t.Fatal(err)
	}
	if !replaced.CreatedAt.Equal(now) || replaced.Overrides != nil {
		t.Fatalf("requeued = %+v, want created_at now and no overrides", replaced)
	}
}

func TestRequeueJobRefusesMissingActiveAndDuplicateJobs(t *testing.T) {
	store := requeueStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	addKeyedJob(t, store, "running", "album:us:1", domain.JobRunning, now, nil)
	addKeyedJob(t, store, "old", "album:us:1", domain.JobCompleted, now.Add(-time.Hour), nil)

	if err := store.RequeueJob(ctx, "missing", RequeueParams{Now: now}); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("missing = %v, want ErrJobNotFound", err)
	}
	if err := store.RequeueJob(ctx, "running", RequeueParams{Now: now}); !errors.Is(err, ErrJobNotFinished) {
		t.Fatalf("running = %v, want ErrJobNotFinished", err)
	}
	if err := store.RequeueJob(ctx, "old", RequeueParams{Now: now}); !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("old row of an active key = %v, want ErrDuplicateActive", err)
	}
	old, err := store.GetJob(ctx, "old")
	if err != nil || old.Status != domain.JobCompleted {
		t.Fatalf("old = %+v, %v, want it left completed", old, err)
	}
}
