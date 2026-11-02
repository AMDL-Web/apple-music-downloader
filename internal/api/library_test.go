package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
)

// fakeLibrary answers the library queries from a set of song ids on disk and
// per-album folder counts.
type fakeLibrary struct {
	onDisk  map[string]bool
	folders map[string]int
	// cancelled makes AlbumTracksOnDisk fail as a request past its deadline.
	cancelled bool
}

func (f *fakeLibrary) AlbumTracksOnDisk(_ context.Context, album applemusic.Collection) (domain.LibraryState, []bool, error) {
	if f.cancelled {
		return domain.LibraryState{}, nil, context.DeadlineExceeded
	}
	got := make([]bool, len(album.Tracks))
	n := 0
	for i, track := range album.Tracks {
		if f.onDisk[track.ID] {
			got[i] = true
			n++
		}
	}
	return domain.NewLibraryState(n, len(album.Tracks)), got, nil
}

func (f *fakeLibrary) SongOnDisk(album applemusic.Collection, songID string) (domain.LibraryState, bool) {
	for _, track := range album.Tracks {
		if track.ID == songID {
			n := 0
			if f.onDisk[songID] {
				n = 1
			}
			return domain.NewLibraryState(n, 1), true
		}
	}
	return domain.LibraryState{}, false
}

func (f *fakeLibrary) AlbumFolderStates(_ context.Context, albums []applemusic.Collection) []*domain.LibraryState {
	out := make([]*domain.LibraryState, len(albums))
	for i, album := range albums {
		if n, ok := f.folders[album.ID]; ok {
			state := domain.NewLibraryState(n, album.TrackCount)
			out[i] = &state
		}
	}
	return out
}

func (f *fakeLibrary) SongHitStates(_ context.Context, songs []applemusic.Song) []*domain.LibraryState {
	out := make([]*domain.LibraryState, len(songs))
	for i, song := range songs {
		if song.AlbumID == "" {
			continue
		}
		n := 0
		if f.onDisk[song.ID] {
			n = 1
		}
		state := domain.NewLibraryState(n, 1)
		out[i] = &state
	}
	return out
}

func newLibraryServer(catalog CatalogService, library LibraryService) http.Handler {
	return (&Server{cfg: config.NewStore(config.Default()), catalog: catalog, library: library, appleLibrary: &fakeAppleLibrary{}}).Routes()
}

func libraryOf(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("library = %#v, want an object", v)
	}
	return m
}

func TestAlbumSaysWhichSongsAreOnDisk(t *testing.T) {
	catalog := &fakeCatalog{t: t, album: sampleAlbum()}
	library := &fakeLibrary{onDisk: map[string]bool{"10": true, "12": true}}
	recorder := requestJSON(t, newLibraryServer(catalog, library), http.MethodGet, "/api/v1/albums/1783644603?storefront=cn", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	got := decodeBody(t, recorder.Body.Bytes())
	if want := map[string]any{"state": "partial", "tracks_on_disk": float64(2), "tracks_total": float64(3)}; !reflect.DeepEqual(got["library"], want) {
		t.Fatalf("library = %v, want %v", got["library"], want)
	}
	// The tracks are sorted by disc and track; on_disk follows each song,
	// not its position in the catalog's list.
	onDisk := map[string]any{}
	for _, tr := range got["tracks"].([]any) {
		m := tr.(map[string]any)
		onDisk[m["id"].(string)] = m["on_disk"]
	}
	if want := map[string]any{"10": true, "11": false, "12": true}; !reflect.DeepEqual(onDisk, want) {
		t.Fatalf("on_disk = %v, want %v", onDisk, want)
	}
}

func TestAlbumLeavesTheLibraryOutWhenItCannotCheck(t *testing.T) {
	for name, library := range map[string]LibraryService{"no library": nil, "past the deadline": &fakeLibrary{cancelled: true}} {
		t.Run(name, func(t *testing.T) {
			catalog := &fakeCatalog{t: t, album: sampleAlbum()}
			recorder := requestJSON(t, newLibraryServer(catalog, library), http.MethodGet, "/api/v1/albums/1783644603?storefront=cn", "")
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
			}
			got := decodeBody(t, recorder.Body.Bytes())
			if _, ok := got["library"]; ok {
				t.Errorf("library present: %v", got["library"])
			}
			for _, tr := range got["tracks"].([]any) {
				if _, ok := tr.(map[string]any)["on_disk"]; ok {
					t.Errorf("on_disk present without a library: %v", tr)
				}
			}
		})
	}
}

func TestLookupAlbumCarriesItsLibraryState(t *testing.T) {
	catalog := &fakeCatalog{t: t, album: sampleAlbum()}
	library := &fakeLibrary{onDisk: map[string]bool{"10": true, "11": true, "12": true}}
	recorder := requestJSON(t, newLibraryServer(catalog, library), http.MethodPost, "/api/v1/lookup", `{"url":"https://music.apple.com/cn/album/two-discs/1783644603"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	got := decodeLookup(t, recorder.Body.Bytes())
	if lib := libraryOf(t, got["library"]); lib["state"] != "complete" || lib["tracks_on_disk"] != float64(3) || lib["tracks_total"] != float64(3) {
		t.Fatalf("library = %v", lib)
	}
	if want := []string{"album cn/1783644603"}; !reflect.DeepEqual(catalog.calls, want) {
		t.Fatalf("catalog calls = %v, want %v (an album needs no further read)", catalog.calls, want)
	}
}

func TestLookupSongReadsItsAlbumForTheLibraryState(t *testing.T) {
	song := &applemusic.Song{ID: "11", Name: "Closer", AlbumID: "1783644603", AlbumName: "Two Discs"}
	catalog := &fakeCatalog{t: t, song: song, album: sampleAlbum()}
	library := &fakeLibrary{onDisk: map[string]bool{"11": true}}
	recorder := requestJSON(t, newLibraryServer(catalog, library), http.MethodPost, "/api/v1/lookup", `{"url":"https://music.apple.com/cn/song/closer/11"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	got := decodeLookup(t, recorder.Body.Bytes())
	if want := map[string]any{"state": "complete", "tracks_on_disk": float64(1), "tracks_total": float64(1)}; !reflect.DeepEqual(got["library"], want) {
		t.Fatalf("library = %v, want %v", got["library"], want)
	}
	if want := []string{"song cn/11", "album cn/1783644603"}; !reflect.DeepEqual(catalog.calls, want) {
		t.Fatalf("catalog calls = %v, want %v", catalog.calls, want)
	}
}

// failingAlbumCatalog answers the song and fails the album read.
type failingAlbumCatalog struct{ *fakeCatalog }

func (failingAlbumCatalog) Album(context.Context, string, string) (applemusic.Collection, error) {
	return applemusic.Collection{}, errors.New("upstream 500")
}

func TestLookupSongAnswersWithoutLibraryWhenItsAlbumCannotPlaceIt(t *testing.T) {
	song := &applemusic.Song{ID: "11", Name: "Closer", AlbumID: "1783644603"}
	library := &fakeLibrary{onDisk: map[string]bool{"11": true}}
	for name, catalog := range map[string]CatalogService{
		"album read fails":         failingAlbumCatalog{&fakeCatalog{t: t, song: song}},
		"album does not list song": &fakeCatalog{t: t, song: song, album: &applemusic.Collection{ID: "1783644603", Tracks: []applemusic.Song{{ID: "99"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := requestJSON(t, newLibraryServer(catalog, library), http.MethodPost, "/api/v1/lookup", `{"url":"https://music.apple.com/cn/song/closer/11"}`)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s (the album read is optional)", recorder.Code, recorder.Body.String())
			}
			got := decodeLookup(t, recorder.Body.Bytes())
			if got["title"] != "Closer" {
				t.Errorf("title = %v", got["title"])
			}
			if _, ok := got["library"]; ok {
				t.Errorf("library present: %v", got["library"])
			}
		})
	}
}

func TestSearchAndArtistAlbumsEstimateAlbumsFromTheirFolders(t *testing.T) {
	library := &fakeLibrary{folders: map[string]int{"6814997249": 9}}
	want := map[string]any{"state": "partial", "tracks_on_disk": float64(9), "tracks_total": float64(16)}

	catalog := &fakeCatalog{t: t, search: sampleSearch()}
	recorder := requestJSON(t, newLibraryServer(catalog, library), http.MethodGet, "/api/v1/search?term=taylor&storefront=us", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("search status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	var res struct {
		Songs, Albums, Playlists struct {
			Items []map[string]any `json:"items"`
		}
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if got := res.Albums.Items[0]["library"]; !reflect.DeepEqual(got, want) {
		t.Errorf("album hit library = %v, want %v", got, want)
	}
	for _, hit := range append(res.Songs.Items, res.Playlists.Items...) {
		if _, ok := hit["library"]; ok {
			t.Errorf("%v hit has library: %v", hit["type"], hit)
		}
	}

	artistCatalog := &fakeCatalog{t: t, artist: &applemusic.Artist{ID: "159260351", Name: "Taylor Swift"},
		albums: &applemusic.AlbumPage{Albums: sampleSearch().Albums}}
	recorder = requestJSON(t, newLibraryServer(artistCatalog, library), http.MethodGet, "/api/v1/artists/159260351/albums?storefront=us", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("artist albums status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	got := decodeBody(t, recorder.Body.Bytes())
	if _, ok := got["artist"].(map[string]any)["library"]; ok {
		t.Errorf("artist hit has library: %v", got["artist"])
	}
	items := got["albums"].(map[string]any)["items"].([]any)
	if lib := items[0].(map[string]any)["library"]; !reflect.DeepEqual(lib, want) {
		t.Errorf("artist album library = %v, want %v", lib, want)
	}
}

// A storefront can withhold tracks from an album's track list while the
// summary's track_count still counts them. An album whose queue entry
// resolved 15 of the summary's 16 tracks, all 15 on disk, reads complete on a
// card, as it does on its album page, instead of 15/16 for good.
func TestAlbumCardsCountTheTracksTheirJobResolved(t *testing.T) {
	catalog := &fakeCatalog{t: t, search: sampleSearch(), artist: &applemusic.Artist{ID: "159260351", Name: "Taylor Swift"},
		albums: &applemusic.AlbumPage{Albums: sampleSearch().Albums}}
	server := newServer(t, Deps{Catalog: catalog, Library: &fakeLibrary{folders: map[string]int{"6814997249": 15}}})
	created := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	if err := server.store.CreateJob(context.Background(), domain.Job{
		ID: "album-done", Type: "album", Input: "input", Storefront: "cn", CanonicalKey: "album:cn:6814997249",
		Status: domain.JobCompleted, TotalItems: 15, CreatedAt: created, UpdatedAt: created, FinishedAt: &created,
	}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"state": "complete", "tracks_on_disk": float64(15), "tracks_total": float64(15)}
	for _, path := range []string{"/api/v1/search?term=taylor&types=albums&storefront=us", "/api/v1/artists/159260351/albums?storefront=us"} {
		recorder := requestJSON(t, server.Routes(), http.MethodGet, path, "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body %s", path, recorder.Code, recorder.Body.String())
		}
		hit := decodeBody(t, recorder.Body.Bytes())["albums"].(map[string]any)["items"].([]any)[0].(map[string]any)
		if got := hit["library"]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s album library = %v, want %v", path, got, want)
		}
	}
}

// A song hit the search placed (its album came with it) says whether its
// file is on disk; one the library cannot place has no library.
func TestSearchMarksSongHitsOnDisk(t *testing.T) {
	results := sampleSearch()
	placed := results.Songs[0]
	placed.ID, placed.AlbumID, placed.AlbumArtist = "s-on-disk", "6814997249", "Taylor Swift"
	missing := placed
	missing.ID = "s-missing"
	results.Songs = append(results.Songs, placed, missing)
	library := &fakeLibrary{onDisk: map[string]bool{"s-on-disk": true}}
	recorder := requestJSON(t, newLibraryServer(&fakeCatalog{t: t, search: results}, library), http.MethodGet, "/api/v1/search?term=taylor&types=songs", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	items := decodeBody(t, recorder.Body.Bytes())["songs"].(map[string]any)["items"].([]any)
	got := make([]any, len(items))
	for i, item := range items {
		got[i] = item.(map[string]any)["library"]
	}
	want := []any{
		nil,
		map[string]any{"state": "complete", "tracks_on_disk": float64(1), "tracks_total": float64(1)},
		map[string]any{"state": "none", "tracks_on_disk": float64(0), "tracks_total": float64(1)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("song hit libraries = %v, want %v", got, want)
	}
}

func TestSearchLeavesAnUncountedAlbumWithoutLibrary(t *testing.T) {
	catalog := &fakeCatalog{t: t, search: sampleSearch()}
	recorder := requestJSON(t, newLibraryServer(catalog, &fakeLibrary{}), http.MethodGet, "/api/v1/search?term=taylor&types=albums", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	got := decodeBody(t, recorder.Body.Bytes())
	hit := got["albums"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if _, ok := hit["library"]; ok {
		t.Errorf("library present for an album the folder listing did not reach: %v", hit["library"])
	}
}
