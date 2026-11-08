package applemusic

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// recordedCatalog answers every request with the recorded amp-api response in
// testdata/<file> (captured live, trimmed of meta, previews and editorial
// notes), or Apple's 404 body when file is empty, and keeps the last request
// URL.
func recordedCatalog(t *testing.T, file string, status int) (*CatalogClient, *url.URL) {
	t.Helper()
	body := `{"errors":[{"status":"404","title":"Resource Not Found"}]}`
	if file != "" {
		body = fixture(t, file)
	}
	last := &url.URL{}
	client := newTestClient(t, zhCN, func(req *http.Request) *http.Response {
		*last = *req.URL
		return jsonResponse(status, body)
	})
	return client, last
}

func TestSearchMapsTheRecordedResponse(t *testing.T) {
	client, req := recordedCatalog(t, "search-us.json", http.StatusOK)
	res, err := client.Search(context.Background(), "us", "Taylor Swift", SearchTypes, 2, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if req.Path != "/v1/catalog/us/search" {
		t.Fatalf("path = %q", req.Path)
	}
	q := req.Query()
	if q.Get("term") != "Taylor Swift" || q.Get("types") != "songs,albums,playlists,artists" || q.Get("limit") != "2" || q.Get("l") != "zh-Hans_CN" || q.Has("offset") {
		t.Fatalf("query = %v", q)
	}
	// A song hit's album places it in the library; it comes in the same request.
	if q.Get("include[songs]") != "albums" {
		t.Fatalf("include[songs] = %q, want albums", q.Get("include[songs]"))
	}

	if got := []string{res.Songs[0].ID, res.Songs[1].ID}; !reflect.DeepEqual(got, []string{"6814997425", "6775527443"}) || len(res.Songs) != 2 {
		t.Fatalf("song ids = %v", got)
	}
	song := res.Songs[0]
	if song.Name != "Patient Zero" || song.ArtistName != "Taylor Swift" || song.AlbumName != "The Life of a Showgirl: The Encore" || song.DurationInMillis == 0 {
		t.Fatalf("song = %+v", song)
	}
	if want := []string{"atmos", "lossless", "lossy-stereo", "spatial"}; !reflect.DeepEqual(song.AudioTraits, want) {
		t.Fatalf("song audio traits = %v, want %v", song.AudioTraits, want)
	}
	if song.URL != "https://music.apple.com/us/album/patient-zero/6814997249?i=6814997425&l=zh-Hans-CN" {
		t.Fatalf("song url = %q", song.URL)
	}
	if !strings.Contains(song.ArtworkURL, "{w}x{h}") || song.ArtworkColors.BgColor == "" {
		t.Fatalf("song artwork = %q %+v", song.ArtworkURL, song.ArtworkColors)
	}

	album := res.Albums[0]
	if album.ID != "6814997249" || album.Name != "The Life of a Showgirl: The Encore" || album.Artist != "Taylor Swift" || album.Type != TypeAlbum {
		t.Fatalf("album = %+v", album)
	}
	if album.ContentRating != "explicit" || album.TrackCount == 0 || album.ReleaseDate == "" || !reflect.DeepEqual(album.AudioTraits, []string{"lossless", "lossy-stereo"}) {
		t.Fatalf("album facts = rating %q, tracks %d, release %q, traits %v", album.ContentRating, album.TrackCount, album.ReleaseDate, album.AudioTraits)
	}
	if album.URL != "https://music.apple.com/us/album/the-life-of-a-showgirl-the-encore/6814997249?l=zh-Hans-CN" {
		t.Fatalf("album url = %q", album.URL)
	}
	if len(res.Albums) != 2 || res.Albums[1].ID != "1742057774" {
		t.Fatalf("albums = %+v", res.Albums)
	}

	if len(res.Artists) != 1 {
		t.Fatalf("artists = %+v", res.Artists)
	}
	artist := res.Artists[0]
	// The catalog answers in the configured language (l=zh-Hans_CN).
	if artist.ID != "159260351" || artist.Name != "Taylor Swift" || PrimaryGenre(artist.GenreNames) != "流行乐" || artist.URL != "https://music.apple.com/us/artist/taylor-swift/159260351?l=zh-Hans-CN" {
		t.Fatalf("artist = %+v", artist)
	}

	// Songs and albums have a next page; the one artist is all there is.
	if want := map[SearchType]int{SearchSongs: 2, SearchAlbums: 2}; !reflect.DeepEqual(res.NextOffset, want) {
		t.Fatalf("next offsets = %v, want %v", res.NextOffset, want)
	}
}

// Playlists come from testdata/search-playlists-us.json, captured live: one a
// listener's playlist (playlistType external) and one Apple's own (editorial).
func TestSearchMapsPlaylists(t *testing.T) {
	client, req := recordedCatalog(t, "search-playlists-us.json", http.StatusOK)
	res, err := client.Search(context.Background(), "us", "lofi beats to study", []SearchType{SearchPlaylists}, 2, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if q := req.Query(); q.Get("types") != "playlists" || q.Get("limit") != "2" || q.Has("include[songs]") {
		t.Fatalf("query = %v", q)
	}
	if len(res.Playlists) != 2 || len(res.Songs)+len(res.Albums)+len(res.Artists) != 0 {
		t.Fatalf("results = %+v", res)
	}
	curated := res.Playlists[0]
	if curated.ID != "pl.2553030a768547e9ab3e91230de8439f" || curated.Type != TypePlaylist || curated.Artist != "BeatTape" || curated.Name == "" {
		t.Fatalf("curated playlist = %+v", curated)
	}
	if curated.URL != "https://music.apple.com/us/playlist/lo-fi-beats-to-study-work-sleep-and-relax-to/pl.2553030a768547e9ab3e91230de8439f?l=zh-Hans-CN" {
		t.Fatalf("curated url = %q", curated.URL)
	}
	if !strings.Contains(curated.ArtworkURL, "{w}x{h}") || curated.TrackCount != 0 || len(curated.Tracks) != 0 {
		t.Fatalf("curated artwork %q, track count %d, tracks %d", curated.ArtworkURL, curated.TrackCount, len(curated.Tracks))
	}
	if editorial := res.Playlists[1]; editorial.ID != "pl.e090651aa73242cb9ed6eedef081fcfe" || editorial.Artist != "Apple Music 嘻哈/说唱" {
		t.Fatalf("editorial playlist = %+v", editorial)
	}
	if want := map[SearchType]int{SearchPlaylists: 2}; !reflect.DeepEqual(res.NextOffset, want) {
		t.Fatalf("next offsets = %v, want %v", res.NextOffset, want)
	}
}

func TestSearchReadsNonLatinTermsAndStorefronts(t *testing.T) {
	client, req := recordedCatalog(t, "search-jp.json", http.StatusOK)
	res, err := client.Search(context.Background(), "jp", "星街すいせい", []SearchType{SearchSongs, SearchArtists}, 2, 4, "")
	if err != nil {
		t.Fatal(err)
	}
	q := req.Query()
	if req.Path != "/v1/catalog/jp/search" || q.Get("term") != "星街すいせい" || q.Get("types") != "songs,artists" || q.Get("offset") != "4" {
		t.Fatalf("request = %s %v", req.Path, q)
	}
	if len(res.Songs) != 2 || res.Songs[0].Name != "ビビデバ" || res.Artists[0].Name != "星街すいせい" {
		t.Fatalf("results = %+v", res)
	}
}

// Apple leaves a group without hits out of results; Search returns it empty.
func TestSearchWithoutHits(t *testing.T) {
	client := newTestClient(t, zhCN, func(*http.Request) *http.Response {
		return jsonResponse(http.StatusOK, `{"results":{},"meta":{"results":{"order":[],"rawOrder":[]}}}`)
	})
	res, err := client.Search(context.Background(), "us", "zzqxjvkwplmnbvc", SearchTypes, 10, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Songs)+len(res.Albums)+len(res.Playlists)+len(res.Artists) != 0 || len(res.NextOffset) != 0 {
		t.Fatalf("results = %+v", res)
	}
}

func TestSearchReturnsTheCatalogError(t *testing.T) {
	client, _ := recordedCatalog(t, "", http.StatusBadRequest)
	_, err := client.Search(context.Background(), "zz", "a", SearchTypes, 10, 0, "")
	if err == nil || err.Error() != "Apple Music catalog search request in storefront zz failed: 400 Bad Request" {
		t.Fatalf("err = %v", err)
	}
}

func TestArtistAlbumsPageReadsOneRelationshipPage(t *testing.T) {
	client, req := recordedCatalog(t, "artist-albums-us.json", http.StatusOK)
	page, err := client.ArtistAlbumsPage(context.Background(), "us", "159260351", 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	q := req.Query()
	if req.Path != "/v1/catalog/us/artists/159260351/albums" || q.Get("limit") != "3" || q.Get("offset") != "3" {
		t.Fatalf("request = %s %v", req.Path, q)
	}
	if got, want := albumIDs(page.Albums), []string{"1650841512", "1833328839", "1528112358"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("album ids = %v, want %v", got, want)
	}
	first := page.Albums[0]
	if first.Name != "Midnights (3am Edition)" || first.TrackCount != 20 || first.ContentRating != "explicit" || first.URL == "" || len(first.AudioTraits) == 0 {
		t.Fatalf("first album = %+v", first)
	}
	if page.NextOffset != 6 {
		t.Fatalf("next offset = %d, want 6", page.NextOffset)
	}
}

func TestArtistAlbumsPageNamesAMissingArtist(t *testing.T) {
	client, _ := recordedCatalog(t, "", http.StatusNotFound)
	_, err := client.ArtistAlbumsPage(context.Background(), "us", "1", 25, 0)
	if err == nil || err.Error() != "Apple Music has no artist 1 in storefront us (404)" {
		t.Fatalf("err = %v", err)
	}
}

func TestArtistAlbumsPagePastTheEndIsEmpty(t *testing.T) {
	client, _ := recordedCatalog(t, "", http.StatusNotFound)
	page, err := client.ArtistAlbumsPage(context.Background(), "us", "159260351", 100, 400)
	if err != nil || len(page.Albums) != 0 || page.NextOffset != 0 {
		t.Fatalf("page = %+v, err = %v; want an empty last page", page, err)
	}
}

func TestNextOffset(t *testing.T) {
	for _, c := range []struct {
		next     string
		fallback int
		want     int
	}{
		{"", 25, 0},
		{"/v1/catalog/us/search?offset=50&term=a&types=songs", 25, 50},
		{"/v1/catalog/us/artists/1/albums", 25, 25},
	} {
		if got := nextOffset(c.next, c.fallback); got != c.want {
			t.Errorf("nextOffset(%q, %d) = %d, want %d", c.next, c.fallback, got, c.want)
		}
	}
}

// inLibrarySearchBody is a search answer with one song marked in the library,
// one album marked not in it and one playlist and one artist without a mark.
const inLibrarySearchBody = `{"results":{
	"songs":{"data":[{"id":"s1","type":"songs","attributes":{"name":"In","inLibrary":true}},
		{"id":"s2","type":"songs","attributes":{"name":"Out","inLibrary":false}},
		{"id":"s3","type":"songs","attributes":{"name":"Unmarked"}}]},
	"albums":{"data":[{"id":"a1","type":"albums","attributes":{"name":"Out","inLibrary":false}}]},
	"playlists":{"data":[{"id":"pl.1","type":"playlists","attributes":{"name":"No mark"}}]},
	"artists":{"data":[{"id":"r1","type":"artists","attributes":{"name":"Artist"}}]}}}`

func TestSearchReadsTheInLibraryMark(t *testing.T) {
	var last *http.Request
	client, _, _ := playbackClient(t, func(req *http.Request) *http.Response {
		last = req
		return jsonResponse(http.StatusOK, inLibrarySearchBody)
	})
	res, err := client.Search(context.Background(), "us", "x", SearchTypes, 5, 0, testUserToken)
	if err != nil {
		t.Fatal(err)
	}
	q := last.URL.Query()
	if q.Get("extend") != "inLibrary" || q.Get("term") != "x" || last.Header.Get("Media-User-Token") != testUserToken {
		t.Fatalf("request = %s, token header set: %v", last.URL, last.Header.Get("Media-User-Token") != "")
	}
	want := map[SearchType]map[string]bool{
		SearchSongs:  {"s1": true, "s2": false},
		SearchAlbums: {"a1": false},
	}
	if !reflect.DeepEqual(res.InLibrary, want) {
		t.Fatalf("InLibrary = %v, want %v (an absent flag is no mark, false is a mark)", res.InLibrary, want)
	}
	if len(res.Songs) != 3 || len(res.Albums) != 1 || len(res.Playlists) != 1 || len(res.Artists) != 1 {
		t.Fatalf("hits = %d songs, %d albums, %d playlists, %d artists", len(res.Songs), len(res.Albums), len(res.Playlists), len(res.Artists))
	}
}

func TestSearchWithoutATokenAsksForNoMark(t *testing.T) {
	var last *http.Request
	client, _, _ := playbackClient(t, func(req *http.Request) *http.Response {
		last = req
		return jsonResponse(http.StatusOK, inLibrarySearchBody)
	})
	res, err := client.Search(context.Background(), "us", "x", SearchTypes, 5, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if last.URL.Query().Has("extend") || last.Header.Get("Media-User-Token") != "" {
		t.Fatalf("a tokenless search sent extend or a token: %s", last.URL)
	}
	// The fake answers with the flags regardless; the real Apple leaves them
	// out. What matters is that the request did not ask for them.
	if len(res.Songs) != 3 {
		t.Fatalf("songs = %d", len(res.Songs))
	}
}

// Apple answers 401 (twice, after the web JWT was refreshed) or 403 when it
// refuses the media-user-token. The search is asked again without the token
// and still returns hits, only without marks.
func TestSearchRetriesWithoutARefusedToken(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client, reqs, _ := playbackClient(t, func(req *http.Request) *http.Response {
				if req.Header.Get("Media-User-Token") != "" {
					return jsonResponse(status, `{}`)
				}
				return jsonResponse(http.StatusOK, `{"results":{"songs":{"data":[{"id":"s1","type":"songs","attributes":{"name":"Hit"}}]}}}`)
			})
			res, err := client.Search(context.Background(), "us", "x", []SearchType{SearchSongs}, 5, 0, testUserToken)
			if err != nil {
				t.Fatalf("search failed because of the token: %v", err)
			}
			if len(res.Songs) != 1 || len(res.InLibrary) != 0 {
				t.Fatalf("songs = %d, marks = %v", len(res.Songs), res.InLibrary)
			}
			last := (*reqs)[len(*reqs)-1]
			if last.Header.Get("Media-User-Token") != "" || last.URL.Query().Has("extend") {
				t.Fatalf("the retry still carried the token or extend: %s", last.URL)
			}
		})
	}
}

// A failure that is not the token's stays a failure: no silent retry hides it.
func TestSearchDoesNotRetryOtherErrors(t *testing.T) {
	client, reqs, _ := playbackClient(t, func(*http.Request) *http.Response {
		return jsonResponse(http.StatusBadRequest, `{}`)
	})
	if _, err := client.Search(context.Background(), "us", "x", []SearchType{SearchSongs}, 5, 0, testUserToken); err == nil {
		t.Fatal("a 400 search succeeded")
	}
	if len(*reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(*reqs))
	}
}
