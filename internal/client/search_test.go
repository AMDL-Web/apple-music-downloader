package client

import (
	"context"
	"fmt"
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
		fmt.Fprint(w, `{"term":"igor","storefront":"us",
			"albums":{"items":[{"id":"1463409338","type":"album","name":"IGOR","url":"https://music.apple.com/us/album/igor/1463409338",
				"artist_name":"Tyler, The Creator","track_count":12,"audio_traits":["lossless"],
				"prior_job":{"id":"job_a","status":"completed","finished_at":"2026-10-30T10:00:00Z"}}],"next_offset":3},
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
	if a.TrackCount != 12 || a.PriorJob == nil || a.PriorJob.Status != "completed" || a.PriorJob.FinishedAt == nil || a.AudioTraits[0] != "lossless" {
		t.Errorf("album hit = %+v", a)
	}
}

func TestArtistAlbums(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/artists/420368335/albums" || r.URL.Query().Get("offset") != "25" {
			t.Errorf("request = %s", r.URL)
		}
		fmt.Fprint(w, `{"storefront":"us","artist":{"id":"420368335","type":"artist","name":"Tyler, The Creator","url":"u"},
			"albums":{"items":[{"id":"1","type":"album","name":"A","url":"u1"}]}}`)
	}))
	r, err := c.ArtistAlbums(context.Background(), "420368335", PageQuery{Offset: 25})
	if err != nil || r.Artist.Name != "Tyler, The Creator" || len(r.Albums.Items) != 1 || r.Albums.NextOffset != 0 {
		t.Errorf("result = %+v, %v", r, err)
	}
}
