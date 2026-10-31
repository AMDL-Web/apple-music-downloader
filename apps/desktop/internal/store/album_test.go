package store

import (
	"errors"
	"testing"

	"amdl/internal/client"
	"amdl/internal/domain"
)

func TestAlbumPageOpensFromItsHitAndCaches(t *testing.T) {
	s, _ := newTestStore(t)
	offlineClient(t, s)
	p := &s.Search.Album
	s.OpenAlbum(hit("album", "1"))
	if p.Status != SearchLoading || p.Hit.Name != "album 1" {
		t.Fatalf("while loading: status %v hit %q", p.Status, p.Hit.Name)
	}
	s.applyAlbum(p.gen, "1", client.Album{ID: "1", Title: "One", Tracks: []client.AlbumTrack{{ID: "t1"}}}, nil)
	s.OpenAlbum(hit("album", "2"))
	// The first album's late answer never lands on the second.
	s.applyAlbum(p.gen-1, "1", client.Album{ID: "1", Title: "Late"}, nil)
	if p.Status != SearchLoading || p.ID != "2" {
		t.Fatalf("a late answer landed: status %v id %q", p.Status, p.ID)
	}
	s.applyAlbum(p.gen, "2", client.Album{}, errors.New("upstream_error"))
	if p.Status != SearchFailed {
		t.Errorf("failed album: status %v", p.Status)
	}
	s.OpenAlbum(hit("album", "1"))
	if p.Status != SearchReady || p.Album.Title != "One" {
		t.Errorf("cached album: status %v title %q", p.Status, p.Album.Title)
	}
	// A new term keeps the album the screen shows.
	s.WantSearch("x")
	s.WantSearch("")
	if p.ID != "1" || p.Status != SearchReady {
		t.Errorf("after clearing the search: id %q status %v", p.ID, p.Status)
	}
}

func TestTrackMarksFollowTheAlbumJobAndTheSongs(t *testing.T) {
	s, _ := newTestStore(t)
	items := []domain.JobItem{
		{AdamID: "t1", Status: domain.ItemCompleted},
		{AdamID: "t2", Status: domain.ItemSkipped},
		{AdamID: "t3", Status: domain.ItemFailed},
		{AdamID: "t4", Status: domain.ItemDownloading},
	}
	track := func(id string) client.AlbumTrack {
		return client.AlbumTrack{ID: id, URL: "https://music.apple.com/cn/song/x/" + id}
	}
	cases := []struct {
		id    string
		album MarkState
		want  MarkState
	}{
		{"t1", MarkDone, MarkDone},
		{"t2", MarkDone, MarkDone},
		{"t3", MarkDone, MarkNone},
		{"t4", MarkActive, MarkActive},
		{"t5", MarkDone, MarkNone},
		// A failed or deleted album job marks none of its tracks.
		{"t1", MarkNone, MarkNone},
	}
	for _, c := range cases {
		if got, _ := s.TrackMark(track(c.id), c.album, items); got != c.want {
			t.Errorf("%s with the album %v: mark %v, want %v", c.id, c.album, got, c.want)
		}
	}
	// A song downloaded alone from the album screen marks its track.
	s.Search.sent[HitKey(TrackHit(track("t5")))] = "song-job"
	s.upsert(domain.Job{ID: "song-job", Status: domain.JobCompleted})
	if got, _ := s.TrackMark(track("t5"), MarkDone, items); got != MarkDone {
		t.Errorf("song job: mark %v", got)
	}
}
