package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
)

func remoteSettings() mode.Settings {
	s := mode.DefaultSettings()
	s.Mode, s.RemoteURL = mode.Remote, "http://nas:18570"
	return s
}

func TestNewDownloadSaysWhatWillStart(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Screen = ScreenNew
	s := a.Store
	s.SetOmniText("https://music.apple.com/cn/album/x/1858184006 https://music.apple.com/cn/music-video/y/1")
	if len(s.Omni.Links) != 2 {
		t.Fatalf("links = %d, want 2", len(s.Omni.Links))
	}
	album, video := s.Omni.Links[0], s.Omni.Links[1]
	album.State = store.LinkReady
	album.Lookup = client.Lookup{Type: "album", ID: "1858184006", Title: "Test Album", ArtistName: "Some Artist", TrackCount: 5, Storefront: "cn"}
	video.State = store.LinkUnsupported
	tt := ui.NewTester(a.View, 1280, 800)
	for _, want := range []string{"LINKS · 2 FOUND", "Test Album", "Ready", "Not supported", "1 album · 5 tracks", "1 not supported", "Download 1 album"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
}

func TestLibraryShowsTheNewestLarge(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Screen = ScreenLibrary
	done := time.Now()
	l := &a.Store.Library
	l.Loaded, l.Total = true, 2
	l.Jobs = []domain.Job{
		{ID: "a", Type: "album", Status: domain.JobCompleted, Title: "Newest Album", ArtistName: "Artist A", TotalItems: 5, FinishedAt: &done},
		{ID: "b", Type: "album", Status: domain.JobCompleted, Title: "Older Album", ArtistName: "Artist B", TotalItems: 9},
	}
	tt := ui.NewTester(a.View, 1280, 800)
	for _, want := range []string{"Library", "Albums", "Newest Album", "Older Album", "Artist B", "Details", "2 albums"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
}

func TestSystemCardsReadTheServer(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Screen = ScreenSystem
	sv := &a.Store.Server
	sv.Read = time.Now()
	sv.Health = &client.Health{Status: "ok"}
	sv.System = &client.SystemInfo{Version: "2.0-dev", Commit: "01339f0aa", UptimeSeconds: 19 * 60}
	sv.System.Jobs.Workers = 3
	sv.System.Tools = []domain.Capability{{Name: "ffmpeg", Available: true}}
	sv.System.Storage = []client.Storage{{Name: "downloads_dir", Path: "data/downloads", FreeBytes: 1_620_000_000_000, TotalBytes: 7_670_000_000_000}}
	sv.Wrapper = &client.WrapperStatus{Ready: true, Status: true, Accounts: []string{"someone@example.com"}, Regions: []string{"cn", "us"}}
	tt := ui.NewTester(a.View, 1280, 800)
	for _, want := range []string{"All good", "01339f0", "of 3 busy", "Downloads folder", "1.62", "TB free", "Signed in", "someone@example.com"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
}

func TestPanelWhenNothingRuns(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	tt := ui.NewTester(a.Panel, PanelWidth, 460)
	for _, want := range []string{"All done", "Today", "Server disk", "Open AMDL"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
}

func TestSearchHitRowsSayWhatTheirTypeHas(t *testing.T) {
	if got := qualityHints([]string{"atmos", "lossless", "hi-res-lossless", "lossy-stereo"}); strings.Join(got, ",") != "Hi-Res Lossless,Dolby Atmos" {
		t.Errorf("hints %v", got)
	}
	if got := qualityHints([]string{"lossy-stereo"}); len(got) != 0 {
		t.Errorf("lossy hints %v", got)
	}
	cases := []struct {
		hit  client.SearchHit
		want string
	}{
		{client.SearchHit{Type: "song", ArtistName: "Yorushika", AlbumName: "Elma", DurationMS: 245_000}, "Yorushika · Elma · 4:05"},
		{client.SearchHit{Type: "album", ArtistName: "Yorushika", ReleaseDate: "2020-07-29", TrackCount: 1}, "Yorushika · 2020 · 1 track"},
		{client.SearchHit{Type: "artist", Genre: "J-Pop"}, "J-Pop"},
		{client.SearchHit{Type: "artist"}, "Artist"},
	}
	for _, c := range cases {
		if got := strings.Join(hitFacts(c.hit), " · "); got != c.want {
			t.Errorf("%s facts %q, want %q", c.hit.Type, got, c.want)
		}
	}
}
