package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// searchApp is home showing albums for "elma": many, so the results scroll,
// the first in the library through job done.
func searchApp(t *testing.T) (*App, []client.SearchHit) {
	t.Helper()
	a, _ := newTestApp(remoteSettings())
	s := a.Store
	s.SetOmniText("elma")
	s.WantSearch("elma")
	var albums []client.SearchHit
	for i := range 30 {
		albums = append(albums, client.SearchHit{Type: "album", ID: fmt.Sprint(100 + i), Name: fmt.Sprintf("Album %d", i), ArtistName: "Yorushika", TrackCount: 3,
			URL: fmt.Sprintf("https://music.apple.com/cn/album/x/%d", 100+i)})
	}
	albums[0].Resource = &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "100", State: domain.RecordComplete, WantedAll: true}

	s.Search.Result = &client.SearchResult{Albums: &client.HitGroup{Items: albums}}
	s.Search.Status = store.SearchReady
	a.Screen = ScreenHome
	return a, albums
}

func TestAlbumTileHasOneControl(t *testing.T) {
	a, albums := searchApp(t)
	tt := ui.NewTester(a.View, 1280, 800)
	if _, ok := tt.Find("Album 0 is in your library: download again"); !ok {
		t.Errorf("no library control on the downloaded album")
	}
	if _, ok := tt.Find("In your library"); ok {
		t.Errorf("the downloaded album still has a badge besides its control")
	}
	// Another album's download shows only while pointed at.
	if _, ok := tt.Find("Download Album 1"); ok {
		t.Errorf("an album not downloaded shows its download at rest")
	}
	r, ok := tt.Find("Album 1")
	if !ok {
		t.Fatalf("no tile for %s: %q", albums[1].Name, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if _, ok := tt.Find("Download Album 1"); !ok {
		t.Errorf("pointing at an album does not show its download")
	}
}

func TestAlbumOpensFromSearchAndBackReturnsThere(t *testing.T) {
	a, albums := searchApp(t)
	s := a.Store
	// The overview holds a row of them; the Albums tab scrolls.
	s.FocusGroup("albums")
	tt := ui.NewTester(a.View, 1280, 800)
	home := a.scrolls["home-body"]
	if home == nil || home.MaxY < 400 {
		t.Fatalf("the results do not scroll: %+v", home)
	}
	home.Y = 400
	tt.Frame()
	// A downloaded album opens its page, not its job.
	a.OpenAlbum(albums[0])
	tt.Frame()
	if a.Screen != ScreenAlbum || !tt.HasText("Results") {
		t.Fatalf("screen %v, texts %q", a.Screen, tt.Texts())
	}
	p := &s.Search.Album
	p.Status, p.Album = store.SearchReady, client.Album{ID: "100", Title: "Album 0", ArtistName: "Yorushika", DiscCount: 2, Tracks: []client.AlbumTrack{
		{ID: "t1", DiscNumber: 1, TrackNumber: 1, Name: "First Song", DurationMS: 200_000},
		{ID: "t2", DiscNumber: 2, TrackNumber: 1, Name: "Second Song", ArtistName: "Yorushika & suis", DurationMS: 61_000, ContentRating: "explicit"},
	}}
	tt.Frame()
	for _, want := range []string{"Album 0", "In your library", "Download again", "DISC 1", "DISC 2", "First Song", "Second Song", "Yorushika & suis", "3:20", "1:01", "ALBUM · 2 TRACKS"} {
		if !tt.HasText(want) {
			t.Errorf("no %q on the album page: %q", want, tt.Texts())
		}
	}
	// A download the feed does not hold has no figures to show: the
	// album's stay.
	for _, want := range []string{"RELEASED", "GENRE", "LENGTH"} {
		if !tt.HasText(want) {
			t.Errorf("no %q for an album whose download the feed does not hold", want)
		}
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.Screen != ScreenHome || s.Omni.Text != "elma" || a.searchSel != store.HitKey(albums[0]) {
		t.Fatalf("Back to search: screen %v text %q chosen %q", a.Screen, s.Omni.Text, a.searchSel)
	}
	tt.Frame()
	if !tt.HasText("Album 29") || home.Y != 400 {
		t.Errorf("the results came back at %v, want 400: %q", home.Y, tt.Texts())
	}
	if !tt.Focused("Paste links or search Apple Music") {
		t.Errorf("Back to search left the keyboard out of the field")
	}
}

func TestSidebarLightsWhereAPageOpened(t *testing.T) {
	a, albums := searchApp(t)
	a.OpenAlbum(albums[0])
	if got := a.navScreen(); got != ScreenHome {
		t.Errorf("album opened from search lights %v", got)
	}
	a.OpenJob("done")
	if got := a.navScreen(); got != ScreenHome {
		t.Errorf("job opened from an album lights %v", got)
	}
	a.Go(ScreenQueue)
	a.OpenJob("done")
	if got := a.navScreen(); got != ScreenQueue {
		t.Errorf("job opened from the queue lights %v", got)
	}
	a.Go(ScreenSettings)
	if got := a.navScreen(); got != ScreenSettings {
		t.Errorf("settings lights %v", got)
	}
}

func TestBackFromAJobReturnsWhereItOpened(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Go(ScreenQueue)
	a.OpenJob("j")
	if a.backLabel() != "Queue" {
		t.Errorf("from the queue, Back says %q", a.backLabel())
	}
	a.Back()
	if a.Screen != ScreenQueue {
		t.Errorf("Back from a job opened in the queue shows %v", a.Screen)
	}
	a.Go(ScreenHome)
	a.Store.SetOmniText("yorushika")
	a.Store.WantSearch("yorushika")
	a.OpenJob("j")
	if a.backLabel() != "Results" {
		t.Errorf("from the search, Back says %q", a.backLabel())
	}
	a.Back()
	if a.Screen != ScreenHome {
		t.Errorf("Back from a job opened in the search shows %v", a.Screen)
	}
	// A job opened from a notification while setup shows leaves setup
	// behind: Back goes to the queue, which the sidebar lights.
	a.OpenSetup("")
	a.OpenJob("j")
	if a.backLabel() != "Queue" || a.navScreen() != ScreenQueue {
		t.Errorf("from setup, Back says %q and the sidebar lights %v", a.backLabel(), a.navScreen())
	}
	a.Back()
	if a.Screen != ScreenQueue {
		t.Errorf("Back from a job opened over setup shows %v", a.Screen)
	}
}

func TestReturnOpensAnAlbum(t *testing.T) {
	a, albums := searchApp(t)
	a.searchSel = store.HitKey(albums[3])
	a.actOnHit()
	if a.Screen != ScreenAlbum || a.Store.Search.Album.ID != albums[3].ID {
		t.Errorf("Return on an album: screen %v album %q", a.Screen, a.Store.Search.Album.ID)
	}
	if len(a.Store.Search.Starting) != 0 {
		t.Errorf("Return on an album started a download")
	}
}

func TestAlbumKindAndClip(t *testing.T) {
	if k, n := store.AlbumKind("Halo - EP"); k != "EP" || n != "Halo" {
		t.Errorf("Halo - EP: %q %q", k, n)
	}
	if k, _ := store.AlbumKind("Elma"); k != "Album" || !strings.HasPrefix(clip("Yorushika and many friends of theirs", 10), "Yorushika") {
		t.Error("Elma is an album, and clip keeps its start")
	}
}

// An album whose record holds only a song added on its own is not in the
// library: its tile offers the rest, and Download adds the whole album
// without asking to replace anything.
func TestAlbumOfOneDownloadedSongOffersTheRest(t *testing.T) {
	a, albums := searchApp(t)
	albums[1].Resource = &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "101", State: domain.RecordComplete, TracksDone: 1, TracksWanted: 1}
	a.Store.Search.Result.Albums.Items[1] = albums[1]
	tt := ui.NewTester(a.View, 1280, 800)
	if mark, _ := a.Store.HitMark(albums[1]); mark != store.MarkPartial {
		t.Fatalf("mark of the one-song album = %v, want partial", mark)
	}
	if _, ok := tt.Find("Album 1: 1 of 3 tracks downloaded, download the rest"); !ok {
		t.Errorf("no partial control on the one-song album: %q", tt.Texts())
	}
	if _, ok := tt.Find("Album 1 is in your library: download again"); ok {
		t.Error("the one-song album reads as in the library")
	}
	a.download(albums[1])
	if a.asking != nil {
		t.Errorf("Download asked %+v, want the album sent as it is", *a.asking)
	}
	if _, on := a.Store.Search.Starting[store.HitKey(albums[1])]; !on {
		t.Error("Download did not send the album")
	}
	// A record of every track of the album is in the library.
	albums[2].Resource = &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "102", State: domain.RecordComplete, TracksDone: 3, TracksWanted: 3}
	if mark, _ := a.Store.HitMark(albums[2]); mark != store.MarkDone {
		t.Errorf("mark of an album whose songs are all downloaded = %v, want done", mark)
	}
}

// The album page's buttons in the title bar keep clear of the window's own
// buttons at its right by captionGap, 16 on Windows, whose caption buttons
// take the hover over their whole box.
func TestTheTitleBarsButtonsKeepClearOfTheWindowButtons(t *testing.T) {
	if captionGapFor("windows") < 16 || captionGapFor("linux") != 8 || captionGapFor("darwin") != 8 {
		t.Fatalf("gaps: windows %v, linux %v, darwin %v", captionGapFor("windows"), captionGapFor("linux"), captionGapFor("darwin"))
	}
	const captions = 138 // three of Windows' 46-wide caption buttons
	defer func(g float32) { captionGap = g }(captionGap)
	for _, gap := range []float32{8, 16} {
		captionGap = gap
		a, albums := searchApp(t)
		a.PageMotion = false
		tt := ui.NewTester(a.View, 1280, 800)
		tt.SetScale(1)
		tt.SetTitleBar(ui.TitleBar{Height: 32, Right: captions})
		a.OpenAlbum(albums[0])
		tt.Frame()
		tt.Frame()
		btn, ok := tt.Find("Download " + albums[0].Name + " again")
		if !ok {
			t.Fatalf("no download button in the title bar: %q", tt.Texts())
		}
		if room := 1280 - (btn.X + btn.W) - captions; room < gap-0.5 || room > gap+0.5 {
			t.Errorf("gap %v: the button ends %.1f from the window buttons", gap, room)
		}
	}
}

// A playlist found by search opens its page, as an album does, with its
// songs in its own order, each by its artist, under the playlist's name.
func TestAPlaylistOpensItsPageWithItsSongs(t *testing.T) {
	a, run := demoApp(t)
	a.Screen, a.PageMotion = ScreenHome, false
	tt := ui.NewTester(a.View, 1280, 800)
	s := a.Store
	s.SetOmniText("Glass and Neon")
	tt.Frame()
	run(func() bool { tt.Frame(); return s.Search.Status == store.SearchReady })
	s.FocusGroup("playlists")
	tt.Frame()
	if err := tt.Click("Glass and Neon"); err != nil {
		t.Fatalf("no playlist tile: %v, %q", err, tt.Texts())
	}
	tt.Frame()
	if a.Screen != ScreenAlbum {
		t.Fatalf("clicking the playlist left the screen at %v", a.Screen)
	}
	p := &s.Search.Album
	run(func() bool { tt.Frame(); return p.Status == store.SearchReady })
	tt.Frame()
	if p.Kind() != domain.ResourcePlaylist || p.ID != "pl.demo0001" {
		t.Fatalf("the page shows %s %q", p.Kind(), p.ID)
	}
	// The demo's playlist takes two songs from each of its eight albums,
	// its cover's (Glass Weather) first.
	if !tt.HasText("PLAYLIST · 16 TRACKS") || !tt.HasText("ARTISTS") {
		t.Errorf("no playlist kicker or artists: %q", tt.Texts())
	}
	for _, name := range []string{"Glass Weather", "Paper Satellites", "Tidal Rooms"} {
		if !tt.HasText(name) {
			t.Errorf("song %q is not listed: %q", name, tt.Texts())
		}
	}
}
