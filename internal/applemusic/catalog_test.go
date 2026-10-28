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

func TestSongRequestsAndMapsExtendedAssetURLs(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Query().Get("extend"); got != "extendedAssetUrls" {
			t.Fatalf("extend query = %q, want extendedAssetUrls", got)
		}
		body := `{"data":[{"id":"123","type":"songs","attributes":{"name":"Song","artistName":"Artist","albumName":"Album","extendedAssetUrls":{"enhancedHls":"https://example.test/master.m3u8"}},"relationships":{}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	song, err := client.Song(context.Background(), "cn", "123")
	if err != nil {
		t.Fatal(err)
	}
	if song.EnhancedHLS != "https://example.test/master.m3u8" {
		t.Fatalf("EnhancedHLS = %q", song.EnhancedHLS)
	}
}

func TestSongMetadataDoesNotFollowAlbumRelationship(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	var paths []string
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		body := `{"data":[{"id":"song-1","type":"songs","attributes":{"name":"Song","artistName":"Artist","albumName":"Album"},"relationships":{"albums":{"data":[{"id":"album-1","type":"albums","attributes":{"name":"Album","artistName":"Album Artist","trackCount":10}}]}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	song, err := client.SongMetadata(context.Background(), "cn", "song-1")
	if err != nil {
		t.Fatal(err)
	}
	if song.AlbumID != "album-1" || song.TrackCount != 10 {
		t.Fatalf("song metadata = %+v, want mapped album relationship", song)
	}
	if got, want := paths, []string{"/v1/catalog/cn/songs/song-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("request paths = %#v, want %#v", got, want)
	}
}

func TestSongStillFollowsAlbumRelationshipForFullMetadata(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	var paths []string
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		switch req.URL.Path {
		case "/v1/catalog/cn/songs/song-1":
			body := `{"data":[{"id":"song-1","type":"songs","attributes":{"name":"Song","artistName":"Artist","albumName":"Album","trackNumber":1,"discNumber":1},"relationships":{"albums":{"data":[{"id":"album-1","type":"albums","attributes":{"name":"Album","artistName":"Album Artist","trackCount":2}}]}}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		case "/v1/catalog/cn/albums/album-1":
			body := `{"data":[{"id":"album-1","type":"albums","attributes":{"name":"Album","artistName":"Album Artist","trackCount":2,"artwork":{"url":"album-art"}},"relationships":{"artists":{"data":[{"id":"artist-1","attributes":{"name":"Album Artist","url":"https://music.apple.com/cn/artist/album-artist/artist-1","artwork":{"url":"artist-art"}}}]},"tracks":{"data":[{"id":"song-1","type":"songs","attributes":{"name":"Song","trackNumber":1,"discNumber":1}},{"id":"song-2","type":"songs","attributes":{"name":"Two","trackNumber":2,"discNumber":1}}]}}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("unexpected path")), Header: make(http.Header)}, nil
		}
	})}

	song, err := client.Song(context.Background(), "cn", "song-1")
	if err != nil {
		t.Fatal(err)
	}
	if song.AlbumArtist != "Album Artist" || song.AlbumArtworkURL != "album-art" || song.AlbumArtistID != "artist-1" || song.AlbumArtistArtworkURL != "artist-art" || song.TrackCount != 2 || song.DiscCount != 1 {
		t.Fatalf("enriched song = %+v", song)
	}
	// The song resource carried no artist relationship of its own, so the
	// artist page has to come from the album it was enriched with.
	if song.ArtistURL != "https://music.apple.com/cn/artist/album-artist/artist-1" {
		t.Fatalf("enriched song artist url = %q", song.ArtistURL)
	}
	if got, want := paths, []string{"/v1/catalog/cn/songs/song-1", "/v1/catalog/cn/albums/album-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("request paths = %#v, want %#v", got, want)
	}
}

func TestAlbumFallsBackToTheExtendedArtistURL(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	var extend string
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		extend = req.URL.Query().Get("extend")
		// Included artist without attributes.url — only the extended
		// album attribute carries the page.
		body := `{"data":[{"id":"album-1","type":"albums","attributes":{"name":"Album","artistName":"Album Artist","artistUrl":"https://music.apple.com/cn/artist/album-artist/artist-1","trackCount":1},"relationships":{"artists":{"data":[{"id":"artist-1","attributes":{"name":"Album Artist"}}]},"tracks":{"data":[{"id":"song-1","type":"songs","attributes":{"name":"Song","trackNumber":1,"discNumber":1}}]}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	album, err := client.Album(context.Background(), "cn", "album-1")
	if err != nil {
		t.Fatal(err)
	}
	if extend != "artistUrl" {
		t.Fatalf("extend = %q, want artistUrl", extend)
	}
	if album.ArtistURL != "https://music.apple.com/cn/artist/album-artist/artist-1" {
		t.Fatalf("album artist url = %q", album.ArtistURL)
	}
}

func TestAlbumFetchesAllTrackPages(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	var paths []string
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.RequestURI())
		if req.URL.Path == "/v1/catalog/cn/albums/album-1" {
			body := `{"data":[{"id":"album-1","type":"albums","attributes":{"name":"Album","artistName":"Album Artist","trackCount":3},"relationships":{"artists":{"data":[{"id":"artist-1","attributes":{"name":"Album Artist","artwork":{"url":"artist-art"}}}]},"tracks":{"data":[{"id":"song-1","type":"songs","attributes":{"name":"One","artistName":"Artist","albumName":"Album","trackNumber":1,"discNumber":1}},{"id":"video-1","type":"music-videos","attributes":{"name":"Video"}}],"next":"/v1/catalog/cn/albums/album-1/tracks?offset=1"}}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		if req.URL.Path == "/v1/catalog/cn/albums/album-1/tracks" {
			body := `{"data":[{"id":"song-2","type":"songs","attributes":{"name":"Two","artistName":"Artist","albumName":"Album","trackNumber":2,"discNumber":1}},{"id":"video-2","type":"music-videos","attributes":{"name":"Video Two"}},{"id":"song-3","type":"songs","attributes":{"name":"Three","artistName":"Artist","albumName":"Album","trackNumber":3,"discNumber":2}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("unexpected path: " + req.URL.RequestURI())), Header: make(http.Header)}, nil
	})}

	album, err := client.Album(context.Background(), "cn", "album-1")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := trackIDs(album.Tracks), []string{"song-1", "song-2", "song-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("track IDs = %#v, want %#v", got, want)
	}
	if got, want := album.Tracks[2].DiscCount, 2; got != want {
		t.Fatalf("last track DiscCount = %d, want %d", got, want)
	}
	if len(paths) != 2 || paths[1] != "/v1/catalog/cn/albums/album-1/tracks?offset=1" {
		t.Fatalf("request paths = %#v", paths)
	}
}

// TestAlbumCountsTracksPerDiscOnMultiDiscReleases pins the "track N of M"
// denominator to the track's own disc. Apple's album trackCount is the whole
// release — a real 29+19 two-disc album reports 48 — which would tag every
// track of both discs as N of 48.
func TestAlbumCountsTracksPerDiscOnMultiDiscReleases(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	client.http = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		body := `{"data":[{"id":"album-1","type":"albums","attributes":{"name":"Album","artistName":"Album Artist","trackCount":3},"relationships":{"tracks":{"data":[` +
			`{"id":"song-1","type":"songs","attributes":{"name":"One","trackNumber":1,"discNumber":1}},` +
			`{"id":"song-2","type":"songs","attributes":{"name":"Two","trackNumber":2,"discNumber":1}},` +
			`{"id":"video-1","type":"music-videos","attributes":{"name":"Video","trackNumber":3,"discNumber":1}},` +
			`{"id":"song-3","type":"songs","attributes":{"name":"Three","trackNumber":1,"discNumber":2}}]}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	album, err := client.Album(context.Background(), "cn", "album-1")
	if err != nil {
		t.Fatal(err)
	}
	// The music video is not downloadable and never becomes a track, so it is
	// excluded from disc 1's total as well.
	want := []int{2, 2, 1}
	for i, track := range album.Tracks {
		if track.TrackCount != want[i] {
			t.Fatalf("track %s TrackCount = %d, want %d", track.ID, track.TrackCount, want[i])
		}
		if track.DiscCount != 2 {
			t.Fatalf("track %s DiscCount = %d, want 2", track.ID, track.DiscCount)
		}
	}
}

// TestAlbumInfersDiscTotalsFromTrackNumbersWhenTracksAreWithheld covers the
// storefront that omits a song from the relationship. Counting the returned
// entries would report 2 for a disc whose numbering already reaches 3, so the
// total comes from Apple's trackNumber instead.
func TestAlbumInfersDiscTotalsFromTrackNumbersWhenTracksAreWithheld(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	client.http = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		// Disc 1 track 2 and disc 2 track 1 are both missing from the payload.
		body := `{"data":[{"id":"album-1","type":"albums","attributes":{"name":"Album","artistName":"Album Artist","trackCount":5},"relationships":{"tracks":{"data":[` +
			`{"id":"song-1","type":"songs","attributes":{"name":"One","trackNumber":1,"discNumber":1}},` +
			`{"id":"song-3","type":"songs","attributes":{"name":"Three","trackNumber":3,"discNumber":1}},` +
			`{"id":"song-5","type":"songs","attributes":{"name":"Five","trackNumber":2,"discNumber":2}}]}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	album, err := client.Album(context.Background(), "cn", "album-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{3, 3, 2}
	for i, track := range album.Tracks {
		if track.TrackCount != want[i] {
			t.Fatalf("track %s TrackCount = %d, want %d", track.ID, track.TrackCount, want[i])
		}
	}
}

// TestAlbumKeepsAppleTrackCountOnSingleDiscReleases guards the other side: on a
// one-disc album Apple's count stays authoritative, because it still reports
// the full length when a storefront withholds individual tracks.
func TestAlbumKeepsAppleTrackCountOnSingleDiscReleases(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	client.http = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		body := `{"data":[{"id":"album-1","type":"albums","attributes":{"name":"Album","artistName":"Album Artist","trackCount":12},"relationships":{"tracks":{"data":[` +
			`{"id":"song-1","type":"songs","attributes":{"name":"One","trackNumber":1,"discNumber":1}},` +
			`{"id":"song-2","type":"songs","attributes":{"name":"Two","trackNumber":2,"discNumber":1}}]}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	album, err := client.Album(context.Background(), "cn", "album-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, track := range album.Tracks {
		if track.TrackCount != 12 {
			t.Fatalf("track %s TrackCount = %d, want 12", track.ID, track.TrackCount)
		}
	}
}

// TestAlbumDecodesArtworkColors pins the wire keys of the artwork palette
// (bgColor/textColor1..4) so a mistyped JSON tag cannot silently drop the
// colors from every collection.
func TestAlbumDecodesArtworkColors(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"data":[{"id":"album-1","type":"albums","attributes":{"name":"Album","artistName":"Album Artist","artwork":{"url":"album-art","bgColor":"1a1a1a","textColor1":"ffffff","textColor2":"eeeeee","textColor3":"cccccc","textColor4":"aaaaaa"}},"relationships":{"tracks":{"data":[{"id":"song-1","type":"songs","attributes":{"name":"One","artwork":{"url":"song-art","bgColor":"0b0b0b","textColor1":"f1f1f1"}}}]}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	album, err := client.Album(context.Background(), "cn", "album-1")
	if err != nil {
		t.Fatal(err)
	}
	want := ArtworkColors{BgColor: "1a1a1a", TextColor1: "ffffff", TextColor2: "eeeeee", TextColor3: "cccccc", TextColor4: "aaaaaa"}
	if album.ArtworkColors != want {
		t.Fatalf("album artwork colors = %+v, want %+v", album.ArtworkColors, want)
	}
	if len(album.Tracks) != 1 {
		t.Fatalf("tracks = %+v, want one song", album.Tracks)
	}
	trackWant := ArtworkColors{BgColor: "0b0b0b", TextColor1: "f1f1f1"}
	if album.Tracks[0].ArtworkColors != trackWant {
		t.Fatalf("track artwork colors = %+v, want %+v", album.Tracks[0].ArtworkColors, trackWant)
	}
}

func TestArtistAlbumsFetchesIncludedAlbumsAndNextPages(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "zh-Hans_CN"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	var paths []string
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.RequestURI())
		if req.URL.Path == "/v1/catalog/cn/artists/artist-1" {
			if got := req.URL.Query().Get("include"); got != "albums" {
				t.Fatalf("include query = %q, want albums", got)
			}
			body := `{"data":[{"id":"artist-1","type":"artists","attributes":{"name":"Artist","artwork":{"url":"artist-art"}},"relationships":{"albums":{"data":[{"id":"album-1","type":"albums","attributes":{"name":"First","artistName":"Artist","artwork":{"url":"first-art"},"releaseDate":"2024-01-01","trackCount":2}},{"id":"video-1","type":"music-videos","attributes":{"name":"Video"}},{"id":"album-2","type":"albums","attributes":{"name":"Second","artistName":"Artist","artwork":{"url":"second-art"},"releaseDate":"2024-02-01","trackCount":1}}],"next":"/v1/catalog/cn/artists/artist-1/albums?offset=2"}}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		if req.URL.Path == "/v1/catalog/cn/artists/artist-1/albums" {
			body := `{"data":[{"id":"album-2","type":"albums","attributes":{"name":"Second Duplicate","artistName":"Artist"}},{"id":"album-3","type":"albums","attributes":{"name":"Third","artistName":"Artist","artwork":{"url":"third-art"},"releaseDate":"2024-03-01","trackCount":3}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("unexpected path: " + req.URL.RequestURI())), Header: make(http.Header)}, nil
	})}

	artist, err := client.ArtistAlbums(context.Background(), "cn", "artist-1")
	if err != nil {
		t.Fatal(err)
	}
	if artist.ID != "artist-1" || artist.Name != "Artist" || artist.ArtworkURL != "artist-art" {
		t.Fatalf("artist metadata = %+v, want id/name/artwork from artist payload", artist.Artist)
	}
	if got, want := albumIDs(artist.Albums), []string{"album-1", "album-2", "album-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("album IDs = %#v, want %#v", got, want)
	}
	if artist.Albums[2].Name != "Third" || artist.Albums[2].Artist != "Artist" || artist.Albums[2].ArtworkURL != "third-art" {
		t.Fatalf("third album metadata = %+v", artist.Albums[2])
	}
	if len(paths) != 2 || paths[1] != "/v1/catalog/cn/artists/artist-1/albums?offset=2" {
		t.Fatalf("request paths = %#v", paths)
	}
}

func TestSongMapsHasLyrics(t *testing.T) {
	tests := []struct {
		name                string
		hasLyrics           bool
		hasTimeSyncedLyrics bool
		want                bool
	}{
		{
			name:                "both true",
			hasLyrics:           true,
			hasTimeSyncedLyrics: true,
			want:                true,
		},
		{
			name:                "only hasLyrics true",
			hasLyrics:           true,
			hasTimeSyncedLyrics: false,
			want:                true,
		},
		{
			name:                "only hasTimeSyncedLyrics true",
			hasLyrics:           false,
			hasTimeSyncedLyrics: true,
			want:                true,
		},
		{
			name:                "both false",
			hasLyrics:           false,
			hasTimeSyncedLyrics: false,
			want:                false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
			client.token = "test-token"
			client.tokenUntil = time.Now().Add(time.Hour)

			var body string
			if tt.hasLyrics && tt.hasTimeSyncedLyrics {
				body = `{"data":[{"id":"123","type":"songs","attributes":{"name":"Song","artistName":"Artist","albumName":"Album","hasLyrics":true,"hasTimeSyncedLyrics":true},"relationships":{}}]}`
			} else if tt.hasLyrics {
				body = `{"data":[{"id":"123","type":"songs","attributes":{"name":"Song","artistName":"Artist","albumName":"Album","hasLyrics":true,"hasTimeSyncedLyrics":false},"relationships":{}}]}`
			} else if tt.hasTimeSyncedLyrics {
				body = `{"data":[{"id":"123","type":"songs","attributes":{"name":"Song","artistName":"Artist","albumName":"Album","hasLyrics":false,"hasTimeSyncedLyrics":true},"relationships":{}}]}`
			} else {
				body = `{"data":[{"id":"123","type":"songs","attributes":{"name":"Song","artistName":"Artist","albumName":"Album","hasLyrics":false,"hasTimeSyncedLyrics":false},"relationships":{}}]}`
			}

			client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}

			song, err := client.Song(context.Background(), "cn", "123")
			if err != nil {
				t.Fatal(err)
			}
			if song.HasLyrics != tt.want {
				t.Fatalf("HasLyrics = %v, want %v", song.HasLyrics, tt.want)
			}
		})
	}
}
