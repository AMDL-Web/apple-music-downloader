package media

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/jobs"
)

// waitJobStatus polls the store until jobID reads want, for at most 10 s.
func waitJobStatus(t *testing.T, store *db.Store, jobID string, want domain.JobStatus) domain.Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		job, err := store.GetJob(context.Background(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == want {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s = %s after 10s, want %s", jobID, job.Status, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Submitting a downloaded album again runs the same queue entry once more,
// and that run finds every track on disk: the tracks this job downloaded stay
// completed, nothing downloads again and no track metadata is read.
func TestResubmittedAlbumRequeuesAndFinishesFromDisk(t *testing.T) {
	cfg := libraryTestConfig(t, true)
	catalog := libraryTestCatalog()
	downloader := newLibraryTestDownloader(cfg, catalog)
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager := jobs.NewManager(store, events.NewHub(), downloader, 1, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	manager.Start(ctx)
	t.Cleanup(func() {
		cancel()
		waitCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := manager.Wait(waitCtx); err != nil {
			t.Errorf("workers did not stop: %v", err)
		}
	})

	const url = "https://music.apple.com/cn/album/album/album-1"
	first := manager.SubmitBatch(ctx, []string{url}, nil).Results[0]
	if first.Status != domain.SubmitAccepted || first.Requeued {
		t.Fatalf("first submit = %+v, want a new job", first)
	}
	done := waitJobStatus(t, store, first.Job.ID, domain.JobCompleted)
	if done.CompletedItems != 2 {
		t.Fatalf("first run = %+v, want 2 completed tracks", done)
	}
	metadataBefore, _ := catalog.counts()

	second := manager.SubmitBatch(ctx, []string{url}, nil).Results[0]
	if second.Status != domain.SubmitAccepted || !second.Requeued || second.Job.ID != first.Job.ID {
		t.Fatalf("resubmit = %+v, want %s requeued", second, first.Job.ID)
	}
	again := waitJobStatus(t, store, first.Job.ID, domain.JobCompleted)
	items, err := store.ListItems(context.Background(), first.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v, want the album's 2", items)
	}
	for _, item := range items {
		if item.Status != domain.ItemCompleted {
			t.Fatalf("item %s = %s, want it kept completed", item.AdamID, item.Status)
		}
	}
	if again.CompletedItems != 2 || again.FailedItems != 0 {
		t.Fatalf("second run = %+v, want the 2 tracks still completed", again)
	}
	evs, err := store.ListEventsAfterLimit(context.Background(), first.Job.ID, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	rerun := false
	for _, ev := range evs {
		switch {
		case ev.Type == domain.EventJobRetried:
			rerun = true
		case rerun && (ev.Type == domain.EventItemCompleted || ev.Type == domain.EventItemFailed):
			t.Fatalf("second run recorded %s for %s, want no track run", ev.Type, ev.ItemID)
		}
	}
	if !rerun {
		t.Fatal("no job_retried event for the resubmit")
	}
	metadataAfter, _ := catalog.counts()
	for id, n := range metadataAfter {
		if n != metadataBefore[id] {
			t.Fatalf("song %s metadata reads %d -> %d, want none in the second run", id, metadataBefore[id], n)
		}
	}
	_, total, err := store.ListJobs(context.Background(), db.JobListFilter{Limit: 10})
	if err != nil || total != 1 {
		t.Fatalf("jobs = %d, %v, want the one queue entry", total, err)
	}
}
