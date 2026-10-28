package api

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/jobs"
)

// TestDownloadsFeedPushesUpsertsAndDeletes exercises the overview SSE feed:
// initial backlog upsert from a cursor, live upsert on a new milestone, and a
// live delete notification. Deletes carry a persisted tombstone cursor so a
// client can resume past them.
func TestDownloadsFeedPushesUpsertsAndDeletes(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())
	server := &Server{store: store, hub: hub, manager: manager}

	ctx := context.Background()
	job1 := domain.Job{ID: "job1", Input: "song|us|1", Type: "song", CanonicalKey: "song:1", Status: domain.JobCompleted}
	if err := store.CreateJob(ctx, job1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddEvent(ctx, domain.Event{JobID: job1.ID, Type: "job_finished", Message: "completed"}); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	getCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(getCtx, http.MethodGet, ts.URL+"/api/v1/downloads/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)

	readMessage := func() (string, domain.DownloadFeedMessage) {
		t.Helper()
		var eventType string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read feed: %v", err)
			}
			if after, ok := strings.CutPrefix(line, "event: "); ok {
				eventType = strings.TrimSpace(after)
			}
			if after, ok := strings.CutPrefix(line, "data: "); ok {
				var msg domain.DownloadFeedMessage
				if err := json.Unmarshal([]byte(strings.TrimSpace(after)), &msg); err != nil {
					t.Fatalf("bad feed JSON %q: %v", after, err)
				}
				return eventType, msg
			}
		}
	}

	// Backlog: the persisted job_finished milestone yields an upsert for job1.
	et, msg := readMessage()
	if et != "download_upserted" || msg.Job == nil || msg.Job.ID != "job1" {
		t.Fatalf("first message = %s %+v, want download_upserted for job1", et, msg)
	}
	if msg.EventID == 0 {
		t.Fatal("upsert event_id = 0, want the milestone's cursor")
	}
	if msg.StatusCounts != (domain.JobStatusCounts{Completed: 1, Total: 1}) {
		t.Fatalf("backlog status_counts = %+v, want completed=1 total=1", msg.StatusCounts)
	}

	// Live upsert: a new job's milestone wakes the feed.
	job2 := domain.Job{ID: "job2", Input: "song|us|2", Type: "song", CanonicalKey: "song:2", Status: domain.JobRunning}
	if err := store.CreateJob(ctx, job2); err != nil {
		t.Fatal(err)
	}
	if err := manager.Event(ctx, domain.Event{JobID: job2.ID, Type: "job_started", Message: "job started"}); err != nil {
		t.Fatal(err)
	}
	et, msg = readMessage()
	if et != "download_upserted" || msg.Job == nil || msg.Job.ID != "job2" {
		t.Fatalf("live message = %s %+v, want download_upserted for job2", et, msg)
	}
	if msg.StatusCounts != (domain.JobStatusCounts{Completed: 1, Running: 1, Total: 2}) {
		t.Fatalf("live status_counts = %+v, want completed=1 running=1 total=2", msg.StatusCounts)
	}

	// Live delete: removing a terminal job pushes download_deleted with the
	// persisted tombstone event_id.
	if err := manager.Delete(ctx, job1.ID); err != nil {
		t.Fatal(err)
	}
	et, msg = readMessage()
	if et != "download_deleted" || msg.JobID != "job1" {
		t.Fatalf("delete message = %s %+v, want download_deleted for job1", et, msg)
	}
	if msg.EventID == 0 {
		t.Fatal("delete event_id = 0, want persisted tombstone cursor")
	}
	if msg.StatusCounts != (domain.JobStatusCounts{Running: 1, Total: 1}) {
		t.Fatalf("delete status_counts = %+v, want running=1 total=1", msg.StatusCounts)
	}
}

func TestDownloadsFeedReplaysDeleteAfterSnapshotCursor(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())
	server := &Server{store: store, hub: hub, manager: manager}

	ctx := context.Background()
	job := domain.Job{ID: "job1", Input: "song|us|1", Type: "song", CanonicalKey: "song:1", Status: domain.JobCompleted}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	finished, err := store.AddEvent(ctx, domain.Event{JobID: job.ID, Type: "job_finished"})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Delete(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	getCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(getCtx, http.MethodGet, ts.URL+"/api/v1/downloads/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", strconv.FormatInt(finished.ID, 10))
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)

	var sseID int64
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read feed: %v", err)
		}
		if after, ok := strings.CutPrefix(line, "id: "); ok {
			sseID, _ = strconv.ParseInt(strings.TrimSpace(after), 10, 64)
		}
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			var msg domain.DownloadFeedMessage
			if err := json.Unmarshal([]byte(strings.TrimSpace(after)), &msg); err != nil {
				t.Fatalf("bad feed JSON %q: %v", after, err)
			}
			if msg.Type != "download_deleted" || msg.JobID != job.ID {
				t.Fatalf("message = %+v, want replayed delete for %s", msg, job.ID)
			}
			if msg.EventID <= finished.ID || sseID != msg.EventID {
				t.Fatalf("delete cursor = event_id %d / id: %d, want tombstone after %d", msg.EventID, sseID, finished.ID)
			}
			if msg.StatusCounts != (domain.JobStatusCounts{}) {
				t.Fatalf("replayed delete status_counts = %+v, want all zero", msg.StatusCounts)
			}
			return
		}
	}
}

// TestDownloadsFeedBatchUsesPerJobCursor guards the resume-cursor bug two
// reviewers caught: when one drain batch contains milestones for several jobs,
// each download_upserted must carry that job's own milestone id, not the
// batch-wide max. Otherwise a client that disconnects after an earlier message
// resumes past the later jobs' ids and skips them permanently (the query is
// strict id>afterID). Events are seeded before the connection so they all land
// in a single initial drain batch.
func TestDownloadsFeedBatchUsesPerJobCursor(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())
	server := &Server{store: store, hub: hub, manager: manager}

	ctx := context.Background()
	for _, id := range []string{"jobA", "jobB"} {
		if err := store.CreateJob(ctx, domain.Job{ID: id, Input: "song|us|" + id, Type: "song", CanonicalKey: "song:" + id, Status: domain.JobRunning}); err != nil {
			t.Fatal(err)
		}
	}
	evA, err := store.AddEvent(ctx, domain.Event{JobID: "jobA", Type: "job_started"})
	if err != nil {
		t.Fatal(err)
	}
	evB, err := store.AddEvent(ctx, domain.Event{JobID: "jobB", Type: "job_started"})
	if err != nil {
		t.Fatal(err)
	}
	if evA.ID >= evB.ID {
		t.Fatalf("expected evA.ID < evB.ID, got %d, %d", evA.ID, evB.ID)
	}

	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	getCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(getCtx, http.MethodGet, ts.URL+"/api/v1/downloads/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)

	// Read one SSE message, returning both its id: line and decoded data.
	readMessage := func() (int64, domain.DownloadFeedMessage) {
		t.Helper()
		var sseID int64
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read feed: %v", err)
			}
			if after, ok := strings.CutPrefix(line, "id: "); ok {
				sseID, _ = strconv.ParseInt(strings.TrimSpace(after), 10, 64)
			}
			if after, ok := strings.CutPrefix(line, "data: "); ok {
				var msg domain.DownloadFeedMessage
				if err := json.Unmarshal([]byte(strings.TrimSpace(after)), &msg); err != nil {
					t.Fatalf("bad feed JSON %q: %v", after, err)
				}
				return sseID, msg
			}
		}
	}

	// jobA came first (lower id) and must be sent first, carrying evA.ID — NOT
	// the batch max (evB.ID). This is the exact value a client's Last-Event-ID
	// would hold if it disconnected right here.
	sseID, msg := readMessage()
	if msg.Job == nil || msg.Job.ID != "jobA" {
		t.Fatalf("first message = %+v, want jobA", msg)
	}
	if msg.EventID != evA.ID || sseID != evA.ID {
		t.Fatalf("jobA cursor = event_id %d / id: %d, want %d (its own event, not batch max %d)", msg.EventID, sseID, evA.ID, evB.ID)
	}

	sseID, msg = readMessage()
	if msg.Job == nil || msg.Job.ID != "jobB" {
		t.Fatalf("second message = %+v, want jobB", msg)
	}
	if msg.EventID != evB.ID || sseID != evB.ID {
		t.Fatalf("jobB cursor = event_id %d / id: %d, want %d", msg.EventID, sseID, evB.ID)
	}
}

func TestDownloadsFeedReplaysMilestonesAcrossDatabasePages(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := domain.Job{ID: "paged-feed", Input: "song|us|1", Type: "song", Status: domain.JobRunning}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	var boundaryID, finalID int64
	for i := 0; i < streamEventPageSize+1; i++ {
		ev, err := store.AddEvent(ctx, domain.Event{JobID: job.ID, Type: "job_started"})
		if err != nil {
			t.Fatal(err)
		}
		if i == streamEventPageSize-1 {
			boundaryID = ev.ID
		}
		finalID = ev.ID
	}

	hub := events.NewHub()
	server := &Server{store: store, hub: hub, manager: jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())}
	sink := &recordingSink{onSend: func(n int) {
		if n == 2 {
			cancel()
		}
	}}
	server.runDownloadsFeed(ctx, sink, 0, make(chan struct{}), streamTicks{})
	var got []domain.DownloadFeedMessage
	for _, frame := range sink.frames {
		var msg domain.DownloadFeedMessage
		if err := json.Unmarshal(frame.data, &msg); err != nil {
			t.Fatal(err)
		}
		got = append(got, msg)
	}
	if len(got) != 2 || got[0].EventID != boundaryID || got[1].EventID != finalID {
		t.Fatalf("paged feed messages = %+v, want page cursors %d then %d", got, boundaryID, finalID)
	}
}

// An event filed after a job's tombstone (a motion artwork lookup racing the
// delete) must not hide the delete from a client replaying past both.
func TestDownloadsFeedDeletesAJobWhoseLatestEventFollowsItsTombstone(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// The deadline ends the loop when no frame comes, so a regression fails
	// instead of hanging.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())
	job := domain.Job{ID: "raced", Input: "album|us|1", Type: "album", CanonicalKey: "album:1", Status: domain.JobCompleted}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	finished, err := store.AddEvent(ctx, domain.Event{JobID: job.ID, Type: domain.EventJobFinished})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Delete(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	orphan, err := store.AddEvent(ctx, domain.Event{JobID: job.ID, Type: domain.EventMotionArtworkResolved, Phase: "motion_artwork"})
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{store: store, hub: hub, manager: manager}
	sink := &recordingSink{onSend: func(int) { cancel() }}
	server.runDownloadsFeed(ctx, sink, finished.ID, make(chan struct{}), streamTicks{})
	if len(sink.frames) != 1 {
		t.Fatalf("frames = %d, want one download_deleted", len(sink.frames))
	}
	var msg domain.DownloadFeedMessage
	if err := json.Unmarshal(sink.frames[0].data, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "download_deleted" || msg.JobID != job.ID || msg.EventID != orphan.ID {
		t.Fatalf("message = %+v, want download_deleted for %s at event %d", msg, job.ID, orphan.ID)
	}
}

// recordingSink is an sseSink that keeps every frame in memory.
type recordingSink struct {
	frames     []sseFrame
	keepalives int
	onSend     func(n int)
}

type sseFrame struct {
	id, name string
	data     []byte
}

func (r *recordingSink) send(id, name string, data []byte) error {
	r.frames = append(r.frames, sseFrame{id: id, name: name, data: append([]byte(nil), data...)})
	if r.onSend != nil {
		r.onSend(len(r.frames))
	}
	return nil
}

func (r *recordingSink) keepalive() error {
	r.keepalives++
	return nil
}

// TestDownloadsFeedFlushesHeadWithoutBacklog guards against the SSE feed
// withholding its 200 response head until the first change or the 10s
// keepalive: a client connecting when nothing is pending must still open
// promptly. The 3s deadline is below the keepalive interval, so it fails if
// the head isn't flushed up front.
func TestDownloadsFeedFlushesHeadWithoutBacklog(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())
	server := &Server{store: store, hub: hub, manager: manager}

	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	getCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(getCtx, http.MethodGet, ts.URL+"/api/v1/downloads/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("feed did not return its response head before the deadline: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
