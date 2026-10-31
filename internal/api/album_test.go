package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

// sampleAlbum is an album as applemusic.Album maps Apple's answer: songs only
// (it drops music videos), in the order the relationship listed them.
func sampleAlbum() *applemusic.Collection {
	return &applemusic.Collection{
		ID: "1783644603", Type: applemusic.TypeAlbum, Name: "Two Discs", Artist: "Some Artist",
		ArtistURL:  "https://music.apple.com/cn/artist/some-artist/42",
		ArtworkURL: "https://is1.mzstatic.com/a/{w}x{h}bb.jpg", ArtworkColors: palette,
		ReleaseDate: "2026-01-02", GenreNames: []string{"Music", "Pop"},
		ContentRating: "explicit", AppleDigitalMaster: true, AudioTraits: []string{"lossless", "lossy-stereo"},
		Tracks: []applemusic.Song{
			{ID: "12", Name: "Second Disc Opener", ArtistName: "Some Artist", DiscNumber: 2, TrackNumber: 1, DurationInMillis: 181000,
				URL: "https://music.apple.com/cn/album/two-discs/1783644603?i=12&l=en-US"},
			{ID: "11", Name: "Closer", ArtistName: "Some Artist feat. Guest", DiscNumber: 1, TrackNumber: 2, DurationInMillis: 200500,
				ContentRating: "explicit", AppleDigitalMaster: true, HasLyrics: true, AudioTraits: []string{"lossless"},
				URL: "https://music.apple.com/cn/album/two-discs/1783644603?i=11&l=en-US"},
			{ID: "10", Name: "Opener", ArtistName: "Some Artist", DiscNumber: 1, TrackNumber: 1, DurationInMillis: 190000},
		},
	}
}

func TestAlbumReturnsMetadataAndTracksInDiscOrder(t *testing.T) {
	catalog := &fakeCatalog{t: t, album: sampleAlbum()}
	recorder := requestJSON(t, newLookupServer(catalog, nil), http.MethodGet, "/api/v1/albums/1783644603?storefront=CN", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	if want := []string{"album cn/1783644603"}; !reflect.DeepEqual(catalog.calls, want) {
		t.Fatalf("catalog calls = %v, want %v", catalog.calls, want)
	}
	got := decodeBody(t, recorder.Body.Bytes())
	tracks, _ := got["tracks"].([]any)
	delete(got, "tracks")
	want := map[string]any{
		"storefront": "cn", "id": "1783644603", "url": "https://music.apple.com/cn/album/1783644603",
		"title": "Two Discs", "artist_name": "Some Artist", "artist_url": "https://music.apple.com/cn/artist/some-artist/42",
		"artwork_url": "https://is1.mzstatic.com/a/{w}x{h}bb.jpg", "artwork_bg_color": "101010",
		"artwork_text_color1": "f1f1f1", "artwork_text_color2": "e2e2e2", "artwork_text_color3": "d3d3d3", "artwork_text_color4": "c4c4c4",
		"release_date": "2026-01-02", "genre": "Pop", "track_count": float64(3), "disc_count": float64(2),
		"content_rating": "explicit", "apple_digital_master": true, "audio_traits": []any{"lossless", "lossy-stereo"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("album = %v\nwant %v", got, want)
	}
	var order []string
	for _, tr := range tracks {
		m := tr.(map[string]any)
		order = append(order, fmt.Sprintf("%v.%v=%v", m["disc_number"], m["track_number"], m["id"]))
	}
	if want := []string{"1.1=10", "1.2=11", "2.1=12"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("track order = %v, want %v", order, want)
	}
	second := tracks[1].(map[string]any)
	wantTrack := map[string]any{
		"id": "11", "url": "https://music.apple.com/cn/song/two-discs/11", "disc_number": float64(1), "track_number": float64(2),
		"name": "Closer", "artist_name": "Some Artist feat. Guest", "duration_ms": float64(200500), "content_rating": "explicit",
		"apple_digital_master": true, "audio_traits": []any{"lossless"}, "has_lyrics": true,
	}
	if !reflect.DeepEqual(second, wantTrack) {
		t.Fatalf("track = %v\nwant %v", second, wantTrack)
	}
	// A track whose catalog entry has no page falls back to a built link and
	// leaves the optional fields out.
	first := tracks[0].(map[string]any)
	if want := map[string]any{"id": "10", "url": "https://music.apple.com/cn/song/10", "disc_number": float64(1), "track_number": float64(1), "name": "Opener", "artist_name": "Some Artist", "duration_ms": float64(190000)}; !reflect.DeepEqual(first, want) {
		t.Fatalf("track = %v\nwant %v", first, want)
	}
}

func TestAlbumWithoutSongsAnswersAnEmptyList(t *testing.T) {
	catalog := &fakeCatalog{t: t, album: &applemusic.Collection{ID: "5", Name: "Videos Only"}}
	recorder := requestJSON(t, newLookupServer(catalog, func(c *config.Config) { c.Catalog.DefaultStorefront = "jp" }), http.MethodGet, "/api/v1/albums/5", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	got := decodeBody(t, recorder.Body.Bytes())
	if tracks, ok := got["tracks"].([]any); !ok || len(tracks) != 0 || got["track_count"] != float64(0) || got["storefront"] != "jp" {
		t.Fatalf("response = %v", got)
	}
	if _, has := got["disc_count"]; has {
		t.Fatalf("disc_count without songs: %v", got)
	}
}

func TestAlbumRefusesBadParameters(t *testing.T) {
	for _, path := range []string{
		"/api/v1/albums/abc",
		"/api/v1/albums/1%2F2",
		"/api/v1/albums/1?storefront=x",
	} {
		catalog := &fakeCatalog{t: t}
		assertErrorCode(t, requestJSON(t, newLookupServer(catalog, nil), http.MethodGet, path, ""), http.StatusBadRequest, CodeInvalidRequest)
		if len(catalog.calls) != 0 {
			t.Fatalf("%s read the catalog: %v", path, catalog.calls)
		}
	}
}

func TestAlbumCatalogFailures(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
		code   ErrorCode
	}{
		{fmt.Errorf("get album: %w", notFoundError{}), http.StatusNotFound, CodeCatalogNotFound},
		{errors.New("catalog request failed: 500 Internal Server Error"), http.StatusBadGateway, CodeUpstreamError},
		{fmt.Errorf("get album: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, CodeUpstreamTimeout},
	} {
		catalog := &fakeCatalog{t: t, err: tt.err}
		assertErrorCode(t, requestJSON(t, newLookupServer(catalog, nil), http.MethodGet, "/api/v1/albums/1", ""), tt.status, tt.code)
	}
}
