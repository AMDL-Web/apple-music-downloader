package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
)

type statsBody struct {
	GeneratedAt time.Time `json:"generated_at"`
	Days        int       `json:"days"`
	Totals      struct {
		Jobs            int   `json:"jobs"`
		TracksCompleted int   `json:"tracks_completed"`
		TracksSkipped   int   `json:"tracks_skipped"`
		TracksFailed    int   `json:"tracks_failed"`
		BytesWritten    int64 `json:"bytes_written"`
		DurationMS      int64 `json:"duration_ms"`
	} `json:"totals"`
	Daily []struct {
		Date            string `json:"date"`
		TracksCompleted int    `json:"tracks_completed"`
		BytesWritten    int64  `json:"bytes_written"`
		JobsCompleted   int    `json:"jobs_completed"`
		JobsFailed      int    `json:"jobs_failed"`
	} `json:"daily"`
	Codecs []struct {
		Codec      string `json:"codec"`
		BitDepth   int    `json:"bit_depth"`
		SampleRate int    `json:"sample_rate"`
		Tracks     int    `json:"tracks"`
	} `json:"codecs"`
	Hooks []struct {
		Name         string     `json:"name"`
		Succeeded    int        `json:"succeeded"`
		Failed       int        `json:"failed"`
		LastFailedAt *time.Time `json:"last_failed_at"`
	} `json:"hooks"`
}

func getStats(t *testing.T, server *Server, query string) statsBody {
	t.Helper()
	rec := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/stats"+query, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats%s = %d, body %s", query, rec.Code, rec.Body.String())
	}
	var body statsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

// seedCompletedTrack adds a completed item of jobID with its item_completed
// event at completedAt.
func seedCompletedTrack(t *testing.T, store *db.Store, jobID, itemID string, size int64, completedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	item := domain.JobItem{
		ID: itemID, JobID: jobID, AdamID: itemID, Kind: "song", Status: domain.ItemCompleted,
		FileSize: size, DurationMS: 200000, Codec: "alac", BitDepth: 24, SampleRate: 48000,
		CreatedAt: completedAt, UpdatedAt: completedAt,
	}
	if err := store.CreateItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddEvent(ctx, domain.Event{JobID: jobID, ItemID: itemID, Type: domain.EventItemCompleted, CreatedAt: completedAt}); err != nil {
		t.Fatal(err)
	}
}

func TestStatsEndpoint(t *testing.T) {
	server := newTestServerWithManager(t)
	now := time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC)
	server.now = func() time.Time { return now }
	ctx := context.Background()
	job := domain.Job{ID: "job_stats", Input: "album|us|1", Type: "album", CanonicalKey: "album:us:1", Status: domain.JobCompleted, CreatedAt: now, UpdatedAt: now}
	if err := server.store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	seedCompletedTrack(t, server.store, job.ID, "item_1", 1000, now.Add(-48*time.Hour))
	seedCompletedTrack(t, server.store, job.ID, "item_2", 500, now.Add(-time.Hour))
	failedAt := now.Add(-30 * time.Minute)
	for _, ev := range []domain.Event{
		{JobID: job.ID, Type: domain.EventJobFinished, CreatedAt: now.Add(-50 * time.Minute)},
		{JobID: job.ID, Type: domain.EventHookSucceeded, Phase: "notify", CreatedAt: now.Add(-40 * time.Minute)},
		{JobID: job.ID, Type: domain.EventHookFailed, Phase: "notify", CreatedAt: failedAt},
	} {
		if _, err := server.store.AddEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	got := getStats(t, server, "?days=3")
	if got.Days != 3 || !got.GeneratedAt.Equal(now) {
		t.Fatalf("days %d, generated_at %v; want 3 and %v", got.Days, got.GeneratedAt, now)
	}
	if got.Totals.Jobs != 1 || got.Totals.TracksCompleted != 2 || got.Totals.BytesWritten != 1500 || got.Totals.DurationMS != 400000 {
		t.Fatalf("totals = %+v", got.Totals)
	}
	if len(got.Daily) != 3 {
		t.Fatalf("daily has %d entries, want 3: %+v", len(got.Daily), got.Daily)
	}
	type day struct {
		date           string
		tracks         int
		bytes          int64
		completed, bad int
	}
	want := []day{{"2026-10-26", 1, 1000, 0, 0}, {"2026-10-27", 0, 0, 0, 0}, {"2026-10-28", 1, 500, 1, 0}}
	for i, w := range want {
		d := got.Daily[i]
		if (day{d.Date, d.TracksCompleted, d.BytesWritten, d.JobsCompleted, d.JobsFailed}) != w {
			t.Errorf("daily[%d] = %+v, want %+v", i, d, w)
		}
	}
	if len(got.Codecs) != 1 || got.Codecs[0].Codec != "alac" || got.Codecs[0].BitDepth != 24 || got.Codecs[0].SampleRate != 48000 || got.Codecs[0].Tracks != 2 {
		t.Errorf("codecs = %+v", got.Codecs)
	}
	if len(got.Hooks) != 1 || got.Hooks[0].Name != "notify" || got.Hooks[0].Succeeded != 1 || got.Hooks[0].Failed != 1 ||
		got.Hooks[0].LastFailedAt == nil || !got.Hooks[0].LastFailedAt.Equal(failedAt) {
		t.Errorf("hooks = %+v", got.Hooks)
	}

	if def := getStats(t, server, ""); def.Days != 14 || len(def.Daily) != 14 || def.Daily[13].Date != "2026-10-28" || def.Daily[0].Date != "2026-10-15" {
		t.Errorf("default window: days %d, %d entries from %v", def.Days, len(def.Daily), def.Daily)
	}
}

func TestStatsEndpointRejectsDays(t *testing.T) {
	server := newTestServerWithManager(t)
	for _, query := range []string{"?days=0", "?days=91", "?days=-1", "?days=abc", "?days=1.5", "?days=1e1"} {
		rec := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/stats"+query, "")
		assertErrorCode(t, rec, http.StatusBadRequest, CodeInvalidRequest)
	}
	for _, query := range []string{"?days=1", "?days=90"} {
		if got := getStats(t, server, query); len(got.Daily) != got.Days {
			t.Errorf("%s: %d daily entries for %d days", query, len(got.Daily), got.Days)
		}
	}
}

func TestStatsEndpointCachesPerDays(t *testing.T) {
	server := newTestServerWithManager(t)
	now := time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC)
	server.now = func() time.Time { return now }
	ctx := context.Background()
	job := domain.Job{ID: "job_cache", Input: "album|us|1", Type: "album", CanonicalKey: "album:us:1", Status: domain.JobRunning, CreatedAt: now, UpdatedAt: now}
	if err := server.store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	seedCompletedTrack(t, server.store, job.ID, "item_1", 100, now.Add(-time.Minute))
	first := getStats(t, server, "?days=7")
	if first.Totals.TracksCompleted != 1 {
		t.Fatalf("first totals = %+v", first.Totals)
	}

	seedCompletedTrack(t, server.store, job.ID, "item_2", 100, now)
	now = now.Add(statsCacheTTL - time.Second)
	if cached := getStats(t, server, "?days=7"); cached.Totals.TracksCompleted != 1 || !cached.GeneratedAt.Equal(first.GeneratedAt) {
		t.Fatalf("within the TTL: totals %+v generated %v; want the cached result from %v", cached.Totals, cached.GeneratedAt, first.GeneratedAt)
	}
	if other := getStats(t, server, "?days=6"); other.Totals.TracksCompleted != 2 {
		t.Fatalf("another days value: totals %+v, want a fresh result", other.Totals)
	}
	now = now.Add(time.Second)
	if fresh := getStats(t, server, "?days=7"); fresh.Totals.TracksCompleted != 2 || !fresh.GeneratedAt.Equal(now) {
		t.Fatalf("after the TTL: totals %+v generated %v; want a fresh result at %v", fresh.Totals, fresh.GeneratedAt, now)
	}
}

func TestStatsEndpointStoreError(t *testing.T) {
	server := newTestServerWithManager(t)
	if err := server.store.Close(); err != nil {
		t.Fatal(err)
	}
	rec := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/stats", "")
	assertErrorCode(t, rec, http.StatusInternalServerError, CodeStoreError)
}
