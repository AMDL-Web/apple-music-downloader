package applemusic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"amdl/internal/limits"
)

type catalogRequestError struct {
	statusCode int
	status     string
	body       string
	retryAfter time.Duration
}

func (e catalogRequestError) Error() string {
	return fmt.Sprintf("catalog request failed: %s: %s", e.status, e.body)
}

// NonRetryable lets download operations stop immediately on deterministic
// client errors such as a missing artist. Rate limits and request timeouts are
// transient and retain the normal retry path.
func (e catalogRequestError) NonRetryable() bool {
	return e.statusCode >= 400 && e.statusCode < 500 && e.statusCode != http.StatusRequestTimeout && e.statusCode != http.StatusTooManyRequests
}

// RetryDelay exposes Apple's Retry-After hint without coupling the generic
// retry helper to the catalog package.
func (e catalogRequestError) RetryDelay() time.Duration { return e.retryAfter }

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
		return 0
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}

const (
	maxCatalogResponseBytes int64 = 64 << 20
	maxArtworkBytes         int64 = 64 << 20
	maxWebPlayerPageBytes   int64 = 16 << 20
	maxWebPlayerJSBytes     int64 = 64 << 20
)

// apiBase returns the catalog host. A self-signed developer token uses the
// official api.music.apple.com; legacy web tokens use the amp-api host.
func (c *CatalogClient) apiBase() string {
	if c.cfg.DeveloperTokenSigningEnabled() {
		return "https://api.music.apple.com"
	}
	return "https://amp-api.music.apple.com"
}

func (c *CatalogClient) get(ctx context.Context, endpoint string, params url.Values, out any) error {
	return c.getWithUserToken(ctx, endpoint, params, "", out)
}

// getWithUserToken is get plus an optional Media-User-Token header, for the
// few catalog reads whose response is richer when the request carries the
// user's subscription identity (e.g. private playlist artwork via
// include=library). An empty token degrades to a plain get.
func (c *CatalogClient) getWithUserToken(ctx context.Context, endpoint string, params url.Values, mediaUserToken string, out any) error {
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	resp, err := c.doWithCatalogAuth(ctx, func(token string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if mediaUserToken != "" {
			req.Header.Set("Media-User-Token", mediaUserToken)
		}
		if !c.cfg.DeveloperTokenSigningEnabled() {
			// The web player token is scoped to music.apple.com; a self-signed
			// developer token carries no origin claim, so no Origin header is sent.
			req.Header.Set("Origin", "https://music.apple.com")
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")
		return req, nil
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return catalogRequestError{
			statusCode: resp.StatusCode,
			status:     resp.Status,
			body:       strings.TrimSpace(string(body)),
			retryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		}
	}
	return decodeJSONLimited(resp.Body, maxCatalogResponseBytes, out)
}

// doAppleRequest applies the singleton Apple request gate. A 429 penalizes
// every caller through the shared cooldown and is retried once; a second 429
// is returned to the existing status handling at the call site.
func (c *CatalogClient) doAppleRequest(ctx context.Context, req *http.Request, rateLimited bool) (*http.Response, error) {
	return c.gate.DoWith429Retry(ctx, c.http, req, rateLimited, c.retryDelay)
}

func defaultRetryDelay(header http.Header) time.Duration {
	return limits.DefaultRetryDelay(header)
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("upstream response exceeded limit of %d bytes", limit)
	}
	return raw, nil
}

func decodeJSONLimited(r io.Reader, limit int64, out any) error {
	raw, err := readLimited(r, limit)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
