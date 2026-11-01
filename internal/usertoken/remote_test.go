package usertoken

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/wrapper"
)

const testAddress = "http://wrapper.test:12340"

var (
	endpointTok = strings.Repeat("E", 244)
	newerTok    = strings.Repeat("N", 244)
)

// lockedBuffer collects log output written from the refresh loop.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// fakeWrapper answers fetches from a script: the last entry repeats.
type fakeWrapper struct {
	mu      sync.Mutex
	replies []fakeReply
	calls   atomic.Int32
}

type fakeReply struct {
	reply wrapper.TokenReply
	err   error
}

func ok(tok string, next int64) fakeReply {
	return fakeReply{reply: wrapper.TokenReply{Region: "cn", MusicToken: tok, RefreshedAt: time.Now().Unix(), NextRefreshAt: next}}
}

func (f *fakeWrapper) set(r ...fakeReply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = r
}

func (f *fakeWrapper) fetch(context.Context) (wrapper.TokenReply, error) {
	n := int(f.calls.Add(1))
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.replies[min(n, len(f.replies))-1]
	return r.reply, r.err
}

// endpointProvider is a Provider with GET /token answered by fetch, its store
// in dir and every wait short unless a test lengthens it.
func endpointProvider(t *testing.T, fetch func(context.Context) (wrapper.TokenReply, error), dir, address string, logs *lockedBuffer) (*Provider, *remote) {
	t.Helper()
	p := New(nil)
	p.UseEndpoint(Endpoint{
		Fetch: fetch, Dir: dir, Address: address,
		Logger: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	r := p.remote.Load()
	r.jitter = func() time.Duration { return 0 }
	r.floor, r.ceiling = time.Hour, time.Hour
	r.loggedOutRetry, r.unsupportedRetry = time.Hour, time.Hour
	r.backoffMin, r.backoffMax = time.Hour, time.Hour
	r.nudgeSpacing = 0
	return p, r
}

func runLoop(t *testing.T, p *Provider) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.RunEndpoint(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func assertNoToken(t *testing.T, logs *lockedBuffer, toks ...string) {
	t.Helper()
	out := logs.String()
	for _, tok := range toks {
		if strings.Contains(out, tok[:32]) {
			t.Fatalf("a token is in the log:\n%s", out)
		}
	}
}

func tokenIs(p *Provider, cfg config.Config, want Token) func() bool {
	return func() bool { return p.Token(cfg) == want }
}

func storeGone(dir string) func() bool {
	return func() bool {
		_, err := os.Stat(filepath.Join(dir, StoreFileName))
		return errors.Is(err, os.ErrNotExist)
	}
}

func TestEndpointTokenIsUsedAndStoredPrivately(t *testing.T) {
	dir := filepath.Join(t.TempDir(), StoreDirName)
	var logs lockedBuffer
	w := &fakeWrapper{}
	w.set(ok(endpointTok, 0))
	p, _ := endpointProvider(t, w.fetch, dir, testAddress, &logs)
	runLoop(t, p)
	waitFor(t, "the /token token", tokenIs(p, cfgWith(configTok, ""), Token{endpointTok, SourceWrapper}))

	st, err := os.Stat(filepath.Join(dir, StoreFileName))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("stored copy mode %v, want 0600", st.Mode().Perm())
	}
	dst, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dst.Mode().Perm() != 0o700 {
		t.Fatalf("store dir mode %v, want 0700", dst.Mode().Perm())
	}
	if !strings.Contains(logs.String(), "using the wrapper's token from GET /token") {
		t.Fatalf("no log line for the new source:\n%s", logs.String())
	}
	assertNoToken(t, &logs, endpointTok)
}

func TestEndpointBeatsDataDirFile(t *testing.T) {
	dataDir := t.TempDir()
	writeFile(t, filepath.Join(dataDir, FileName), wrapperTok, time.Now())
	w := &fakeWrapper{}
	w.set(ok(endpointTok, 0))
	var logs lockedBuffer
	p, _ := endpointProvider(t, w.fetch, t.TempDir(), testAddress, &logs)
	if got := p.Token(cfgWith(configTok, dataDir)); got.Value != wrapperTok {
		t.Fatalf("before any /token answer: %d bytes, want the data_dir file", len(got.Value))
	}
	runLoop(t, p)
	waitFor(t, "/token to win over data_dir", tokenIs(p, cfgWith(configTok, dataDir), Token{endpointTok, SourceWrapper}))
}

func TestStoredCopyServesARestart(t *testing.T) {
	dir := t.TempDir()
	var logs lockedBuffer
	first := &fakeWrapper{}
	first.set(ok(endpointTok, 0))
	p, _ := endpointProvider(t, first.fetch, dir, testAddress, &logs)
	runLoop(t, p)
	waitFor(t, "the first run's token", tokenIs(p, cfgWith("", ""), Token{endpointTok, SourceWrapper}))

	// The second run's wrapper is restarting: nothing answers.
	down := &fakeWrapper{}
	down.set(fakeReply{err: errors.New("connection refused")})
	again, _ := endpointProvider(t, down.fetch, dir, testAddress, &logs)
	if got := again.Token(cfgWith(configTok, "")); got != (Token{endpointTok, SourceWrapper}) {
		t.Fatalf("after a restart: {%d bytes, %q}, want the stored copy", len(got.Value), got.Source)
	}
	runLoop(t, again)
	waitFor(t, "a failed fetch", func() bool { return down.calls.Load() >= 1 })
	if got := again.Token(cfgWith(configTok, "")); got.Value != endpointTok {
		t.Fatalf("a failed fetch dropped the stored copy: %d bytes", len(got.Value))
	}

	dataDir := t.TempDir()
	writeFile(t, filepath.Join(dataDir, FileName), wrapperTok, time.Now())
	if got := again.Token(cfgWith(configTok, dataDir)); got.Value != wrapperTok {
		t.Fatalf("data_dir file vs stored copy: %d bytes, want the file", len(got.Value))
	}

	other, _ := endpointProvider(t, down.fetch, dir, "http://another-wrapper:12340", &logs)
	if got := other.Token(cfgWith(configTok, "")); got.Source != SourceConfig {
		t.Fatalf("a copy from another wrapper.address was used: %q", got.Source)
	}
	assertNoToken(t, &logs, endpointTok, wrapperTok)
}

func TestLoggedOutDropsTheStoredCopy(t *testing.T) {
	dir := t.TempDir()
	if err := (tokenStore{dir: dir}).save(storedToken{Address: testAddress, MusicToken: endpointTok}); err != nil {
		t.Fatal(err)
	}
	var logs lockedBuffer
	w := &fakeWrapper{}
	w.set(fakeReply{err: wrapper.ErrNoToken})
	p, _ := endpointProvider(t, w.fetch, dir, testAddress, &logs)
	if got := p.Token(cfgWith(configTok, "")); got.Value != endpointTok {
		t.Fatal("the stored copy was not loaded")
	}
	runLoop(t, p)
	waitFor(t, "the config fallback", tokenIs(p, cfgWith(configTok, ""), Token{configTok, SourceConfig}))
	// The token stops being used first; the copy goes and the logout is
	// logged right after.
	waitFor(t, "the stored copy to go after a 503", storeGone(dir))
	waitFor(t, "the logout's log line", func() bool { return strings.Contains(logs.String(), "no logged-in account") })
	again, _ := endpointProvider(t, w.fetch, dir, testAddress, &logs)
	if got := again.Token(cfgWith("", "")); got != (Token{}) {
		t.Fatal("a restart brought the logged-out token back")
	}
	assertNoToken(t, &logs, endpointTok)
}

func TestNoTokenEndpointFallsBackAndIsNotHammered(t *testing.T) {
	dataDir := t.TempDir()
	writeFile(t, filepath.Join(dataDir, FileName), wrapperTok, time.Now())
	var logs lockedBuffer
	w := &fakeWrapper{}
	w.set(fakeReply{err: wrapper.ErrTokenUnsupported})
	p, _ := endpointProvider(t, w.fetch, t.TempDir(), testAddress, &logs)
	runLoop(t, p)
	waitFor(t, "the first /token answer", func() bool { return w.calls.Load() == 1 })
	for range 5 {
		p.RefreshSoon()
		time.Sleep(10 * time.Millisecond)
	}
	if n := w.calls.Load(); n != 1 {
		t.Fatalf("/token asked %d times after a 404, want 1", n)
	}
	if got := p.Token(cfgWith(configTok, dataDir)); got.Value != wrapperTok {
		t.Fatalf("after a 404: %d bytes, want the data_dir file", len(got.Value))
	}
	if c := strings.Count(logs.String(), "has no GET /token"); c != 1 {
		t.Fatalf("the 404 was logged %d times, want once:\n%s", c, logs.String())
	}
}

func TestRefetchesAndPicksUpTheNewToken(t *testing.T) {
	var logs lockedBuffer
	w := &fakeWrapper{}
	// next_refresh_at already passed: the floor applies.
	w.set(ok(endpointTok, 1), ok(newerTok, 1))
	p, r := endpointProvider(t, w.fetch, t.TempDir(), testAddress, &logs)
	r.floor = 20 * time.Millisecond
	runLoop(t, p)
	waitFor(t, "the refreshed token", tokenIs(p, cfgWith("", ""), Token{newerTok, SourceWrapper}))
	assertNoToken(t, &logs, endpointTok, newerTok)
}

func TestRefreshSoonFetches(t *testing.T) {
	var logs lockedBuffer
	w := &fakeWrapper{}
	w.set(ok(endpointTok, 0))
	p, _ := endpointProvider(t, w.fetch, t.TempDir(), testAddress, &logs)
	runLoop(t, p)
	waitFor(t, "the first fetch", func() bool { return w.calls.Load() == 1 })
	p.RefreshSoon()
	waitFor(t, "the nudged fetch", func() bool { return w.calls.Load() == 2 })
}

func TestRefreshSoonWaitsOutTheSpacing(t *testing.T) {
	var logs lockedBuffer
	w := &fakeWrapper{}
	w.set(ok(endpointTok, 0))
	p, r := endpointProvider(t, w.fetch, t.TempDir(), testAddress, &logs)
	r.nudgeSpacing = time.Hour
	runLoop(t, p)
	waitFor(t, "the first fetch", func() bool { return w.calls.Load() == 1 })
	for range 3 {
		p.RefreshSoon()
	}
	time.Sleep(50 * time.Millisecond)
	if n := w.calls.Load(); n != 1 {
		t.Fatalf("%d fetches, want 1: a nudge inside the spacing fetched", n)
	}
}

func TestWaitAfterToken(t *testing.T) {
	r := newRemote(Endpoint{Fetch: (&fakeWrapper{}).fetch, Dir: t.TempDir()})
	r.jitter = func() time.Duration { return 7 * time.Second }
	now := time.Unix(1_800_000_000, 0)
	tests := []struct {
		name string
		next time.Time
		want time.Duration
	}{
		{"wrapper's next refresh plus jitter", now.Add(30 * time.Minute), 30*time.Minute + 7*time.Second},
		{"no next_refresh_at", time.Time{}, refreshFloor},
		{"next_refresh_at passed", now.Add(-time.Hour), refreshFloor},
		{"next_refresh_at now", now, refreshFloor},
		{"too soon is the floor", now.Add(10 * time.Second), refreshFloor},
		{"far future is the ceiling", now.Add(48 * time.Hour), refreshCeiling},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.afterToken(now, tt.next); got != tt.want {
				t.Fatalf("afterToken = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFailureBacksOffAndKeepsTheToken(t *testing.T) {
	var logs lockedBuffer
	w := &fakeWrapper{}
	w.set(ok(endpointTok, 0))
	_, r := endpointProvider(t, w.fetch, t.TempDir(), testAddress, &logs)
	r.backoffMin, r.backoffMax = 10*time.Millisecond, 40*time.Millisecond
	ctx := context.Background()
	r.fetchOnce(ctx)
	w.set(fakeReply{err: errors.New("dial tcp: connection refused")})
	var waits []time.Duration
	for range 4 {
		waits = append(waits, r.fetchOnce(ctx))
	}
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("backoff %v, want %v", waits, want)
		}
	}
	if r.liveToken() != endpointTok {
		t.Fatal("a failed fetch dropped the token")
	}
	if c := strings.Count(logs.String(), "could not fetch"); c != 1 {
		t.Fatalf("a failure streak was logged %d times, want once", c)
	}
	w.set(ok("not a token", 0))
	r.fetchOnce(ctx)
	if r.liveToken() != endpointTok {
		t.Fatal("an invalid token replaced the good one")
	}
	assertNoToken(t, &logs, endpointTok)
}

func TestManagedProviderIgnoresTheEndpoint(t *testing.T) {
	var calls int
	p := New(managedReader(wrapperTok, nil, &calls))
	p.UseEndpoint(Endpoint{Fetch: (&fakeWrapper{}).fetch, Dir: t.TempDir()})
	if p.remote.Load() != nil {
		t.Fatal("a managed provider took an endpoint")
	}
	p.RefreshSoon()
	p.RunEndpoint(context.Background()) // returns at once
}

// TestRealClientAgainstAWrapper drives the provider through wrapper.Client
// and an HTTP wrapper: a 200, then a logout's 503.
func TestRealClientAgainstAWrapper(t *testing.T) {
	var loggedIn atomic.Bool
	loggedIn.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if !loggedIn.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 503, "msg": "music token not available"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "SUCCESS", "data": map[string]any{
			"region": "cn", "music_token": endpointTok, "refreshed_at": time.Now().Unix(), "next_refresh_at": time.Now().Unix() - 1,
		}})
	}))
	defer srv.Close()
	client, err := wrapper.NewClient(config.WrapperConfig{Address: srv.URL, TimeoutSeconds: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	dir := t.TempDir()
	var logs lockedBuffer
	p, r := endpointProvider(t, client.MusicToken, dir, srv.URL, &logs)
	r.floor, r.loggedOutRetry = 20*time.Millisecond, 20*time.Millisecond
	runLoop(t, p)
	waitFor(t, "the token over HTTP", tokenIs(p, cfgWith(configTok, ""), Token{endpointTok, SourceWrapper}))
	loggedIn.Store(false)
	waitFor(t, "the logout", tokenIs(p, cfgWith(configTok, ""), Token{configTok, SourceConfig}))
	waitFor(t, "the stored copy to go after a 503", storeGone(dir))
	assertNoToken(t, &logs, endpointTok)
}

func TestLeftoverTempCopyIsRemoved(t *testing.T) {
	// Glob metacharacters in the path must not hide the leftover.
	dir := filepath.Join(t.TempDir(), "data[1]")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	left := filepath.Join(dir, StoreFileName+".123.tmp")
	if err := os.WriteFile(left, []byte(endpointTok), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs lockedBuffer
	w := &fakeWrapper{}
	w.set(fakeReply{err: errors.New("connection refused")})
	endpointProvider(t, w.fetch, dir, testAddress, &logs)
	if _, err := os.Stat(left); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a crashed save's temporary copy survived a start: %v", err)
	}
}

func TestUnwritableStoreWarnsOnce(t *testing.T) {
	// A regular file where the store directory should be: every save fails.
	dir := filepath.Join(t.TempDir(), StoreDirName)
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var logs lockedBuffer
	w := &fakeWrapper{}
	w.set(ok(endpointTok, 0))
	p, _ := endpointProvider(t, w.fetch, dir, testAddress, &logs)
	runLoop(t, p)
	waitFor(t, "the /token token", tokenIs(p, cfgWith(configTok, ""), Token{endpointTok, SourceWrapper}))
	for i := int32(2); i <= 3; i++ {
		p.RefreshSoon()
		waitFor(t, "another fetch", func() bool { return w.calls.Load() >= i })
	}
	waitFor(t, "the third answer's log line", func() bool {
		return strings.Count(logs.String(), "using the wrapper's token from GET /token") >= 3
	})
	if c := strings.Count(logs.String(), "could not store a copy"); c != 1 {
		t.Fatalf("the unwritable store was logged %d times over 3 answers, want once:\n%s", c, logs.String())
	}
	assertNoToken(t, &logs, endpointTok)
}
