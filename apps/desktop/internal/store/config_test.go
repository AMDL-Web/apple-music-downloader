package store

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"amdl/internal/client"
)

// fakeBackend answers the configuration and login routes as the API
// contract describes them (internal/api/openapi.yaml), and 404 elsewhere.
type fakeBackend struct {
	mu     sync.Mutex
	config map[string]map[string]any
	bodies []string
	// old answers /config 404, as a backend older than the route.
	old   bool
	reads int
}

// body is the i-th request body the backend read.
func (f *fakeBackend) body(i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.bodies) {
		return ""
	}
	return f.bodies[i]
}

func (f *fakeBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v) // the test reads what arrives
	}
	fail := func(status int, code, msg string) {
		reply(status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
	}
	body, _ := io.ReadAll(r.Body)
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/config":
		f.reads++
		if f.old {
			http.NotFound(w, r)
			return
		}
		reply(http.StatusOK, map[string]any{"config": f.config, "persisted": true})
	case "PUT /api/v1/config":
		f.bodies = append(f.bodies, string(body))
		var patch map[string]map[string]any
		_ = json.Unmarshal(body, &patch)
		if n, ok := patch["download"]["max_attempts"].(float64); ok && n > 10 {
			fail(http.StatusUnprocessableEntity, "invalid_config", "download.max_attempts must be between 1 and 10")
			return
		}
		for section, keys := range patch {
			for k, v := range keys {
				f.config[section][k] = v
			}
		}
		reply(http.StatusOK, map[string]any{"config": f.config, "persisted": true})
	case "POST /api/v1/wrapper/login":
		f.bodies = append(f.bodies, string(body))
		reply(http.StatusAccepted, map[string]string{"status": "needs_2fa", "login_id": "l-1"})
	case "POST /api/v1/wrapper/login/l-1/2fa":
		f.bodies = append(f.bodies, string(body))
		if string(body) == `{"two_step_code":"12345"}` {
			fail(http.StatusBadRequest, "invalid_request", "a two-step code is six digits")
			return
		}
		fail(http.StatusUnauthorized, "authentication_failed", "wrong code")
	default:
		http.NotFound(w, r)
	}
}

// live connects a store to the fake backend. Its posts queue up and run on
// the test's goroutine in settle, as the UI thread runs them.
func live(t *testing.T, f *fakeBackend) (*Store, func(until func() bool)) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	queue := make(chan func(), 256)
	s := New(func(fn func()) { queue <- fn }, nil)
	c, err := client.New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	s.Connect(c)
	t.Cleanup(s.Close)
	return s, func(until func() bool) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for !until() {
			select {
			case fn := <-queue:
				fn()
			case <-deadline:
				t.Fatal("the backend's answer never arrived")
			}
		}
	}
}

func TestSetConfigSendsOneKeyAndKeepsTheOldValueWhenRefused(t *testing.T) {
	f := &fakeBackend{config: map[string]map[string]any{
		"download": {"cover_size": "5000x5000", "max_attempts": 3, "embed_cover": true},
	}}
	s, settle := live(t, f)
	s.WatchConfig()
	settle(func() bool { return s.Config.Values != nil })

	s.SetConfig("download.cover_size", "3000x3000")
	if v := s.Config.Value("download.cover_size"); v != "3000x3000" {
		t.Errorf("while it saves the field shows %v; want the new value", v)
	}
	read := time.Now().Add(-time.Second)
	s.Server.Read = read
	settle(func() bool { return !s.Config.Saving["download.cover_size"] })
	// The server's state reads again at once, and the last read stays on
	// screen meanwhile: cards that show it do not drop out for a frame.
	if !s.Server.Read.Equal(read) || !s.Server.stale {
		t.Errorf("after a saved change: read %v (was %v), stale %v", s.Server.Read, read, s.Server.stale)
	}
	if got := f.body(0); got != `{"download":{"cover_size":"3000x3000"}}` {
		t.Errorf("PUT body %s; want the one key, which the backend merges", got)
	}
	if v := s.Config.Value("download.cover_size"); v != "3000x3000" {
		t.Errorf("saved %v", v)
	}

	s.SetConfig("download.max_attempts", 11)
	settle(func() bool { return !s.Config.Saving["download.max_attempts"] })
	if v := s.Config.Value("download.max_attempts"); v != 3.0 {
		t.Errorf("after a refusal the field shows %v; want the saved 3", v)
	}
	if msg := s.Config.Failed["download.max_attempts"]; msg != "download.max_attempts must be between 1 and 10" {
		t.Errorf("refusal reads %q; want the backend's message", msg)
	}
}

func TestLoginAsksForTheCodeAndSaysWhyItFailed(t *testing.T) {
	f := &fakeBackend{config: map[string]map[string]any{}}
	s, settle := live(t, f)
	s.StartLogin("someone@example.com", "not-a-real-password")
	settle(func() bool { return !s.Login.Busy })
	if s.Login.LoginID != "l-1" || s.Login.Err != "" {
		t.Fatalf("after the first step %+v; want the code step", s.Login)
	}
	// A local wrapper keeps the login waiting after a code that is not six
	// digits (internal/api/openapi.yaml, submitWrapperTwoStepCode).
	s.SubmitCode("12345")
	settle(func() bool { return !s.Login.Busy })
	if s.Login.LoginID != "l-1" || s.Login.Code != "invalid_request" {
		t.Fatalf("after a malformed code %+v; want the code step kept", s.Login)
	}
	s.SubmitCode("123456")
	settle(func() bool { return !s.Login.Busy })
	if s.Login.LoginID != "" || s.Login.Code != "authentication_failed" {
		t.Errorf("after a wrong code %+v; want the login over with authentication_failed", s.Login)
	}
	if got := f.body(2); got != `{"two_step_code":"123456"}` {
		t.Errorf("2fa body %s", got)
	}
}

func TestAFailedConfigReadWaitsBeforeTheNext(t *testing.T) {
	f := &fakeBackend{old: true}
	s, settle := live(t, f)
	now := time.Now()
	s.now = func() time.Time { return now }
	s.WatchConfig()
	settle(func() bool { return s.Config.Err != "" })
	// Views call WatchConfig every frame; hovering redraws many a second.
	for range 5 {
		s.WatchConfig()
	}
	now = now.Add(ServerEvery)
	s.WatchConfig()
	settle(func() bool { return !s.Config.reading })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reads != 2 {
		t.Errorf("%d reads of /config; want one, then one more after ServerEvery", f.reads)
	}
}
