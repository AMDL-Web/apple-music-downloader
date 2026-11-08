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
	if err := tt.Click("Overview"); err != nil {
		t.Fatalf("the way back is gone: %v in %q", err, tt.Texts())
	}
	tt.Frame()
	if s.Search.Focus != "" || !tt.HasText("One Last Kiss") {
		t.Errorf("Overview: focus %q, texts %q", s.Search.Focus, tt.Texts())
	}
	// Only when every group is empty does the search answer as a whole.
	s.Search.Result = &client.SearchResult{}
	a.focusGroup("playlists")
	tt.Frame()
	if !tt.HasText("Nothing found for “one last kiss”") {
		t.Errorf("no group has hits: %q", tt.Texts())
	}
}
