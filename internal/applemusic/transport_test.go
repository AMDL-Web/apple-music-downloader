package applemusic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/limits"
)

type closeTrackingBody struct {
	io.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return nil
}

func TestAppleRequestRetries429OnceAndClosesFirstBody(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{
		MaxParallelRequests: 2,
	}, slog.Default())
	firstBody := &closeTrackingBody{Reader: strings.NewReader("limited")}
	calls := 0
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests", Header: make(http.Header), Body: firstBody}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})}
	client.retryDelay = func(http.Header) time.Duration { return 30 * time.Millisecond }
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.music.apple.com/test", nil)
	started := time.Now()
	resp, err := client.doAppleRequest(context.Background(), req, true)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if calls != 2 {
		t.Fatalf("calls = %d, want exactly one retry", calls)
	}
	if !firstBody.closed {
		t.Fatal("first 429 body was not closed before retry")
	}
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond {
		t.Fatalf("retry began after %v, want shared cooldown delay", elapsed)
	}
}

func TestAppleRequestReturnsSecond429(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{
		MaxParallelRequests: 2,
	}, slog.Default())
	calls := 0
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("limited"))}, nil
	})}
	client.retryDelay = func(http.Header) time.Duration { return 0 }
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.music.apple.com/test", nil)
	resp, err := client.doAppleRequest(context.Background(), req, true)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || calls != 2 {
		t.Fatalf("status=%d calls=%d, want second 429 returned after 2 calls", resp.StatusCode, calls)
	}
}

func TestAppleRequestRateAppliesOnlyToAuthenticatedAPI(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{MaxParallelRequests: 2}, slog.Default())
	client.gate = limits.NewRequestGate(2, 1, 1)
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})}
	req, _ := http.NewRequest(http.MethodGet, "https://api.music.apple.com/first", nil)
	resp, err := client.doAppleRequest(context.Background(), req, true)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "https://is1-ssl.mzstatic.com/cover", nil)
	resp, err = client.doAppleRequest(ctx, req, false)
	if err != nil {
		t.Fatalf("non-authenticated Apple request was incorrectly rate limited: %v", err)
	}
	resp.Body.Close()

	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "https://api.music.apple.com/second", nil)
	if _, err := client.doAppleRequest(ctx, req, true); err == nil {
		t.Fatal("second authenticated request bypassed the exhausted rate bucket")
	}
}

func TestRetryAfterParsing(t *testing.T) {
	if got := defaultRetryDelay(http.Header{"Retry-After": []string{"3"}}); got != 3*time.Second {
		t.Fatalf("delta Retry-After = %v, want 3s", got)
	}
	future := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	got := defaultRetryDelay(http.Header{"Retry-After": []string{future}})
	if got < 500*time.Millisecond || got > 2*time.Second {
		t.Fatalf("date Retry-After = %v, want remaining duration", got)
	}
	got = defaultRetryDelay(make(http.Header))
	if got < time.Second || got >= 1250*time.Millisecond {
		t.Fatalf("missing Retry-After fallback = %v, want [1s, 1.25s)", got)
	}
}

func TestReadLimitedRejectsOversizedUpstreamResponse(t *testing.T) {
	if _, err := readLimited(strings.NewReader("12345"), 4); err == nil {
		t.Fatal("readLimited accepted an upstream response larger than its limit")
	}
}

func TestCatalogRequestErrorClassificationAndRetryAfter(t *testing.T) {
	if !(catalogRequestError{statusCode: http.StatusNotFound}).NonRetryable() {
		t.Fatal("404 should be non-retryable")
	}
	if (catalogRequestError{statusCode: http.StatusTooManyRequests}).NonRetryable() {
		t.Fatal("429 should remain retryable")
	}
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	if got := parseRetryAfter("3", now); got != 3*time.Second {
		t.Fatalf("delta Retry-After=%s, want 3s", got)
	}
	if got := parseRetryAfter(now.Add(5*time.Second).Format(http.TimeFormat), now); got != 5*time.Second {
		t.Fatalf("date Retry-After=%s, want 5s", got)
	}
}

// A catalogRequestError is what job rows and API responses show, so Error()
// must read like a sentence and never quote Apple's raw JSON; the body is
// reachable for logs through UpstreamBody (logging.UpstreamBody unwraps to it).
func TestCatalogRequestErrorMessage(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  catalogRequestError
		want string
	}{
		{"404 names the resource", catalogRequestError{statusCode: 404, status: "404 Not Found", path: "/v1/catalog/us/albums/123"},
			"Apple Music has no album 123 in storefront us (404)"},
		{"type without id names both", catalogRequestError{statusCode: 404, status: "404 Not Found", path: "/v1/catalog/zz/albums"},
			"Apple Music catalog albums request in storefront zz failed: 404 Not Found"},
		{"unparseable path names the path", catalogRequestError{statusCode: 404, status: "404 Not Found", path: "/v1/catalog/zz"},
			"Apple Music request /v1/catalog/zz failed: 404 Not Found"},
		{"429 keeps the status", catalogRequestError{statusCode: 429, status: "429 Too Many Requests", path: "/v1/catalog/us/albums/123"},
			"Apple Music request for album 123 in storefront us failed: 429 Too Many Requests"},
		{"non-catalog path names the path", catalogRequestError{statusCode: 401, status: "401 Unauthorized", path: "/v1/me/library/songs"},
			"Apple Music request /v1/me/library/songs failed: 401 Unauthorized"},
		{"empty path falls back to status", catalogRequestError{statusCode: 503, status: "503 Service Unavailable"},
			"Apple Music request failed: 503 Service Unavailable"},
		{"missing status synthesizes one", catalogRequestError{statusCode: 404, path: "/v1/catalog/us/songs/9"},
			"Apple Music has no song 9 in storefront us (404)"},
		{"plural unknown type stays plural", catalogRequestError{statusCode: 404, path: "/v1/catalog/us/widgets/7"},
			"Apple Music has no widgets 7 in storefront us (404)"},
	} {
		if got := tt.err.Error(); got != tt.want {
			t.Errorf("%s: Error() = %q, want %q", tt.name, got, tt.want)
		}
	}
	raw := `{"errors":[{"id":"E1","status":"404","title":"Resource Not Found"}]}`
	err := catalogRequestError{statusCode: 404, status: "404 Not Found", path: "/v1/catalog/us/albums/123", body: raw}
	if strings.Contains(err.Error(), raw) || strings.Contains(err.Error(), "Resource Not Found") {
		t.Fatalf("Error() leaks the upstream body: %q", err.Error())
	}
	wrapped := fmt.Errorf("resolve tracks: %w", err)
	var carrier interface{ UpstreamBody() string }
	if !errors.As(wrapped, &carrier) || carrier.UpstreamBody() != raw {
		t.Fatal("UpstreamBody() did not reach the body through wrapping")
	}
}

// decodeCatalogResponse records the request path so the message can name the
// resource; a synthetic response without a Request still yields a clean error.
func TestDecodeCatalogResponseKeepsRequestPath(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.music.apple.com/v1/catalog/zz/albums/abc", nil)
	resp := &http.Response{StatusCode: 404, Status: "404 Not Found", Request: req, Body: io.NopCloser(strings.NewReader(`{"errors":[]}`)), Header: make(http.Header)}
	var out struct{}
	err := decodeCatalogResponse(resp, &out)
	if err == nil || err.Error() != "Apple Music has no album abc in storefront zz (404)" {
		t.Fatalf("decodeCatalogResponse() error = %v", err)
	}
	var requestErr catalogRequestError
	if !errors.As(err, &requestErr) || requestErr.UpstreamBody() != `{"errors":[]}` {
		t.Fatalf("decodeCatalogResponse() error = %#v, want the body kept for logs", err)
	}
	resp = &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("oops")), Header: make(http.Header)}
	err = decodeCatalogResponse(resp, &out)
	if err == nil || err.Error() != "Apple Music request failed: 500 Internal Server Error" {
		t.Fatalf("decodeCatalogResponse() without Request error = %v", err)
	}
}
