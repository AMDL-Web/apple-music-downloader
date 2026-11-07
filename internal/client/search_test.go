package client

import (
	"context"
	"net/http"
	"testing"
)

func TestSearchSendsTheQueryAndDecodesGroups(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/search" || q.Get("term") != "igor" || q.Get("types") != "albums,artists" ||
			q.Get("storefront") != "us" || q.Get("limit") != "3" || q.Has("offset") {
			t.Errorf("request = %s", r.URL)
		}
		writeJSON(w, http.StatusOK, `{"term":"igor","storefront":"us",
			"albums":{"items":[{"id":"1463409338","type":"album","name":"IGOR","url":"https://music.apple.com/us/album/igor/1463409338",
				"artist_name":"Tyler, The Creator","track_count":12,"audio_traits":["lossless"],
				"resource":{"kind":"album","id":"1","state":"partial","tracks_done":9,"tracks_wanted":12,"withheld_count":1,"wanted_all":true,"paused":false}}],"next_offset":3},
			"artists":{"items":[]}}`)
	}))
	r, err := c.Search(context.Background(), SearchQuery{Term: "igor", Types: []string{"albums", "artists"}, Storefront: "us", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.Songs != nil || r.Albums == nil || r.Albums.NextOffset != 3 || len(r.Albums.Items) != 1 || r.Artists == nil {
		t.Fatalf("result = %+v", r)
	}
	a := r.Albums.Items[0]
	if a.TrackCount != 12 || a.AudioTraits[0] != "lossless" {
		t.Errorf("album hit = %+v", a)
	}
	if r := a.Resource; r == nil || r.State != "partial" || r.TracksDone != 9 || r.TracksWanted != 12 || r.WithheldCount != 1 {
		t.Errorf("album hit resource = %+v", a.Resource)
	}
}

func TestArtistAlbums(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/artists/420368335/albums" || r.URL.Query().Get("offset") != "25" {
			t.Errorf("request = %s", r.URL)
		}
		writeJSON(w, http.StatusOK, `{"storefront":"us","artist":{"id":"420368335","type":"artist","name":"Tyler, The Creator","url":"u"},
			"albums":{"items":[{"id":"1","type":"album","name":"A","url":"u1"}]}}`)
	}))
	r, err := c.ArtistAlbums(context.Background(), "420368335", PageQuery{Offset: 25})
	if err != nil || r.Artist.Name != "Tyler, The Creator" || len(r.Albums.Items) != 1 || r.Albums.NextOffset != 0 {
		t.Errorf("result = %+v, %v", r, err)
	}
}

func TestAlbum(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/albums/1783644603" || r.URL.Query().Get("storefront") != "cn" {
			t.Errorf("request = %s", r.URL)
		}
		writeJSON(w, http.StatusOK, `{"storefront":"cn","id":"1783644603","url":"u","title":"T","track_count":2,"disc_count":2,
			"resource":{"kind":"album","id":"1783644603","state":"partial","tracks_done":1,"tracks_wanted":2,"withheld_count":0,"wanted_all":true,"paused":false},
			"tracks":[{"id":"1","url":"s1","disc_number":1,"track_number":1,"name":"A","duration_ms":1000,"has_lyrics":true,"state":"done"},
			{"id":"2","url":"s2","disc_number":2,"track_number":1,"name":"B","state":"failed"}]}`)
	}))
	a, err := c.Album(context.Background(), "1783644603", "cn")
	if err != nil || a.Title != "T" || a.DiscCount != 2 || len(a.Tracks) != 2 || a.Tracks[0].DurationMS != 1000 || !a.Tracks[0].HasLyrics || a.Tracks[1].DiscNumber != 2 {
		t.Errorf("album = %+v, %v", a, err)
	}
	if a.Resource == nil || a.Resource.TracksDone != 1 || a.Tracks[0].State != "done" || a.Tracks[1].State != "failed" {
		t.Errorf("album resource = %+v, states = %q, %q", a.Resource, a.Tracks[0].State, a.Tracks[1].State)
	}
}
