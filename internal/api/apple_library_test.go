package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"amdl/internal/applelibrary"
	"amdl/internal/applemusic"
	"amdl/internal/config"
)

// fakeAppleLibrary answers membership from a fixed map; the other methods are
// not used by the routes that take it.
type fakeAppleLibrary struct {
	applelibrary.Engine
	in map[string]map[string]bool
}

func (f *fakeAppleLibrary) Membership(_ context.Context, kind string, ids []string) map[string]bool {
	if f.in[kind] == nil {
		return nil
	}
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = f.in[kind][id]
	}
	return out
}

// pagedApple serves a fixed Apple library the way amp-api pages it.
type pagedApple struct {
	items    map[applemusic.LibraryKind][]applemusic.LibraryItem
	tracks   map[string][]applemusic.LibraryItem
	requests int
	err      error
}

func (a *pagedApple) page(all []applemusic.LibraryItem, offset, limit int) applemusic.LibraryPage {
	end := min(offset+limit, len(all))
	return applemusic.LibraryPage{Items: all[min(offset, end):end], Total: len(all), HasMore: end < len(all)}
}

func (a *pagedApple) LibraryItems(_ context.Context, _ string, kind applemusic.LibraryKind, _ bool, offset, limit int) (applemusic.LibraryPage, error) {
	a.requests++
	if a.err != nil {
		return applemusic.LibraryPage{}, a.err
	}
	return a.page(a.items[kind], offset, limit), nil
}

func (a *pagedApple) LibraryPlaylistTracks(_ context.Context, _ string, id string, offset, limit int) (applemusic.LibraryPage, error) {
	a.requests++
	return a.page(a.tracks[id], offset, limit), nil
}

func (a *pagedApple) AccountStorefront(context.Context, string) (string, error) {
	a.requests++
	return "cn", a.err
}

type refusedToken struct{}

func (refusedToken) Error() string           { return "403 Forbidden" }
func (refusedToken) UserTokenRejected() bool { return true }

func newAppleLibraryServer(t *testing.T, token string) (*Server, *applelibrary.Engine, *pagedApple) {
	t.Helper()
	cfg := config.Default()
	cfg.Catalog.MediaUserToken = token
	cfg.Catalog.DefaultStorefront = "cn"
	store := config.NewStore(cfg)
	songs := make([]applemusic.LibraryItem, 130)
	for i := range songs {
		songs[i] = applemusic.LibraryItem{ID: fmt.Sprintf("i.%03d", i), CatalogID: fmt.Sprintf("%d", 1000+i), Name: fmt.Sprintf("Song %03d", i),
			ArtistName: []string{"Beta", "Alpha"}[i%2], AlbumName: "Album"}
	}
	apple := &pagedApple{
		items: map[applemusic.LibraryKind][]applemusic.LibraryItem{
			applemusic.LibrarySongs: songs,
			applemusic.LibraryAlbums: {
				{ID: "l.1", CatalogID: "6814997249", CatalogURL: "https://music.apple.com/cn/album/x/6814997249", Name: "Encore", ArtistName: "Taylor Swift", DateAdded: "2026-10-26T15:20:21Z"},
				{ID: "l.2", Name: "Uploaded", ArtistName: "Me"},
			},
			applemusic.LibraryPlaylists: {{ID: "p.1", CatalogID: "pl.u-abc", Name: "Mine", CanEdit: true, CanDelete: true}},
			applemusic.LibraryArtists:   {{ID: "r.1", CatalogID: "159260351", Name: "Taylor Swift"}},
		},
		tracks: map[string][]applemusic.LibraryItem{"p.1": songs[:3]},
	}
	server := newServer(t, Deps{Config: store})
	engine := applelibrary.New(store, nil, apple, server.store, slog.Default())
	server.appleLibrary = engine
	return server, engine, apple
}

func TestAppleLibraryRoutesServeTheCache(t *testing.T) {
	server, engine, apple := newAppleLibraryServer(t, "mut")
	handler := server.Routes()

	recorder := requestJSON(t, handler, http.MethodGet, "/api/v1/apple-library/songs", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("before sync: %d %s", recorder.Code, recorder.Body)
	}
	if got := decodeBody(t, recorder.Body.Bytes()); got["synced_at"] != nil || len(got["items"].([]any)) != 0 {
		t.Fatalf("before sync = %v", got)
	}

	if _, err := engine.SyncNow(context.Background(), applelibrary.ModeFull); err != nil {
		t.Fatal(err)
	}
	afterSync := apple.requests

	recorder = requestJSON(t, handler, http.MethodGet, "/api/v1/apple-library/songs?limit=50&offset=100", "")
	got := decodeBody(t, recorder.Body.Bytes())
	items := got["items"].([]any)
	if recorder.Code != http.StatusOK || got["total"] != float64(130) || got["offset"] != float64(100) || len(items) != 30 || got["synced_at"] == nil {
		t.Fatalf("page 3 = %d %v", recorder.Code, got)
	}
	// Newest first by default: the 101st song Apple listed comes first here.
	first := items[0].(map[string]any)
	if first["id"] != "i.100" || first["catalog_id"] != "1100" || first["url"] != "https://music.apple.com/cn/song/1100" {
		t.Fatalf("first item of page 3 = %v", first)
	}

	recorder = requestJSON(t, handler, http.MethodGet, "/api/v1/apple-library/songs?sort=artist&order=asc&limit=1&q=song%20001", "")
	got = decodeBody(t, recorder.Body.Bytes())
	if got["total"] != float64(1) || got["items"].([]any)[0].(map[string]any)["artist_name"] != "Alpha" {
		t.Fatalf("filtered = %v", got)
	}

	recorder = requestJSON(t, handler, http.MethodGet, "/api/v1/apple-library/albums?sort=name", "")
	got = decodeBody(t, recorder.Body.Bytes())
	albums := got["items"].([]any)
	if len(albums) != 2 || albums[0].(map[string]any)["url"] != "https://music.apple.com/cn/album/x/6814997249" {
		t.Fatalf("albums = %v", got)
	}
	if _, has := albums[1].(map[string]any)["url"]; has {
		t.Fatalf("an album without a catalog counterpart has a url: %v", albums[1])
	}

	recorder = requestJSON(t, handler, http.MethodGet, "/api/v1/apple-library/playlists/p.1/tracks", "")
	got = decodeBody(t, recorder.Body.Bytes())
	if recorder.Code != http.StatusOK || got["total"] != float64(3) || got["playlist_id"] != "p.1" {
		t.Fatalf("tracks = %d %v", recorder.Code, got)
	}
	assertErrorCode(t, requestJSON(t, handler, http.MethodGet, "/api/v1/apple-library/playlists/p.nope/tracks", ""), http.StatusNotFound, CodeCatalogNotFound)

	recorder = requestJSON(t, handler, http.MethodGet, "/api/v1/apple-library/status", "")
	got = decodeBody(t, recorder.Body.Bytes())
	counts := got["counts"].(map[string]any)
	if got["available"] != true || got["state"] != "idle" || counts["songs"] != float64(130) || counts["albums"] != float64(2) {
		t.Fatalf("status = %v", got)
	}
	if apple.requests != afterSync {
		t.Fatalf("browsing made %d Apple requests", apple.requests-afterSync)
	}

	for _, path := range []string{
		"/api/v1/apple-library/movies", "/api/v1/apple-library/artists?sort=added",
		"/api/v1/apple-library/playlists?sort=artist", "/api/v1/apple-library/songs?order=up",
		"/api/v1/apple-library/songs?limit=201",
	} {
		assertErrorCode(t, requestJSON(t, handler, http.MethodGet, path, ""), http.StatusBadRequest, CodeInvalidRequest)
	}
	assertErrorCode(t, requestJSON(t, handler, http.MethodPost, "/api/v1/apple-library/sync", `{"mode":"partial"}`), http.StatusBadRequest, CodeInvalidRequest)

	recorder = requestJSON(t, handler, http.MethodPost, "/api/v1/apple-library/sync", `{"mode":"incremental"}`)
	got = decodeBody(t, recorder.Body.Bytes())
	if recorder.Code != http.StatusAccepted || got["current"] == nil && got["last_sync"] == nil {
		t.Fatalf("sync = %d %v", recorder.Code, got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for engine.Running() != "" {
		if time.Now().After(deadline) {
			t.Fatal("the triggered sync did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAppleLibraryUnavailableWithoutTokenOrWhenRefused(t *testing.T) {
	server, _, _ := newAppleLibraryServer(t, "")
	handler := server.Routes()
	recorder := requestJSON(t, handler, http.MethodGet, "/api/v1/apple-library/albums", "")
	assertErrorCode(t, recorder, http.StatusServiceUnavailable, CodeAppleLibraryUnavailable)
	if details := decodeBody(t, recorder.Body.Bytes())["error"].(map[string]any)["details"].(map[string]any); details["reason"] != "no_token" {
		t.Fatalf("details = %v", details)
	}
	assertErrorCode(t, requestJSON(t, handler, http.MethodPost, "/api/v1/apple-library/sync", ""), http.StatusServiceUnavailable, CodeAppleLibraryUnavailable)
	got := decodeBody(t, requestJSON(t, handler, http.MethodGet, "/api/v1/apple-library/status", "").Body.Bytes())
	if got["available"] != false || got["reason"] != "no_token" {
		t.Fatalf("status = %v", got)
	}

	server, engine, apple := newAppleLibraryServer(t, "expired")
	apple.err = refusedToken{}
	if run, _ := engine.SyncNow(context.Background(), applelibrary.ModeFull); run.OK {
		t.Fatal("a refused token synced")
	}
	recorder = requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/apple-library/songs", "")
	assertErrorCode(t, recorder, http.StatusServiceUnavailable, CodeAppleLibraryUnavailable)
	if details := decodeBody(t, recorder.Body.Bytes())["error"].(map[string]any)["details"].(map[string]any); details["reason"] != "token_refused" {
		t.Fatalf("details = %v", details)
	}
}

func TestSearchMarksHitsInTheAppleLibrary(t *testing.T) {
	search := sampleSearch()
	search.Artists = []applemusic.Artist{{ID: "159260351", Name: "Taylor Swift"}}
	// Apple marked the song and the album; the playlist has no mark, so the
	// cache answers for it; the artist comes from the cache only.
	search.InLibrary = map[applemusic.SearchType]map[string]bool{
		applemusic.SearchSongs:  {"6814997425": true},
		applemusic.SearchAlbums: {"6814997249": false},
	}
	catalog := &fakeCatalog{t: t, search: search}
	cfg := config.Default()
	cfg.Catalog.DefaultStorefront = "us"
	cfg.Catalog.MediaUserToken = "mut"
	server := newServer(t, Deps{Config: config.NewStore(cfg), Catalog: catalog})
	server.appleLibrary = &fakeAppleLibrary{in: map[string]map[string]bool{
		"playlists": {"pl.2553030a768547e9ab3e91230de8439f": true},
		"artists":   {},
	}}
	got := decodeBody(t, requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/search?term=taylor", "").Body.Bytes())
	mark := func(group string) any {
		return got[group].(map[string]any)["items"].([]any)[0].(map[string]any)["in_apple_library"]
	}
	if mark("songs") != true || mark("albums") != false || mark("playlists") != true || mark("artists") != false {
		t.Fatalf("marks = songs %v albums %v playlists %v artists %v", mark("songs"), mark("albums"), mark("playlists"), mark("artists"))
	}
	if catalog.searchToken != "mut" {
		t.Fatalf("search carried token %q", catalog.searchToken)
	}

	// No token, nothing cached: no mark at all, and search answers as before.
	search.InLibrary = nil
	cfg.Catalog.MediaUserToken = ""
	server = newServer(t, Deps{Config: config.NewStore(cfg), Catalog: catalog})
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/search?term=taylor", "")
	got = decodeBody(t, recorder.Body.Bytes())
	if recorder.Code != http.StatusOK || catalog.searchToken != "" {
		t.Fatalf("search without token = %d, token %q", recorder.Code, catalog.searchToken)
	}
	for _, group := range []string{"songs", "albums", "playlists", "artists"} {
		if _, has := got[group].(map[string]any)["items"].([]any)[0].(map[string]any)["in_apple_library"]; has {
			t.Fatalf("%s hit carries in_apple_library without a token or cache", group)
		}
	}
}
