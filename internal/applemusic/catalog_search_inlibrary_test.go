package applemusic

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

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
