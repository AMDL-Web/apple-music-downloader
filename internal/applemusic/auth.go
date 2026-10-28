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
	_, err = c.catalogToken.get(context.Background())
	return err
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

// fetchCatalogToken is catalogToken's fetch: a locally signed developer token
// once InitDeveloperToken configured a signer, otherwise a scraped web-player
// JWT.
func (c *CatalogClient) fetchCatalogToken(ctx context.Context) (string, time.Duration, error) {
	if c.signer != nil {
		token, _, err := c.signer.sign(time.Now(), internalDeveloperTokenTTL, nil)
		return token, internalDeveloperTokenTTL, err
	}
	return c.scrapeWebToken(ctx)
}

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
// its cached expiry. The catalog cache is skipped when a signer is configured:
// it then holds a locally minted token with nothing to keep warm.
func (c *CatalogClient) refreshExpiringWebTokens(ctx context.Context) {
	if c.signer == nil {
		if err := c.catalogToken.refreshIfExpiring(ctx, tokenRefreshLead); err != nil {
			c.logger.Warn("proactive legacy token refresh failed; keeping cached token until it expires", "error", err)
		}
	}
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

// doWithCatalogAuth sends r to apiBase's host with catalogToken. The Origin
// header goes only with a scraped token (legacy mode): a self-signed developer
// token carries no origin claim.
func (c *CatalogClient) doWithCatalogAuth(ctx context.Context, r appleRequest) (*http.Response, error) {
	return c.doWithAuth(ctx, &c.catalogToken, !c.cfg.DeveloperTokenSigningEnabled(), r)
}

// doWithWebAuth sends r to amp-api.music.apple.com with the scraped web-player
// JWT and the Origin header that token is scoped to, whatever the signing mode.
func (c *CatalogClient) doWithWebAuth(ctx context.Context, r appleRequest) (*http.Response, error) {
	return c.doWithAuth(ctx, &c.webToken, true, r)
}

// doWithAuth sends r with a bearer token from cache. On a 401 it drops that
// token (only if it is still the cached one), fetches another and sends r once
// more; any other status, a second 401 included, goes back to the caller as-is.
func (c *CatalogClient) doWithAuth(ctx context.Context, cache *tokenCache, origin bool, r appleRequest) (*http.Response, error) {
	send := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, r.method, r.url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if r.mediaUserToken != "" {
			req.Header.Set("Media-User-Token", r.mediaUserToken)
		}
		if origin {
			req.Header.Set("Origin", "https://music.apple.com")
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")
		return c.doAppleRequest(ctx, req, true)
	}

	token, err := cache.get(ctx)
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

	cache.invalidate(token)
	token, err = cache.get(ctx)
	if err != nil {
		return nil, err
	}
	return send(token)
}
