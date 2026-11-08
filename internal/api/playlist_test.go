package api

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"amdl/internal/applemusic"
)

// samplePlaylist is a playlist as applemusic.Playlist maps Apple's answer:
// the curator as Artist, songs in the order the playlist lists them.
func samplePlaylist() *applemusic.Collection {
	return &applemusic.Collection{
		ID: "pl.f4d106fed2bd41149aaacabb233eb5eb", Type: applemusic.TypePlaylist, Name: "Today's Hits", Artist: "Apple Music",
		ArtworkURL: "https://is1.mzstatic.com/p/{w}x{h}bb.jpg", ArtworkColors: palette,
		URL: "https://music.apple.com/cn/playlist/todays-hits/pl.f4d106fed2bd41149aaacabb233eb5eb?l=en-US",
		Tracks: []applemusic.Song{
			{ID: "30", Name: "Later Song", ArtistName: "B", DiscNumber: 2, TrackNumber: 7, DurationInMillis: 181000,
				URL: "https://music.apple.com/cn/album/later-song/99?i=30"},
			{ID: "20", Name: "Earlier Song", ArtistName: "A", DiscNumber: 1, TrackNumber: 1, ContentRating: "explicit"},
		},
	}
}

// A playlist answers in the album's shape, its songs in the playlist's own
// order (not by their albums' disc and track), numbered from 1 on disc 1.
func TestPlaylistReturnsItsSongsInItsOwnOrder(t *testing.T) {
	catalog := &fakeCatalog{t: t, playlist: samplePlaylist()}
	recorder := requestJSON(t, newLookupServer(t, catalog, nil), http.MethodGet, "/api/v1/playlists/pl.f4d106fed2bd41149aaacabb233eb5eb?storefront=CN", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	if want := []string{"playlist cn/pl.f4d106fed2bd41149aaacabb233eb5eb"}; !reflect.DeepEqual(catalog.calls, want) {
		t.Fatalf("catalog calls = %v, want %v", catalog.calls, want)
	}
	got := decodeBody(t, recorder.Body.Bytes())
	tracks, _ := got["tracks"].([]any)
	delete(got, "tracks")
	want := map[string]any{
		"storefront": "cn", "id": "pl.f4d106fed2bd41149aaacabb233eb5eb",
		"url":   "https://music.apple.com/cn/playlist/todays-hits/pl.f4d106fed2bd41149aaacabb233eb5eb",
		"title": "Today's Hits", "artist_name": "Apple Music",
		"artwork_url": "https://is1.mzstatic.com/p/{w}x{h}bb.jpg", "artwork_bg_color": "101010",
		"artwork_text_color1": "f1f1f1", "artwork_text_color2": "e2e2e2", "artwork_text_color3": "d3d3d3", "artwork_text_color4": "c4c4c4",
		"track_count": float64(2), "disc_count": float64(1),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("playlist = %v\nwant %v", got, want)
	}
	var order []string
	for _, tr := range tracks {
		m := tr.(map[string]any)
		order = append(order, fmt.Sprintf("%v.%v=%v", m["disc_number"], m["track_number"], m["id"]))
	}
	if want := []string{"1.1=30", "1.2=20"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("track order = %v, want %v", order, want)
	}
}
