package applemusic

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"amdl/internal/config"
)

// The scrape reads the index script's path out of the web player's page, the
// JWT out of that script, and reports the configured TTL; the half-life
// caching on top of it is tokenCache's (tokencache_test.go).
func TestScrapeWebTokenReadsTheJWTFromThePlayerScript(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	client := newTestClient(t, config.CatalogConfig{TokenCacheTTLHours: 6}, func(req *http.Request) *http.Response {
		mu.Lock()
		paths = append(paths, req.URL.Host+req.URL.Path)
		mu.Unlock()
		if resp, ok := scrapeResponse(req); ok {
			return resp
		}
		return jsonResponse(http.StatusNotFound, "unexpected")
	})

	token, ttl, err := client.scrapeWebToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "eyJhbGciOiJFUzI1NiJ9.eyJmb28iOiJiYXIifQ.refreshed-sig" || ttl != 6*time.Hour {
		t.Fatalf("scrape = (%q, %v), want the script's JWT and the configured 6h", token, ttl)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"music.apple.com/", "music.apple.com/assets/index~abc123.js"}; len(paths) != 2 || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("requests = %q, want %q", paths, want)
	}
}

func TestGetWithUserTokenRetriesOnce401ThenSucceeds(t *testing.T) {
	var catalogCalls int
	var authHeaders []string
	client := newTestClient(t, config.CatalogConfig{Language: "en-US"}, func(req *http.Request) *http.Response {
		if resp, ok := scrapeResponse(req); ok {
			return resp
		}
		if req.URL.Path != "/v1/catalog/cn/songs/song-1" {
			return jsonResponse(http.StatusNotFound, "unexpected: "+req.URL.String())
		}
		catalogCalls++
		authHeaders = append(authHeaders, req.Header.Get("Authorization"))
		if catalogCalls == 1 {
			return jsonResponse(http.StatusUnauthorized, `{"errors":[{"status":"401"}]}`)
		}
		return jsonResponse(http.StatusOK, `{"data":[{"id":"song-1","type":"songs","attributes":{"name":"Song","artistName":"Artist","albumName":"Album"},"relationships":{}}]}`)
	})

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
	if len(authHeaders) != 2 || authHeaders[0] != "Bearer "+testWebToken || authHeaders[0] == authHeaders[1] {
		t.Fatalf("auth headers = %#v, want the cached token then a different re-minted token", authHeaders)
	}
}

func TestGetWithUserTokenReturnsErrorAfterSingleRetryOn401Twice(t *testing.T) {
	var catalogCalls int
	client := newTestClient(t, config.CatalogConfig{Language: "en-US"}, func(req *http.Request) *http.Response {
		if resp, ok := scrapeResponse(req); ok {
			return resp
		}
		if req.URL.Path != "/v1/catalog/cn/songs/song-1" {
			return jsonResponse(http.StatusNotFound, "unexpected: "+req.URL.String())
		}
		catalogCalls++
		return jsonResponse(http.StatusUnauthorized, `{"errors":[{"status":"401"}]}`)
	})

	if _, err := client.SongMetadata(context.Background(), "cn", "song-1"); err == nil {
		t.Fatal("expected error after repeated 401")
	}
	if catalogCalls != 2 {
		t.Fatalf("catalog calls = %d, want 2 (one retry, no infinite loop)", catalogCalls)
	}
}

// RunTokenRefresher wakes every tokenRefreshCheckInterval, never scrapes a
// token nothing has used, renews a used one once it is within tokenRefreshLead
// of its cached expiry, and returns only after a refresh in flight has stopped,
// so shutdown can wait for it.
func TestRunTokenRefresher(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := newTestCatalogClient(config.CatalogConfig{TokenCacheTTLHours: 12}, slog.Default())
		var fetches atomic.Int32
		var fetchedAt time.Time
		entered := make(chan struct{})
		var once sync.Once
		var finished atomic.Bool
		client.webToken.fetch = func(ctx context.Context) (string, time.Duration, error) {
			fetches.Add(1)
			once.Do(func() {
				fetchedAt = time.Now()
				close(entered)
			})
			<-ctx.Done()
			finished.Store(true)
			return "", 0, ctx.Err()
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			client.RunTokenRefresher(ctx)
			close(done)
		}()

		// Nothing has fetched a token yet: the refresher leaves the cache alone.
		time.Sleep(3 * tokenRefreshCheckInterval)
		synctest.Wait()
		if got := fetches.Load(); got != 0 {
			t.Fatalf("fetches with an empty cache = %d, want 0", got)
		}

		start := time.Now()
		client.webToken.mu.Lock()
		client.webToken.value, client.webToken.until = "used", start.Add(time.Hour)
		client.webToken.mu.Unlock()

		<-entered
		if want := start.Add(time.Hour - tokenRefreshLead); !fetchedAt.Equal(want) {
			t.Fatalf("refresh began at +%v, want +%v (tokenRefreshLead before the cached expiry)", fetchedAt.Sub(start), want.Sub(start))
		}
		cancel()
		<-done
		if !finished.Load() {
			t.Fatal("RunTokenRefresher returned while a refresh was still in flight")
		}
	})
}
