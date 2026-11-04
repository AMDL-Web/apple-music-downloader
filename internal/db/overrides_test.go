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
		Overrides: &config.DownloadOverrides{CodecAlternative: &force},
		Status:    domain.JobQueued, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetJob(ctx, job.ID)
	if err != nil || loaded.Overrides == nil || loaded.Overrides.CodecAlternative == nil || !*loaded.Overrides.CodecAlternative {
		t.Fatalf("healthy round trip failed: %+v, %v", loaded.Overrides, err)
	}

	// Corrupt the column behind the store's back: the load must fail loudly
	// instead of silently running the job with the global config.
	if _, err := store.db.ExecContext(ctx, `UPDATE runs SET overrides='{broken' WHERE id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetJob(ctx, job.ID); err == nil || !strings.Contains(err.Error(), "decode overrides") {
		t.Fatalf("corrupt overrides load err = %v, want decode overrides error", err)
	}
}

// A run whose overrides do not decode still lists, complete but for them, so
// the queues can show it and its record; reading it to run it still fails.
func TestAListKeepsARunWhoseOverridesDoNotDecode(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()

	now := time.Now().UTC()
	for i, id := range []string{"job-a", "job-bad", "job-c"} {
		job := domain.Job{
			ID: id, Input: "song|us|" + id, Type: "song", CanonicalKey: "song:us:" + id,
			Status: domain.JobCompleted, CreatedAt: now.Add(time.Duration(i) * time.Second), UpdatedAt: now,
		}
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE runs SET overrides='{broken' WHERE id='job-bad'`); err != nil {
		t.Fatal(err)
	}

	listed, total, err := store.ListJobs(ctx, JobListFilter{Limit: 10})
	if err != nil || total != 3 || len(listed) != 3 {
		t.Fatalf("ListJobs = %d of %d, %v; want all three runs", len(listed), total, err)
	}
	for _, j := range listed {
		if j.ID == "job-bad" && (j.Overrides != nil || j.Status != domain.JobCompleted || j.CreatedAt.IsZero()) {
			t.Fatalf("listed run with broken overrides = %+v, want its row without overrides", j)
		}
	}
	if _, err := store.GetJob(ctx, "job-bad"); err == nil || !strings.Contains(err.Error(), "decode overrides") {
		t.Fatalf("GetJob of the broken run err = %v, want decode overrides error", err)
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
	legacy := `{"media_user_token":"old-token","hooks":["notify"],"downloads_dir":"/elsewhere","max_parallel_tracks":64,"embed_lyrics":false,"force_overwrite":true,"codec_alternative":true}`
	if _, err := store.db.ExecContext(ctx, `UPDATE runs SET overrides=? WHERE id=?`, legacy, job.ID); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("load historical job: %v", err)
	}
	if loaded.Overrides == nil || loaded.Overrides.CodecAlternative == nil || !*loaded.Overrides.CodecAlternative {
		t.Fatalf("kept override not retained: %+v", loaded.Overrides)
	}
	recoverable, err := store.ListRecoverableJobs(ctx)
	if err != nil || len(recoverable) != 1 || recoverable[0].Overrides == nil || recoverable[0].Overrides.CodecAlternative == nil {
		t.Fatalf("ListRecoverableJobs = %+v, %v; want the historical job with its kept override", recoverable, err)
	}

	loaded.Status = domain.JobCompleted
	loaded.UpdatedAt = time.Now().UTC()
	if _, err := store.FinalizeJob(ctx, loaded, domain.Event{JobID: job.ID, Type: domain.EventJobFinished, Message: "done"}); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := store.db.QueryRowContext(ctx, `SELECT overrides FROM runs WHERE id=?`, job.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != `{"codec_alternative":true}` {
		t.Fatalf("overrides after the terminal write = %s, want only codec_alternative", stored)
	}
}
