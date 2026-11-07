package applemusic

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"amdl/internal/config"
)

var zhCN = config.CatalogConfig{Language: "zh-Hans_CN"}

func TestPlaylistFetchesAllTrackPages(t *testing.T) {
	var paths []string
	serve := routes(map[string]string{
		"/v1/catalog/cn/playlists/pl.1":        fixture(t, "catalog/playlist-paged.json"),
		"/v1/catalog/cn/playlists/pl.1/tracks": fixture(t, "catalog/playlist-paged-tracks-offset-1.json"),
	})
	client := newTestClient(t, enUS, func(req *http.Request) *http.Response {
		paths = append(paths, req.URL.RequestURI())
		return serve(req)
	})

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
	type seen struct{ mediaToken, include string }
	var requests []seen
	body := fixture(t, "catalog/playlist-private-with-library.json")
	client := newTestClient(t, zhCN, func(req *http.Request) *http.Response {
		requests = append(requests, seen{req.Header.Get("Media-User-Token"), req.URL.Query().Get("include")})
		return jsonResponse(http.StatusOK, body)
	})

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
	var calls int
	client := newTestClient(t, zhCN, func(req *http.Request) *http.Response {
		calls++
		if req.URL.Query().Get("include") == "library" {
			return jsonResponse(http.StatusOK, `{"data":[{"id":"pl.u-1","type":"playlists","relationships":{"library":{"data":[{"id":"p.lib1","type":"library-playlists","attributes":{"artwork":{"url":"https://blob.example/correct-cover?X-Amz-Date=20260722T000000Z&X-Amz-Expires=3600"}}}]}}}]}`)
		}
		return jsonResponse(http.StatusOK, `{"data":[{"id":"pl.u-1","type":"playlists","attributes":{"name":"Shared","artwork":{"url":"catalog-mosaic"}},"relationships":{"tracks":{"data":[]}}}]}`)
	})

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
	var calls int
	client := newTestClient(t, zhCN, func(*http.Request) *http.Response {
		calls++
		return jsonResponse(http.StatusOK, `{"data":[{"id":"pl.123","type":"playlists","attributes":{"name":"Editorial","artwork":{"url":""}},"relationships":{"tracks":{"data":[]}}}]}`)
	})

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
	var calls int
	client := newTestClient(t, zhCN, func(*http.Request) *http.Response {
		calls++
		if calls == 2 {
			// Enrichment request rejected, e.g. an expired media-user-token.
			return jsonResponse(http.StatusForbidden, `{"errors":[{"status":"403"}]}`)
		}
		return jsonResponse(http.StatusOK, `{"data":[{"id":"pl.u-1","type":"playlists","attributes":{"name":"Private","artwork":{"url":"catalog-mosaic"}},"relationships":{"tracks":{"data":[{"id":"song-1","type":"songs","attributes":{"name":"One","artistName":"Artist"}}]}}}]}`)
	})

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
	var sawMediaToken bool
	var gotInclude string
	client := newTestClient(t, zhCN, func(req *http.Request) *http.Response {
		sawMediaToken = req.Header.Get("Media-User-Token") != ""
		gotInclude = req.URL.Query().Get("include")
		return jsonResponse(http.StatusOK, `{"data":[{"id":"pl.1","type":"playlists","attributes":{"name":"Editorial","artwork":{"url":"catalog-art"}},"relationships":{"tracks":{"data":[]}}}]}`)
	})

	if _, err := client.Playlist(context.Background(), "cn", "pl.1", ""); err != nil {
		t.Fatal(err)
	}
	if sawMediaToken || gotInclude != "" {
		t.Fatalf("tokenless playlist request leaked auth: header=%v include=%q", sawMediaToken, gotInclude)
	}
}
