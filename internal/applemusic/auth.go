package applemusic

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"
)

// InitDeveloperToken loads the signing key and mints the first developer token
// when signing is configured. It must be called once at startup; any error
// should stop the process from starting. It is a no-op in legacy mode.
func (c *CatalogClient) InitDeveloperToken() error {
	if !c.cfg.DeveloperTokenSigningEnabled() {
		return nil
	}
	signer, err := newDeveloperTokenSigner(c.cfg.AppleMusicPrivateKeyPath, c.cfg.AppleMusicKeyID, c.cfg.AppleMusicTeamID)
	if err != nil {
		return err
	}
	c.signer = signer
	token, _, err := signer.sign(time.Now(), internalDeveloperTokenTTL, nil)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.token = token
	// Cache for only half the signed lifetime: a still-valid-on-paper token
	// can start getting rejected by Apple before its exp claim, so refreshing
	// proactively at the half-life avoids waiting for a 401 to notice.
	c.tokenUntil = cacheUntil(time.Now(), internalDeveloperTokenTTL)
	c.mu.Unlock()
	return nil
}

// MintDeveloperToken signs a fresh developer token for external clients using
// the configured endpoint TTL and allowed-origins list. It fails when
// developer-token signing is disabled (legacy web-token mode). Expiry is not
// returned: clients read the exp claim from the JWT payload.
func (c *CatalogClient) MintDeveloperToken() (string, error) {
	if c.signer == nil {
		return "", fmt.Errorf("developer token signing is not configured")
	}
	token, _, err := c.signer.sign(time.Now(), c.cfg.DeveloperTokenTTL(), c.cfg.AllowedOrigins)
	return token, err
}

var (
	indexJSPattern = regexp.MustCompile(`/assets/index~[^"']+\.js`)
	jwtPattern     = regexp.MustCompile(`eyJ[A-Za-z0-9_\-=]+\.[A-Za-z0-9_\-=]+\.[A-Za-z0-9_\-=]+`)
)

// cacheUntil is the half-life proactive-refresh policy: a token is only
// trusted from the cache for half of its validity window (ttl), so it
// rotates well before Apple's own expiry - or an early rejection of a
// still-technically-valid token - could otherwise stall catalog requests
// until a full TTL passes.
func cacheUntil(now time.Time, ttl time.Duration) time.Time {
	return now.Add(ttl / 2)
}

func (c *CatalogClient) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.tokenUntil) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	c.tokenRefreshMu.Lock()
	defer c.tokenRefreshMu.Unlock()
	// Another caller may have refreshed while this one waited.
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.tokenUntil) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	if c.signer != nil {
		token, _, err := c.signer.sign(time.Now(), internalDeveloperTokenTTL, nil)
		if err != nil {
			return "", err
		}
		c.mu.Lock()
		c.token = token
		c.tokenUntil = cacheUntil(time.Now(), internalDeveloperTokenTTL)
		c.mu.Unlock()
		return token, nil
	}

	token, err := c.fetchToken(ctx)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.token = token
	c.tokenUntil = cacheUntil(time.Now(), c.cfg.TokenTTL())
	c.mu.Unlock()
	return token, nil
}

// invalidateToken clears rejected only when it is still the cached token. A
// concurrent request may already have refreshed the cache after this request
// was sent; an old 401 must not evict that newer token.
func (c *CatalogClient) invalidateToken(rejected string) {
	c.mu.Lock()
	if c.token == rejected {
		c.token = ""
		c.tokenUntil = time.Time{}
	}
	c.mu.Unlock()
}

// doWithCatalogAuth sends a request built by buildReq (called with the
// current bearer token) and, if Apple responds 401, invalidates the cached
// token, re-authenticates once via Token(), and retries the same request
// exactly once with the new token. Any other status - including a second
// 401 - is returned as-is; callers keep their existing status/body handling.
func (c *CatalogClient) doWithCatalogAuth(ctx context.Context, buildReq func(token string) (*http.Request, error)) (*http.Response, error) {
	token, err := c.Token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := buildReq(token)
	if err != nil {
		return nil, err
	}
	resp, err := c.doAppleRequest(ctx, req, true)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	resp.Body.Close()

	c.invalidateToken(token)
	token, err = c.Token(ctx)
	if err != nil {
		return nil, err
	}
	req, err = buildReq(token)
	if err != nil {
		return nil, err
	}
	return c.doAppleRequest(ctx, req, true)
}

func (c *CatalogClient) fetchToken(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://music.apple.com", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := c.doAppleRequest(ctx, req, false)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch Apple Music web player failed: %s", resp.Status)
	}
	body, err := readLimited(resp.Body, maxWebPlayerPageBytes)
	if err != nil {
		return "", err
	}
	jsPath := indexJSPattern.FindString(string(body))
	if jsPath == "" {
		return "", fmt.Errorf("cannot find Apple Music index js")
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "https://music.apple.com"+jsPath, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	jsResp, err := c.doAppleRequest(ctx, req, false)
	if err != nil {
		return "", err
	}
	defer jsResp.Body.Close()
	if jsResp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch Apple Music web player script failed: %s", jsResp.Status)
	}
	jsBody, err := readLimited(jsResp.Body, maxWebPlayerJSBytes)
	if err != nil {
		return "", err
	}
	token := jwtPattern.FindString(string(jsBody))
	if token == "" {
		return "", fmt.Errorf("cannot find Apple Music authorization token")
	}
	return token, nil
}

// webJWTToken returns a cached music.apple.com web-player JWT, scraping a
// fresh one when the cache is empty or expired. It is independent of Token,
// which serves the signed developer token once signing is enabled; this
// cache exists so EnhancedHLSViaWebToken can keep working in signed mode
// without touching the signed-token cache.
func (c *CatalogClient) webJWTToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.webToken != "" && time.Now().Before(c.webTokenUntil) {
		token := c.webToken
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	c.webRefreshMu.Lock()
	defer c.webRefreshMu.Unlock()
	// Another caller may have refreshed while this one waited.
	c.mu.Lock()
	if c.webToken != "" && time.Now().Before(c.webTokenUntil) {
		token := c.webToken
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	token, err := c.fetchToken(ctx)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.webToken = token
	c.webTokenUntil = cacheUntil(time.Now(), c.cfg.TokenTTL())
	c.mu.Unlock()
	return token, nil
}

func (c *CatalogClient) invalidateWebToken(rejected string) {
	c.mu.Lock()
	if c.webToken == rejected {
		c.webToken = ""
		c.webTokenUntil = time.Time{}
	}
	c.mu.Unlock()
}

// tokenRefreshCheckInterval bounds how often the background refresher started
// by StartTokenRefresher wakes to re-check the scraped-token caches. Each wake
// only compares timestamps under a lock; it scrapes Apple solely when a cached
// token is within tokenRefreshLead of its cached expiry.
const tokenRefreshCheckInterval = time.Minute

// tokenRefreshLead is how far before a scraped token's cached expiry the
// background refresher renews it, so a request never has to scrape inline and
// the token never lapses in the gap between two refresh checks. It stays
// comfortably below the smallest realistic effective TTL (half of the one-hour
// minimum token_cache_ttl) and above tokenRefreshCheckInterval.
const tokenRefreshLead = 5 * time.Minute

// StartTokenRefresher runs a background loop that proactively re-scrapes the
// music.apple.com web tokens shortly before their cached validity lapses, so
// catalog and enhanced-HLS requests in legacy or signed_mode_hls_source=
// web_token mode never pay the inline scrape latency and never hit a lapsed
// token. It only renews a token that has already been fetched at least once (a
// non-empty cache); it never scrapes a token nothing has used yet, preserving
// the lazy-first behavior for deployments that never touch the scraped path.
// The loop stops when ctx is done; call it once at startup.
func (c *CatalogClient) StartTokenRefresher(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(tokenRefreshCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.refreshExpiringWebTokens(ctx)
			}
		}
	}()
}

// refreshExpiringWebTokens renews any scraped token within tokenRefreshLead of
// its cached expiry. The self-signed developer token is skipped: when a signer
// is configured Token mints it locally with no scrape, so there is nothing to
// keep warm.
func (c *CatalogClient) refreshExpiringWebTokens(ctx context.Context) {
	if c.signer == nil {
		c.refreshLegacyTokenIfExpiring(ctx)
	}
	c.refreshWebTokenIfExpiring(ctx)
}

func (c *CatalogClient) refreshLegacyTokenIfExpiring(ctx context.Context) {
	c.mu.Lock()
	cached, until := c.token, c.tokenUntil
	c.mu.Unlock()
	if cached == "" || time.Now().Before(until.Add(-tokenRefreshLead)) {
		return
	}

	c.tokenRefreshMu.Lock()
	defer c.tokenRefreshMu.Unlock()
	// An inline caller may have refreshed while this loop waited; re-read under
	// the same lock Token uses so the two never scrape at once.
	c.mu.Lock()
	cached, until = c.token, c.tokenUntil
	c.mu.Unlock()
	if cached == "" || time.Now().Before(until.Add(-tokenRefreshLead)) {
		return
	}

	token, err := c.fetchToken(ctx)
	if err != nil {
		c.logger.Warn("proactive legacy token refresh failed; keeping cached token until it expires", "error", err)
		return
	}
	c.mu.Lock()
	c.token = token
	c.tokenUntil = cacheUntil(time.Now(), c.cfg.TokenTTL())
	c.mu.Unlock()
}

func (c *CatalogClient) refreshWebTokenIfExpiring(ctx context.Context) {
	c.mu.Lock()
	cached, until := c.webToken, c.webTokenUntil
	c.mu.Unlock()
	if cached == "" || time.Now().Before(until.Add(-tokenRefreshLead)) {
		return
	}

	c.webRefreshMu.Lock()
	defer c.webRefreshMu.Unlock()
	// Re-read under the same lock webJWTToken uses so a concurrent inline miss
	// and this proactive refresh never scrape at once.
	c.mu.Lock()
	cached, until = c.webToken, c.webTokenUntil
	c.mu.Unlock()
	if cached == "" || time.Now().Before(until.Add(-tokenRefreshLead)) {
		return
	}

	token, err := c.fetchToken(ctx)
	if err != nil {
		c.logger.Warn("proactive web token refresh failed; keeping cached token until it expires", "error", err)
		return
	}
	c.mu.Lock()
	c.webToken = token
	c.webTokenUntil = cacheUntil(time.Now(), c.cfg.TokenTTL())
	c.mu.Unlock()
}

// doWithWebAuth mirrors doWithCatalogAuth for the independent web-player JWT
// cache used by signed_mode_hls_source=web_token.
func (c *CatalogClient) doWithWebAuth(ctx context.Context, buildReq func(token string) (*http.Request, error)) (*http.Response, error) {
	token, err := c.webJWTToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := buildReq(token)
	if err != nil {
		return nil, err
	}
	resp, err := c.doAppleRequest(ctx, req, true)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	resp.Body.Close()

	c.invalidateWebToken(token)
	token, err = c.webJWTToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err = buildReq(token)
	if err != nil {
		return nil, err
	}
	return c.doAppleRequest(ctx, req, true)
}
