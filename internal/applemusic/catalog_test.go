package applemusic

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"amdl/internal/config"
)

var enUS = config.CatalogConfig{Language: "en-US"}

func TestSongRequestsAndMapsExtendedAssetURLs(t *testing.T) {
	var extend string
	client := newTestClient(t, enUS, func(req *http.Request) *http.Response {
		extend = req.URL.Query().Get("extend")
		return jsonResponse(http.StatusOK, `{"data":[{"id":"123","type":"songs","attributes":{"name":"Song","artistName":"Artist","albumName":"Album","extendedAssetUrls":{"enhancedHls":"https://example.test/master.m3u8"}},"relationships":{}}]}`)
	})

	song, err := client.Song(context.Background(), "cn", "123")
	if err != nil {
		t.Fatal(err)
	}
	if extend != "extendedAssetUrls" {
		t.Fatalf("extend query = %q, want extendedAssetUrls", extend)
	}
	if song.EnhancedHLS != "https://example.test/master.m3u8" {
		t.Fatalf("EnhancedHLS = %q", song.EnhancedHLS)
	}
}

func TestSongMetadataDoesNotFollowAlbumRelationship(t *testing.T) {
	var paths []string
	client := newTestClient(t, enUS, func(req *http.Request) *http.Response {
		paths = append(paths, req.URL.Path)
		return jsonResponse(http.StatusOK, `{"data":[{"id":"song-1","type":"songs","attributes":{"name":"Song","artistName":"Artist","albumName":"Album"},"relationships":{"albums":{"data":[{"id":"album-1","type":"albums","attributes":{"name":"Album","artistName":"Album Artist","trackCount":10}}]}}}]}`)
	})

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
	var paths []string
	serve := routes(map[string]string{
		"/v1/catalog/cn/songs/song-1":   fixture(t, "catalog/song-1-in-album-1.json"),
		"/v1/catalog/cn/albums/album-1": fixture(t, "catalog/album-1-two-tracks.json"),
	})
	client := newTestClient(t, enUS, func(req *http.Request) *http.Response {
		paths = append(paths, req.URL.Path)
		return serve(req)
	})

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
	var extend string
	client := newTestClient(t, enUS, func(req *http.Request) *http.Response {
		extend = req.URL.Query().Get("extend")
		// Included artist without attributes.url — only the extended
		// album attribute carries the page.
		return jsonResponse(http.StatusOK, `{"data":[{"id":"album-1","type":"albums","attributes":{"name":"Album","artistName":"Album Artist","artistUrl":"https://music.apple.com/cn/artist/album-artist/artist-1","trackCount":1},"relationships":{"artists":{"data":[{"id":"artist-1","attributes":{"name":"Album Artist"}}]},"tracks":{"data":[{"id":"song-1","type":"songs","attributes":{"name":"Song","trackNumber":1,"discNumber":1}}]}}}]}`)
	})

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
	var paths []string
	serve := routes(map[string]string{
		"/v1/catalog/cn/albums/album-1":        fixture(t, "catalog/album-paged.json"),
		"/v1/catalog/cn/albums/album-1/tracks": fixture(t, "catalog/album-paged-tracks-offset-1.json"),
	})
	client := newTestClient(t, enUS, func(req *http.Request) *http.Response {
		paths = append(paths, req.URL.RequestURI())
		return serve(req)
	})

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

// albumWithTracks is an album answer whose track relationship lists tracks,
// each given as {id, type, trackNumber, discNumber}.
func albumWithTracks(trackCount int, tracks ...[4]any) string {
	list := ""
	for i, track := range tracks {
		if i > 0 {
			list += ","
		}
		list += fmt.Sprintf(`{"id":%q,"type":%q,"attributes":{"name":"T","trackNumber":%d,"discNumber":%d}}`, track[0], track[1], track[2], track[3])
	}
	return fmt.Sprintf(`{"data":[{"id":"album-1","type":"albums","attributes":{"name":"Album","artistName":"Album Artist","trackCount":%d},"relationships":{"tracks":{"data":[%s]}}}]}`, trackCount, list)
}

// TestAlbumTrackCounts pins the "track N of M" denominator.
func TestAlbumTrackCounts(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantTrackCount []int
		wantDiscCount  int
		wantAlbumCount int
	}{
		{
			// Apple's album trackCount is the whole release — a real 29+19
			// two-disc album reports 48 — which would tag every track of both
			// discs as N of 48. The music video is not downloadable and never
			// becomes a track, so it is excluded from disc 1's total as well.
			name: "multi-disc counts tracks per disc",
			body: albumWithTracks(3,
				[4]any{"song-1", "songs", 1, 1}, [4]any{"song-2", "songs", 2, 1},
				[4]any{"video-1", "music-videos", 3, 1}, [4]any{"song-3", "songs", 1, 2}),
			wantTrackCount: []int{2, 2, 1},
			wantDiscCount:  2,
		},
		{
			// [regression] A storefront that withholds songs from the
			// relationship: counting the returned entries would report 2 for a
			// disc whose numbering already reaches 3, so the total comes from
			// Apple's trackNumber. Disc 1 track 2 and disc 2 track 1 are missing.
			name: "withheld tracks infer disc totals from track numbers",
			body: albumWithTracks(5,
				[4]any{"song-1", "songs", 1, 1}, [4]any{"song-3", "songs", 3, 1}, [4]any{"song-5", "songs", 2, 2}),
			wantTrackCount: []int{3, 3, 2},
			wantDiscCount:  2,
		},
		{
			// On a one-disc album Apple's count stays authoritative, because it
			// still reports the full length when a storefront withholds
			// individual tracks; the record's withheld_count is the difference.
			name:           "single disc keeps Apple's track count",
			body:           albumWithTracks(12, [4]any{"song-1", "songs", 1, 1}, [4]any{"song-2", "songs", 2, 1}),
			wantTrackCount: []int{12, 12},
			wantDiscCount:  1,
			wantAlbumCount: 12,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, enUS, func(*http.Request) *http.Response { return jsonResponse(http.StatusOK, tt.body) })
			album, err := client.Album(context.Background(), "cn", "album-1")
			if err != nil {
				t.Fatal(err)
			}
			var got []int
			for _, track := range album.Tracks {
				got = append(got, track.TrackCount)
				if track.DiscCount != tt.wantDiscCount {
					t.Errorf("track %s DiscCount = %d, want %d", track.ID, track.DiscCount, tt.wantDiscCount)
				}
			}
			if !reflect.DeepEqual(got, tt.wantTrackCount) {
				t.Fatalf("TrackCount per track = %v, want %v", got, tt.wantTrackCount)
			}
			if tt.wantAlbumCount != 0 && album.TrackCount != tt.wantAlbumCount {
				t.Fatalf("album TrackCount = %d, want %d", album.TrackCount, tt.wantAlbumCount)
			}
		})
	}
}

// TestAlbumDecodesArtworkColors pins the wire keys of the artwork palette
// (bgColor/textColor1..4) so a mistyped JSON tag cannot silently drop the
// colors from every collection.
func TestAlbumDecodesArtworkColors(t *testing.T) {
	body := fixture(t, "catalog/album-artwork-colors.json")
	client := newTestClient(t, enUS, func(*http.Request) *http.Response { return jsonResponse(http.StatusOK, body) })

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
	var paths []string
	var include string
	serve := routes(map[string]string{
		"/v1/catalog/cn/artists/artist-1":        fixture(t, "catalog/artist-with-albums.json"),
		"/v1/catalog/cn/artists/artist-1/albums": fixture(t, "catalog/artist-albums-offset-2.json"),
	})
	client := newTestClient(t, config.CatalogConfig{Language: "zh-Hans_CN"}, func(req *http.Request) *http.Response {
		paths = append(paths, req.URL.RequestURI())
		if req.URL.Path == "/v1/catalog/cn/artists/artist-1" {
			include = req.URL.Query().Get("include")
		}
		return serve(req)
	})

	artist, err := client.ArtistAlbums(context.Background(), "cn", "artist-1")
	if err != nil {
		t.Fatal(err)
	}
	if include != "albums" {
		t.Fatalf("include query = %q, want albums", include)
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
		hasLyrics, hasTimeSyncedLyrics, want bool
	}{
		{true, true, true},
		{true, false, true},
		{false, true, true},
		{false, false, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("hasLyrics=%v,hasTimeSyncedLyrics=%v", tt.hasLyrics, tt.hasTimeSyncedLyrics), func(t *testing.T) {
			body := fmt.Sprintf(`{"data":[{"id":"123","type":"songs","attributes":{"name":"Song","artistName":"Artist","albumName":"Album","hasLyrics":%v,"hasTimeSyncedLyrics":%v},"relationships":{}}]}`, tt.hasLyrics, tt.hasTimeSyncedLyrics)
			client := newTestClient(t, enUS, func(*http.Request) *http.Response { return jsonResponse(http.StatusOK, body) })

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

func TestAlbumMapsContentRatingAndAppleDigitalMaster(t *testing.T) {
	client := newTestClient(t, enUS, routes(map[string]string{
		"/v1/catalog/us/albums/1": fixture(t, "catalog/album-explicit-adm.json"),
		"/v1/catalog/us/albums/2": fixture(t, "catalog/album-plain.json"),
	}))
	rated, err := client.Album(context.Background(), "us", "1")
	if err != nil {
		t.Fatal(err)
	}
	if rated.ContentRating != "explicit" || !rated.AppleDigitalMaster || !reflect.DeepEqual(rated.AudioTraits, []string{"lossy-stereo"}) {
		t.Fatalf("album 1 rating/ADM/traits = %q/%v/%v, want explicit/true/[lossy-stereo]", rated.ContentRating, rated.AppleDigitalMaster, rated.AudioTraits)
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
	client := newTestClient(t, enUS, routes(map[string]string{
		"/v1/catalog/us/songs/7": `{"data":[{"id":"7","type":"songs","attributes":{"name":"Seven","isAppleDigitalMaster":true,"contentRating":"clean"}}]}`,
	}))
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
	client := newTestClient(t, enUS, routes(map[string]string{
		"/v1/catalog/cn/songs/hires":    `{"data":[{"id":"hires","type":"songs","attributes":{"name":"TV","audioTraits":["atmos","hi-res-lossless","lossless","lossy-stereo","spatial"]}}]}`,
		"/v1/catalog/cn/songs/lossless": `{"data":[{"id":"lossless","type":"songs","attributes":{"name":"Give Life Back to Music","audioTraits":["lossless","lossy-stereo"]}}]}`,
		"/v1/catalog/cn/songs/none":     `{"data":[{"id":"none","type":"songs","attributes":{"name":"No traits"}}]}`,
	}))
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
	client := newTestClient(t, enUS, routes(map[string]string{
		"/v1/catalog/us/artists/9": `{"data":[{"id":"9","type":"artists","attributes":{"name":"Nine","url":"https://music.apple.com/us/artist/nine/9","genreNames":["Hip-Hop/Rap"],"artwork":{"url":"artist-art","bgColor":"101010"}}}]}`,
	}))
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
