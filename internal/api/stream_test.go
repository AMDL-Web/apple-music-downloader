package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/jobs"
	"amdl/internal/logging"
)

// newStreamServer wires every dependency a stream needs: store, hub, manager
// and a log buffer.
func newStreamServer(t *testing.T) (*Server, *logging.System) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := config.Default()
	cfg.Logging.Console = false
	system, err := logging.New(cfg.Logging)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = system.Close() })
	hub := events.NewHub()
	return New(Deps{
		Config: config.NewStore(cfg), Store: store, Hub: hub,
		Manager: jobs.NewManager(store, hub, stubProcessor{}, 1, slog.Default()),
		Logger:  system.Logger, Logging: system,
	}), system
}

// addEvents creates a terminal job with one event per type and returns the
// stored event ids in order.
func addEvents(t *testing.T, s *Server, jobID string, types ...domain.EventType) []int64 {
	t.Helper()
	ctx := context.Background()
	job := domain.Job{ID: jobID, Input: "song|us|" + jobID, Type: "song", CanonicalKey: "song:" + jobID, Status: domain.JobCompleted}
	if err := s.store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, ty := range types {
		ev, err := s.store.AddEvent(ctx, domain.Event{JobID: jobID, Type: ty})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, ev.ID)
	}
	return ids
}

// sseIDs returns the id: lines of a finished SSE body.
func sseIDs(body string) []string {
	var ids []string
	for _, line := range strings.Split(body, "\n") {
		if id, ok := strings.CutPrefix(line, "id: "); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func TestJobStreamCursorPrecedence(t *testing.T) {
	server, _ := newStreamServer(t)
	ids := addEvents(t, server, "job1", "job_started", "item_completed", "job_finished")
	id := func(i int) string { return strconv.FormatInt(ids[i], 10) }

	tests := []struct {
		name   string
		header string
		query  string
		want   []string
	}{
		{name: "no cursor replays everything", want: []string{id(0), id(1), id(2)}},
		{name: "query cursor", query: id(0), want: []string{id(1), id(2)}},
		{name: "header cursor", header: id(1), want: []string{id(2)}},
		// EventSource sends Last-Event-ID on reconnect while the URL still
		// carries the cursor of the first connect, so the header is newer.
		{name: "header wins over query", header: id(1), query: id(0), want: []string{id(2)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := "/api/v1/downloads/job1/events"
			if tt.query != "" {
				target += "?cursor=" + tt.query
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			if tt.header != "" {
				req.Header.Set("Last-Event-ID", tt.header)
			}
			recorder := httptest.NewRecorder()
			server.Routes().ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if got := sseIDs(recorder.Body.String()); strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("ids = %v, want %v", got, tt.want)
			}
		})
	}
}

// A job that is terminal and fully delivered answers 204 with no body, which
// makes a browser EventSource stop reconnecting.
func TestJobStreamCaughtUpIsNoContent(t *testing.T) {
	server, _ := newStreamServer(t)
	ids := addEvents(t, server, "job1", "job_started", "job_finished")
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads/job1/events?cursor="+strconv.FormatInt(ids[1], 10), "")
	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
		t.Fatalf("status = %d body %q, want an empty 204", recorder.Code, recorder.Body.String())
	}
}

func TestStreamsRejectMalformedCursor(t *testing.T) {
	server, _ := newStreamServer(t)
	routes := server.Routes()
	for _, path := range []string{"/api/v1/downloads/events", "/api/v1/downloads/job1/events", "/api/v1/logs/stream"} {
		for _, bad := range []string{"abc", "-1", "1.5", "99999999999999999999"} {
			req := httptest.NewRequest(http.MethodGet, path+"?cursor="+bad, nil)
			recorder := httptest.NewRecorder()
			routes.ServeHTTP(recorder, req)
			assertErrorCode(t, recorder, http.StatusBadRequest, "invalid_request")

			req = httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Last-Event-ID", bad)
			recorder = httptest.NewRecorder()
			routes.ServeHTTP(recorder, req)
			assertErrorCode(t, recorder, http.StatusBadRequest, "invalid_request")
		}
	}
	recorder := requestJSON(t, routes, http.MethodGet, "/api/v1/logs?cursor=abc", "")
	assertErrorCode(t, recorder, http.StatusBadRequest, "invalid_request")
}

func TestStreamHeaders(t *testing.T) {
	server, _ := newStreamServer(t)
	addEvents(t, server, "job1", "job_finished")
	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	for _, path := range []string{"/api/v1/downloads/events", "/api/v1/downloads/job1/events", "/api/v1/logs/stream"} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		want := map[string]string{"Content-Type": "text/event-stream", "Cache-Control": "no-cache", "X-Accel-Buffering": "no"}
		for key, value := range want {
			if got := resp.Header.Get(key); got != value {
				t.Errorf("%s: %s = %q, want %q", path, key, got, value)
			}
		}
		resp.Body.Close()
		cancel()
	}

	// Connection is hop-by-hop: a handler must not set it (HTTP/2 forbids it).
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/downloads/job1/events", nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Connection") != "" {
		t.Fatalf("status %d, Connection header %q, want 200 without Connection", recorder.Code, recorder.Header().Get("Connection"))
	}
}

// EndStreams runs when http.Server.Shutdown starts, so an open stream must not
// hold the drain until its timeout.
func TestEndStreamsLetsShutdownDrain(t *testing.T) {
	server, _ := newStreamServer(t)
	running := domain.Job{ID: "live", Input: "song|us|live", Type: "song", CanonicalKey: "song:live", Status: domain.JobRunning}
	if err := server.store.CreateJob(context.Background(), running); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(server.Routes())
	ts.Config.RegisterOnShutdown(server.EndStreams)
	ts.Start()
	defer ts.Close()

	var bodies []io.Reader
	for _, path := range []string{"/api/v1/downloads/events", "/api/v1/downloads/live/events", "/api/v1/logs/stream"} {
		resp, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d, want an open stream", path, resp.StatusCode)
		}
		bodies = append(bodies, resp.Body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ts.Config.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown with open streams: %v, want a drain that ends them", err)
	}
	for _, body := range bodies {
		if _, err := io.ReadAll(body); err != nil {
			t.Errorf("stream did not end cleanly: %v", err)
		}
	}
}

func TestFeedCursorPrecedence(t *testing.T) {
	server, _ := newStreamServer(t)
	a := addEvents(t, server, "jobA", "job_finished")[0]
	b := addEvents(t, server, "jobB", "job_finished")[0]
	addEvents(t, server, "jobC", "job_finished")
	ts := httptest.NewServer(server.Routes())
	defer ts.Close()

	firstJob := func(header, query string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		target := ts.URL + "/api/v1/downloads/events"
		if query != "" {
			target += "?cursor=" + query
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if header != "" {
			req.Header.Set("Last-Event-ID", header)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read feed: %v", err)
			}
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				var msg domain.DownloadFeedMessage
				if err := json.Unmarshal([]byte(data), &msg); err != nil {
					t.Fatal(err)
				}
				return msg.Job.ID
			}
		}
	}
	format := func(id int64) string { return strconv.FormatInt(id, 10) }
	if got := firstJob("", format(a)); got != "jobB" {
		t.Fatalf("?cursor=%d: first job = %s, want jobB", a, got)
	}
	if got := firstJob(format(b), format(a)); got != "jobC" {
		t.Fatalf("Last-Event-ID %d with ?cursor=%d: first job = %s, want jobC (header wins)", b, a, got)
	}
}

func TestLogStreamCursorPrecedenceAndPaging(t *testing.T) {
	server, system := newStreamServer(t)
	for _, msg := range []string{"one", "two", "three"} {
		system.Logger.Info(msg, "component", "cursortest")
	}
	page := system.Store.List(logging.Filter{Component: "cursortest", Limit: 10})
	if len(page.Entries) != 3 {
		t.Fatalf("entries = %+v, want 3", page.Entries)
	}
	seq := func(i int) string { return strconv.FormatUint(page.Entries[i].Sequence, 10) }

	// GET /logs pages forward from ?cursor=.
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/logs?component=cursortest&cursor="+seq(0), "")
	var list struct {
		Logs []logging.Entry `json:"logs"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || len(list.Logs) != 2 || list.Logs[0].Message != "two" {
		t.Fatalf("GET /logs?cursor = %d %s, want two then three", recorder.Code, recorder.Body.String())
	}

	ts := httptest.NewServer(server.Routes())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/logs/stream?component=cursortest&cursor="+seq(0), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", seq(1))
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read log stream: %v", err)
		}
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			if !strings.Contains(data, `"message":"three"`) {
				t.Fatalf("first streamed entry = %s, want three (Last-Event-ID wins over ?cursor=)", data)
			}
			return
		}
	}
}

// scriptedSink fails its writes on demand and counts them; read its fields
// only after the stream loop returned.
type scriptedSink struct {
	sendErr, keepaliveErr error
	sends, keepalives     int
}

func (s *scriptedSink) send(string, string, []byte) error {
	s.sends++
	return s.sendErr
}

func (s *scriptedSink) keepalive() error {
	s.keepalives++
	return s.keepaliveErr
}

// runUntilReturn runs loop and fails the test if it is still running after
// two seconds.
func runUntilReturn(t *testing.T, loop func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		loop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream loop did not end")
	}
}

// Every stream writes a keepalive comment on its keepalive tick, and a failed
// write (the client is gone) ends the loop instead of idling until the next
// event.
func TestStreamLoopsSendKeepaliveAndEndOnWriteError(t *testing.T) {
	gone := errors.New("client gone")
	server, _ := newStreamServer(t)
	ctx := context.Background()
	running := domain.Job{ID: "running", Input: "song|us|1", Type: "song", Status: domain.JobRunning}
	if err := server.store.CreateJob(ctx, running); err != nil {
		t.Fatal(err)
	}

	keepalive := make(chan time.Time, 1)
	keepalive <- time.Now()
	ticks := streamTicks{keepalive: keepalive}

	jobSink := &scriptedSink{keepaliveErr: gone}
	runUntilReturn(t, func() {
		server.runJobEventStream(ctx, jobSink, running.ID, 0, nil, make(chan struct{}), ticks)
	})
	if jobSink.keepalives != 1 {
		t.Fatalf("job stream keepalives = %d, want 1", jobSink.keepalives)
	}

	keepalive <- time.Now()
	feedSink := &scriptedSink{keepaliveErr: gone}
	runUntilReturn(t, func() {
		server.runDownloadsFeed(ctx, feedSink, 0, make(chan struct{}), ticks)
	})
	if feedSink.keepalives != 1 {
		t.Fatalf("feed keepalives = %d, want 1", feedSink.keepalives)
	}

	keepalive <- time.Now()
	logSink := &scriptedSink{keepaliveErr: gone}
	runUntilReturn(t, func() {
		runLogStream(ctx, logSink, nil, make(chan logging.Entry), keepalive)
	})
	if logSink.keepalives != 1 {
		t.Fatalf("log stream keepalives = %d, want 1", logSink.keepalives)
	}

	// A failed event write ends the loop at once.
	live := make(chan logging.Entry, 1)
	live <- logging.Entry{Sequence: 1, Message: "x"}
	sendSink := &scriptedSink{sendErr: gone}
	runUntilReturn(t, func() {
		runLogStream(ctx, sendSink, nil, live, make(chan time.Time))
	})
	if sendSink.sends != 1 {
		t.Fatalf("log stream sends = %d, want 1", sendSink.sends)
	}
}

// The keepalive comment interval is documented as 15 s (docs/api.md,
// openapi.yaml) and is separate from the store re-drain.
func TestStreamIntervals(t *testing.T) {
	if sseKeepaliveInterval != 15*time.Second {
		t.Fatalf("sseKeepaliveInterval = %s, want the documented 15s", sseKeepaliveInterval)
	}
	if storeRedrainInterval == sseKeepaliveInterval {
		t.Fatal("storeRedrainInterval must be its own interval, not the keepalive")
	}
}

func TestDownloadSnapshotsReturnEventCursor(t *testing.T) {
	server, _ := newStreamServer(t)
	ids := addEvents(t, server, "job1", "job_started", "job_finished")
	other := addEvents(t, server, "job2", "job_finished")
	for _, tc := range []struct {
		path string
		want int64
	}{
		{"/api/v1/downloads/job1", ids[1]},
		{"/api/v1/downloads", other[0]},
	} {
		recorder := requestJSON(t, server.Routes(), http.MethodGet, tc.path, "")
		var body map[string]json.RawMessage
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if got := string(body["event_cursor"]); got != strconv.FormatInt(tc.want, 10) {
			t.Fatalf("%s event_cursor = %s, want %d", tc.path, got, tc.want)
		}
		if _, ok := body["last_event_id"]; ok {
			t.Fatalf("%s still returns last_event_id", tc.path)
		}
	}
}
