package applemusic

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/config"
)

func TestCacheUntilIsHalfLife(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name string
		ttl  time.Duration
	}{
		{"12h default", 12 * time.Hour},
		{"custom 6h", 6 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cacheUntil(now, tt.ttl)
			want := now.Add(tt.ttl / 2)
			if !got.Equal(want) {
				t.Fatalf("cacheUntil(%v, %v) = %v, want %v", now, tt.ttl, got, want)
			}
		})
	}
}

func TestTokenRefreshUsesHalfLife(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{TokenCacheTTLHours: 12}, slog.Default())
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://music.apple.com" {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<script src="/assets/index~abc123.js"></script>`)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`token:"eyJhbGciOiJFUzI1NiJ9.eyJmb28iOiJiYXIifQ.sig-part-here"`)), Header: make(http.Header)}, nil
	})}

	before := time.Now()
	if _, err := client.webToken.get(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}
	after := time.Now()

	wantTTL := client.cfg.TokenTTL()
	minUntil := before.Add(wantTTL / 2)
	maxUntil := after.Add(wantTTL / 2)
	if client.webToken.until.Before(minUntil) || client.webToken.until.After(maxUntil) {
		t.Fatalf("tokenUntil = %v, want within [%v, %v] (half of %v)", client.webToken.until, minUntil, maxUntil, wantTTL)
	}
}

func TestProactiveTokenRefresh(t *testing.T) {
	newScrapingClient := func(calls *int32) *CatalogClient {
		client := newTestCatalogClient(config.CatalogConfig{TokenCacheTTLHours: 12}, slog.Default())
		client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case "https://music.apple.com":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<script src="/assets/index~abc123.js"></script>`)), Header: make(http.Header)}, nil
			case "https://music.apple.com/assets/index~abc123.js":
				atomic.AddInt32(calls, 1)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`token:"eyJhbGciOiJFUzI1NiJ9.eyJmb28iOiJiYXIifQ.fresh-signature"`)), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("unexpected")), Header: make(http.Header)}, nil
			}
		})}
		return client
	}

	t.Run("renews a token near expiry", func(t *testing.T) {
		var calls int32
		client := newScrapingClient(&calls)
		client.webToken.value = "stale"
		client.webToken.until = time.Now().Add(tokenRefreshLead / 2)

		client.refreshExpiringWebToken(context.Background())

		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Fatalf("scrape calls = %d, want 1", got)
		}
		if client.webToken.value == "stale" || client.webToken.value == "" {
			t.Fatalf("webToken = %q, want a freshly scraped value", client.webToken.value)
		}
	})

	t.Run("leaves a token that is not near expiry", func(t *testing.T) {
		var calls int32
		client := newScrapingClient(&calls)
		client.webToken.value, client.webToken.until = "still-good", time.Now().Add(time.Hour)

		client.refreshExpiringWebToken(context.Background())

		if got := atomic.LoadInt32(&calls); got != 0 {
			t.Fatalf("scrape calls = %d, want 0 (nothing near expiry)", got)
		}
	})

	t.Run("never scrapes an empty cache", func(t *testing.T) {
		var calls int32
		client := newScrapingClient(&calls)
		// webToken left empty: nothing has been fetched yet, so the
		// proactive loop must not scrape Apple on its own.

		client.refreshExpiringWebToken(context.Background())

		if got := atomic.LoadInt32(&calls); got != 0 {
			t.Fatalf("scrape calls = %d, want 0 (empty cache must not trigger a scrape)", got)
		}
	})
}

func TestConcurrentTokenCacheMissScrapesOnce(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{TokenCacheTTLHours: 12}, slog.Default())
	var mu sync.Mutex
	homeCalls, jsCalls := 0, 0
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		switch req.URL.String() {
		case "https://music.apple.com":
			homeCalls++
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<script src="/assets/index~abc123.js"></script>`)), Header: make(http.Header)}, nil
		case "https://music.apple.com/assets/index~abc123.js":
			jsCalls++
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`token:"eyJhbGciOiJFUzI1NiJ9.eyJmb28iOiJiYXIifQ.sig-part-here"`)), Header: make(http.Header)}, nil
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("unexpected")), Header: make(http.Header)}, nil
		}
	})}

	const callers = 20
	start := make(chan struct{})
	errCh := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := client.webToken.get(context.Background())
			errCh <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("Token: %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if homeCalls != 1 || jsCalls != 1 {
		t.Fatalf("token scrape calls = home %d, js %d; want one each", homeCalls, jsCalls)
	}
}

func TestGetWithUserTokenRetriesOnce401ThenSucceeds(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.webToken.value = "stale-token"
	client.webToken.until = time.Now().Add(time.Hour)

	var catalogCalls int
	var authHeaders []string
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/v1/catalog/cn/songs/song-1" {
			catalogCalls++
			authHeaders = append(authHeaders, req.Header.Get("Authorization"))
			if catalogCalls == 1 {
				return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(`{"errors":[{"status":"401"}]}`)), Header: make(http.Header)}, nil
			}
			body := `{"data":[{"id":"song-1","type":"songs","attributes":{"name":"Song","artistName":"Artist","albumName":"Album"},"relationships":{}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		if req.URL.String() == "https://music.apple.com" {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<script src="/assets/index~abc123.js"></script>`)), Header: make(http.Header)}, nil
		}
		if req.URL.String() == "https://music.apple.com/assets/index~abc123.js" {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`token:"eyJhbGciOiJFUzI1NiJ9.eyJmb28iOiJiYXIifQ.sig-part-here"`)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("unexpected: " + req.URL.String())), Header: make(http.Header)}, nil
	})}

	song, err := client.SongMetadata(context.Background(), "cn", "song-1")
	if err != nil {
		t.Fatalf("expected success after 401 retry, got %v", err)
	}
	if song.ID != "song-1" {
		t.Fatalf("song = %+v", song)
	}
	if catalogCalls != 2 {
		t.Fatalf("catalog calls = %d, want 2 (401 then retry)", catalogCalls)
	}
	if len(authHeaders) != 2 || authHeaders[0] != "Bearer stale-token" || authHeaders[0] == authHeaders[1] {
		t.Fatalf("auth headers = %#v, want stale token then a different re-minted token", authHeaders)
	}
}

func TestGetWithUserTokenReturnsErrorAfterSingleRetryOn401Twice(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.webToken.value = "stale-token"
	client.webToken.until = time.Now().Add(time.Hour)

	var catalogCalls int
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/v1/catalog/cn/songs/song-1" {
			catalogCalls++
			return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(`{"errors":[{"status":"401"}]}`)), Header: make(http.Header)}, nil
		}
		if req.URL.String() == "https://music.apple.com" {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<script src="/assets/index~abc123.js"></script>`)), Header: make(http.Header)}, nil
		}
		if req.URL.String() == "https://music.apple.com/assets/index~abc123.js" {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`token:"eyJhbGciOiJFUzI1NiJ9.eyJmb28iOiJiYXIifQ.sig-part-here"`)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("unexpected: " + req.URL.String())), Header: make(http.Header)}, nil
	})}

	_, err := client.SongMetadata(context.Background(), "cn", "song-1")
	if err == nil {
		t.Fatal("expected error after repeated 401")
	}
	if catalogCalls != 2 {
		t.Fatalf("catalog calls = %d, want 2 (one retry, no infinite loop)", catalogCalls)
	}
}

// RunTokenRefresher blocks until its context ends and returns only after a
// refresh in flight has stopped, so shutdown can wait for it.
func TestRunTokenRefresherReturnsAfterCancelAndInFlightRefresh(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	var finished atomic.Bool
	client := newTestCatalogClient(config.CatalogConfig{TokenCacheTTLHours: 12}, slog.Default())
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		once.Do(func() { close(entered) })
		<-req.Context().Done()
		finished.Store(true)
		return nil, req.Context().Err()
	})}
	client.webToken.value = "stale"
	client.webToken.until = time.Now().Add(tokenRefreshLead / 2)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.runTokenRefresher(ctx, time.Millisecond)
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("refresher never refreshed the expiring token")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunTokenRefresher did not return after its context ended")
	}
	if !finished.Load() {
		t.Fatal("RunTokenRefresher returned while a refresh was still in flight")
	}
}
