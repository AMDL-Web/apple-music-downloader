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
)

func TestEnhancedHLSViaWebTokenRetriesOnce401ThenSucceeds(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US", TokenCacheTTLHours: 12}, slog.Default())
	client.webToken.value = "stale-web-token"
	client.webToken.until = time.Now().Add(time.Hour)

	var catalogCalls int
	var authHeaders []string
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "amp-api.music.apple.com":
			catalogCalls++
			authHeaders = append(authHeaders, req.Header.Get("Authorization"))
			if catalogCalls == 1 {
				return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(`{"errors":[{"status":"401"}]}`)), Header: make(http.Header)}, nil
			}
			body := `{"data":[{"id":"song-1","type":"songs","attributes":{"extendedAssetUrls":{"enhancedHls":"https://example.test/master.m3u8"}}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		case "music.apple.com":
			if req.URL.Path == "/" || req.URL.Path == "" {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<script src="/assets/index~abc123.js"></script>`)), Header: make(http.Header)}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`token:"eyJhbGciOiJFUzI1NiJ9.eyJmb28iOiJiYXIifQ.sig-part-here"`)), Header: make(http.Header)}, nil
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("unexpected")), Header: make(http.Header)}, nil
		}
	})}

	master, err := client.EnhancedHLSViaWebToken(context.Background(), "cn", "song-1")
	if err != nil {
		t.Fatalf("EnhancedHLSViaWebToken: %v", err)
	}
	if master != "https://example.test/master.m3u8" {
		t.Fatalf("master = %q", master)
	}
	if catalogCalls != 2 || len(authHeaders) != 2 || authHeaders[0] != "Bearer stale-web-token" || authHeaders[0] == authHeaders[1] {
		t.Fatalf("calls=%d auth=%#v, want stale token then one refreshed retry", catalogCalls, authHeaders)
	}
}

// TestAppleEndpointsClassifyRetries pins that the station, motion-artwork and
// enhanced-HLS reads report the same retry classification as every other
// catalog read: a 4xx stops retries at once, a 5xx stays retryable and
// carries Apple's Retry-After.
func TestAppleEndpointsClassifyRetries(t *testing.T) {
	endpoints := []struct {
		name string
		path string // the request that answers with the status under test
		call func(*CatalogClient) error
	}{
		{
			name: "station next-tracks",
			path: "/v1/me/stations/next-tracks/ra.1",
			call: func(c *CatalogClient) error {
				_, err := c.StationTracks(context.Background(), "us", "ra.1", "media-token")
				return err
			},
		},
		{
			name: "motion artwork",
			path: "/v1/catalog/us/albums/album-1",
			call: func(c *CatalogClient) error {
				_, err := c.MotionArtworkViaWebToken(context.Background(), "us", "album-1")
				return err
			},
		},
		{
			name: "enhanced hls",
			path: "/v1/catalog/us/songs/song-1",
			call: func(c *CatalogClient) error {
				_, err := c.EnhancedHLSViaWebToken(context.Background(), "us", "song-1")
				return err
			},
		},
	}
	statuses := []struct {
		code             int
		retryAfter       string
		wantNonRetryable bool
		wantDelay        time.Duration
	}{
		{code: http.StatusNotFound, wantNonRetryable: true},
		{code: http.StatusForbidden, wantNonRetryable: true},
		{code: http.StatusServiceUnavailable, retryAfter: "7", wantNonRetryable: false, wantDelay: 7 * time.Second},
	}
	for _, endpoint := range endpoints {
		for _, status := range statuses {
			t.Run(fmt.Sprintf("%s %d", endpoint.name, status.code), func(t *testing.T) {
				client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
				client.catalogToken.value, client.catalogToken.until = "catalog-token", time.Now().Add(time.Hour)
				client.webToken.value, client.webToken.until = "web-token", time.Now().Add(time.Hour)
				var hits int
				client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					switch req.URL.Path {
					case endpoint.path:
						hits++
						header := make(http.Header)
						if status.retryAfter != "" {
							header.Set("Retry-After", status.retryAfter)
						}
						return &http.Response{StatusCode: status.code, Status: http.StatusText(status.code), Body: io.NopCloser(strings.NewReader(`{"errors":[]}`)), Header: header}, nil
					case "/v1/catalog/us/stations/ra.1":
						body := `{"data":[{"id":"ra.1","type":"stations","attributes":{"name":"Station","playParams":{"format":"tracks"}}}]}`
						return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
					}
					return &http.Response{StatusCode: http.StatusTeapot, Body: io.NopCloser(strings.NewReader("unexpected " + req.URL.Path)), Header: make(http.Header)}, nil
				})}

				err := endpoint.call(client)
				if err == nil {
					t.Fatal("expected an error")
				}
				if hits != 1 {
					t.Fatalf("requests to %s = %d, want 1", endpoint.path, hits)
				}
				var marker interface{ NonRetryable() bool }
				if !errors.As(err, &marker) {
					t.Fatalf("error %v carries no retry classification", err)
				}
				if got := marker.NonRetryable(); got != status.wantNonRetryable {
					t.Fatalf("NonRetryable() = %v, want %v (error %v)", got, status.wantNonRetryable, err)
				}
				var hint interface{ RetryDelay() time.Duration }
				if !errors.As(err, &hint) || hint.RetryDelay() != status.wantDelay {
					t.Fatalf("RetryDelay = %v, want %v", hint, status.wantDelay)
				}
			})
		}
	}
}

func TestEnhancedHLSViaWebTokenWithoutManifestIsNonRetryable(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.webToken.value, client.webToken.until = "web-token", time.Now().Add(time.Hour)
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"data":[{"id":"song-1","type":"songs","attributes":{"extendedAssetUrls":{}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	_, err := client.EnhancedHLSViaWebToken(context.Background(), "us", "song-1")
	if !errors.Is(err, ErrNoEnhancedHLS) {
		t.Fatalf("error = %v, want ErrNoEnhancedHLS", err)
	}
	var marker interface{ NonRetryable() bool }
	if !errors.As(err, &marker) || !marker.NonRetryable() {
		t.Fatalf("error = %v, want it to report NonRetryable", err)
	}
	if !strings.Contains(err.Error(), "song-1") {
		t.Fatalf("error = %q, want it to name the song", err)
	}
}
