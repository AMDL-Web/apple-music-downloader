package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"amdl/internal/domain"
)

// The hook payload carries the API's disjoint job counters and timing, so a
// script reads the same numbers as GET /downloads/{id}.
func TestJobPayloadCarriesTheAPICountersAndTiming(t *testing.T) {
	started := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	finished := started.Add(90 * time.Second)
	raw, err := json.Marshal(jobPayload(domain.Job{
		ID: "job_1", Status: domain.JobFailed, TotalItems: 8,
		CompletedItems: 3, SkippedItems: 2, FailedItems: 1, CancelledItems: 2,
		StartedAt: &started, FinishedAt: &finished,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"total_items": 8, "completed_items": 3, "skipped_items": 2, "failed_items": 1, "cancelled_items": 2}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %v, want %v (payload %s)", key, got[key], value, raw)
		}
	}
	if _, ok := got["done_items"]; ok {
		t.Errorf("payload still carries done_items: %s", raw)
	}
	if got["started_at"] != "2026-10-28T12:00:00Z" || got["finished_at"] != "2026-10-28T12:01:30Z" {
		t.Errorf("timing = %v / %v, want RFC 3339 started_at and finished_at", got["started_at"], got["finished_at"])
	}
	if _, ok := got["error"]; ok {
		t.Errorf("empty error is present: %s", raw)
	}

	raw, err = json.Marshal(jobPayload(domain.Job{ID: "job_2", Status: domain.JobQueued}))
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"started_at", "finished_at"} {
		if _, ok := got[key]; ok {
			t.Errorf("queued job payload carries %s: %s", key, raw)
		}
	}
	if got["cancelled_items"] != float64(0) {
		t.Errorf("zero counter missing from %s; counters are always present", raw)
	}
}

func TestItemPayloadOutputPathIsAbsolute(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	absolute := filepath.Join(t.TempDir(), "albums", "A", "01. Song.m4a")
	items := itemPayloads([]domain.JobItem{
		{ID: "relative", OutputPath: filepath.Join("data", "downloads", "albums", "A", "01. Song.m4a")},
		{ID: "absolute", OutputPath: absolute},
		{ID: "none"},
	})
	if want := filepath.Join(wd, "data", "downloads", "albums", "A", "01. Song.m4a"); items[0].OutputPath != want {
		t.Errorf("relative output path = %q, want %q", items[0].OutputPath, want)
	}
	if items[1].OutputPath != absolute {
		t.Errorf("absolute output path = %q, want it unchanged (%q)", items[1].OutputPath, absolute)
	}
	if items[2].OutputPath != "" {
		t.Errorf("missing output path = %q, want empty", items[2].OutputPath)
	}
}
