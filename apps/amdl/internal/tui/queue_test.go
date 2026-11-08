package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// queueServer answers the TUI's moves and queue reads as POST
// /downloads/{id}/move and GET /downloads do, from orders it is given.
type queueServer struct {
	mu     sync.Mutex
	order  map[string]int64 // the queued jobs and their queue_order
	moves  []domain.MoveRequest
	refuse bool // answer the next move 409 job_not_queued
	// place is the queue_order a move gives its job.
	place func(id string, req domain.MoveRequest) int64
}

func (s *queueServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/move"):
		var req domain.MoveRequest
		_ = json.NewDecoder(r.Body).Decode(&req) // a bad body fails the test's comparison
		s.moves = append(s.moves, req)
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/downloads/"), "/move")
		if s.refuse {
			s.refuse = false
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"job_not_queued","message":"job ` + id + ` is not queued"}}`))
			return
		}
		s.order[id] = s.place(id, req)
		v := s.order[id]
		_ = json.NewEncoder(w).Encode(map[string]domain.Job{"job": {ID: id, Status: domain.JobQueued, QueueOrder: &v}})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/downloads":
		l := client.DownloadList{}
		for id, v := range s.order {
			l.Downloads = append(l.Downloads, domain.Job{ID: id, Status: domain.JobQueued, QueueOrder: &v})
		}
		_ = json.NewEncoder(w).Encode(l)
	default:
		http.NotFound(w, r)
	}
}

// queued is sample with three queued jobs, job_c, job_d and job_b in the
// order the server starts them, and a client of srv.
func queued(t *testing.T, srv *queueServer) *model {
	t.Helper()
	m := sample(t)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	c, err := client.New(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	m.c = c
	for id, v := range srv.order {
		if m.jobs[id] == nil {
			m.jobs[id] = &jobState{job: domain.Job{ID: id, Type: "album", Status: domain.JobQueued, Title: "Album " + id}}
		}
		v := v
		m.jobs[id].job.QueueOrder = &v
	}
	m.number("job_c")
	m.number("job_d")
	return m
}

// drain runs cmd and what it batches, and returns the messages that came
// back.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, drain(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func feedAll(m *model, cmd tea.Cmd) {
	for _, msg := range drain(cmd) {
		switch msg.(type) {
		case movedMsg, queueReadMsg:
			feedAll(m, m.update(msg))
		}
	}
}

// The cards run as the server does: the running ones, then the queue in its
// order. The queue's first job, once it starts, stays where its card was.
func TestCardsFollowTheQueueAndAStartedJobStaysPut(t *testing.T) {
	m := queued(t, &queueServer{order: map[string]int64{"job_b": 30, "job_c": 10, "job_d": 20}})
	if got := m.activeIDs(); !slices.Equal(got, []string{"job_a", "job_c", "job_d", "job_b"}) {
		t.Fatalf("cards %v", got)
	}
	c := &m.jobs["job_c"].job
	c.Status, c.QueueOrder = domain.JobRunning, nil
	if got := m.activeIDs(); !slices.Equal(got, []string{"job_a", "job_c", "job_d", "job_b"}) {
		t.Errorf("after job_c started: %v", got)
	}
}

// /queue moves a job at once, one request at a time: presses made while one
// is in flight become the next, from where the person left the job; the
// queue is read back after the last.
func TestQueueMovesFromTheKeyboard(t *testing.T) {
	srv := &queueServer{order: map[string]int64{"job_b": 30, "job_c": 10, "job_d": 20}}
	srv.place = func(id string, req domain.MoveRequest) int64 {
		if req.To == "last" {
			return 40
		}
		return 25 // after job_d
	}
	m := queued(t, srv)
	if cmd := m.command("/queue"); cmd != nil || m.panel != panelQueue {
		t.Fatalf("panel %v", m.panel)
	}
	first, _ := m.queueKey("shift+down")
	if first == nil || !slices.Equal(m.queuedIDs(), []string{"job_d", "job_c", "job_b"}) {
		t.Fatalf("after shift+down: %v", m.queuedIDs())
	}
	if cmd, _ := m.queueKey("shift+down"); cmd != nil {
		t.Error("a second move was sent while the first was in flight")
	}
	if got := m.queuedIDs(); !slices.Equal(got, []string{"job_d", "job_b", "job_c"}) {
		t.Errorf("the second press shows at once: %v", got)
	}
	feedAll(m, first)
	want := []domain.MoveRequest{{After: "job_d"}, {To: "last"}}
	if !slices.Equal(srv.moves, want) {
		t.Errorf("moves sent %+v, want %+v", srv.moves, want)
	}
	if m.moves.order != nil || m.moves.busy {
		t.Errorf("the local order is kept after the read: %+v", m.moves)
	}
	if got := m.activeIDs(); !slices.Equal(got, []string{"job_a", "job_d", "job_b", "job_c"}) {
		t.Errorf("cards after the read %v", got)
	}
	if m.queue.id != "job_c" {
		t.Errorf("the cursor left the moved job for %s", m.queue.id)
	}
}

// A move the server refuses because the queue changed puts the server's
// order back at once and reads the queue again.
func TestARefusedMoveRollsBack(t *testing.T) {
	srv := &queueServer{order: map[string]int64{"job_b": 30, "job_c": 10, "job_d": 20}, refuse: true}
	m := queued(t, srv)
	m.openQueue()
	cmd, _ := m.queueKey("shift+end")
	if got := m.queuedIDs(); !slices.Equal(got, []string{"job_d", "job_b", "job_c"}) {
		t.Fatalf("shift+end: %v", got)
	}
	msgs := drain(cmd)
	if len(msgs) != 1 {
		t.Fatalf("messages %v", msgs)
	}
	next := m.update(msgs[0])
	if got := m.queuedIDs(); !slices.Equal(got, []string{"job_c", "job_d", "job_b"}) {
		t.Errorf("after the refusal: %v, want the server's order", got)
	}
	reads := 0
	for _, msg := range drain(next) {
		if _, ok := msg.(queueReadMsg); ok {
			reads++
		}
	}
	if reads != 1 {
		t.Errorf("%d queue reads after the refusal, want 1", reads)
	}
	if out := stripANSI(m.transcript[len(m.transcript)-1](100)); !strings.Contains(out, "the queue changed") {
		t.Errorf("printed %q", out)
	}
	if !slices.Equal(srv.moves, []domain.MoveRequest{{To: "last"}}) {
		t.Errorf("moves sent %+v", srv.moves)
	}
}

func TestQueuePanelFitsAndSaysItsKeys(t *testing.T) {
	m := queued(t, &queueServer{order: map[string]int64{"job_b": 30, "job_c": 10, "job_d": 20}})
	m.openQueue()
	m.queueKey("down")
	for _, w := range []int{120, 72, 46, 40} {
		out := stripANSI(render(m, w))
		if !strings.Contains(out, "queue · 3 waiting") || !strings.Contains(out, "▸ 2  Album job_d") {
			t.Errorf("%d columns:\n%s", w, out)
		}
		for _, l := range strings.Split(out, "\n") {
			if lipgloss.Width(l) > w {
				t.Errorf("%d columns: a %d-column line %q", w, lipgloss.Width(l), l)
			}
		}
	}
	if !strings.Contains(stripANSI(render(m, 120)), "shift+↑↓ moves") {
		t.Error("the status line does not say how to move")
	}
	m.queueKey("esc")
	if m.panel != panelNone {
		t.Errorf("esc: panel %v", m.panel)
	}
	if !slices.Contains(m.hints(), "/queue reorders") {
		t.Errorf("idle hints %v", m.hints())
	}
}
