// Package testkit is what the desktop app's tests share: a UI thread the
// test's goroutine runs (Thread), and the backends a store reads, the
// demo's (Demo) or a test's own handler on a local listener (Serve), with
// the API's answer shapes (Reply, Fail). Only tests import it.
package testkit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"amdl/internal/client"
	"amdl/internal/demo"
)

// Thread is a test's UI thread: what a store or a cover cache posts waits
// in it until Until or Drain runs it, in order, on the test's goroutine,
// as the app's UI thread runs it between frames.
type Thread struct {
	t     testing.TB
	queue chan func()
}

// NewThread returns an empty thread.
func NewThread(t testing.TB) *Thread {
	return &Thread{t: t, queue: make(chan func(), 4096)}
}

// Post queues fn for the thread. It is the post a store or a cover cache
// takes.
func (th *Thread) Post(fn func()) { th.queue <- fn }

// Stall is how long Until waits for anything to be posted before it fails
// the test: an answer that never comes, not a slow one. The race detector
// on a busy runner slows everything several times over.
const Stall = 30 * time.Second

// Until runs what is posted until cond holds, trying cond before it waits
// and again after each run of what arrived meanwhile. Nothing else makes
// cond change: a cond that builds a frame may be what sends the request it
// waits for, and its answer is posted. It fails the test if nothing is
// posted for Stall while cond does not hold.
func (th *Thread) Until(cond func() bool) { th.until(cond, true) }

// UntilExact is Until that tries cond after each posted func, so it stops
// at the first state where cond holds and leaves what came later queued: a
// test that waits for a request to be sent can look at the screen before
// the answer is applied, however fast the answer arrives.
func (th *Thread) UntilExact(cond func() bool) { th.until(cond, false) }

func (th *Thread) until(cond func() bool, drain bool) {
	th.t.Helper()
	var stall *time.Timer
	for !cond() {
		if stall == nil {
			stall = time.NewTimer(Stall)
			defer stall.Stop()
		}
		select {
		case fn := <-th.queue:
			fn()
			if drain {
				// What arrived meanwhile goes first: the answer waited on
				// may queue behind every cover that loads.
				th.Drain()
			}
			stall.Reset(Stall)
		case <-stall.C:
			th.t.Fatalf("nothing was posted for %v and the condition never held", Stall)
		}
	}
}

// Drain runs what is posted now, and what that posts, and returns.
func (th *Thread) Drain() {
	for {
		select {
		case fn := <-th.queue:
			fn()
		default:
			return
		}
	}
}

// Demo returns the demo's in-memory backend (package demo), its clock
// now, and a client of it.
func Demo(t testing.TB, now func() time.Time) (*demo.Backend, *client.Client) {
	t.Helper()
	b := demo.New(now)
	return b, Client(t, demo.Address, b.Transport())
}

// Client returns a client of the backend at base, reached through rt.
func Client(t testing.TB, base string, rt http.RoundTripper) *client.Client {
	t.Helper()
	c, err := client.NewWith(base, rt)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Serve serves h on a local listener until the test ends, and returns a
// client of it.
func Serve(t testing.TB, h http.Handler) *client.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Reply answers with status and v as JSON, as the API does.
func Reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the client under test reads what arrives
}

// Fail answers with status and the API's error body.
func Fail(w http.ResponseWriter, status int, code, message string) {
	Reply(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// Override serves the requests that match one of routes' patterns (as
// http.ServeMux reads them, "GET /api/v1/accounts") with its handler, and
// the rest through base: the demo's backend, say, with one answer a test
// sets.
func Override(base http.RoundTripper, routes map[string]http.HandlerFunc) http.RoundTripper {
	mux := http.NewServeMux()
	for pattern, h := range routes {
		mux.Handle(pattern, h)
	}
	return override{base, mux}
}

type override struct {
	base http.RoundTripper
	mux  *http.ServeMux
}

func (o override) RoundTrip(r *http.Request) (*http.Response, error) {
	h, pattern := o.mux.Handler(r)
	if pattern == "" {
		return o.base.RoundTrip(r)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	res := rec.Result()
	res.Request = r
	return res, nil
}

// Soon runs f at once, for a store's SetAfter: a test does not wait the
// pauses the app waits for, as for typing to pause before a search starts.
func Soon(_ time.Duration, f func()) { f() }
