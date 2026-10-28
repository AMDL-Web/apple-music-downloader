package applemusic

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
)

// libraryHostMode is one way the client can be configured, with the host and
// headers Apple must see in that mode.
type libraryHostMode struct {
	name       string
	host       string
	wantOrigin string
	setup      func(t *testing.T) *CatalogClient
}

func libraryHostModes() []libraryHostMode {
	return []libraryHostMode{
		{
			name:       "legacy web token",
			host:       "amp-api.music.apple.com",
			wantOrigin: "https://music.apple.com",
			setup: func(t *testing.T) *CatalogClient {
				client := newTestCatalogClient(config.CatalogConfig{Language: "en-US", TokenCacheTTLHours: 12}, slog.Default())
				client.catalogToken.value, client.catalogToken.until = "cached-token", time.Now().Add(time.Hour)
				return client
			},
		},
		{
			name:       "signed developer token",
			host:       "api.music.apple.com",
			wantOrigin: "",
			setup: func(t *testing.T) *CatalogClient {
				path, _ := writeP8Key(t)
				client := newTestCatalogClient(config.CatalogConfig{
					Language:                 "en-US",
					AppleMusicPrivateKeyPath: path,
					AppleMusicKeyID:          "KID1234567",
					AppleMusicTeamID:         "TEAM123456",
				}, slog.Default())
				if err := client.InitDeveloperToken(); err != nil {
					t.Fatalf("InitDeveloperToken: %v", err)
				}
				return client
			},
		},
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

// scrapeResponse serves the two music.apple.com requests a legacy-mode token
// refresh makes, or reports false for any other request.
func scrapeResponse(req *http.Request) (*http.Response, bool) {
	if req.URL.Host != "music.apple.com" {
		return nil, false
	}
	if req.URL.Path == "" || req.URL.Path == "/" {
		return jsonResponse(http.StatusOK, `<script src="/assets/index~abc123.js"></script>`), true
	}
	return jsonResponse(http.StatusOK, `token:"eyJhbGciOiJFUzI1NiJ9.eyJmb28iOiJiYXIifQ.refreshed-sig"`), true
}

func TestLibrarySongsNewestFirstReadsOnePageInBothHostModes(t *testing.T) {
	for _, mode := range libraryHostModes() {
		t.Run(mode.name, func(t *testing.T) {
			client := mode.setup(t)
			var got *http.Request
			client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				got = req
				body := `{"next":"/v1/me/library/songs?offset=125","data":[
					{"id":"i.1","attributes":{"name":"One"},"relationships":{"albums":{"data":[{"id":"l.a","attributes":{"name":"Album","artistName":"Artist"}}]}}},
					{"id":"i.2","attributes":{"name":"Loose"},"relationships":{"albums":{"data":[]}}}]}`
				return jsonResponse(http.StatusOK, body), nil
			})}

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
			if got.URL.Host != mode.host || got.URL.Path != "/v1/me/library/songs" {
				t.Fatalf("request = %s, want host %s path /v1/me/library/songs", got.URL, mode.host)
			}
			query := got.URL.Query()
			// Apple caps library pages at 100, so a larger request is clamped.
			if query.Get("limit") != "100" || query.Get("offset") != "25" || query.Get("sort") != "-dateAdded" || query.Get("include") != "albums" {
				t.Fatalf("query = %v, want limit=100 offset=25 sort=-dateAdded include=albums", query)
			}
			if got.Header.Get("Media-User-Token") != "user-token" {
				t.Fatalf("Media-User-Token = %q", got.Header.Get("Media-User-Token"))
			}
			if got.Header.Get("Origin") != mode.wantOrigin {
				t.Fatalf("Origin = %q, want %q", got.Header.Get("Origin"), mode.wantOrigin)
			}
			if !strings.HasPrefix(got.Header.Get("Authorization"), "Bearer ") {
				t.Fatalf("Authorization = %q, want a bearer token", got.Header.Get("Authorization"))
			}
		})
	}
}

func TestLibrarySongsNewestFirstLastPageHasNoMore(t *testing.T) {
	client := libraryHostModes()[0].setup(t)
	client.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"data":[{"id":"i.9","attributes":{"name":"Last"}}]}`), nil
	})}

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
	for _, mode := range libraryHostModes() {
		for _, tt := range tests {
			t.Run(mode.name+"/"+tt.name, func(t *testing.T) {
				client := mode.setup(t)
				var got *http.Request
				client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					got = req
					return jsonResponse(tt.status, tt.body), nil
				})}

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
				if got.URL.Host != mode.host || got.URL.Path != "/v1/me/library/albums/l.abc/catalog" {
					t.Fatalf("request = %s, want host %s path /v1/me/library/albums/l.abc/catalog", got.URL, mode.host)
				}
				if got.Header.Get("Media-User-Token") != "user-token" || got.Header.Get("Origin") != mode.wantOrigin {
					t.Fatalf("headers = Media-User-Token %q, Origin %q; want user-token, %q", got.Header.Get("Media-User-Token"), got.Header.Get("Origin"), mode.wantOrigin)
				}
			})
		}
	}
}

// A library read that meets a 401 drops the rejected token, gets a new one
// (a scrape in legacy mode, a fresh signature in signed mode) and retries once.
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
	for _, mode := range libraryHostModes() {
		for _, call := range calls {
			t.Run(mode.name+"/"+call.name, func(t *testing.T) {
				client := mode.setup(t)
				var auth []string
				client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if resp, ok := scrapeResponse(req); ok {
						return resp, nil
					}
					if req.URL.Path != call.path {
						return jsonResponse(http.StatusTeapot, "unexpected "+req.URL.Path), nil
					}
					auth = append(auth, req.Header.Get("Authorization"))
					if len(auth) == 1 {
						return jsonResponse(http.StatusUnauthorized, `{"errors":[{"status":"401"}]}`), nil
					}
					return jsonResponse(http.StatusOK, call.ok), nil
				})}

				if err := call.call(client); err != nil {
					t.Fatalf("expected success after one refresh, got %v", err)
				}
				if len(auth) != 2 || auth[0] == auth[1] {
					t.Fatalf("Authorization per attempt = %q, want two attempts with different tokens", auth)
				}
			})
		}
	}
}

func TestLibraryReadsRequireAMediaUserToken(t *testing.T) {
	client := libraryHostModes()[0].setup(t)
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected request %s without a media-user-token", req.URL)
		return nil, nil
	})}

	if _, _, err := client.LibrarySongsNewestFirst(context.Background(), "", 0, 10); err == nil {
		t.Fatal("LibrarySongsNewestFirst without a token: want an error")
	}
	if _, _, err := client.LibraryAlbumCatalogURL(context.Background(), "", "l.abc"); err == nil {
		t.Fatal("LibraryAlbumCatalogURL without a token: want an error")
	}
}
