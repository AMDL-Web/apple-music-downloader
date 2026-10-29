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

// fixtureCatalogClient answers each request path from bodies; any other path
// is a 404, so a test fails loudly when the client asks for something else.
func fixtureCatalogClient(t *testing.T, bodies map[string]string) *CatalogClient {
	t.Helper()
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.webToken.value = "test-token"
	client.webToken.until = time.Now().Add(time.Hour)
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, ok := bodies[req.URL.Path]
		status := http.StatusOK
		if !ok {
			status, body = http.StatusNotFound, "unexpected path: "+req.URL.RequestURI()
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	return client
}

func TestAlbumMapsContentRatingAndAppleDigitalMaster(t *testing.T) {
	client := fixtureCatalogClient(t, map[string]string{
		"/v1/catalog/us/albums/1": `{"data":[{"id":"1","type":"albums","attributes":{"name":"A","artistName":"X","contentRating":"explicit","isMasteredForItunes":true,"artwork":{"url":"art"}},"relationships":{"tracks":{"data":[{"id":"s1","type":"songs","attributes":{"name":"One"}}]}}}]}`,
		"/v1/catalog/us/albums/2": `{"data":[{"id":"2","type":"albums","attributes":{"name":"B","artistName":"Y","artwork":{"url":"art"}},"relationships":{"tracks":{"data":[{"id":"s2","type":"songs","attributes":{"name":"Two"}}]}}}]}`,
	})
	rated, err := client.Album(context.Background(), "us", "1")
	if err != nil {
		t.Fatal(err)
	}
	if rated.ContentRating != "explicit" || !rated.AppleDigitalMaster {
		t.Fatalf("album 1 rating/ADM = %q/%v, want explicit/true", rated.ContentRating, rated.AppleDigitalMaster)
	}
	plain, err := client.Album(context.Background(), "us", "2")
	if err != nil {
		t.Fatal(err)
	}
	if plain.ContentRating != "" || plain.AppleDigitalMaster {
		t.Fatalf("album 2 rating/ADM = %q/%v, want empty/false", plain.ContentRating, plain.AppleDigitalMaster)
	}
}

func TestSongMetadataMapsAppleDigitalMaster(t *testing.T) {
	client := fixtureCatalogClient(t, map[string]string{
		"/v1/catalog/us/songs/7": `{"data":[{"id":"7","type":"songs","attributes":{"name":"Seven","isAppleDigitalMaster":true,"contentRating":"clean"}}]}`,
	})
	song, err := client.SongMetadata(context.Background(), "us", "7")
	if err != nil {
		t.Fatal(err)
	}
	if !song.AppleDigitalMaster || song.ContentRating != "clean" {
		t.Fatalf("song ADM/rating = %v/%q, want true/clean", song.AppleDigitalMaster, song.ContentRating)
	}
}

// The trait lists are amp-api's own, as returned for songs 1635388718 and
// 617154353 in the cn storefront.
func TestSongMetadataMapsHiResLosslessTrait(t *testing.T) {
	client := fixtureCatalogClient(t, map[string]string{
		"/v1/catalog/cn/songs/hires":    `{"data":[{"id":"hires","type":"songs","attributes":{"name":"TV","audioTraits":["atmos","hi-res-lossless","lossless","lossy-stereo","spatial"]}}]}`,
		"/v1/catalog/cn/songs/lossless": `{"data":[{"id":"lossless","type":"songs","attributes":{"name":"Give Life Back to Music","audioTraits":["lossless","lossy-stereo"]}}]}`,
		"/v1/catalog/cn/songs/none":     `{"data":[{"id":"none","type":"songs","attributes":{"name":"No traits"}}]}`,
	})
	for id, want := range map[string]bool{"hires": true, "lossless": false, "none": false} {
		song, err := client.SongMetadata(context.Background(), "cn", id)
		if err != nil {
			t.Fatal(err)
		}
		if song.HiResLossless != want {
			t.Errorf("song %s HiResLossless = %v, want %v", id, song.HiResLossless, want)
		}
	}
}

func TestArtistMapsPageAndGenres(t *testing.T) {
	client := fixtureCatalogClient(t, map[string]string{
		"/v1/catalog/us/artists/9": `{"data":[{"id":"9","type":"artists","attributes":{"name":"Nine","url":"https://music.apple.com/us/artist/nine/9","genreNames":["Hip-Hop/Rap"],"artwork":{"url":"artist-art","bgColor":"101010"}}}]}`,
	})
	artist, err := client.Artist(context.Background(), "us", "9")
	if err != nil {
		t.Fatal(err)
	}
	want := Artist{
		ID: "9", Name: "Nine", URL: "https://music.apple.com/us/artist/nine/9", GenreNames: []string{"Hip-Hop/Rap"},
		ArtworkURL: "artist-art", ArtworkColors: ArtworkColors{BgColor: "101010"},
	}
	if !reflect.DeepEqual(artist, want) {
		t.Fatalf("artist = %+v, want %+v", artist, want)
	}
}

func TestStationDownloadableOnlyForTrackFormat(t *testing.T) {
	for format, want := range map[string]bool{"tracks": true, "stream": false, "": false} {
		if got := (StationInfo{Format: format}).Downloadable(); got != want {
			t.Errorf("Downloadable(format %q) = %v, want %v", format, got, want)
		}
	}
}
