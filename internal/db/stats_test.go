package db

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"amdl/internal/domain"
)

func TestStats(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	created := at("2026-10-01T00:00:00Z")

	for _, job := range []domain.Job{
		{ID: "job_a", Input: "album|us|1", Type: "album", CanonicalKey: "album:us:1", Status: domain.JobCompleted},
		{ID: "job_b", Input: "album|us|2", Type: "album", CanonicalKey: "album:us:2", Status: domain.JobFailed},
		{ID: "job_c", Input: "album|us|3", Type: "album", CanonicalKey: "album:us:3", Status: domain.JobQueued},
	} {
		job.CreatedAt, job.UpdatedAt = created, created
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []domain.JobItem{
		{ID: "a1", JobID: "job_a", Index: 1, Status: domain.ItemCompleted, FileSize: 100, DurationMS: 1000, Codec: "alac", BitDepth: 24, SampleRate: 96000},
		{ID: "a2", JobID: "job_a", Index: 2, Status: domain.ItemCompleted, FileSize: 50, DurationMS: 2000, Codec: "alac", BitDepth: 24, SampleRate: 96000},
		{ID: "a3", JobID: "job_a", Index: 3, Status: domain.ItemSkipped, FileSize: 999, DurationMS: 9000, Codec: "alac", BitDepth: 24, SampleRate: 96000},
		{ID: "b1", JobID: "job_b", Index: 1, Status: domain.ItemCompleted, FileSize: 30, DurationMS: 500, Codec: "aac", BitDepth: 16, SampleRate: 44100},
		{ID: "b2", JobID: "job_b", Index: 2, Status: domain.ItemFailed, FileSize: 0, DurationMS: 700},
	} {
		item.AdamID, item.Kind, item.CreatedAt, item.UpdatedAt = item.ID, "song", created, created
		if err := store.CreateItem(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	for _, ev := range []domain.Event{
		{JobID: "job_a", ItemID: "a1", Type: domain.EventItemCompleted, CreatedAt: at("2026-10-26T10:00:00Z")},
		// a2 completed twice; only its latest completion counts.
		{JobID: "job_a", ItemID: "a2", Type: domain.EventItemCompleted, CreatedAt: at("2026-10-27T09:00:00Z")},
		{JobID: "job_a", ItemID: "a2", Type: domain.EventItemCompleted, CreatedAt: at("2026-10-28T09:00:00Z")},
		{JobID: "job_a", Type: domain.EventJobFinished, CreatedAt: at("2026-10-28T09:01:00Z")},
		// b1 completed before the window; b2 has a stale completion but failed.
		{JobID: "job_b", ItemID: "b1", Type: domain.EventItemCompleted, CreatedAt: at("2026-10-20T10:00:00Z")},
		{JobID: "job_b", ItemID: "b2", Type: domain.EventItemCompleted, CreatedAt: at("2026-10-26T10:30:00Z")},
		{JobID: "job_b", Type: domain.EventJobFailed, CreatedAt: at("2026-10-26T11:00:00Z")},
		{JobID: "job_b", Type: domain.EventHookStarted, Phase: "notify", CreatedAt: at("2026-10-26T11:00:01Z")},
		{JobID: "job_b", Type: domain.EventHookSucceeded, Phase: "notify", CreatedAt: at("2026-10-26T11:00:02Z")},
		{JobID: "job_b", Type: domain.EventHookFailed, Phase: "notify", CreatedAt: at("2026-10-27T08:00:00Z")},
		{JobID: "job_b", Type: domain.EventHookFailed, Phase: "notify", CreatedAt: at("2026-10-28T07:00:00Z")},
		{JobID: "job_b", Type: domain.EventHookFailed, Phase: "archive", CreatedAt: at("2026-10-01T07:00:00Z")},
		// After the end of the window's last day.
		{JobID: "job_c", Type: domain.EventJobFinished, CreatedAt: at("2026-10-29T00:00:00Z")},
	} {
		if _, err := store.AddEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	got, err := store.Stats(ctx, at("2026-10-28T15:00:00Z"), 4)
	if err != nil {
		t.Fatal(err)
	}
	lastFailed := at("2026-10-28T07:00:00Z")
	want := Stats{
		Totals: StatsTotals{Jobs: 3, TracksCompleted: 3, TracksSkipped: 1, TracksFailed: 1, BytesWritten: 180, DurationMS: 3500},
		Daily: []DailyStats{
			{Date: "2026-10-25"},
			{Date: "2026-10-26", TracksCompleted: 1, BytesWritten: 100, JobsFailed: 1},
			{Date: "2026-10-27"},
			{Date: "2026-10-28", TracksCompleted: 1, BytesWritten: 50, JobsCompleted: 1},
		},
		Codecs: []CodecStats{
			{Codec: "alac", BitDepth: 24, SampleRate: 96000, Tracks: 2},
			{Codec: "aac", BitDepth: 16, SampleRate: 44100, Tracks: 1},
		},
		Hooks: []HookStats{{Name: "notify", Succeeded: 1, Failed: 2, LastFailedAt: &lastFailed}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Stats() =\n%+v\nwant\n%+v", got, want)
	}

	// A one-day window starting at UTC midnight sees only that day.
	got, err = store.Stats(ctx, at("2026-10-26T00:00:00Z"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if wantDaily := []DailyStats{{Date: "2026-10-26", TracksCompleted: 1, BytesWritten: 100, JobsFailed: 1}}; !reflect.DeepEqual(got.Daily, wantDaily) {
		t.Fatalf("one-day Daily = %+v, want %+v", got.Daily, wantDaily)
	}
	if len(got.Hooks) != 1 || got.Hooks[0].Succeeded != 1 || got.Hooks[0].Failed != 0 || got.Hooks[0].LastFailedAt != nil {
		t.Fatalf("one-day Hooks = %+v, want notify with one success and no failure", got.Hooks)
	}
}

func TestStatsEmptyStore(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("UTC+9", 9*3600))
	got, err := store.Stats(context.Background(), now, 3)
	if err != nil {
		t.Fatal(err)
	}
	// 2026-01-02 03:04 at UTC+9 is still 2026-01-01 in UTC.
	want := Stats{
		Daily:  []DailyStats{{Date: "2025-12-30"}, {Date: "2025-12-31"}, {Date: "2026-01-01"}},
		Codecs: []CodecStats{},
		Hooks:  []HookStats{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Stats() = %+v, want %+v", got, want)
	}
	if _, err := store.Stats(context.Background(), now, 0); err == nil {
		t.Fatal("Stats with a zero-day window succeeded, want an error")
	}
}

func TestStatsWindowQueriesUseTypeCreatedIndex(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for name, q := range map[string]struct {
		sql  string
		args []any
	}{
		"daily tracks": {statsDailyTracksSQL, []any{"item_completed", "2026", "2027", "completed"}},
		"daily jobs":   {statsDailyJobsSQL, []any{"job_finished", "job_failed", "2026", "2027"}},
		"hooks":        {statsHooksSQL, []any{"hook_succeeded", "hook_failed", "hook_failed", "hook_succeeded", "hook_failed", "2026", "2027"}},
	} {
		rows, err := store.db.Query(`EXPLAIN QUERY PLAN `+q.sql, q.args...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		if !strings.Contains(strings.Join(plan, "\n"), "idx_job_events_type_created") {
			t.Errorf("%s: query plan %q does not use idx_job_events_type_created", name, plan)
		}
	}
}
