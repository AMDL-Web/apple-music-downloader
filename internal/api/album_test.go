package api

import (
	"errors"
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
	recorder := requestJSON(t, newLookupServer(t, catalog, nil), http.MethodGet, "/api/v1/albums/1783644603?storefront=CN", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	if want := []string{"album cn/1783644603"}; !reflect.DeepEqual(catalog.calls, want) {
		t.Fatalf("catalog calls = %v, want %v", catalog.calls, want)
	}
	// The golden body pins the metadata, the palette, the tracks sorted by
	// disc then track number, and the built link (without the optional
	// fields) of a track whose catalog entry has no page.
	assertGolden(t, "album", recorder.Body.Bytes())
}

func TestAlbumWithoutSongsAnswersAnEmptyList(t *testing.T) {
	catalog := &fakeCatalog{t: t, album: &applemusic.Collection{ID: "5", Name: "Videos Only"}}
	recorder := requestJSON(t, newLookupServer(t, catalog, func(c *config.Config) { c.Catalog.DefaultStorefront = "jp" }), http.MethodGet, "/api/v1/albums/5", "")
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

// An album carries its animated cover and the cover's own palette, read
// alongside it; a failed read, or the read turned off, leaves them out and
// the album answers as before.
func TestAlbumCarriesItsAnimatedCoverWhenItCanBeRead(t *testing.T) {
	art := applemusic.MotionArtwork{Square: "https://mvod.itunes.apple.com/a/square.m3u8", Tall: "https://mvod.itunes.apple.com/a/tall.m3u8",
		SquareColors: applemusic.ArtworkColors{BgColor: "5c6786", TextColor1: "ffffff", TextColor2: "eeeeee", TextColor3: "dddddd", TextColor4: "cccccc"}}
	catalog := &motionCatalog{fakeCatalog: &fakeCatalog{t: t, album: sampleAlbum()}, art: art}
	recorder := requestJSON(t, newLookupServer(t, catalog, nil), http.MethodGet, "/api/v1/albums/1783644603?storefront=CN", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	got := decodeBody(t, recorder.Body.Bytes())
	for key, want := range map[string]string{
		"motion_artwork_url": art.Square, "motion_artwork_tall_url": art.Tall, "motion_artwork_bg_color": "5c6786",
		"motion_artwork_text_color1": "ffffff", "motion_artwork_text_color4": "cccccc", "artwork_bg_color": "101010",
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %s", key, got[key], want)
		}
	}

	for name, c := range map[string]struct {
		catalog *motionCatalog
		mutate  func(*config.Config)
		asked   int
	}{
		"read fails": {&motionCatalog{fakeCatalog: &fakeCatalog{t: t, album: sampleAlbum()}, err: errors.New("no editorialVideo")}, nil, 1},
		"turned off": {&motionCatalog{fakeCatalog: &fakeCatalog{t: t, album: sampleAlbum()}, art: art}, func(c *config.Config) { c.Catalog.MotionArtworkEnabled = false }, 0},
	} {
		recorder := requestJSON(t, newLookupServer(t, c.catalog, c.mutate), http.MethodGet, "/api/v1/albums/1783644603?storefront=CN", "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body %s", name, recorder.Code, recorder.Body.String())
		}
		got := decodeBody(t, recorder.Body.Bytes())
		if _, ok := got["motion_artwork_url"]; ok || got["title"] != "Two Discs" {
			t.Errorf("%s: album = %v", name, got)
		}
		if c.catalog.asked != c.asked {
			t.Errorf("%s: the animated cover was asked for %d times, want %d", name, c.catalog.asked, c.asked)
		}
	}
}
