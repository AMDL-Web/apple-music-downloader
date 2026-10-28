package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"amdl/internal/domain"
)

func TestJobForceRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	want := domain.Job{
		ID: "job-force", Input: "https://music.apple.com/cn/song/test/1", Type: "song", Storefront: "cn",
		Force: true, Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetJob(context.Background(), want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Force {
		t.Fatal("Force was not persisted")
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

	job.Status = domain.JobCompleted
	job.UpdatedAt = now.Add(time.Second)
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
	events, err := store.ListEventsAfter(ctx, job.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "job_finished" {
		t.Fatalf("events after FinalizeJob = %+v, want a single job_finished event", events)
	}
}

func TestUpdateJobStatusPreservesCountsAndError(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	job := domain.Job{
		ID: "job-status", Input: "https://music.apple.com/cn/artist/example/1", Type: "artist", Storefront: "cn",
		Status: domain.JobRunning, TotalItems: 4, DoneItems: 2, FailedItems: 1, Error: "partial", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}

	if err := store.UpdateJobStatus(ctx, job.ID, domain.JobCancelled, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobCancelled {
		t.Fatalf("status = %s, want %s", got.Status, domain.JobCancelled)
	}
	if got.TotalItems != 4 || got.DoneItems != 2 || got.FailedItems != 1 || got.Error != "partial" {
		t.Fatalf("job fields were overwritten: %+v", got)
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

// TestCreateJobEnforcesActiveKeyUniqueness exercises idx_jobs_active_key
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

func TestListRecoverableJobsOnlyReturnsQueuedAndRunning(t *testing.T) {
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
	if got[0].ID != "queued" || got[1].ID != "running" {
		t.Fatalf("recoverable job order/ids = [%s, %s], want [queued, running]", got[0].ID, got[1].ID)
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
	if tombstone.Type != domain.EventDeleted || tombstone.JobID != job.ID || tombstone.ID <= finished.ID {
		t.Fatalf("tombstone = %+v, want job_deleted for %s after event %d", tombstone, job.ID, finished.ID)
	}

	events, err := store.ListMilestoneEventsAfter(ctx, finished.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != domain.EventDeleted || events[0].ID != tombstone.ID {
		t.Fatalf("ListMilestoneEventsAfter(%d) = %+v, want tombstone %d", finished.ID, events, tombstone.ID)
	}
	if _, err := store.GetJob(ctx, job.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetJob after delete err = %v, want sql.ErrNoRows", err)
	}
}

func TestListJobsFilterPaginationAndSort(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	jobs := []domain.Job{
		{ID: "j1", Input: "https://music.apple.com/us/song/one/1", Type: "song", Storefront: "us", Title: "Alpha Song", CanonicalKey: "song|us|1", Status: domain.JobCompleted, CreatedAt: base, UpdatedAt: base.Add(3 * time.Hour)},
		{ID: "j2", Input: "https://music.apple.com/cn/album/two/2", Type: "album", Storefront: "cn", Title: "Beta Album", CanonicalKey: "album|cn|2", Status: domain.JobFailed, CreatedAt: base.Add(1 * time.Hour), UpdatedAt: base.Add(2 * time.Hour)},
		{ID: "j3", Input: "https://music.apple.com/jp/playlist/three/3", Type: "playlist", Storefront: "jp", Title: "Gamma Playlist", CanonicalKey: "playlist|jp|3", Status: domain.JobRunning, CreatedAt: base.Add(2 * time.Hour), UpdatedAt: base.Add(4 * time.Hour)},
		{ID: "j4", Input: "https://music.apple.com/us/artist/four/4", Type: "artist", Storefront: "us", Title: "Delta Artist", CanonicalKey: "artist|us|4", Status: domain.JobQueued, CreatedAt: base.Add(3 * time.Hour), UpdatedAt: base.Add(1 * time.Hour)},
	}
	for _, job := range jobs {
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CreateItem(ctx, domain.JobItem{
		ID: "i1", JobID: "j3", AdamID: "1", Kind: "song", Index: 1,
		Status: domain.ItemCompleted, CreatedAt: base, UpdatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateItem(ctx, domain.JobItem{
		ID: "i2", JobID: "j3", AdamID: "2", Kind: "song", Index: 2,
		Status: domain.ItemFailed, CreatedAt: base, UpdatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}

	listed, total, err := store.ListJobs(ctx, JobListFilter{Limit: 2, Offset: 1, Sort: JobListSortCreatedAt, Order: JobListOrderDesc})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
	if len(listed) != 2 || listed[0].ID != "j3" || listed[1].ID != "j2" {
		t.Fatalf("page = %+v, want j3 then j2", idsOf(listed))
	}
	if listed[0].DoneItems != 1 || listed[0].FailedItems != 1 {
		t.Fatalf("j3 progress done=%d failed=%d, want 1/1", listed[0].DoneItems, listed[0].FailedItems)
	}
	counts, err := store.CountJobsByStatus(ctx, JobListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for status, want := range map[domain.JobStatus]int{
		domain.JobQueued: 1, domain.JobRunning: 1, domain.JobCompleted: 1, domain.JobFailed: 1, domain.JobCancelled: 0,
	} {
		if got := counts[status]; got != want {
			t.Fatalf("status count %s = %d, want %d", status, got, want)
		}
	}

	listed, total, err = store.ListJobs(ctx, JobListFilter{
		Statuses: []domain.JobStatus{domain.JobFailed, domain.JobCancelled},
		Limit:    50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(listed) != 1 || listed[0].ID != "j2" {
		t.Fatalf("status filter = ids=%v total=%d, want [j2]/1", idsOf(listed), total)
	}

	listed, total, err = store.ListJobs(ctx, JobListFilter{Types: []string{"song", "artist"}, Storefront: "us", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(listed) != 2 {
		t.Fatalf("type+storefront = ids=%v total=%d, want 2 us song/artist", idsOf(listed), total)
	}
	counts, err = store.CountJobsByStatus(ctx, JobListFilter{Types: []string{"song", "artist"}, Storefront: "us"})
	if err != nil {
		t.Fatal(err)
	}
	if counts[domain.JobCompleted] != 1 || counts[domain.JobQueued] != 1 || len(counts) != 2 {
		t.Fatalf("filtered status counts = %+v, want completed=1 queued=1", counts)
	}

	listed, total, err = store.ListJobs(ctx, JobListFilter{Query: "beta", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || listed[0].ID != "j2" {
		t.Fatalf("q=beta = ids=%v total=%d, want [j2]", idsOf(listed), total)
	}

	listed, total, err = store.ListJobs(ctx, JobListFilter{Query: "%", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("literal %% query matched %d jobs, want 0", total)
	}

	after := base.Add(90 * time.Minute)
	before := base.Add(150 * time.Minute)
	listed, total, err = store.ListJobs(ctx, JobListFilter{CreatedAfter: &after, CreatedBefore: &before, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || listed[0].ID != "j3" {
		t.Fatalf("created window = ids=%v total=%d, want [j3]", idsOf(listed), total)
	}

	listed, total, err = store.ListJobs(ctx, JobListFilter{Sort: JobListSortUpdatedAt, Order: JobListOrderAsc, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || idsOf(listed)[0] != "j4" || idsOf(listed)[3] != "j3" {
		t.Fatalf("sort updated_at asc = %v, want j4 ... j3", idsOf(listed))
	}
}

// TestListJobsWholeSecondBoundaryWithFractionalTimes guards the lexicographic
// TEXT compare bug: a whole-second filter like created_after=...T12:00:00Z must
// still include rows stored with sub-second fractions in that same second, and
// ORDER BY must keep chronological order across whole-second vs fractional rows.
func TestListJobsWholeSecondBoundaryWithFractionalTimes(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	sec := time.Date(2024, 7, 10, 12, 0, 0, 0, time.UTC)
	jobs := []domain.Job{
		{ID: "exact", Input: "song|us|exact", Type: "song", CanonicalKey: "song|us|exact", Status: domain.JobCompleted, CreatedAt: sec, UpdatedAt: sec},
		{ID: "frac", Input: "song|us|frac", Type: "song", CanonicalKey: "song|us|frac", Status: domain.JobCompleted, CreatedAt: sec.Add(123 * time.Millisecond), UpdatedAt: sec.Add(123 * time.Millisecond)},
		{ID: "next", Input: "song|us|next", Type: "song", CanonicalKey: "song|us|next", Status: domain.JobCompleted, CreatedAt: sec.Add(time.Second), UpdatedAt: sec.Add(time.Second)},
	}
	for _, job := range jobs {
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}

	// Persist via CreateJob uses formatTime; assert fixed-width storage.
	var stored string
	if err := store.db.QueryRowContext(ctx, `SELECT created_at FROM jobs WHERE id='frac'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	wantStored := "2024-07-10T12:00:00.123000000Z"
	if stored != wantStored {
		t.Fatalf("stored created_at = %q, want fixed-width %q", stored, wantStored)
	}

	after := sec
	listed, total, err := store.ListJobs(ctx, JobListFilter{CreatedAfter: &after, Sort: JobListSortCreatedAt, Order: JobListOrderAsc, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || idsOf(listed)[0] != "exact" || idsOf(listed)[1] != "frac" || idsOf(listed)[2] != "next" {
		t.Fatalf("created_after whole-second = ids=%v total=%d, want [exact frac next]", idsOf(listed), total)
	}

	before := sec
	listed, total, err = store.ListJobs(ctx, JobListFilter{CreatedBefore: &before, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || listed[0].ID != "exact" {
		t.Fatalf("created_before whole-second = ids=%v total=%d, want [exact] only (frac must be excluded)", idsOf(listed), total)
	}
}

func idsOf(jobs []domain.Job) []string {
	out := make([]string, len(jobs))
	for i, job := range jobs {
		out[i] = job.ID
	}
	return out
}
