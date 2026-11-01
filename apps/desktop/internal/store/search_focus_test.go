package store

import (
	"testing"

	"amdl/internal/client"
)

func TestSearchFocusStaysWhileTheWordsChangeAndEndsWithThem(t *testing.T) {
	s, _ := newTestStore(t)
	offlineClient(t, s)
	s.WantSearch("one last k")
	s.FocusGroup("playlists")
	s.WantSearch("one last kiss")
	if s.Search.Focus != "playlists" {
		t.Errorf("editing the words left the focus on %q", s.Search.Focus)
	}
	s.WantSearch("")
	if s.Search.Focus != "" {
		t.Errorf("clearing the field left the focus on %q", s.Search.Focus)
	}
}

func TestNoHitsMeansEveryGroupIsEmpty(t *testing.T) {
	if NoHits(nil) {
		t.Error("no answer yet is not an empty answer")
	}
	if !NoHits(&client.SearchResult{Songs: group(0), Playlists: group(0)}) {
		t.Error("groups without hits")
	}
	if NoHits(&client.SearchResult{Songs: group(0), Artists: group(0, hit("artist", "1"))}) {
		t.Error("one group has a hit")
	}
}
