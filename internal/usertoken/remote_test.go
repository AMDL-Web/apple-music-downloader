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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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

func (f *fakeWrapper) fetch(context.Context, string) (wrapper.TokenReply, error) {
	n := int(f.calls.Add(1))
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.replies[min(n, len(f.replies))-1]
	return r.reply, r.err
}

// endpointProvider is a Provider with GET /token answered by fetch and its
// store in dir, with the production refresh timing. Tests that run its loop
// do so inside a synctest bubble, where those waits pass in virtual time.
func endpointProvider(t *testing.T, fetch func(context.Context, string) (wrapper.TokenReply, error), dir, address string, logs *lockedBuffer) (*Provider, *remote) {
	t.Helper()
	p := New()
	p.UseEndpoint(Endpoint{
		Fetch: fetch, Dir: dir, Address: address,
		Logger: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	return p, p.remotes[implicitID]
}

// runLoop runs p's GET /token loops until the test ends and waits until they
// have made their first fetches. It must run inside a synctest bubble.
func runLoop(t *testing.T, p *Provider) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.RunEndpoint(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	synctest.Wait()
}

// advance moves the bubble's clock by d and waits for the loops to settle.
func advance(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
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

func assertToken(t *testing.T, p *Provider, want Token) {
	t.Helper()
	if got := p.Token(cfg0()); got != want {
		t.Fatalf("Token = {%d bytes, %q, %q}, want {%d bytes, %q, %q}", len(got.Value), got.Source, got.Account, len(want.Value), want.Source, want.Account)
	}
}

func assertCalls(t *testing.T, w *fakeWrapper, want int32) {
	t.Helper()
	if n := w.calls.Load(); n != want {
		t.Fatalf("/token asked %d times, want %d", n, want)
	}
}

func storeGone(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, StoreFileName))
	return errors.Is(err, os.ErrNotExist)
}

func TestEndpointTokenIsUsedAndStoredPrivately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), StoreDirName)
		var logs lockedBuffer
		w := &fakeWrapper{}
		w.set(ok(endpointTok, 0))
		p, _ := endpointProvider(t, w.fetch, dir, testAddress, &logs)
		runLoop(t, p)
		assertToken(t, p, Token{endpointTok, SourceWrapper, implicitID})

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
	})
}

func TestStoredCopyServesARestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		var logs lockedBuffer
		first := &fakeWrapper{}
		first.set(ok(endpointTok, 0))
		p, _ := endpointProvider(t, first.fetch, dir, testAddress, &logs)
		runLoop(t, p)
		assertToken(t, p, Token{endpointTok, SourceWrapper, implicitID})

		// The second run's wrapper is restarting: nothing answers.
		down := &fakeWrapper{}
		down.set(fakeReply{err: errors.New("connection refused")})
		again, _ := endpointProvider(t, down.fetch, dir, testAddress, &logs)
		if got := again.Token(cfg0()); got != (Token{endpointTok, SourceWrapper, implicitID}) {
			t.Fatalf("after a restart: {%d bytes, %q}, want the stored copy", len(got.Value), got.Source)
		}
		runLoop(t, again)
		assertCalls(t, down, 1)
		if got := again.Token(cfg0()); got.Value != endpointTok {
			t.Fatalf("a failed fetch dropped the stored copy: %d bytes", len(got.Value))
		}

		other, _ := endpointProvider(t, down.fetch, dir, "http://another-wrapper:12340", &logs)
		if got := other.Token(cfg0()); got.Source != SourceNone {
			t.Fatalf("a copy from another wrapper.address was used: %q", got.Source)
		}
		assertNoToken(t, &logs, endpointTok)
	})
}

func TestLoggedOutDropsTheStoredCopy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		if err := (tokenStore{dir: dir}).save(storedToken{Address: testAddress, MusicToken: endpointTok}); err != nil {
			t.Fatal(err)
		}
		var logs lockedBuffer
		w := &fakeWrapper{}
		w.set(fakeReply{err: wrapper.ErrNoToken})
		p, _ := endpointProvider(t, w.fetch, dir, testAddress, &logs)
		if got := p.Token(cfg0()); got.Value != endpointTok {
			t.Fatal("the stored copy was not loaded")
		}
		runLoop(t, p)
		assertToken(t, p, Token{Account: implicitID})
		if !storeGone(dir) {
			t.Fatal("the stored copy survived a 503")
		}
		if !strings.Contains(logs.String(), "no logged-in account") {
			t.Fatalf("the logout was not logged:\n%s", logs.String())
		}
		again, _ := endpointProvider(t, w.fetch, dir, testAddress, &logs)
		if got := again.Token(cfg0()); got != (Token{Account: implicitID}) {
			t.Fatal("a restart brought the logged-out token back")
		}
		assertNoToken(t, &logs, endpointTok)
	})
}

func TestNoTokenEndpointIsNotHammered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs lockedBuffer
		w := &fakeWrapper{}
		w.set(fakeReply{err: wrapper.ErrTokenUnsupported})
		p, _ := endpointProvider(t, w.fetch, t.TempDir(), testAddress, &logs)
		runLoop(t, p)
		assertCalls(t, w, 1)
		// Nudges spread over almost the whole retry interval fetch nothing.
		for range 5 {
			p.RefreshSoon()
			advance(unsupportedRetry / 6)
		}
		assertCalls(t, w, 1)
		if got := p.Token(cfg0()); got.Value != "" {
			t.Fatalf("after a 404: %d bytes, want none", len(got.Value))
		}
		advance(unsupportedRetry / 6)
		assertCalls(t, w, 2)
		if c := strings.Count(logs.String(), "has no GET /token"); c != 1 {
			t.Fatalf("the 404 was logged %d times, want once:\n%s", c, logs.String())
		}
	})
}

func TestRefetchesAndPicksUpTheNewToken(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs lockedBuffer
		w := &fakeWrapper{}
		// next_refresh_at already passed: the floor applies.
		w.set(ok(endpointTok, 1), ok(newerTok, 1))
		p, _ := endpointProvider(t, w.fetch, t.TempDir(), testAddress, &logs)
		runLoop(t, p)
		assertToken(t, p, Token{endpointTok, SourceWrapper, implicitID})
		advance(refreshFloor - time.Nanosecond)
		assertCalls(t, w, 1)
		advance(time.Nanosecond)
		assertCalls(t, w, 2)
		assertToken(t, p, Token{newerTok, SourceWrapper, implicitID})
		assertNoToken(t, &logs, endpointTok, newerTok)
	})
}

func TestRefreshSoonFetches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs lockedBuffer
		w := &fakeWrapper{}
		w.set(ok(endpointTok, 0))
		p, _ := endpointProvider(t, w.fetch, t.TempDir(), testAddress, &logs)
		runLoop(t, p)
		assertCalls(t, w, 1)
		advance(nudgeSpacing)
		p.RefreshSoon()
		synctest.Wait()
		assertCalls(t, w, 2)
	})
}

func TestRefreshSoonWaitsOutTheSpacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs lockedBuffer
		w := &fakeWrapper{}
		w.set(ok(endpointTok, 0))
		p, _ := endpointProvider(t, w.fetch, t.TempDir(), testAddress, &logs)
		runLoop(t, p)
		assertCalls(t, w, 1)
		for range 3 {
			p.RefreshSoon()
		}
		advance(nudgeSpacing - time.Nanosecond)
		assertCalls(t, w, 1) // a nudge inside the spacing fetched
		advance(time.Nanosecond)
		assertCalls(t, w, 2) // the three nudges made one fetch, once allowed
	})
}

func TestWaitAfterToken(t *testing.T) {
	r := newRemote(Endpoint{Fetch: (&fakeWrapper{}).fetch, Dir: t.TempDir()}, implicitID, "")
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
	ctx := context.Background()
	r.fetchOnce(ctx)
	w.set(fakeReply{err: errors.New("dial tcp: connection refused")})
	var waits []time.Duration
	for range 8 {
		waits = append(waits, r.fetchOnce(ctx))
	}
	want := []time.Duration{backoffMin, 2 * backoffMin, 4 * backoffMin, 8 * backoffMin, 16 * backoffMin, 32 * backoffMin, backoffMax, backoffMax}
	if !slices.Equal(waits, want) {
		t.Fatalf("backoff %v, want %v", waits, want)
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

// TestRealClientAgainstAWrapper drives the provider through wrapper.Client
// and an HTTP wrapper: a 200, then a logout's 503. Real network I/O cannot
// run in a synctest bubble, so this one shortens the waits and polls.
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
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.RunEndpoint(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	tokenIs := func(want Token) func() bool { return func() bool { return p.Token(cfg0()) == want } }
	waitFor("the token over HTTP", tokenIs(Token{endpointTok, SourceWrapper, implicitID}))
	loggedIn.Store(false)
	waitFor("the logout", tokenIs(Token{Account: implicitID}))
	waitFor("the stored copy to go after a 503", func() bool { return storeGone(dir) })
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
	synctest.Test(t, func(t *testing.T) {
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
		assertToken(t, p, Token{endpointTok, SourceWrapper, implicitID})
		advance(2 * refreshFloor)
		assertCalls(t, w, 3)
		if c := strings.Count(logs.String(), "using the wrapper's token from GET /token"); c != 3 {
			t.Fatalf("%d answers logged, want 3:\n%s", c, logs.String())
		}
		if c := strings.Count(logs.String(), "could not store a copy"); c != 1 {
			t.Fatalf("the unwritable store was logged %d times over 3 answers, want once:\n%s", c, logs.String())
		}
		assertNoToken(t, &logs, endpointTok)
	})
}
