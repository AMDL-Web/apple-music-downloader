package applemusic

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingFetch returns a fetch func that hands out tok-1, tok-2, ... with ttl
// and counts its calls.
func countingFetch(calls *atomic.Int32, ttl time.Duration) func(context.Context) (string, time.Duration, error) {
	return func(context.Context) (string, time.Duration, error) {
		n := calls.Add(1)
		return fmt.Sprintf("tok-%d", n), ttl, nil
	}
}

func TestTokenCacheGetFetchesOnceAndCachesForHalfTheTTL(t *testing.T) {
	var calls atomic.Int32
	cache := &tokenCache{fetch: countingFetch(&calls, 2*time.Hour)}

	before := time.Now()
	first, err := cache.get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	second, err := cache.get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != "tok-1" || second != "tok-1" || calls.Load() != 1 {
		t.Fatalf("tokens = %q, %q after %d fetches; want tok-1 twice from one fetch", first, second, calls.Load())
	}
	if cache.until.Before(before.Add(time.Hour)) || cache.until.After(after.Add(time.Hour)) {
		t.Fatalf("until = %v, want one hour (half of the 2h TTL) after the fetch", cache.until)
	}
}

func TestTokenCacheGetRefetchesAnExpiredToken(t *testing.T) {
	var calls atomic.Int32
	cache := &tokenCache{fetch: countingFetch(&calls, time.Hour)}
	cache.value, cache.until = "old", time.Now().Add(-time.Second)

	got, err := cache.get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "tok-1" || calls.Load() != 1 {
		t.Fatalf("get = %q after %d fetches, want a fresh tok-1", got, calls.Load())
	}
}

func TestTokenCacheConcurrentMissesShareOneFetch(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	cache := &tokenCache{fetch: func(context.Context) (string, time.Duration, error) {
		calls.Add(1)
		<-release
		return "shared", time.Hour, nil
	}}

	const callers = 16
	var wg sync.WaitGroup
	results := make(chan string, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := cache.get(context.Background())
			if err != nil {
				t.Error(err)
			}
			results <- token
		}()
	}
	close(release)
	wg.Wait()
	close(results)
	for token := range results {
		if token != "shared" {
			t.Fatalf("token = %q, want shared", token)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1", got)
	}
}

func TestTokenCacheFetchErrorLeavesCacheEmpty(t *testing.T) {
	boom := errors.New("scrape failed")
	cache := &tokenCache{fetch: func(context.Context) (string, time.Duration, error) { return "", 0, boom }}

	if _, err := cache.get(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("get error = %v, want %v", err, boom)
	}
	if cache.value != "" {
		t.Fatalf("cached value = %q after a failed fetch, want empty", cache.value)
	}
}

func TestTokenCacheInvalidateEvictsOnlyTheRejectedToken(t *testing.T) {
	cache := &tokenCache{}
	until := time.Now().Add(time.Hour)
	cache.value, cache.until = "newer", until

	cache.invalidate("older")
	if cache.value != "newer" || !cache.until.Equal(until) {
		t.Fatalf("a stale rejection evicted the newer token: value=%q until=%v", cache.value, cache.until)
	}
	cache.invalidate("newer")
	if cache.value != "" || !cache.until.IsZero() {
		t.Fatalf("rejected token still cached: value=%q until=%v", cache.value, cache.until)
	}
}

func TestTokenCacheRefreshIfExpiring(t *testing.T) {
	const lead = 5 * time.Minute
	tests := []struct {
		name      string
		value     string
		until     time.Duration // from now
		wantFetch int32
		wantValue string
	}{
		{name: "empty cache is never fetched", value: "", until: 0, wantFetch: 0, wantValue: ""},
		{name: "token outside the lead is kept", value: "cached", until: time.Hour, wantFetch: 0, wantValue: "cached"},
		{name: "token inside the lead is renewed", value: "cached", until: lead / 2, wantFetch: 1, wantValue: "tok-1"},
		{name: "expired token is renewed", value: "cached", until: -time.Minute, wantFetch: 1, wantValue: "tok-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			cache := &tokenCache{fetch: countingFetch(&calls, time.Hour)}
			cache.value = tt.value
			if tt.value != "" {
				cache.until = time.Now().Add(tt.until)
			}
			if err := cache.refreshIfExpiring(context.Background(), lead); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != tt.wantFetch || cache.value != tt.wantValue {
				t.Fatalf("fetches = %d, value = %q; want %d, %q", calls.Load(), cache.value, tt.wantFetch, tt.wantValue)
			}
		})
	}
}

func TestTokenCacheFailedRefreshKeepsTheCachedToken(t *testing.T) {
	boom := errors.New("scrape failed")
	cache := &tokenCache{fetch: func(context.Context) (string, time.Duration, error) { return "", 0, boom }}
	until := time.Now().Add(time.Minute)
	cache.value, cache.until = "still-valid", until

	if err := cache.refreshIfExpiring(context.Background(), 5*time.Minute); !errors.Is(err, boom) {
		t.Fatalf("refreshIfExpiring error = %v, want %v", err, boom)
	}
	if got, err := cache.get(context.Background()); err != nil || got != "still-valid" {
		t.Fatalf("get after failed refresh = %q, %v; want the old token until it expires", got, err)
	}
}
