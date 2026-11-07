package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The catalog routes (album, playlist, search, an artist's albums) refuse a
// malformed id or query with 400 invalid_request before reading Apple.
func TestCatalogRoutesRefuseBadParameters(t *testing.T) {
	for _, path := range []string{
		"/api/v1/albums/abc",
		"/api/v1/albums/1%2F2",
		"/api/v1/albums/1?storefront=x",
		"/api/v1/playlists/123",
		"/api/v1/playlists/pl.a%2Fb",
		"/api/v1/playlists/pl.abc?storefront=x",
		"/api/v1/artists/abc/albums",
		"/api/v1/artists/1%2F2/albums",
		"/api/v1/artists/1/albums?limit=101",
		"/api/v1/artists/1/albums?storefront=x",
		"/api/v1/search",
		"/api/v1/search?term=%20%20",
		"/api/v1/search?term=" + strings.Repeat("a", searchMaxTermRunes+1),
		"/api/v1/search?term=a&types=tracks",
		"/api/v1/search?term=a&storefront=usa",
		"/api/v1/search?term=a&limit=0",
		"/api/v1/search?term=a&limit=51",
		"/api/v1/search?term=a&limit=x",
		"/api/v1/search?term=a&offset=-1",
	} {
		t.Run(path, func(t *testing.T) {
			catalog := &fakeCatalog{t: t}
			assertErrorCode(t, requestJSON(t, newLookupServer(t, catalog, nil), http.MethodGet, path, ""), http.StatusBadRequest, CodeInvalidRequest)
			if len(catalog.calls) != 0 {
				t.Fatalf("read the catalog: %v", catalog.calls)
			}
		})
	}
}

// A failed catalog read maps to the same code on every catalog route: Apple's
// 404 is catalog_not_found, a deadline upstream_timeout, anything else
// upstream_error. (POST /lookup maps them too, with the parsed link in the
// details: lookup_test.go.)
func TestCatalogRoutesMapUpstreamFailures(t *testing.T) {
	failures := []struct {
		name   string
		err    error
		status int
		code   ErrorCode
	}{
		{"not found", fmt.Errorf("read: %w", notFoundError{}), http.StatusNotFound, CodeCatalogNotFound},
		{"upstream", errors.New("catalog request failed: 500 Internal Server Error"), http.StatusBadGateway, CodeUpstreamError},
		{"timeout", fmt.Errorf("read: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, CodeUpstreamTimeout},
	}
	for _, path := range []string{
		"/api/v1/albums/1",
		"/api/v1/playlists/pl.abc",
		"/api/v1/artists/1/albums",
		"/api/v1/search?term=a",
	} {
		for _, f := range failures {
			t.Run(path+" "+f.name, func(t *testing.T) {
				catalog := &fakeCatalog{t: t, err: f.err}
				assertErrorCode(t, requestJSON(t, newLookupServer(t, catalog, nil), http.MethodGet, path, ""), f.status, f.code)
			})
		}
	}
}
