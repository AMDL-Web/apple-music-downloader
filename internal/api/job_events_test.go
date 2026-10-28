package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
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
	"amdl/internal/hooks"
	"amdl/internal/jobs"

	"github.com/coder/websocket"
)

func TestEventsWebSocketStreamsBacklogLiveAndResume(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())
	server := &Server{store: store, hub: hub, manager: manager}

	ctx := context.Background()
	job := domain.Job{ID: "job1", Input: "song|us|1", Type: "song", Status: domain.JobRunning}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := manager.Event(ctx, domain.Event{JobID: job.ID, Type: "job_started", Message: "job started"}); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/downloads/job1/events/ws"

	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	readEvent := func(c *websocket.Conn) domain.Event {
		t.Helper()
		msgType, raw, err := c.Read(dialCtx)
		if err != nil {
			t.Fatal(err)
		}
		if msgType != websocket.MessageText {
			t.Fatalf("message type = %v, want text", msgType)
		}
		var ev domain.Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("invalid event JSON %q: %v", raw, err)
		}
		return ev
	}

	// Backlog: the event persisted before the connection must be replayed.
	first := readEvent(conn)
	if first.Type != "job_started" || first.JobID != job.ID {
		t.Fatalf("first event = %+v, want job_started for job1", first)
	}

	// Live: a hub publish must wake the drain without waiting for the ticker.
	item := domain.JobItem{
		ID: "item-1", JobID: job.ID, AdamID: "song-1", Kind: "song", Index: 1,
		ArtworkURL: "https://example.test/{w}x{h}.jpg", Status: domain.ItemDownloading,
		Progress: domain.ItemProgress{Download: 0.5, Resolved: true},
		BitDepth: 24, SampleRate: 96000, Bitrate: 2500000, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := manager.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: "item_progress", Message: "halfway", Payload: domain.MarshalEventPayload(item, nil)}); err != nil {
		t.Fatal(err)
	}
	second := readEvent(conn)
	if second.Type != "item_progress" || second.ID <= first.ID {
		t.Fatalf("second event = %+v, want item_progress with id > %d", second, first.ID)
	}
	var streamed domain.JobItem
	if err := json.Unmarshal(second.Payload, &streamed); err != nil {
		t.Fatalf("decode streamed item snapshot: %v", err)
	}
	if streamed.ID != item.ID || streamed.ArtworkURL != item.ArtworkURL || streamed.Bitrate != item.Bitrate || !streamed.UpdatedAt.Equal(item.UpdatedAt) {
		t.Fatalf("streamed item = %+v, want REST-equivalent snapshot %+v", streamed, item)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")

	// Resume: reconnecting with last_event_id must skip already-seen events.
	resume, _, err := websocket.Dial(dialCtx, wsURL+"?last_event_id="+strconv.FormatInt(first.ID, 10), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resume.CloseNow()
	got := readEvent(resume)
	if got.ID != second.ID {
		t.Fatalf("resumed event id = %d, want %d (events at or before last_event_id must be skipped)", got.ID, second.ID)
	}
	_ = resume.Close(websocket.StatusNormalClosure, "")
}

// TestEventsClosesWhenLiveJobBecomesTerminal guards the snapshot regression:
// a stream opened while a job was running used to retain that running Job
// value forever, so even after delivering job_finished it never closed.
func TestEventsClosesWhenLiveJobBecomesTerminal(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())
	server := &Server{store: store, hub: hub, manager: manager}

	ctx := context.Background()
	job := domain.Job{ID: "live-sse", Input: "song|us|1", Type: "song", Status: domain.JobRunning}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := manager.Event(ctx, domain.Event{JobID: job.ID, Type: "job_started", Message: "job started"}); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	streamCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, ts.URL+"/api/v1/downloads/"+job.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	reader := bufio.NewReader(resp.Body)
	readType := func() string {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read SSE event: %v", err)
			}
			if value, ok := strings.CutPrefix(line, "event: "); ok {
				return strings.TrimSpace(value)
			}
		}
	}
	if got := readType(); got != "job_started" {
		t.Fatalf("first event = %q, want job_started", got)
	}

	job.Status = domain.JobCompleted
	job.UpdatedAt = time.Now().UTC()
	terminal, err := store.FinalizeJob(ctx, job, domain.Event{JobID: job.ID, Type: "job_finished", Message: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	hub.Publish(terminal)
	if got := readType(); got != "job_finished" {
		t.Fatalf("second event = %q, want job_finished", got)
	}
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("stream did not close after live job became terminal: %v", err)
	}
}

// TestEventsWSClosesWhenLiveJobBecomesTerminal is the WebSocket twin of the
// SSE regression above; both handlers must refresh the job state while live.
func TestEventsWSClosesWhenLiveJobBecomesTerminal(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())
	server := &Server{store: store, hub: hub, manager: manager}

	ctx := context.Background()
	job := domain.Job{ID: "live-ws", Input: "song|us|1", Type: "song", Status: domain.JobRunning}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := manager.Event(ctx, domain.Event{JobID: job.ID, Type: "job_started", Message: "job started"}); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	streamCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/downloads/" + job.ID + "/events/ws"
	conn, _, err := websocket.Dial(streamCtx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	readType := func() (domain.EventType, error) {
		_, raw, err := conn.Read(streamCtx)
		if err != nil {
			return "", err
		}
		var ev domain.Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			return "", err
		}
		return ev.Type, nil
	}
	if got, err := readType(); err != nil || got != "job_started" {
		t.Fatalf("first event = %q, %v; want job_started", got, err)
	}

	job.Status = domain.JobCompleted
	job.UpdatedAt = time.Now().UTC()
	terminal, err := store.FinalizeJob(ctx, job, domain.Event{JobID: job.ID, Type: "job_finished", Message: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	hub.Publish(terminal)
	if got, err := readType(); err != nil || got != "job_finished" {
		t.Fatalf("second event = %q, %v; want job_finished", got, err)
	}
	if _, err := readType(); err == nil {
		t.Fatal("WebSocket remained open after live job became terminal")
	}
}

func TestEventsRejectsTerminalJob(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())
	server := &Server{store: store, hub: hub, manager: manager}

	ctx := context.Background()
	job := domain.Job{ID: "done1", Input: "song|us|1", Type: "song", Status: domain.JobCompleted}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}

	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads/done1/events", "")
	if recorder.Code != http.StatusConflict {
		t.Fatalf("SSE subscribe to terminal job: status = %d, want %d", recorder.Code, http.StatusConflict)
	}

	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/downloads/done1/events/ws"
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(dialCtx, wsURL, nil)
	if err == nil {
		t.Fatal("WS subscribe to terminal job: dial succeeded, want rejection")
	}
	if resp == nil || resp.StatusCode != http.StatusConflict {
		status := -1
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("WS subscribe to terminal job: status = %d, want %d", status, http.StatusConflict)
	}

	recorder = requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads/missing/events", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("SSE subscribe to missing job: status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

// TestEventsReplaysBacklogForTerminalJobInsteadOfRejecting guards against the
// regression a PR reviewer caught: rejecting a terminal job's subscription
// before checking for undelivered backlog would let a client miss events it
// never saw (including the terminal event itself, in the narrow window where
// the status write and the terminal event write used to land as separate,
// non-atomic statements). A client that is not yet caught up must still get
// the backlog; only a client with nothing left to receive gets rejected.
func TestEventsReplaysBacklogForTerminalJobInsteadOfRejecting(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())
	server := &Server{store: store, hub: hub, manager: manager}

	ctx := context.Background()
	job := domain.Job{ID: "done2", Input: "song|us|1", Type: "song", Status: domain.JobCompleted}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := manager.Event(ctx, domain.Event{JobID: job.ID, Type: "job_started", Message: "job started"}); err != nil {
		t.Fatal(err)
	}
	stored, err := store.AddEvent(ctx, domain.Event{JobID: job.ID, Type: "job_finished", Message: "completed"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/downloads/done2/events", nil)
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("SSE with pending backlog on terminal job: status = %d, want %d; body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "job_started") || !strings.Contains(recorder.Body.String(), "job_finished") {
		t.Fatalf("SSE body missing backlog events: %s", recorder.Body.String())
	}

	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/downloads/done2/events/ws"
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, wsURL, nil)
	if err != nil {
		t.Fatalf("WS with pending backlog on terminal job: dial failed: %v", err)
	}
	defer conn.CloseNow()
	_, raw, err := conn.Read(dialCtx)
	if err != nil {
		t.Fatal(err)
	}
	var ev domain.Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("invalid event JSON %q: %v", raw, err)
	}
	if ev.Type != "job_started" {
		t.Fatalf("first WS event = %+v, want job_started", ev)
	}

	// Fully caught up now: a fresh connection with last_event_id at the
	// terminal event must go back to being rejected outright.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/downloads/done2/events", nil)
	req.Header.Set("Last-Event-ID", strconv.FormatInt(stored.ID, 10))
	recorder = httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("SSE fully caught up on terminal job: status = %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestEventsReplaysTerminalBacklogAcrossDatabasePages(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())
	server := &Server{store: store, hub: hub, manager: manager}
	job := domain.Job{ID: "paged-terminal", Input: "song|us|1", Type: "song", Status: domain.JobCompleted}
	if err := store.CreateJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < streamEventPageSize+1; i++ {
		if _, err := store.AddEvent(context.Background(), domain.Event{JobID: job.ID, Type: "item_progress"}); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/downloads/"+job.ID+"/events", nil)
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", recorder.Code, recorder.Body.String())
	}
	if got := strings.Count(recorder.Body.String(), "event: item_progress"); got != streamEventPageSize+1 {
		t.Fatalf("replayed events = %d, want %d across pages", got, streamEventPageSize+1)
	}
}

// TestEventsWaitsForPendingHookBeforeClosingTerminalJob guards against the
// gap a PR reviewer caught: a job's own terminal event is not the last event
// its stream will ever see, because post-download hook dispatch is
// fire-and-forget and can keep recording hook_started/hook_succeeded well
// after the job itself reached a terminal status. Connecting while a hook is
// still in flight must not be rejected — the stream must stay open and
// deliver the hook's own events. Once the hook finishes, a fresh connection
// (fully caught up) must go back to being rejected outright, proving
// eventsExhausted picks up the hook's completion rather than staying stuck
// on the stale "pending" state forever.
func TestEventsWaitsForPendingHookBeforeClosingTerminalJob(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())

	release := make(chan struct{})
	hookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer hookServer.Close()
	dispatcher := hooks.NewDispatcher(hooks.Config{Enabled: true, TimeoutSeconds: 5, Entries: []hooks.Entry{
		{Name: "notify", Type: "webhook", Events: []string{"job_finished"}, URL: hookServer.URL},
	}}, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)
	defer dispatcher.Shutdown(context.Background())

	server := &Server{store: store, hub: hub, manager: manager}

	ctx := context.Background()
	job := domain.Job{ID: "done3", Input: "song|us|1", Type: "song", Status: domain.JobCompleted}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := manager.Event(ctx, domain.Event{JobID: job.ID, Type: "job_finished", Message: "completed"}); err != nil {
		t.Fatal(err)
	}
	// Simulate the hook that finalizeJob would have fired for this terminal job.
	dispatcher.Dispatch("job_finished", job, nil)
	deadline := time.After(2 * time.Second)
	for !dispatcher.Pending(job.ID) {
		select {
		case <-deadline:
			t.Fatal("hook never became pending")
		case <-time.After(time.Millisecond):
		}
	}

	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	getCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(getCtx, http.MethodGet, ts.URL+"/api/v1/downloads/done3/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (a pending hook must not be rejected as if the job's stream were exhausted)", resp.StatusCode, http.StatusOK)
	}

	reader := bufio.NewReader(resp.Body)
	readEventType := func() string {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read SSE stream: %v", err)
			}
			if after, ok := strings.CutPrefix(line, "event: "); ok {
				return strings.TrimSpace(after)
			}
		}
	}
	if got := readEventType(); got != "job_finished" {
		t.Fatalf("first event = %q, want job_finished", got)
	}
	if got := readEventType(); got != "hook_started" {
		t.Fatalf("second event = %q, want hook_started (hook is pending, stream must not have closed)", got)
	}

	close(release) // let the blocked webhook call, and hook_succeeded, proceed
	if got := readEventType(); got != "hook_succeeded" {
		t.Fatalf("third event = %q, want hook_succeeded", got)
	}
	resp.Body.Close()

	deadline = time.After(2 * time.Second)
	for dispatcher.Pending(job.ID) {
		select {
		case <-deadline:
			t.Fatal("hook never stopped being pending after completing")
		case <-time.After(time.Millisecond):
		}
	}

	// The hook is done and the job was already terminal: a fresh, fully
	// caught-up connection must now be rejected outright, same as any other
	// exhausted terminal job.
	latestID, err := store.LatestEventID(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	req, err = http.NewRequestWithContext(getCtx, http.MethodGet, ts.URL+"/api/v1/downloads/done3/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", strconv.FormatInt(latestID, 10))
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status after hook completion = %d, want %d (nothing left to deliver)", resp.StatusCode, http.StatusConflict)
	}
}

func TestEventsExhaustedAfterJobDeletion(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	server := &Server{
		store:   store,
		manager: jobs.NewManager(store, events.NewHub(), stubProcessor{}, 1, slog.Default()),
	}
	if !server.eventsExhausted(context.Background(), "already-deleted") {
		t.Fatal("eventsExhausted = false for a deleted job; an existing stream would never close")
	}
}

// TestEventsWSWaitsForPendingHookBeforeClosingTerminalJob is the WebSocket
// twin of TestEventsWaitsForPendingHookBeforeClosingTerminalJob: eventsWS
// shares the same close-timing logic as the SSE handler, and the two are kept
// consistent by hand, so each needs its own regression coverage.
func TestEventsWSWaitsForPendingHookBeforeClosingTerminalJob(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default())

	release := make(chan struct{})
	hookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer hookServer.Close()
	dispatcher := hooks.NewDispatcher(hooks.Config{Enabled: true, TimeoutSeconds: 5, Entries: []hooks.Entry{
		{Name: "notify", Type: "webhook", Events: []string{"job_finished"}, URL: hookServer.URL},
	}}, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)
	defer dispatcher.Shutdown(context.Background())

	server := &Server{store: store, hub: hub, manager: manager}

	ctx := context.Background()
	job := domain.Job{ID: "done4", Input: "song|us|1", Type: "song", Status: domain.JobCompleted}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := manager.Event(ctx, domain.Event{JobID: job.ID, Type: "job_finished", Message: "completed"}); err != nil {
		t.Fatal(err)
	}
	// Simulate the hook that finalizeJob would have fired for this terminal job.
	dispatcher.Dispatch("job_finished", job, nil)
	deadline := time.After(2 * time.Second)
	for !dispatcher.Pending(job.ID) {
		select {
		case <-deadline:
			t.Fatal("hook never became pending")
		case <-time.After(time.Millisecond):
		}
	}

	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/downloads/done4/events/ws"
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, wsURL, nil)
	if err != nil {
		t.Fatalf("WS with pending hook on terminal job: dial failed: %v (a pending hook must not be rejected as if the job's stream were exhausted)", err)
	}
	defer conn.CloseNow()

	readEventType := func() domain.EventType {
		t.Helper()
		_, raw, err := conn.Read(dialCtx)
		if err != nil {
			t.Fatalf("read WS stream: %v", err)
		}
		var ev domain.Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("invalid event JSON %q: %v", raw, err)
		}
		return ev.Type
	}
	if got := readEventType(); got != "job_finished" {
		t.Fatalf("first event = %q, want job_finished", got)
	}
	if got := readEventType(); got != "hook_started" {
		t.Fatalf("second event = %q, want hook_started (hook is pending, stream must not have closed)", got)
	}

	close(release) // let the blocked webhook call, and hook_succeeded, proceed
	if got := readEventType(); got != "hook_succeeded" {
		t.Fatalf("third event = %q, want hook_succeeded", got)
	}
	conn.CloseNow()

	deadline = time.After(2 * time.Second)
	for dispatcher.Pending(job.ID) {
		select {
		case <-deadline:
			t.Fatal("hook never stopped being pending after completing")
		case <-time.After(time.Millisecond):
		}
	}

	// The hook is done and the job was already terminal: a fresh, fully
	// caught-up connection must now be rejected during the handshake, same as
	// any other exhausted terminal job.
	latestID, err := store.LatestEventID(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	conn2, resp, err := websocket.Dial(dialCtx, wsURL+"?last_event_id="+strconv.FormatInt(latestID, 10), nil)
	if err == nil {
		conn2.CloseNow()
		t.Fatal("WS dial after hook completion succeeded, want handshake rejection (nothing left to deliver)")
	}
	if resp == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("WS handshake status after hook completion = %+v, want %d", resp, http.StatusConflict)
	}
}
