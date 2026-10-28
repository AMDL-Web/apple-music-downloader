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

// catalogRequestError is a non-2xx answer from an Apple JSON endpoint.
// Error() is the short message job and lookup error text shows; the raw body
// excerpt stays on the value for log lines, which reach it through
// logging.UpstreamBody.
type catalogRequestError struct {
	statusCode int
	status     string
	path       string
	body       string
	retryAfter time.Duration
}

func (e catalogRequestError) Error() string {
	resourceType, id, storefront := catalogResource(e.path)
	status := e.statusText()
	switch {
	case id != "" && e.statusCode == http.StatusNotFound:
		return fmt.Sprintf("Apple Music has no %s %s in storefront %s (%d)", singularCatalogType(resourceType), id, storefront, e.statusCode)
	case id != "":
		return fmt.Sprintf("Apple Music request for %s %s in storefront %s failed: %s", singularCatalogType(resourceType), id, storefront, status)
	case storefront != "":
		return fmt.Sprintf("Apple Music catalog %s request in storefront %s failed: %s", resourceType, storefront, status)
	case e.path != "":
		return fmt.Sprintf("Apple Music request %s failed: %s", e.path, status)
	default:
		return fmt.Sprintf("Apple Music request failed: %s", status)
	}
}

// UpstreamBody exposes the raw response body excerpt for log lines. It stays
// out of Error() because that text is persisted in job and item errors and
// returned in API responses.
func (e catalogRequestError) UpstreamBody() string { return e.body }

// statusText renders the failure as "404 Not Found". resp.Status carries that
// on a real response; a synthesized one may hold only "Not Found" or "".
func (e catalogRequestError) statusText() string {
	code := strconv.Itoa(e.statusCode)
	if strings.HasPrefix(e.status, code) {
		return e.status
	}
	if text := http.StatusText(e.statusCode); text != "" {
		return code + " " + text
	}
	return code
}

// catalogTypeSingular is how a catalog resource type reads in a message. A
// type missing here (a newer or private Apple type) reads as its path word.
var catalogTypeSingular = map[string]string{
	"albums": "album", "songs": "song", "artists": "artist",
	"playlists": "playlist", "stations": "station", "music-videos": "music video",
	"curators": "curator", "apple-curators": "curator", "genres": "genre",
	"activities": "activity",
}

func singularCatalogType(resourceType string) string {
	if singular, ok := catalogTypeSingular[resourceType]; ok {
		return singular
	}
	return resourceType
}

// catalogResource splits a "/v1/catalog/<storefront>/<type>[/<id>/…]" request
// path into the resource type, id and storefront an error names. A path in
// another shape ("/v1/me/library/…") returns empty strings.
func catalogResource(path string) (resourceType, id, storefront string) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 4 || segments[0] != "v1" || segments[1] != "catalog" {
		return "", "", ""
	}
	resourceType, storefront = segments[3], segments[2]
	if len(segments) > 4 {
		id = segments[4]
	}
	return resourceType, id, storefront
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

// nonRetryableError is a deterministic failure that carries the same
// NonRetryable marker as a 4xx catalogRequestError, so retry helpers give up at
// once. Values are comparable, so a sentinel of this type works with errors.Is.
type nonRetryableError string

func (e nonRetryableError) Error() string { return string(e) }

// NonRetryable always reports true.
func (nonRetryableError) NonRetryable() bool { return true }

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
	// maxErrorBodyExcerptBytes is how much of a non-2xx body a
	// catalogRequestError keeps for log lines.
	maxErrorBodyExcerptBytes int64 = 2048
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
	resp, err := c.doWithCatalogAuth(ctx, appleRequest{method: http.MethodGet, url: endpoint, mediaUserToken: mediaUserToken})
	if err != nil {
		return err
	}
	return decodeCatalogResponse(resp, out)
}

// decodeCatalogResponse decodes a 2xx JSON body into out and closes it. Any
// other status becomes a catalogRequestError, so retry helpers see whether it
// is worth retrying and how long Apple asked to wait.
func decodeCatalogResponse(resp *http.Response, out any) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		// The body is only an excerpt for the error's UpstreamBody; a failed
		// read leaves it shorter and changes nothing else.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyExcerptBytes))
		requestPath := ""
		if resp.Request != nil && resp.Request.URL != nil {
			requestPath = resp.Request.URL.Path
		}
		return catalogRequestError{
			statusCode: resp.StatusCode,
			status:     resp.Status,
			path:       requestPath,
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
