package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
)

const fieldLabel = "Paste links or search Apple Music"

func TestFocusFieldOpensHomeFromAnotherScreen(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Screen = ScreenQueue
	tt := ui.NewTester(a.View, 1280, 800)
	if tt.HasText(fieldLabel) {
		t.Fatal("the queue shows home's field")
	}
	tt.Key(ui.Cmd, ui.KeyL)
	tt.Frame()
	if a.Screen != ScreenHome || !tt.Focused(fieldLabel) {
		t.Errorf("after Cmd+L: screen %v, field focused %v", a.Screen, tt.Focused(fieldLabel))
	}
	tt.Key(ui.Cmd, ui.Key2)
	tt.Frame()
	if a.Screen != ScreenQueue {
		t.Errorf("Cmd+2 shows screen %v, want the queue", a.Screen)
	}
}

func TestFieldKeysActOnTheResults(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	s := a.Store
	s.SetOmniText("yorushika")
	s.WantSearch("yorushika")
	album := client.SearchHit{Type: "album", ID: "1", Name: "Elma", ArtistName: "Yorushika"}
	song := client.SearchHit{Type: "song", ID: "2", Name: "Hitchcock", ArtistName: "Yorushika"}
	s.Search.Result = &client.SearchResult{Albums: &client.HitGroup{Items: []client.SearchHit{album}}, Songs: &client.HitGroup{Items: []client.SearchHit{song}}}
	s.Search.Status = store.SearchReady
	tt := ui.NewTester(a.View, 1280, 800)
	a.FocusField()
	tt.Frame()
	if !tt.Focused(fieldLabel) || !tt.HasText("Elma") || !tt.HasText("Hitchcock") {
		t.Fatalf("focused %v, texts %q", tt.Focused(fieldLabel), tt.Texts())
	}
	// Return picks the first result, albums first; Down the next.
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.searchSel != store.HitKey(album) {
		t.Errorf("Return chose %q, want the album", a.searchSel)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.searchSel != store.HitKey(song) || s.Omni.Text != "yorushika" {
		t.Errorf("Down chose %q and left %q in the field", a.searchSel, s.Omni.Text)
	}
	// Escape forgets the choice, then clears the field.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.searchSel != "" || s.Omni.Text != "yorushika" {
		t.Errorf("first Escape: chosen %q, text %q", a.searchSel, s.Omni.Text)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.Omni.Text != "" {
		t.Errorf("second Escape left %q", s.Omni.Text)
	}
}
