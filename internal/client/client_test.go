package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"amdl/internal/api"
	"amdl/internal/api/openapitest"
	"amdl/internal/domain"
)

// contract is the backend's openapi.yaml. Every fake backend below runs behind
// contract.Middleware, so a fake response the real server could not send
// fails the test instead of letting client and server drift apart.
var contract = openapitest.MustLoad(api.OpenAPI)

// newTestClient returns a client of a fake backend served by h on a local
// listener, with short retry backoffs.
func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(contract.Middleware(t, h))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.RetryMin, c.RetryMax = time.Millisecond, 5*time.Millisecond
	return c
}

// writeJSON answers like the backend: a JSON content type, then the body.
func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// encodeJSON answers v as JSON like the backend.
func encodeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// pipeListener is a net.Listener over in-memory pipes. Inside a synctest
// bubble a goroutine blocked on a pipe is durably blocked, which a real
// socket is not, so the bubble's clock can jump over the server's waits.
type pipeListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

func (l *pipeListener) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.done:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

func TestWithTimeoutOutwaitsTheHeaderBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// A login to a wrapper amdl runs answers only after the guest has
		// rebooted, long after the usual header bound.
		const reboot = 2 * headerTimeoutForTest
		ln := newPipeListener()
		srv := &http.Server{Handler: contract.Middleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(reboot):
			case <-r.Context().Done(): // the client gave up
				return
			}
			writeJSON(w, http.StatusOK, `{"status":"logged_in"}`)
		}))}
		go func() { _ = srv.Serve(ln) }()
		defer srv.Close()

		defer func(d time.Duration) { headerTimeout = d }(headerTimeout)
		headerTimeout = headerTimeoutForTest
		c, err := New("http://amdl.test")
		if err != nil {
			t.Fatal(err)
		}
		transport := c.api.Transport.(*http.Transport)
		transport.DialContext = ln.DialContext
		defer transport.CloseIdleConnections()

		start := time.Now()
		if _, err := c.Call(t.Context(), http.MethodPost, "/api/v1/wrapper/login", nil, nil); err == nil {
			t.Fatal("the default client waited past its header bound")
		}
		if waited := time.Since(start); waited != headerTimeoutForTest {
			t.Errorf("the default client gave up after %v, want its header bound %v", waited, headerTimeoutForTest)
		}
		raw, err := c.WithTimeout(time.Hour).Call(t.Context(), http.MethodPost, "/api/v1/wrapper/login", nil, nil)
		if err != nil || !strings.Contains(string(raw), "logged_in") {
			t.Fatalf("WithTimeout call = %s, %v", raw, err)
		}
		if _, err := c.Call(t.Context(), http.MethodPost, "/api/v1/wrapper/login", nil, nil); err == nil {
			t.Fatal("WithTimeout changed the original client's bound")
		}
	})
}

// headerTimeoutForTest stands in for the 20 s header bound; in the synctest
// bubble it costs no wall time.
const headerTimeoutForTest = 20 * time.Second

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewWithSendsThroughItsTransport(t *testing.T) {
	var paths []string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.Host+r.URL.Path)
		rec := httptest.NewRecorder()
		fmt.Fprint(rec, `{"status":"ok"}`)
		return rec.Result(), nil
	})
	c, err := NewWith("demo", rt)
	if err != nil {
		t.Fatal(err)
	}
	// WithTimeout keeps a transport that is not an *http.Transport.
	for _, c := range []*Client{c, c.WithTimeout(time.Minute)} {
		if h, err := c.Health(context.Background()); err != nil || h.Status != "ok" {
			t.Fatalf("health = %+v, %v", h, err)
		}
	}
	if strings.Join(paths, " ") != "demo/api/v1/health demo/api/v1/health" {
		t.Errorf("requests %q", paths)
	}
}

func TestNormalizeBase(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.3.5:18570":            "http://192.168.3.5:18570",
		" http://nas.local:18570/ ":    "http://nas.local:18570",
		"https://amdl.example/console": "https://amdl.example",
	} {
		got, err := NormalizeBase(in)
		if err != nil || got != want {
			t.Errorf("NormalizeBase(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "ftp://nas", "http://"} {
		if _, err := NormalizeBase(in); err == nil {
			t.Errorf("NormalizeBase(%q) succeeded", in)
		}
	}
}

func TestSubmitReturnsBatchOnRejection(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"error":{"code":"no_urls_accepted","message":"no URL was accepted",
			"details":{"accepted":0,"rejected":1,"results":[{"url":"x","status":"invalid",
			"error":{"code":"invalid_url","message":"bad"}}]}}}`)
	}))
	res, err := c.Submit(context.Background(), []string{"x"}, nil)
	if ErrorCode(err) != "no_urls_accepted" {
		t.Fatalf("err = %v", err)
	}
	if res.Rejected != 1 || len(res.Results) != 1 || res.Results[0].Error.Code != "invalid_url" {
		t.Fatalf("batch = %+v", res)
	}
}

func TestStreamParsesEventsAndResumes(t *testing.T) {
	var gotCursor string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCursor = r.URL.Query().Get("cursor")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": keepalive\n\nid: 7\nevent: item_progress\ndata: {\"a\":\ndata: 1}\n\nid: 8\r\ndata: x\r\n\r\n")
	}))
	var events []Event
	opened := false
	res, err := c.Stream(context.Background(), "/s", "6", func() { opened = true }, func(e Event) { events = append(events, e) })
	if err != nil || !opened {
		t.Fatalf("err %v, opened %v", err, opened)
	}
	if gotCursor != "6" {
		t.Errorf("cursor = %q", gotCursor)
	}
	want := []Event{{ID: "7", Name: "item_progress", Data: "{\"a\":\n1}"}, {ID: "8", Data: "x"}}
	if fmt.Sprint(events) != fmt.Sprint(want) || res.LastID != "8" || res.Events != 2 {
		t.Fatalf("events %q, result %+v", events, res)
	}
}

func TestStreamWatchdog(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select { // a half-open connection: nothing ever arrives
		case <-release:
		case <-r.Context().Done():
		}
	}))
	c.Watchdog = 20 * time.Millisecond
	_, err := c.Stream(context.Background(), "/s", "", nil, func(Event) {})
	if !errors.Is(err, ErrWatchdog) {
		t.Fatalf("err = %v, want ErrWatchdog", err)
	}
}

// feedServer serves a snapshot and then one feed connection per element of
// conns; each connection writes its messages and closes. Its list's cursor is
// the newest event it sent, from 10, until it replaces its database after
// the replaceAfter-th connection: then it lists no jobs at cursor 0.
type feedServer struct {
	mu           sync.Mutex
	conns        [][]domain.DownloadFeedMessage
	cursors      []string
	newest       int64
	replaceAfter int
	served       int
}

func (s *feedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.URL.Path {
	case "/api/v1/downloads":
		l := DownloadList{Downloads: []domain.Job{{ID: "a", Type: "song", Status: domain.JobRunning}}, EventCursor: max(s.newest, 10)}
		if s.replaceAfter > 0 && s.served >= s.replaceAfter {
			l = DownloadList{Downloads: []domain.Job{}, EventCursor: s.newest}
		}
		encodeJSON(w, l)
	case "/api/v1/downloads/events":
		s.cursors = append(s.cursors, r.URL.Query().Get("cursor"))
		if len(s.conns) == 0 {
			writeJSON(w, http.StatusBadRequest, `{"error":{"code":"invalid_request","message":"cursor is past the newest event"}}`)
			return
		}
		msgs := s.conns[0]
		s.conns = s.conns[1:]
		w.Header().Set("Content-Type", "text/event-stream")
		for _, m := range msgs {
			raw, _ := json.Marshal(m)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", m.EventID, raw)
			s.newest = max(s.newest, m.EventID)
		}
		if s.served++; s.served == s.replaceAfter {
			s.newest = 0
		}
	}
}

func TestFollowFeedResumesFromLastEvent(t *testing.T) {
	srv := &feedServer{conns: [][]domain.DownloadFeedMessage{
		{{Type: "download_upserted", EventID: 11, Job: &domain.Job{ID: "a", Type: "song", Status: domain.JobCompleted}}},
		{{Type: "download_deleted", EventID: 12, JobID: "a"}},
	}}
	c := newTestClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got []string
	snapshots := 0
	c.FollowFeed(ctx, FeedHandlers{
		Snapshot: func(DownloadList) {
			snapshots++
			if snapshots == 2 { // the 400 that ends the script asks for a new snapshot
				cancel()
			}
		},
		Message: func(m domain.DownloadFeedMessage) { got = append(got, fmt.Sprintf("%s@%d", m.Type, m.EventID)) },
	})
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if want := "[download_upserted@11 download_deleted@12]"; fmt.Sprint(got) != want {
		t.Errorf("messages %v, want %s", got, want)
	}
	// 10 from the snapshot, then each reconnect resumes after the last
	// message delivered; the rejected third cursor forces a new snapshot.
	if want := "[10 11 12]"; fmt.Sprint(srv.cursors[:3]) != want {
		t.Errorf("cursors %v, want %s", srv.cursors, want)
	}
}

// A backend whose newest event is older than the feed's cursor, as one whose
// database was replaced while the stream was down, is followed from a new
// snapshot: its events would reach a stream resumed at the old cursor only
// once they passed it.
func TestFollowFeedStartsOverWhenTheBackendHasFewerEvents(t *testing.T) {
	srv := &feedServer{replaceAfter: 1, conns: [][]domain.DownloadFeedMessage{
		{{Type: "download_upserted", EventID: 11, Job: &domain.Job{ID: "a", Type: "song", Status: domain.JobCompleted}}},
		{{Type: "download_upserted", EventID: 1, Job: &domain.Job{ID: "b", Type: "song", Status: domain.JobQueued}}},
	}}
	c := newTestClient(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var lists []int
	c.FollowFeed(ctx, FeedHandlers{
		Snapshot: func(l DownloadList) { lists = append(lists, len(l.Downloads)) },
		Message: func(m domain.DownloadFeedMessage) {
			if m.EventID == 1 {
				cancel()
			}
		},
	})
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if want := "[1 0]"; fmt.Sprint(lists) != want {
		t.Errorf("snapshots of %v jobs, want %s", lists, want)
	}
	if want := "[10 0]"; fmt.Sprint(srv.cursors) != want {
		t.Errorf("cursors %v, want %s", srv.cursors, want)
	}
}

func TestFollowJobConfirmsTerminalAfter204(t *testing.T) {
	var mu sync.Mutex
	details := 0
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/events"):
			if r.URL.Query().Get("cursor") == "3" {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, `id: 4`+"\nevent: item_progress\n"+`data: {"id":4,"item_id":"i1","type":"item_progress","payload":{"id":"i1","status":"downloading","progress":{"download":0.5}}}`+"\n\n")
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			details++
			status := domain.JobRunning
			if details > 1 {
				status = domain.JobCompleted
			}
			encodeJSON(w, DownloadDetail{Job: domain.Job{ID: "j", Type: "song", Status: status}, Items: []domain.JobItem{}, Hooks: []domain.HookState{}, EventCursor: 3})
		}
	}))
	var events []domain.Event
	err := c.FollowJob(context.Background(), "j", JobHandlers{Event: func(e domain.Event) { events = append(events, e) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ItemID != "i1" {
		t.Fatalf("events = %+v", events)
	}
	var item domain.JobItem
	if err := json.Unmarshal(events[0].Payload, &item); err != nil || item.Progress.Download != 0.5 {
		t.Fatalf("payload item %+v, %v", item, err)
	}
	if details != 2 {
		t.Errorf("read the detail %d times, want 2 (start, then to confirm the end)", details)
	}
}

// While a hook runs the backend keeps the stream open after the job is done.
func TestFollowJobStopsAtTerminalWithoutWaitingForHooks(t *testing.T) {
	var details atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/events") {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, `id: 4`+"\nevent: job_finished\n"+`data: {"id":4,"job_id":"j","type":"job_finished"}`+"\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		status := domain.JobRunning
		hooks := []domain.HookState{}
		if details.Add(1) > 1 {
			status = domain.JobCompleted
			hooks = []domain.HookState{{Name: "notify", Status: "running"}}
		}
		encodeJSON(w, DownloadDetail{Job: domain.Job{ID: "j", Type: "song", Status: status}, Items: []domain.JobItem{}, Hooks: hooks, EventCursor: 3})
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var last DownloadDetail
	err := c.FollowJob(ctx, "j", JobHandlers{StopAtTerminal: true, Detail: func(d DownloadDetail) { last = d }})
	if err != nil {
		t.Fatal(err)
	}
	if last.Job.Status != domain.JobCompleted || !last.HooksRunning() {
		t.Errorf("last detail = %+v, want a completed job whose hook runs", last)
	}
}

func TestFollowJobGone(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, `{"error":{"code":"job_not_found","message":"no such job"}}`)
	}))
	if err := c.FollowJob(context.Background(), "x", JobHandlers{}); !errors.Is(err, ErrJobGone) {
		t.Fatalf("err = %v", err)
	}
}
