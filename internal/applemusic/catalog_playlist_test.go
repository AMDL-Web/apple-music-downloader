package applemusic

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
)

func TestPlaylistFetchesAllTrackPages(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.catalogToken.value = "test-token"
	client.catalogToken.until = time.Now().Add(time.Hour)
	var paths []string
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.RequestURI())
		if req.URL.Path == "/v1/catalog/cn/playlists/pl.1" {
			body := `{"data":[{"id":"pl.1","type":"playlists","attributes":{"name":"Playlist","curatorName":"Curator","artwork":{"url":"playlist-art"}},"relationships":{"tracks":{"data":[{"id":"song-1","type":"songs","attributes":{"name":"One","artistName":"Artist"}}],"next":"/v1/catalog/cn/playlists/pl.1/tracks?offset=1"}}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		if req.URL.Path == "/v1/catalog/cn/playlists/pl.1/tracks" {
			body := `{"data":[{"id":"song-2","type":"songs","attributes":{"name":"Two","artistName":"Artist"}},{"id":"video-1","type":"music-videos","attributes":{"name":"Video"}},{"id":"song-3","type":"songs","attributes":{"name":"Three","artistName":"Artist"}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("unexpected path: " + req.URL.RequestURI())), Header: make(http.Header)}, nil
	})}

	playlist, err := client.Playlist(context.Background(), "cn", "pl.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := trackIDs(playlist.Tracks), []string{"song-1", "song-2", "song-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("track IDs = %#v, want %#v", got, want)
	}
	if len(paths) != 2 || paths[1] != "/v1/catalog/cn/playlists/pl.1/tracks?offset=1" {
		t.Fatalf("request paths = %#v", paths)
	}
}

func TestPlaylistPrivateCoverFallsBackToLibraryArtwork(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "zh-Hans_CN"}, slog.Default())
	client.catalogToken.value = "test-token"
	client.catalogToken.until = time.Now().Add(time.Hour)
	type seen struct{ mediaToken, include string }
	var requests []seen
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, seen{req.Header.Get("Media-User-Token"), req.URL.Query().Get("include")})
		body := `{"data":[{"id":"pl.u-1","type":"playlists","attributes":{"name":"Private","artwork":{"url":""}},"relationships":{"tracks":{"data":[{"id":"song-1","type":"songs","attributes":{"name":"One","artistName":"Artist"}}]},"library":{"data":[{"id":"p.lib1","type":"library-playlists","attributes":{"name":"Private","artwork":{"url":"https://blob.example/image?sig=abc"}}}]}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	playlist, err := client.Playlist(context.Background(), "cn", "pl.u-1", "media-token-xyz")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2 (plain fetch + library enrichment)", len(requests))
	}
	if requests[0].mediaToken != "" || requests[0].include != "" {
		t.Fatalf("plain fetch leaked auth: %+v", requests[0])
	}
	if requests[1].mediaToken != "media-token-xyz" || requests[1].include != "library" {
		t.Fatalf("enrichment request = %+v, want token + include=library", requests[1])
	}
	if playlist.ArtworkURL != "https://blob.example/image?sig=abc" {
		t.Fatalf("ArtworkURL = %q, want library artwork", playlist.ArtworkURL)
	}
}

func TestPlaylistLibraryArtworkOverridesCatalogMosaic(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "zh-Hans_CN"}, slog.Default())
	client.catalogToken.value = "test-token"
	client.catalogToken.until = time.Now().Add(time.Hour)
	var calls int
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		body := `{"data":[{"id":"pl.u-1","type":"playlists","attributes":{"name":"Shared","artwork":{"url":"catalog-mosaic"}},"relationships":{"tracks":{"data":[]}}}]}`
		if req.URL.Query().Get("include") == "library" {
			body = `{"data":[{"id":"pl.u-1","type":"playlists","relationships":{"library":{"data":[{"id":"p.lib1","type":"library-playlists","attributes":{"artwork":{"url":"https://blob.example/correct-cover?X-Amz-Date=20260722T000000Z&X-Amz-Expires=3600"}}}]}}}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	playlist, err := client.Playlist(context.Background(), "cn", "pl.u-1", "media-token-xyz")
	if err != nil {
		t.Fatal(err)
	}
	if playlist.ArtworkURL != "https://blob.example/correct-cover?X-Amz-Date=20260722T000000Z&X-Amz-Expires=3600" {
		t.Fatalf("ArtworkURL = %q, want library artwork to override catalog mosaic", playlist.ArtworkURL)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (catalog plus library enrichment)", calls)
	}
}

func TestPlaylistPublicIDWithTokenSkipsEnrichment(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "zh-Hans_CN"}, slog.Default())
	client.catalogToken.value = "test-token"
	client.catalogToken.until = time.Now().Add(time.Hour)
	var calls int
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		body := `{"data":[{"id":"pl.123","type":"playlists","attributes":{"name":"Editorial","artwork":{"url":""}},"relationships":{"tracks":{"data":[]}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	playlist, err := client.Playlist(context.Background(), "cn", "pl.123", "media-token-xyz")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (non-private ids never trigger enrichment)", calls)
	}
	if playlist.ArtworkURL != "" {
		t.Fatalf("ArtworkURL = %q, want empty", playlist.ArtworkURL)
	}
}

func TestPlaylistLibraryLookupFailureIsNonFatal(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "zh-Hans_CN"}, slog.Default())
	client.catalogToken.value = "test-token"
	client.catalogToken.until = time.Now().Add(time.Hour)
	var calls int
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 2 {
			// Enrichment request rejected, e.g. an expired media-user-token.
			return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(`{"errors":[{"status":"403"}]}`)), Header: make(http.Header)}, nil
		}
		body := `{"data":[{"id":"pl.u-1","type":"playlists","attributes":{"name":"Private","artwork":{"url":"catalog-mosaic"}},"relationships":{"tracks":{"data":[{"id":"song-1","type":"songs","attributes":{"name":"One","artistName":"Artist"}}]}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	playlist, err := client.Playlist(context.Background(), "cn", "pl.u-1", "expired-token")
	if err != nil {
		t.Fatalf("playlist resolve must survive a failed artwork enrichment, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if playlist.ArtworkURL != "catalog-mosaic" {
		t.Fatalf("ArtworkURL = %q, want catalog fallback after failed enrichment", playlist.ArtworkURL)
	}
	if len(playlist.Tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(playlist.Tracks))
	}
}

func TestPlaylistWithoutTokenSkipsLibraryInclude(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "zh-Hans_CN"}, slog.Default())
	client.catalogToken.value = "test-token"
	client.catalogToken.until = time.Now().Add(time.Hour)
	var sawMediaToken bool
	var gotInclude string
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		sawMediaToken = req.Header.Get("Media-User-Token") != ""
		gotInclude = req.URL.Query().Get("include")
		body := `{"data":[{"id":"pl.1","type":"playlists","attributes":{"name":"Editorial","artwork":{"url":"catalog-art"}},"relationships":{"tracks":{"data":[]}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	if _, err := client.Playlist(context.Background(), "cn", "pl.1", ""); err != nil {
		t.Fatal(err)
	}
	if sawMediaToken || gotInclude != "" {
		t.Fatalf("tokenless playlist request leaked auth: header=%v include=%q", sawMediaToken, gotInclude)
	}
}
