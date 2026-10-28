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
		{"signed 24h internal ttl", internalDeveloperTokenTTL},
		{"legacy 12h default", 12 * time.Hour},
		{"legacy custom 6h", 6 * time.Hour},
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

func TestInitDeveloperTokenCachesForHalfTheSignedLifetime(t *testing.T) {
	path, _ := writeP8Key(t)
	cfg := config.CatalogConfig{
		AppleMusicPrivateKeyPath: path,
		AppleMusicKeyID:          "KID1234567",
		AppleMusicTeamID:         "TEAM123456",
	}
	client := newTestCatalogClient(cfg, slog.Default())

	before := time.Now()
	if err := client.InitDeveloperToken(); err != nil {
		t.Fatalf("InitDeveloperToken: %v", err)
	}
	after := time.Now()

	minUntil := before.Add(internalDeveloperTokenTTL / 2)
	maxUntil := after.Add(internalDeveloperTokenTTL / 2)
	if client.catalogToken.until.Before(minUntil) || client.catalogToken.until.After(maxUntil) {
		t.Fatalf("tokenUntil = %v, want within [%v, %v] (half of %v, not exp-5m)", client.catalogToken.until, minUntil, maxUntil, internalDeveloperTokenTTL)
	}
}

func TestTokenRefreshUsesHalfLifeForBothModes(t *testing.T) {
	t.Run("signed", func(t *testing.T) {
		path, _ := writeP8Key(t)
		cfg := config.CatalogConfig{
			AppleMusicPrivateKeyPath: path,
			AppleMusicKeyID:          "KID1234567",
			AppleMusicTeamID:         "TEAM123456",
		}
		client := newTestCatalogClient(cfg, slog.Default())
		client.signer, _ = newDeveloperTokenSigner(path, "KID1234567", "TEAM123456")
		// Force a refresh by expiring the cached token.
		client.catalogToken.value = "stale"
		client.catalogToken.until = time.Now().Add(-time.Minute)

		before := time.Now()
		if _, err := client.catalogToken.get(context.Background()); err != nil {
			t.Fatalf("Token: %v", err)
		}
		after := time.Now()

		minUntil := before.Add(internalDeveloperTokenTTL / 2)
		maxUntil := after.Add(internalDeveloperTokenTTL / 2)
		if client.catalogToken.until.Before(minUntil) || client.catalogToken.until.After(maxUntil) {
			t.Fatalf("tokenUntil = %v, want within [%v, %v]", client.catalogToken.until, minUntil, maxUntil)
		}
	})

	t.Run("legacy", func(t *testing.T) {
		client := newTestCatalogClient(config.CatalogConfig{TokenCacheTTLHours: 12}, slog.Default())
		client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() == "https://music.apple.com" {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<script src="/assets/index~abc123.js"></script>`)), Header: make(http.Header)}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`token:"eyJhbGciOiJFUzI1NiJ9.eyJmb28iOiJiYXIifQ.sig-part-here"`)), Header: make(http.Header)}, nil
		})}

		before := time.Now()
		if _, err := client.catalogToken.get(context.Background()); err != nil {
			t.Fatalf("Token: %v", err)
		}
		after := time.Now()

		wantTTL := client.cfg.TokenTTL()
		minUntil := before.Add(wantTTL / 2)
		maxUntil := after.Add(wantTTL / 2)
		if client.catalogToken.until.Before(minUntil) || client.catalogToken.until.After(maxUntil) {
			t.Fatalf("tokenUntil = %v, want within [%v, %v] (half of %v)", client.catalogToken.until, minUntil, maxUntil, wantTTL)
		}
	})
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

	t.Run("renews a legacy token near expiry", func(t *testing.T) {
		var calls int32
		client := newScrapingClient(&calls)
		client.catalogToken.value = "stale"
		client.catalogToken.until = time.Now().Add(tokenRefreshLead / 2) // inside the lead window

		client.refreshExpiringWebTokens(context.Background())

		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Fatalf("scrape calls = %d, want 1", got)
		}
		if client.catalogToken.value == "stale" || client.catalogToken.value == "" {
			t.Fatalf("token = %q, want a freshly scraped value", client.catalogToken.value)
		}
		if !client.catalogToken.until.After(time.Now().Add(tokenRefreshLead)) {
			t.Fatalf("tokenUntil = %v, want pushed well past the lead window", client.catalogToken.until)
		}
	})

	t.Run("renews a web token near expiry", func(t *testing.T) {
		var calls int32
		client := newScrapingClient(&calls)
		client.webToken.value = "stale"
		client.webToken.until = time.Now().Add(tokenRefreshLead / 2)

		client.refreshExpiringWebTokens(context.Background())

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
		client.catalogToken.value, client.catalogToken.until = "still-good", time.Now().Add(time.Hour)
		client.webToken.value, client.webToken.until = "still-good", time.Now().Add(time.Hour)

		client.refreshExpiringWebTokens(context.Background())

		if got := atomic.LoadInt32(&calls); got != 0 {
			t.Fatalf("scrape calls = %d, want 0 (nothing near expiry)", got)
		}
	})

	t.Run("never scrapes an empty cache", func(t *testing.T) {
		var calls int32
		client := newScrapingClient(&calls)
		// token and webToken left empty: nothing has been fetched yet, so the
		// proactive loop must not scrape Apple on its own.

		client.refreshExpiringWebTokens(context.Background())

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
			_, err := client.catalogToken.get(context.Background())
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
	client.catalogToken.value = "stale-token"
	client.catalogToken.until = time.Now().Add(time.Hour)

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
	client.catalogToken.value = "stale-token"
	client.catalogToken.until = time.Now().Add(time.Hour)

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
