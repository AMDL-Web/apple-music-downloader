package applemusic

import (
	"context"
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
	client.webToken = "stale-web-token"
	client.webTokenUntil = time.Now().Add(time.Hour)

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
