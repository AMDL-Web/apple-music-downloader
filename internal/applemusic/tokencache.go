package applemusic

import (
	"context"
	"sync"
	"time"
)

// tokenCache holds one bearer token and replaces it through fetch when it is
// empty or past its cached expiry. A fetched token is trusted for half of the
// TTL fetch reports (cacheUntil).
//
// mu guards value and until and is never held across fetch. refreshMu
// serializes fetches, and the cache is checked again once it is held, so a
// burst of callers shares one scrape or signature instead of stampeding Apple.
type tokenCache struct {
	fetch func(ctx context.Context) (token string, ttl time.Duration, err error)

	refreshMu sync.Mutex
	mu        sync.Mutex
	value     string
	until     time.Time
}

// cacheUntil is the half-life refresh policy: a token is trusted from the cache
// for half of its validity window (ttl), so it rotates well before Apple's own
// expiry, or an early rejection of a still-valid token, could stall requests
// until a full TTL passes.
func cacheUntil(now time.Time, ttl time.Duration) time.Time {
	return now.Add(ttl / 2)
}

// get returns the cached token, fetching a new one when the cache is empty or
// expired.
func (t *tokenCache) get(ctx context.Context) (string, error) {
	if token, ok := t.current(); ok {
		return token, nil
	}
	t.refreshMu.Lock()
	defer t.refreshMu.Unlock()
	if token, ok := t.current(); ok {
		return token, nil
	}
	return t.refreshLocked(ctx)
}

// invalidate clears rejected only while it is still the cached token. A
// concurrent request may already have replaced it after this one was sent; an
// old 401 must not evict that newer token.
func (t *tokenCache) invalidate(rejected string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.value == rejected {
		t.value = ""
		t.until = time.Time{}
	}
}

// refreshIfExpiring fetches a replacement when the cached token is within lead
// of its cached expiry. An empty cache is left alone, so a token nothing has
// asked for is never fetched in the background. On error the old token stays
// cached until it expires.
func (t *tokenCache) refreshIfExpiring(ctx context.Context, lead time.Duration) error {
	if !t.expiring(lead) {
		return nil
	}
	t.refreshMu.Lock()
	defer t.refreshMu.Unlock()
	// A get may have refreshed while this waited for refreshMu.
	if !t.expiring(lead) {
		return nil
	}
	_, err := t.refreshLocked(ctx)
	return err
}

func (t *tokenCache) current() (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.value != "" && time.Now().Before(t.until) {
		return t.value, true
	}
	return "", false
}

func (t *tokenCache) expiring(lead time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.value != "" && !time.Now().Before(t.until.Add(-lead))
}

// refreshLocked fetches and stores a new token. The caller holds refreshMu.
func (t *tokenCache) refreshLocked(ctx context.Context) (string, error) {
	token, ttl, err := t.fetch(ctx)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	t.value = token
	t.until = cacheUntil(time.Now(), ttl)
	t.mu.Unlock()
	return token, nil
}
