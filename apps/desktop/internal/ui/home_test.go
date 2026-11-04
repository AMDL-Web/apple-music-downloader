package ui

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
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

// A pasted link of what amdl's record already holds says so before the
// submit: a song downloaded, as an album whole, reads "In your library" and
// starts nothing, while a song not downloaded stays Ready.
func TestAPastedLinkOfADownloadedSongReadsInYourLibrary(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Screen = ScreenHome
	s := a.Store
	held, fresh := "https://music.apple.com/cn/song/x/1", "https://music.apple.com/cn/song/y/2"
	s.SetOmniText(held + "\n" + fresh)
	rec := func(song domain.AudioState) *domain.ResourceMark {
		return &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "9", State: domain.RecordComplete, TracksDone: 1, TracksWanted: 1, SongState: song}
	}
	for i, l := range s.Omni.Links {
		l.State = store.LinkReady
		l.Lookup = client.Lookup{Type: "song", ID: fmt.Sprint(i + 1), Title: fmt.Sprint("Song ", i+1), Storefront: "cn", TrackCount: 1}
	}
	s.Omni.Links[0].Lookup.Resource = rec(domain.AudioDone)
	s.Omni.Links[1].Lookup.Resource = rec("")
	tt := ui.NewTester(a.View, 1280, 800)
	tt.Frame()
	if !tt.HasText("In your library") || !tt.HasText("Ready") {
		t.Errorf("want one link in your library and one ready: %q", tt.Texts())
	}
	if got := s.Startable(); len(got) != 1 || got[0].URL != fresh {
		t.Errorf("a submit would start %v, want only the song not downloaded", got)
	}
	if _, ok := tt.Find("Download 1 song"); !ok {
		t.Errorf("the dock does not offer the one song: %q", tt.Texts())
	}
}
