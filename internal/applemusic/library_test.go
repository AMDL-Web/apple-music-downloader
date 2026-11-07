package applemusic

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"amdl/internal/config"
)

// Library reads go to amp-api with the scraped web-player token, and that host
// requires the Origin header.
const (
	libraryHost   = "amp-api.music.apple.com"
	libraryOrigin = "https://music.apple.com"
)

// libraryConfig is the catalog configuration of the library and playback
// tests.
var libraryConfig = config.CatalogConfig{Language: "en-US", TokenCacheTTLHours: 12}

func TestLibrarySongsNewestFirstReadsOnePage(t *testing.T) {
	var got *http.Request
	client := newTestClient(t, libraryConfig, func(req *http.Request) *http.Response {
		got = req
		return jsonResponse(http.StatusOK, `{"next":"/v1/me/library/songs?offset=125","data":[
			{"id":"i.1","attributes":{"name":"One"},"relationships":{"albums":{"data":[{"id":"l.a","attributes":{"name":"Album","artistName":"Artist"}}]}}},
			{"id":"i.2","attributes":{"name":"Loose"},"relationships":{"albums":{"data":[]}}}]}`)
	})

	songs, hasMore, err := client.LibrarySongsNewestFirst(context.Background(), "user-token", 25, 500)
	if err != nil {
		t.Fatal(err)
	}
	want := []LibrarySong{
		{ID: "i.1", Name: "One", LibraryAlbumID: "l.a", AlbumName: "Album", AlbumArtist: "Artist"},
		{ID: "i.2", Name: "Loose"},
	}
	if !reflect.DeepEqual(songs, want) || !hasMore {
		t.Fatalf("songs = %+v, hasMore = %v; want %+v, true", songs, hasMore, want)
	}
	if got.URL.Host != libraryHost || got.URL.Path != "/v1/me/library/songs" {
		t.Fatalf("request = %s, want host %s path /v1/me/library/songs", got.URL, libraryHost)
	}
	query := got.URL.Query()
	// Apple caps library pages at 100, so a larger request is clamped.
	if query.Get("limit") != "100" || query.Get("offset") != "25" || query.Get("sort") != "-dateAdded" || query.Get("include") != "albums" {
		t.Fatalf("query = %v, want limit=100 offset=25 sort=-dateAdded include=albums", query)
	}
	if got.Header.Get("Media-User-Token") != "user-token" {
		t.Fatalf("Media-User-Token = %q", got.Header.Get("Media-User-Token"))
	}
	if got.Header.Get("Origin") != libraryOrigin {
		t.Fatalf("Origin = %q, want %q", got.Header.Get("Origin"), libraryOrigin)
	}
	if !strings.HasPrefix(got.Header.Get("Authorization"), "Bearer ") {
		t.Fatalf("Authorization = %q, want a bearer token", got.Header.Get("Authorization"))
	}
}

func TestLibrarySongsNewestFirstLastPageHasNoMore(t *testing.T) {
	client := newTestClient(t, libraryConfig, func(*http.Request) *http.Response {
		return jsonResponse(http.StatusOK, `{"data":[{"id":"i.9","attributes":{"name":"Last"}}]}`)
	})

	songs, hasMore, err := client.LibrarySongsNewestFirst(context.Background(), "user-token", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 1 || hasMore {
		t.Fatalf("songs = %+v, hasMore = %v; want one song and no next page", songs, hasMore)
	}
}

func TestLibraryAlbumCatalogURL(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantURL string
		wantOK  bool
		wantErr bool
	}{
		{name: "catalog counterpart", status: http.StatusOK, body: `{"data":[{"id":"1440","attributes":{"name":"Album","url":"https://music.apple.com/us/album/album/1440"}}]}`, wantURL: "https://music.apple.com/us/album/album/1440", wantOK: true},
		{name: "no counterpart answers 404", status: http.StatusNotFound, body: `{"errors":[{"status":"404"}]}`, wantOK: false},
		{name: "empty data", status: http.StatusOK, body: `{"data":[]}`, wantOK: false},
		{name: "server error is an error", status: http.StatusBadGateway, body: `{"errors":[]}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *http.Request
			client := newTestClient(t, libraryConfig, func(req *http.Request) *http.Response {
				got = req
				return jsonResponse(tt.status, tt.body)
			})

			catalogURL, ok, err := client.LibraryAlbumCatalogURL(context.Background(), "user-token", "l.abc")
			if (err != nil) != tt.wantErr || catalogURL != tt.wantURL || ok != tt.wantOK {
				t.Fatalf("LibraryAlbumCatalogURL = (%q, %v, %v), want (%q, %v, err=%v)", catalogURL, ok, err, tt.wantURL, tt.wantOK, tt.wantErr)
			}
			if tt.wantErr {
				var requestErr catalogRequestError
				if !errors.As(err, &requestErr) || requestErr.NonRetryable() {
					t.Fatalf("error = %v, want a retryable catalogRequestError", err)
				}
			}
			if got.URL.Host != libraryHost || got.URL.Path != "/v1/me/library/albums/l.abc/catalog" {
				t.Fatalf("request = %s, want host %s path /v1/me/library/albums/l.abc/catalog", got.URL, libraryHost)
			}
			if got.Header.Get("Media-User-Token") != "user-token" || got.Header.Get("Origin") != libraryOrigin {
				t.Fatalf("headers = Media-User-Token %q, Origin %q; want user-token, %q", got.Header.Get("Media-User-Token"), got.Header.Get("Origin"), libraryOrigin)
			}
		})
	}
}

// A library read that meets a 401 drops the rejected token, scrapes a new one
// and retries once.
func TestLibraryReadsRefreshTheTokenOn401(t *testing.T) {
	calls := []struct {
		name string
		path string
		ok   string
		call func(*CatalogClient) error
	}{
		{
			name: "songs",
			path: "/v1/me/library/songs",
			ok:   `{"data":[]}`,
			call: func(c *CatalogClient) error {
				_, _, err := c.LibrarySongsNewestFirst(context.Background(), "user-token", 0, 50)
				return err
			},
		},
		{
			name: "album catalog url",
			path: "/v1/me/library/albums/l.abc/catalog",
			ok:   `{"data":[{"id":"1","attributes":{"url":"https://music.apple.com/us/album/x/1"}}]}`,
			call: func(c *CatalogClient) error {
				_, _, err := c.LibraryAlbumCatalogURL(context.Background(), "user-token", "l.abc")
				return err
			},
		},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			var auth []string
			client := newTestClient(t, libraryConfig, func(req *http.Request) *http.Response {
				if resp, ok := scrapeResponse(req); ok {
					return resp
				}
				if req.URL.Path != call.path {
					return jsonResponse(http.StatusTeapot, "unexpected "+req.URL.Path)
				}
				auth = append(auth, req.Header.Get("Authorization"))
				if len(auth) == 1 {
					return jsonResponse(http.StatusUnauthorized, `{"errors":[{"status":"401"}]}`)
				}
				return jsonResponse(http.StatusOK, call.ok)
			})

			if err := call.call(client); err != nil {
				t.Fatalf("expected success after one refresh, got %v", err)
			}
			if len(auth) != 2 || auth[0] == auth[1] {
				t.Fatalf("Authorization per attempt = %q, want two attempts with different tokens", auth)
			}
		})
	}
}

func TestLibraryReadsRequireAMediaUserToken(t *testing.T) {
	client := newTestClient(t, libraryConfig, func(req *http.Request) *http.Response {
		t.Errorf("unexpected request %s without a media-user-token", req.URL)
		return nil
	})

	if _, _, err := client.LibrarySongsNewestFirst(context.Background(), "", 0, 10); err == nil {
		t.Fatal("LibrarySongsNewestFirst without a token: want an error")
	}
	if _, _, err := client.LibraryAlbumCatalogURL(context.Background(), "", "l.abc"); err == nil {
		t.Fatal("LibraryAlbumCatalogURL without a token: want an error")
	}
}
