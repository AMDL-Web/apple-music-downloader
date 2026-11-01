package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
)

// A group with no hits, chosen by its tab, answers inside the tab: the tabs
// and the way back stay, so the other groups are one click away.
func TestAnEmptyGroupKeepsTheTabsAndTheWayBack(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	s := a.Store
	s.SetOmniText("one last kiss")
	s.WantSearch("one last kiss")
	album := client.SearchHit{Type: "album", ID: "1", Name: "One Last Kiss", ArtistName: "Utada"}
	artist := client.SearchHit{Type: "artist", ID: "3", Name: "Utada Hikaru"}
	s.Search.Result = &client.SearchResult{
		Albums:  &client.HitGroup{Items: []client.SearchHit{album}},
		Artists: &client.HitGroup{Items: []client.SearchHit{artist}},
	}
	s.Search.Status = store.SearchReady
	tt := ui.NewTester(a.View, 1280, 800)
	a.FocusField()
	tt.Frame()
	a.focusGroup("playlists")
	tt.Frame()
	if tt.HasText("Nothing found for “one last kiss”") {
		t.Fatalf("an empty group stands for the whole search: %q", tt.Texts())
	}
	for _, want := range []string{"Albums", "Songs", "Playlists", "Artists", "No playlists for “one last kiss”"} {
		if !tt.HasText(want) {
			t.Errorf("no %q with Playlists chosen and empty: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Artists"); err != nil {
		t.Fatalf("the Artists tab is gone: %v in %q", err, tt.Texts())
	}
	tt.Frame()
	if s.Search.Focus != "artists" || !tt.HasText("Utada Hikaru") {
		t.Errorf("Artists tab: focus %q, texts %q", s.Search.Focus, tt.Texts())
	}
	a.focusGroup("playlists")
	tt.Frame()
	if err := tt.Click("All results"); err != nil {
		t.Fatalf("the way back is gone: %v in %q", err, tt.Texts())
	}
	tt.Frame()
	if s.Search.Focus != "" || !tt.HasText("One Last Kiss") {
		t.Errorf("All results: focus %q, texts %q", s.Search.Focus, tt.Texts())
	}
	// Only when every group is empty does the search answer as a whole.
	s.Search.Result = &client.SearchResult{}
	a.focusGroup("playlists")
	tt.Frame()
	if !tt.HasText("Nothing found for “one last kiss”") {
		t.Errorf("no group has hits: %q", tt.Texts())
	}
}
