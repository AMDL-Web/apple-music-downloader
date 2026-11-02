package db

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"amdl/internal/domain"
)

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
		{ID: "j3", Input: "https://music.apple.com/jp/playlist/three/3", Type: "playlist", Storefront: "jp", Title: "Gamma Playlist", ArtistName: "Omega Quartet", CanonicalKey: "playlist|jp|3", Status: domain.JobRunning, CreatedAt: base.Add(2 * time.Hour), UpdatedAt: base.Add(4 * time.Hour)},
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
	if listed[0].CompletedItems != 1 || listed[0].FailedItems != 1 {
		t.Fatalf("j3 progress completed=%d failed=%d, want 1/1", listed[0].CompletedItems, listed[0].FailedItems)
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

	listed, total, err = store.ListJobs(ctx, JobListFilter{Types: []string{"song", "artist"}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(listed) != 2 {
		t.Fatalf("type filter = ids=%v total=%d, want the song and the artist", idsOf(listed), total)
	}
	counts, err = store.CountJobsByStatus(ctx, JobListFilter{Types: []string{"song", "artist"}})
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

	listed, total, err = store.ListJobs(ctx, JobListFilter{Query: "omega", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || listed[0].ID != "j3" {
		t.Fatalf("q=omega (an artist name) = ids=%v total=%d, want [j3]", idsOf(listed), total)
	}

	_, total, err = store.ListJobs(ctx, JobListFilter{Query: "%", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("literal %% query matched %d jobs, want 0", total)
	}

	listed, total, err = store.ListJobs(ctx, JobListFilter{Sort: JobListSortUpdatedAt, Order: JobListOrderAsc, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || idsOf(listed)[0] != "j4" || idsOf(listed)[3] != "j3" {
		t.Fatalf("sort updated_at asc = %v, want j4 ... j3", idsOf(listed))
	}
}

// TestListJobsOrdersFractionalTimes guards the lexicographic TEXT compare:
// timestamps are stored fixed-width, so ORDER BY keeps chronological order
// across whole-second and sub-second rows.
func TestListJobsOrdersFractionalTimes(t *testing.T) {
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
	if err := store.db.QueryRowContext(ctx, `SELECT created_at FROM runs WHERE id='frac'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	wantStored := "2024-07-10T12:00:00.123000000Z"
	if stored != wantStored {
		t.Fatalf("stored created_at = %q, want fixed-width %q", stored, wantStored)
	}

	listed, total, err := store.ListJobs(ctx, JobListFilter{Sort: JobListSortCreatedAt, Order: JobListOrderAsc, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || idsOf(listed)[0] != "exact" || idsOf(listed)[1] != "frac" || idsOf(listed)[2] != "next" {
		t.Fatalf("created_at asc = ids=%v total=%d, want [exact frac next]", idsOf(listed), total)
	}
}

func idsOf(jobs []domain.Job) []string {
	out := make([]string, len(jobs))
	for i, job := range jobs {
		out[i] = job.ID
	}
	return out
}

// Jobs submitted together share created_at and updated_at and have random ids,
// so the list must break the tie by insert order: a batch reads in submission
// order, and pages cut inside it neither skip nor repeat a job.
func TestListJobsBatchSharingTimestampKeepsSubmissionOrderAcrossPages(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	// Ids that sort against insert order, so an id tie-break would fail.
	ids := []string{"job_e", "job_c", "job_a", "job_d", "job_b"}
	for i, id := range ids {
		job := domain.Job{ID: id, Input: "in", Type: "song", Storefront: "us", CanonicalKey: "song|us|" + id, Status: domain.JobQueued, CreatedAt: base, UpdatedAt: base}
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
		// A later batch, one hour on, to check batches still order by time.
		later := domain.Job{ID: "later_" + strconv.Itoa(i), Input: "in", Type: "song", Storefront: "us", CanonicalKey: "song|us|later" + strconv.Itoa(i), Status: domain.JobQueued, CreatedAt: base.Add(time.Hour), UpdatedAt: base.Add(time.Hour)}
		if i < 2 {
			if err := store.CreateJob(ctx, later); err != nil {
				t.Fatal(err)
			}
		}
	}

	walk := func(filter JobListFilter) []string {
		t.Helper()
		var got []string
		for offset := 0; ; offset += 2 {
			filter.Limit, filter.Offset = 2, offset
			page, total, err := store.ListJobs(ctx, filter)
			if err != nil {
				t.Fatal(err)
			}
			if total != 7 {
				t.Fatalf("total = %d, want 7", total)
			}
			if len(page) == 0 {
				return got
			}
			for _, j := range page {
				got = append(got, j.ID)
			}
		}
	}
	batch := []string{"job_e", "job_c", "job_a", "job_d", "job_b"}
	later := []string{"later_0", "later_1"}
	for _, tc := range []struct {
		name   string
		filter JobListFilter
		want   []string
	}{
		{"created_at desc", JobListFilter{Sort: JobListSortCreatedAt, Order: JobListOrderDesc}, append(append([]string{}, later...), batch...)},
		{"created_at asc", JobListFilter{Sort: JobListSortCreatedAt, Order: JobListOrderAsc}, append(append([]string{}, batch...), later...)},
		{"updated_at desc", JobListFilter{Sort: JobListSortUpdatedAt, Order: JobListOrderDesc}, append(append([]string{}, later...), batch...)},
	} {
		got := walk(tc.filter)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s: paged list = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Current leaves out a failed run that a newer run of its record (or its
// station link) replaced, and a run of a record that is now complete.
func TestListJobsCurrentLeavesOutReplacedRuns(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	if _, err := store.AddAlbum(ctx, AlbumAdd{ID: "B", WantedAll: true, Source: domain.SourceManual, Now: base}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE albums SET state = 'complete' WHERE id = 'B'`); err != nil {
		t.Fatal(err)
	}
	run := func(id string, kind domain.ResourceKind, rid, key string, status domain.JobStatus, at int) domain.Job {
		created := base.Add(time.Duration(at) * time.Minute)
		return domain.Job{ID: id, Input: "x", Type: "album", Status: status, ResourceKind: kind, ResourceID: rid, CanonicalKey: key, CreatedAt: created, UpdatedAt: created}
	}
	for _, job := range []domain.Job{
		run("a1", domain.ResourceAlbum, "A", "", domain.JobFailed, 1),
		run("a2", domain.ResourceAlbum, "A", "", domain.JobFailed, 2),
		run("b1", domain.ResourceAlbum, "B", "", domain.JobFailed, 3),
		run("c1", domain.ResourceAlbum, "C", "", domain.JobFailed, 4),
		run("s1", "", "", "station|us|9", domain.JobFailed, 5),
		run("s2", "", "", "station|us|9", domain.JobQueued, 6),
		run("s3", "", "", "station|us|8", domain.JobFailed, 7),
	} {
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	filter := JobListFilter{Statuses: []domain.JobStatus{domain.JobFailed}, Current: true}
	listed, total, err := store.ListJobs(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(idsOf(listed), ","); got != "s3,c1,a2" || total != 3 {
		t.Fatalf("current failed runs = %s (total %d), want s3,c1,a2", got, total)
	}
	counts, err := store.CountJobsByStatus(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if counts[domain.JobFailed] != 3 {
		t.Fatalf("failed count = %d, want 3", counts[domain.JobFailed])
	}
}
