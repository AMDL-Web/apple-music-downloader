package applemusic

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

// The samples below are small hand-written trims of /v1/me/library responses:
// only the fields mapLibraryItem reads, in the shapes amp-api sends them.
func TestMapLibraryItem(t *testing.T) {
	cases := []struct {
		name   string
		sample string
		want   LibraryItem
	}{
		{
			name: "song with playParams.catalogId",
			sample: `{"id":"i.AbCd12","type":"library-songs","attributes":{
				"name":"Patient Zero","artistName":"Taylor Swift","albumName":"The Encore",
				"durationInMillis":201000,"trackNumber":3,"discNumber":1,
				"genreNames":["Music","Pop"],"releaseDate":"2026-10-03",
				"artwork":{"url":"https://img.example/song/{w}x{h}.jpg"},
				"playParams":{"id":"i.AbCd12","kind":"song","isLibrary":true,"catalogId":"6814997425","reporting":true}}}`,
			want: LibraryItem{
				ID: "i.AbCd12", CatalogID: "6814997425", Name: "Patient Zero", ArtistName: "Taylor Swift",
				AlbumName: "The Encore", ArtworkURL: "https://img.example/song/{w}x{h}.jpg", Genre: "Pop",
				ReleaseDate: "2026-10-03", DurationMS: 201000, TrackNumber: 3, DiscNumber: 1,
			},
		},
		{
			name: "album with the include=catalog relationship",
			sample: `{"id":"l.Xy9","type":"library-albums","attributes":{
				"name":"Showgirl","artistName":"Taylor Swift","trackCount":12,
				"dateAdded":"2026-05-01T10:00:00Z","genreNames":[],"artwork":{"url":""},
				"playParams":{"id":"l.Xy9","kind":"album","isLibrary":true}},
				"relationships":{"catalog":{"data":[{"id":"900","type":"albums","attributes":{
					"url":"https://music.apple.com/cn/album/showgirl/900?l=en-GB",
					"artwork":{"url":"https://img.example/album/{w}x{h}.jpg"},
					"genreNames":["Music","Pop"]}}]}}}`,
			want: LibraryItem{
				ID: "l.Xy9", CatalogID: "900", CatalogURL: "https://music.apple.com/cn/album/showgirl/900?l=en-GB",
				Name: "Showgirl", ArtistName: "Taylor Swift", ArtworkURL: "https://img.example/album/{w}x{h}.jpg",
				Genre: "Pop", TrackCount: 12, DateAdded: "2026-05-01T10:00:00Z",
			},
		},
		{
			name: "artist with the include=catalog relationship",
			sample: `{"id":"r.Qq1","type":"library-artists","attributes":{"name":"Taylor Swift"},
				"relationships":{"catalog":{"data":[{"id":"159260351","type":"artists","attributes":{
					"url":"https://music.apple.com/cn/artist/taylor-swift/159260351"}}]}}}`,
			want: LibraryItem{
				ID: "r.Qq1", CatalogID: "159260351", CatalogURL: "https://music.apple.com/cn/artist/taylor-swift/159260351",
				Name: "Taylor Swift",
			},
		},
		{
			name: "playlist with playParams.globalId",
			sample: `{"id":"p.Zz7","type":"library-playlists","attributes":{
				"name":"Chill","canEdit":false,"canDelete":true,
				"description":{"standard":"Slow ones."},"lastModifiedDate":"2026-09-01T00:00:00Z",
				"dateAdded":"2026-08-01T00:00:00Z",
				"playParams":{"id":"p.Zz7","kind":"playlist","isLibrary":true,"globalId":"pl.0123abcd","versionHash":"v1"}}}`,
			want: LibraryItem{
				ID: "p.Zz7", CatalogID: "pl.0123abcd", Name: "Chill", Description: "Slow ones.",
				LastModified: "2026-09-01T00:00:00Z", DateAdded: "2026-08-01T00:00:00Z", VersionHash: "v1", CanDelete: true,
			},
		},
		{
			name: "own playlist without a catalog counterpart and a plain-string description",
			sample: `{"id":"p.Own","type":"library-playlists","attributes":{
				"name":"Mine","canEdit":true,"canDelete":true,"description":"Plain text",
				"playParams":{"id":"p.Own","kind":"playlist","isLibrary":true}}}`,
			want: LibraryItem{ID: "p.Own", Name: "Mine", Description: "Plain text", CanEdit: true, CanDelete: true},
		},
		{
			name: "uploaded song without a catalog counterpart",
			sample: `{"id":"i.Up1","type":"library-songs","attributes":{"name":"My Upload",
				"playParams":{"id":"i.Up1","kind":"song","isLibrary":true}}}`,
			want: LibraryItem{ID: "i.Up1", Name: "My Upload"},
		},
		{
			name: "album whose catalog relationship is empty",
			sample: `{"id":"l.Gone","type":"library-albums","attributes":{"name":"Withdrawn","artistName":"Someone"},
				"relationships":{"catalog":{"data":[]}}}`,
			want: LibraryItem{ID: "l.Gone", Name: "Withdrawn", ArtistName: "Someone"},
		},
		{
			name: "playParams.catalogId wins over the relationship's id",
			sample: `{"id":"i.Both","type":"library-songs","attributes":{"name":"Both",
				"playParams":{"catalogId":"111"}},
				"relationships":{"catalog":{"data":[{"id":"222","attributes":{"url":"https://music.apple.com/cn/album/x/222"}}]}}}`,
			want: LibraryItem{ID: "i.Both", CatalogID: "111", CatalogURL: "https://music.apple.com/cn/album/x/222", Name: "Both"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw libraryResource
			if err := json.Unmarshal([]byte(tc.sample), &raw); err != nil {
				t.Fatal(err)
			}
			if got := mapLibraryItem(raw); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mapLibraryItem =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestLibraryItemsReadsAPageAndAsksForTheCatalogOfAlbumsAndArtists(t *testing.T) {
	var last *http.Request
	client, _, _ := playbackClient(t, func(req *http.Request) *http.Response {
		last = req
		return jsonResponse(http.StatusOK, `{"next":"/v1/me/library/albums?offset=1","meta":{"total":7},"data":[
			{"id":"l.1","attributes":{"name":"One"},"relationships":{"catalog":{"data":[{"id":"9","attributes":{"url":"https://music.apple.com/cn/album/one/9"}}]}}}]}`)
	})
	page, err := client.LibraryItems(context.Background(), testUserToken, LibraryAlbums, true, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 7 || !page.HasMore || len(page.Items) != 1 || page.Items[0].CatalogID != "9" {
		t.Fatalf("page = %+v", page)
	}
	q := last.URL.Query()
	if last.URL.Path != "/v1/me/library/albums" || q.Get("include") != "catalog" || q.Get("sort") != "-dateAdded" || q.Get("limit") != "1" {
		t.Fatalf("request = %s", last.URL)
	}
	if last.Header.Get("Media-User-Token") != testUserToken {
		t.Fatal("the request carried no media-user-token")
	}

	if _, err := client.LibraryItems(context.Background(), testUserToken, LibrarySongs, false, 200, 500); err != nil {
		t.Fatal(err)
	}
	q = last.URL.Query()
	if q.Has("include") || q.Get("sort") != "name" || q.Get("offset") != "200" || q.Get("limit") != "100" {
		t.Fatalf("songs request = %s", last.URL)
	}
	if _, err := client.LibraryItems(context.Background(), "", LibrarySongs, false, 0, 10); err == nil {
		t.Fatal("a library read without a media-user-token succeeded")
	}
}
