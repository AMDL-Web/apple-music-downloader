package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/domain"
)

func TestGetJobFailsOnCorruptOverrides(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()

	force := true
	now := time.Now().UTC()
	job := domain.Job{
		ID: "job-1", Input: "song|us|1", Type: "song", CanonicalKey: "song:us:1",
		Overrides: &config.DownloadOverrides{ForceOverwrite: &force},
		Status:    domain.JobQueued, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetJob(ctx, job.ID)
	if err != nil || loaded.Overrides == nil || loaded.Overrides.ForceOverwrite == nil || !*loaded.Overrides.ForceOverwrite {
		t.Fatalf("healthy round trip failed: %+v, %v", loaded.Overrides, err)
	}

	// Corrupt the column behind the store's back: the load must fail loudly
	// instead of silently running the job with the global config.
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET overrides='{broken' WHERE id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetJob(ctx, job.ID); err == nil || !strings.Contains(err.Error(), "decode overrides") {
		t.Fatalf("corrupt overrides load err = %v, want decode overrides error", err)
	}
}

// Rows stored before the overrides shrank keep keys that are no longer
// fields (media_user_token, hooks, downloads_dir, max_parallel_tracks, ...).
// No migration removes them: every read decodes the row with those keys
// ignored, and the job's terminal write drops them.
func TestRemovedOverrideKeysDoNotBlockHistoricalJobLoad(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()

	now := time.Now().UTC()
	job := domain.Job{
		ID: "job-old", Input: "station|us|ra.1", Type: "station", CanonicalKey: "station:us:ra.1:0123456789ab",
		Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	legacy := `{"media_user_token":"old-token","hooks":["notify"],"downloads_dir":"/elsewhere","max_parallel_tracks":64,"embed_lyrics":false,"force_overwrite":true}`
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET overrides=? WHERE id=?`, legacy, job.ID); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("load historical job: %v", err)
	}
	if loaded.Overrides == nil || loaded.Overrides.ForceOverwrite == nil || !*loaded.Overrides.ForceOverwrite {
		t.Fatalf("kept override not retained: %+v", loaded.Overrides)
	}
	recoverable, err := store.ListRecoverableJobs(ctx)
	if err != nil || len(recoverable) != 1 || recoverable[0].Overrides == nil || recoverable[0].Overrides.ForceOverwrite == nil {
		t.Fatalf("ListRecoverableJobs = %+v, %v; want the historical job with its kept override", recoverable, err)
	}

	loaded.Status = domain.JobCompleted
	loaded.UpdatedAt = time.Now().UTC()
	if _, err := store.FinalizeJob(ctx, loaded, domain.Event{JobID: job.ID, Type: domain.EventJobFinished, Message: "done"}); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := store.db.QueryRowContext(ctx, `SELECT overrides FROM jobs WHERE id=?`, job.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != `{"force_overwrite":true}` {
		t.Fatalf("overrides after the terminal write = %s, want only force_overwrite", stored)
	}
}
