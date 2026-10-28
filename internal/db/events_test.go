package db

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"amdl/internal/domain"
)

func TestListEventsAfterLimitPagesWithoutSkipping(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	job := domain.Job{ID: "job-page", Input: "song|us|1", Type: "song", Status: domain.JobQueued}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := store.AddEvent(ctx, domain.Event{JobID: job.ID, Type: "item_progress"}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ListEventsAfterLimit(ctx, job.ID, 0, 2)
	if err != nil || len(first) != 2 {
		t.Fatalf("first page = (%+v, %v), want 2 events", first, err)
	}
	second, err := store.ListEventsAfterLimit(ctx, job.ID, first[1].ID, 2)
	if err != nil || len(second) != 1 || second[0].ID <= first[1].ID {
		t.Fatalf("second page = (%+v, %v), want final event after cursor", second, err)
	}
}

func TestListMilestoneEventsAfterFiltersAndOrders(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, job := range []domain.Job{
		{ID: "job1", Input: "song|us|1", Type: "song", CanonicalKey: "song:us:1", Status: domain.JobRunning},
		{ID: "job2", Input: "song|us|2", Type: "song", CanonicalKey: "song:us:2", Status: domain.JobRunning},
	} {
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	// Mix milestone and non-milestone events across two jobs.
	for _, ev := range []domain.Event{
		{JobID: "job1", Type: "job_started"},    // milestone
		{JobID: "job1", Type: "codec_selected"}, // not a milestone
		{JobID: "job2", Type: "job_queued"},     // milestone
		{JobID: "job1", Type: "item_completed"}, // milestone
	} {
		if _, err := store.AddEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	got, err := store.ListMilestoneEventsAfterLimit(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantTypes := []domain.EventType{"job_started", "job_queued", "item_completed"}
	if len(got) != len(wantTypes) {
		t.Fatalf("got %d milestone events, want %d: %+v", len(got), len(wantTypes), got)
	}
	for i, ty := range wantTypes {
		if got[i].Type != ty {
			t.Fatalf("milestone[%d].Type = %s, want %s (codec_selected must be filtered out, order by id)", i, got[i].Type, ty)
		}
	}

	// The global cursor is the max id across all jobs.
	last, err := store.LatestGlobalEventID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if last != got[len(got)-1].ID {
		t.Fatalf("LatestGlobalEventID = %d, want %d", last, got[len(got)-1].ID)
	}

	// Resuming from that cursor yields nothing new.
	after, err := store.ListMilestoneEventsAfterLimit(ctx, last, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("ListMilestoneEventsAfter(last) = %+v, want empty", after)
	}
}

func TestEventPayloadRoundTripsAsJSONObject(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.AddEvent(ctx, domain.Event{JobID: "job1", Type: "item_completed", Payload: json.RawMessage(`{"status":"completed","attempts":2}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddEvent(ctx, domain.Event{JobID: "job1", Type: "job_deleted"}); err != nil {
		t.Fatal(err)
	}
	// A row written by hand (or by a broken writer) that is not JSON must not
	// make the event unencodable.
	if _, err := store.db.ExecContext(ctx, `INSERT INTO job_events(job_id,type,payload,created_at) VALUES('job1','item_failed','{broken',?)`, formatTime(now())); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := store.db.QueryRowContext(ctx, `SELECT typeof(payload) || ':' || payload FROM job_events WHERE type='item_completed'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != `text:{"status":"completed","attempts":2}` {
		t.Fatalf("stored payload = %s, want the JSON object as TEXT", stored)
	}

	events, err := store.ListEventsAfterLimit(ctx, "job1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	var payload map[string]any
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil || payload["status"] != "completed" || payload["attempts"] != float64(2) {
		t.Fatalf("payload = %s (%v), want the stored object", events[0].Payload, err)
	}
	if events[1].Payload != nil || events[2].Payload != nil {
		t.Fatalf("empty and invalid payloads = %q, %q, want nil", events[1].Payload, events[2].Payload)
	}
	for _, ev := range events {
		if _, err := json.Marshal(ev); err != nil {
			t.Fatalf("event %d does not encode: %v", ev.ID, err)
		}
	}
}

func TestListHookEventsFiltersHookTypes(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, ev := range []domain.Event{
		{JobID: "job1", Type: "job_finished"},
		{JobID: "job1", Type: "hook_started", Phase: "notify"},
		{JobID: "job2", Type: "hook_started", Phase: "notify"},
		{JobID: "job1", Type: "item_progress"},
		{JobID: "job1", Type: "hook_failed", Phase: "notify"},
		{JobID: "job1", Type: "hook_succeeded", Phase: "emby"},
	} {
		if _, err := store.AddEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.ListHookEvents(ctx, "job1")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.EventType{"hook_started", "hook_failed", "hook_succeeded"}
	if len(got) != len(want) {
		t.Fatalf("ListHookEvents = %+v, want types %v", got, want)
	}
	for i := range want {
		if got[i].Type != want[i] || got[i].JobID != "job1" {
			t.Fatalf("ListHookEvents[%d] = %+v, want %s of job1", i, got[i], want[i])
		}
	}
}
