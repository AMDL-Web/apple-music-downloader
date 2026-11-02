package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"amdl/internal/domain"
)

func TestJobCountersAreDerivedFromItemsOnEveryRead(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	job := domain.Job{
		ID: "job-counters", Input: "album|us|1", Type: "album", Storefront: "us", CanonicalKey: "album:us:1",
		Status: domain.JobRunning, TotalItems: 9, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	// 3 completed, 2 skipped_existing, 1 failed, 1 cancelled, 2 still pending.
	statuses := []domain.ItemStatus{
		domain.ItemCompleted, domain.ItemCompleted, domain.ItemCompleted,
		domain.ItemSkipped, domain.ItemSkipped,
		domain.ItemFailed, domain.ItemCancelled,
		domain.ItemDownloading, domain.ItemQueued,
	}
	for i, status := range statuses {
		item := domain.JobItem{ID: fmt.Sprintf("item-%d", i), JobID: job.ID, AdamID: "1", Kind: "song", Index: i + 1, Status: status, CreatedAt: now, UpdatedAt: now}
		if err := store.CreateItem(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	// Another job's items must not leak into this job's counters.
	if err := store.CreateItem(ctx, domain.JobItem{ID: "other", JobID: "job-other", AdamID: "2", Kind: "song", Index: 1, Status: domain.ItemCompleted, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	check := func(source string, got domain.Job) {
		t.Helper()
		if got.TotalItems != 9 || got.CompletedItems != 3 || got.SkippedItems != 2 || got.FailedItems != 1 || got.CancelledItems != 1 {
			t.Fatalf("%s counters = total %d completed %d skipped %d failed %d cancelled %d, want 9/3/2/1/1",
				source, got.TotalItems, got.CompletedItems, got.SkippedItems, got.FailedItems, got.CancelledItems)
		}
	}
	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	check("GetJob", got)
	listed, _, err := store.ListJobs(ctx, JobListFilter{})
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListJobs = %+v, %v", listed, err)
	}
	check("ListJobs", listed[0])
	recoverable, err := store.ListRecoverableJobs(ctx)
	if err != nil || len(recoverable) != 1 {
		t.Fatalf("ListRecoverableJobs = %+v, %v", recoverable, err)
	}
	check("ListRecoverableJobs", recoverable[0])
	active, found, err := store.FindActiveJobByKey(ctx, job.CanonicalKey)
	if err != nil || !found {
		t.Fatalf("FindActiveJobByKey = %v, %v", found, err)
	}
	check("FindActiveJobByKey", active)

	// Writing a job never stores counters: a stale in-memory value is ignored.
	got.CompletedItems, got.FailedItems = 0, 7
	if err := store.UpdateJob(ctx, got); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	check("GetJob after UpdateJob", got)
}

func TestJobTimingRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	created := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	job := domain.Job{ID: "job-timing", Input: "song|us|1", Type: "song", CanonicalKey: "song:us:1", Status: domain.JobQueued, CreatedAt: created, UpdatedAt: created}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StartedAt != nil || got.FinishedAt != nil {
		t.Fatalf("new job timing = %v / %v, want both nil", got.StartedAt, got.FinishedAt)
	}

	started := created.Add(1500 * time.Millisecond)
	finished := created.Add(time.Minute)
	got.Status, got.StartedAt, got.FinishedAt = domain.JobFailed, &started, &finished
	if err := store.UpdateJob(ctx, got); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StartedAt == nil || !got.StartedAt.Equal(started) || got.FinishedAt == nil || !got.FinishedAt.Equal(finished) {
		t.Fatalf("timing = %v / %v, want %v / %v", got.StartedAt, got.FinishedAt, started, finished)
	}

	got.FinishedAt = nil
	if err := store.UpdateJob(ctx, got); err != nil {
		t.Fatal(err)
	}
	var finishedCol sql.NullString
	if err := store.db.QueryRowContext(ctx, `SELECT finished_at FROM runs WHERE id=?`, job.ID).Scan(&finishedCol); err != nil {
		t.Fatal(err)
	}
	if finishedCol.Valid {
		t.Fatalf("cleared finished_at stored as %q, want NULL", finishedCol.String)
	}
}

func TestFinalizeJobPersistsStatusAndEventTogether(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	job := domain.Job{ID: "job-finalize", Input: "song|us|1", Type: "song", Status: domain.JobRunning, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}

	for _, ev := range []domain.Event{
		{JobID: job.ID, Type: "job_started"},
		{JobID: job.ID, ItemID: "item-1", Type: "item_progress", Phase: "downloading"},
		{JobID: job.ID, ItemID: "item-1", Type: "item_progress", Phase: "decrypting"},
		{JobID: job.ID, ItemID: "item-1", Type: "item_completed"},
		{JobID: "job-other", ItemID: "item-2", Type: "item_progress", Phase: "downloading"},
	} {
		if _, err := store.AddEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	finishedAt := now.Add(time.Second)
	job.Status = domain.JobCompleted
	job.UpdatedAt = finishedAt
	job.FinishedAt = &finishedAt
	ev := domain.Event{JobID: job.ID, Type: "job_finished", Message: string(job.Status)}
	stored, err := store.FinalizeJob(ctx, job, ev)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID == 0 {
		t.Fatal("FinalizeJob did not assign an event id")
	}

	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobCompleted {
		t.Fatalf("job status = %s, want %s", got.Status, domain.JobCompleted)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(finishedAt) {
		t.Fatalf("finished_at = %v, want %v", got.FinishedAt, finishedAt)
	}
	events, err := store.ListEventsAfterLimit(ctx, job.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	// item_progress is live telemetry and is pruned in the same transaction;
	// milestone events and other jobs' progress stay.
	want := []domain.EventType{"job_started", "item_completed", "job_finished"}
	if len(events) != len(want) {
		t.Fatalf("events after FinalizeJob = %+v, want types %v", events, want)
	}
	for i := range want {
		if events[i].Type != want[i] {
			t.Fatalf("events after FinalizeJob = %+v, want types %v", events, want)
		}
	}
	other, err := store.ListEventsAfterLimit(ctx, "job-other", 0, 0)
	if err != nil || len(other) != 1 {
		t.Fatalf("other job's progress events = %+v, %v; want the one kept", other, err)
	}
}

func TestArtworkURLRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	job := domain.Job{
		ID: "job-art", Input: "https://music.apple.com/cn/album/test/1", Type: "album", Storefront: "cn",
		Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}

	// Artwork is written back after the input resolves, via UpdateJob.
	job.ArtworkURL = "https://is1-ssl.mzstatic.com/image/thumb/Music/album/{w}x{h}bb.jpg"
	job.Status = domain.JobRunning
	if err := store.UpdateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtworkURL != job.ArtworkURL {
		t.Fatalf("job artwork_url = %q, want %q", got.ArtworkURL, job.ArtworkURL)
	}
	listed, total, err := store.ListJobs(ctx, JobListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if len(listed) != 1 || listed[0].ArtworkURL != job.ArtworkURL {
		t.Fatalf("listed job artwork_url = %+v, want %q", listed, job.ArtworkURL)
	}

	item := domain.JobItem{
		ID: "item-art", JobID: job.ID, AdamID: "123", Kind: "song", Index: 1,
		ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music/track/{w}x{h}bb.jpg",
		Status:     domain.ItemQueued, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	item.ArtworkURL = "https://is1-ssl.mzstatic.com/image/thumb/Music/track-refreshed/{w}x{h}bb.jpg"
	if err := store.UpdateItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ArtworkURL != item.ArtworkURL {
		t.Fatalf("item artwork_url = %+v, want %q", items, item.ArtworkURL)
	}
}

func TestJobDisplayMetadataRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	job := domain.Job{
		ID: "job-meta", Input: "https://music.apple.com/cn/album/test/1", Type: "album", Storefront: "cn",
		Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}

	// Display metadata is written back after the input resolves, via UpdateJob.
	job.ArtistName = "Album Artist"
	job.ArtistURL = "https://music.apple.com/cn/artist/album-artist/1"
	job.CuratorName = "Curator"
	job.ReleaseDate = "2024-06-01"
	job.Genre = "Pop"
	job.ArtworkBgColor = "1a1a1a"
	job.ArtworkTextColor1 = "ffffff"
	job.ArtworkTextColor2 = "eeeeee"
	job.ArtworkTextColor3 = "cccccc"
	job.ArtworkTextColor4 = "aaaaaa"
	job.Status = domain.JobRunning
	if err := store.UpdateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	assertMeta := func(where string, got domain.Job) {
		t.Helper()
		if got.ArtistURL != job.ArtistURL {
			t.Fatalf("%s artist url = %q, want %q", where, got.ArtistURL, job.ArtistURL)
		}
		if got.ArtistName != job.ArtistName || got.CuratorName != job.CuratorName || got.ReleaseDate != job.ReleaseDate || got.Genre != job.Genre {
			t.Fatalf("%s metadata = %q/%q/%q/%q, want %q/%q/%q/%q", where,
				got.ArtistName, got.CuratorName, got.ReleaseDate, got.Genre,
				job.ArtistName, job.CuratorName, job.ReleaseDate, job.Genre)
		}
		if got.ArtworkBgColor != job.ArtworkBgColor || got.ArtworkTextColor1 != job.ArtworkTextColor1 ||
			got.ArtworkTextColor2 != job.ArtworkTextColor2 || got.ArtworkTextColor3 != job.ArtworkTextColor3 ||
			got.ArtworkTextColor4 != job.ArtworkTextColor4 {
			t.Fatalf("%s artwork colors = %q/%q/%q/%q/%q, want %q/%q/%q/%q/%q", where,
				got.ArtworkBgColor, got.ArtworkTextColor1, got.ArtworkTextColor2, got.ArtworkTextColor3, got.ArtworkTextColor4,
				job.ArtworkBgColor, job.ArtworkTextColor1, job.ArtworkTextColor2, job.ArtworkTextColor3, job.ArtworkTextColor4)
		}
	}
	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertMeta("GetJob", got)
	listed, total, err := store.ListJobs(ctx, JobListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(listed) != 1 {
		t.Fatalf("listed = %+v (total %d), want exactly the created job", listed, total)
	}
	assertMeta("ListJobs", listed[0])
	recoverable, err := store.ListRecoverableJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(recoverable) != 1 {
		t.Fatalf("recoverable = %+v, want the running job", recoverable)
	}
	assertMeta("ListRecoverableJobs", recoverable[0])
	active, ok, err := store.FindActiveJobByKey(ctx, job.CanonicalKey)
	if err != nil || !ok {
		t.Fatalf("FindActiveJobByKey ok=%v err=%v, want the active job", ok, err)
	}
	assertMeta("FindActiveJobByKey", active)
}

// TestCreateJobEnforcesActiveKeyUniqueness exercises idx_runs_active_key
// directly. It is the backstop under the manager's dedup lookup — the thing
// that closes the race between two concurrent submits of the same key — so it
// has to keep rejecting a genuine duplicate no matter what goes into the key,
// and it has to keep ignoring terminal rows.
func TestCreateJobEnforcesActiveKeyUniqueness(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	// A key carrying a destination segment, as written since destinations
	// joined the key.
	const key = "album:us:111:0123456789ab"
	first := domain.Job{ID: "job-1", Input: "album|us|111", Type: "album", Storefront: "us", CanonicalKey: key, Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateJob(ctx, first); err != nil {
		t.Fatal(err)
	}
	dup := first
	dup.ID = "job-2"
	if err := store.CreateJob(ctx, dup); !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("duplicate queued job err = %v, want ErrDuplicateActive", err)
	}
	// Running counts as active too.
	if err := store.UpdateJobStatus(ctx, first.ID, domain.JobRunning, now); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateJob(ctx, dup); !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("duplicate against a running job err = %v, want ErrDuplicateActive", err)
	}
	// The index is partial: once the first job is terminal the same key is
	// free again.
	if err := store.UpdateJobStatus(ctx, first.ID, domain.JobCompleted, now); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateJob(ctx, dup); err != nil {
		t.Fatalf("resubmit after completion: %v", err)
	}
	// A different destination is a different key, so it is not blocked even
	// while job-2 is active.
	other := dup
	other.ID, other.CanonicalKey = "job-3", "album:us:111:ba9876543210"
	if err := store.CreateJob(ctx, other); err != nil {
		t.Fatalf("same album to a different destination: %v", err)
	}
}

func TestListRecoverableJobsListsRunningThenQueuedInRunOrder(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	base := time.Now().UTC()
	jobs := []domain.Job{
		{ID: "completed", Input: "https://music.apple.com/cn/song/completed/1", Type: "song", Storefront: "cn", CanonicalKey: "song:cn:1", Status: domain.JobCompleted, CreatedAt: base, UpdatedAt: base},
		{ID: "queued", Input: "https://music.apple.com/cn/song/queued/2", Type: "song", Storefront: "cn", CanonicalKey: "song:cn:2", Status: domain.JobQueued, CreatedAt: base.Add(time.Second), UpdatedAt: base.Add(time.Second)},
		{ID: "running", Input: "https://music.apple.com/cn/song/running/3", Type: "song", Storefront: "cn", CanonicalKey: "song:cn:3", Status: domain.JobRunning, CreatedAt: base.Add(2 * time.Second), UpdatedAt: base.Add(2 * time.Second)},
		{ID: "failed", Input: "https://music.apple.com/cn/song/failed/4", Type: "song", Storefront: "cn", CanonicalKey: "song:cn:4", Status: domain.JobFailed, CreatedAt: base.Add(3 * time.Second), UpdatedAt: base.Add(3 * time.Second)},
		{ID: "cancelled", Input: "https://music.apple.com/cn/song/cancelled/5", Type: "song", Storefront: "cn", CanonicalKey: "song:cn:5", Status: domain.JobCancelled, CreatedAt: base.Add(4 * time.Second), UpdatedAt: base.Add(4 * time.Second)},
	}
	for _, job := range jobs {
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}

	got, err := store.ListRecoverableJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d recoverable jobs, want 2: %+v", len(got), got)
	}
	// The running job was ahead of every queued one when it started.
	if got[0].ID != "running" || got[1].ID != "queued" {
		t.Fatalf("recoverable job order/ids = [%s, %s], want [running, queued]", got[0].ID, got[1].ID)
	}
}

func TestDeleteJobPersistsOverviewTombstone(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	job := domain.Job{ID: "job1", Input: "song|us|1", Type: "song", CanonicalKey: "song:us:1", Status: domain.JobCompleted}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	finished, err := store.AddEvent(ctx, domain.Event{JobID: job.ID, Type: "job_finished"})
	if err != nil {
		t.Fatal(err)
	}
	tombstone, err := store.DeleteJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tombstone.Type != "job_deleted" || tombstone.JobID != job.ID || tombstone.ID <= finished.ID {
		t.Fatalf("tombstone = %+v, want job_deleted for %s after event %d", tombstone, job.ID, finished.ID)
	}

	events, err := store.ListMilestoneEventsAfterLimit(ctx, finished.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "job_deleted" || events[0].ID != tombstone.ID {
		t.Fatalf("ListMilestoneEventsAfterLimit(%d) = %+v, want tombstone %d", finished.ID, events, tombstone.ID)
	}
	if _, err := store.GetJob(ctx, job.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetJob after delete err = %v, want sql.ErrNoRows", err)
	}
}
