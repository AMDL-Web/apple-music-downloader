package applemusic

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"
)

var (
	indexJSPattern = regexp.MustCompile(`/assets/index~[^"']+\.js`)
	jwtPattern     = regexp.MustCompile(`eyJ[A-Za-z0-9_\-=]+\.[A-Za-z0-9_\-=]+\.[A-Za-z0-9_\-=]+`)
)

// scrapeWebToken is webToken's fetch: it reads the web-player JWT out of
// music.apple.com's JS bundle.
func (c *CatalogClient) scrapeWebToken(ctx context.Context) (string, time.Duration, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://music.apple.com", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := c.doAppleRequest(ctx, req, false)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("fetch Apple Music web player failed: %s", resp.Status)
	}
	body, err := readLimited(resp.Body, maxWebPlayerPageBytes)
	if err != nil {
		return "", 0, err
	}
	jsPath := indexJSPattern.FindString(string(body))
	if jsPath == "" {
		return "", 0, fmt.Errorf("cannot find Apple Music index js")
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "https://music.apple.com"+jsPath, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	jsResp, err := c.doAppleRequest(ctx, req, false)
	if err != nil {
		return "", 0, err
	}
	defer jsResp.Body.Close()
	if jsResp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("fetch Apple Music web player script failed: %s", jsResp.Status)
	}
	jsBody, err := readLimited(jsResp.Body, maxWebPlayerJSBytes)
	if err != nil {
		return "", 0, err
	}
	token := jwtPattern.FindString(string(jsBody))
	if token == "" {
		return "", 0, fmt.Errorf("cannot find Apple Music authorization token")
	}
	return token, c.cfg.TokenTTL(), nil
}

// tokenRefreshCheckInterval bounds how often the background refresher run by
// RunTokenRefresher wakes to re-check the scraped-token caches. Each wake
// only compares timestamps under a lock; it scrapes Apple solely when a cached
// token is within tokenRefreshLead of its cached expiry.
const tokenRefreshCheckInterval = time.Minute

// tokenRefreshLead is how far before a scraped token's cached expiry the
// background refresher renews it, so a request never has to scrape inline and
// the token never lapses in the gap between two refresh checks. It stays
// comfortably below the smallest realistic effective TTL (half of the one-hour
// minimum token_cache_ttl) and above tokenRefreshCheckInterval.
const tokenRefreshLead = 5 * time.Minute

// RunTokenRefresher proactively re-scrapes the music.apple.com web tokens
// shortly before their cached validity lapses, so catalog and enhanced-HLS
// requests never pay the inline scrape latency and never hit a lapsed token. It
// only renews a token that has already been fetched at least once (a non-empty
// cache); it never scrapes a token nothing has used yet. It blocks until ctx is
// done and returns once a refresh in flight has stopped; run it once, on its
// own goroutine.
func (c *CatalogClient) RunTokenRefresher(ctx context.Context) {
	c.runTokenRefresher(ctx, tokenRefreshCheckInterval)
}

func (c *CatalogClient) runTokenRefresher(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshExpiringWebToken(ctx)
		}
	}
}

// refreshExpiringWebToken renews the scraped token when it is within
// tokenRefreshLead of its cached expiry.
func (c *CatalogClient) refreshExpiringWebToken(ctx context.Context) {
	if err := c.webToken.refreshIfExpiring(ctx, tokenRefreshLead); err != nil {
		c.logger.Warn("proactive web token refresh failed; keeping cached token until it expires", "error", err)
	}
}

// appleRequest is an authorized Apple request as doWithAuth rebuilds it for
// its retry. Every caller sends no body.
type appleRequest struct {
	method string
	url    string
	// mediaUserToken, when set, is sent as Media-User-Token for reads that
	// need the user's subscription identity.
	mediaUserToken string
}

// doWithWebAuth sends r with the scraped web-player JWT and the Origin header
// that token is scoped to. On a 401 it drops that token (only if it is still
// the cached one), fetches another and sends r once more; any other status, a
// second 401 included, goes back to the caller as-is.
func (c *CatalogClient) doWithWebAuth(ctx context.Context, r appleRequest) (*http.Response, error) {
	send := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, r.method, r.url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if r.mediaUserToken != "" {
			req.Header.Set("Media-User-Token", r.mediaUserToken)
		}
		req.Header.Set("Origin", "https://music.apple.com")
		req.Header.Set("User-Agent", "Mozilla/5.0")
		return c.doAppleRequest(ctx, req, true)
	}

	token, err := c.webToken.get(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := send(token)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	resp.Body.Close()

	c.webToken.invalidate(token)
	token, err = c.webToken.get(ctx)
	if err != nil {
		return nil, err
	}
	return send(token)
}
