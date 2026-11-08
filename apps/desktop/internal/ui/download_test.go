package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// The choices go out as the job's overrides, or as the codec order of a
// reset where the sheet downloads again; which choices a hit offers is
// format.QualityChoices.
func TestTheDownloadChoicesBecomeTheJobsOverrides(t *testing.T) {
	d := downloadAsk{codecs: []string{"alac", "ec3"}, priority: []string{"alac", "ec3", "aac"}, codec: 1}
	o := d.overrides()
	if o == nil || !slices.Equal(*o.QualityPriority, []string{"ec3", "alac", "aac"}) {
		t.Errorf("Dolby chosen: %+v", o)
	}
	if d.replaces() {
		t.Error("a hit with nothing downloaded downloads again")
	}
	if o := (&downloadAsk{}).overrides(); o != nil {
		t.Errorf("nothing to ask: %+v", o)
	}
	for _, c := range []struct {
		again store.MarkState
		all   int
		want  bool
	}{{store.MarkDone, 0, true}, {store.MarkPartial, 0, false}, {store.MarkPartial, 1, true}, {store.MarkNone, 1, false}} {
		if got := (&downloadAsk{again: c.again, all: c.all}).replaces(); got != c.want {
			t.Errorf("again %v, all %d: replaces %v, want %v", c.again, c.all, got, c.want)
		}
	}
}

func TestSearchShowsOneGroupUnderTabsAndReadsItsNextPages(t *testing.T) {
	a, run := demoApp(t)
	a.Screen = ScreenHome
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	a.Store.SetOmniText("a")
	tt.Frame()
	run(func() bool { return a.Store.Search.Status == store.SearchReady })
	tt.Frame()
	songs := len(a.Store.Search.Result.Songs.Items)
	if songs != store.SearchFirst || !tt.HasText("Playlists") {
		t.Fatalf("first page: %d songs, texts %q", songs, tt.Texts())
	}
	if err := tt.Click("See all songs"); err != nil {
		t.Fatal(err)
	}
	if a.Store.Search.Focus != "songs" {
		t.Fatalf("focus %q after See all songs", a.Store.Search.Focus)
	}
	// The short page asks for the next one at once.
	run(func() bool {
		tt.Frame()
		return len(a.Store.Search.Result.Songs.Items) > songs
	})
	for _, g := range store.SearchGroups {
		if _, ok := tt.Find(groupTitle(g)); !ok {
			t.Errorf("no %s tab in %q", groupTitle(g), tt.Texts())
		}
	}
	if hits := a.Store.Search.ShownHits(); len(hits) == 0 || hits[0].Type != "song" {
		t.Errorf("the keys walk %d hits, the first a %s", len(hits), hits[0].Type)
	}
	if err := tt.Click("Playlists"); err != nil {
		t.Fatal(err)
	}
	if a.Store.Search.Focus != "playlists" || tt.HasText("Show more") {
		t.Errorf("after the Playlists tab: focus %q, texts %q", a.Store.Search.Focus, tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	if a.Store.Search.Focus != "" {
		t.Errorf("Escape left the focus on %q", a.Store.Search.Focus)
	}
}

func TestASongInLosslessAndDolbyAsksWhichBeforeItDownloads(t *testing.T) {
	a, run := demoApp(t)
	a.Screen = ScreenHome
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	// Its album's record downloaded two tracks and has no run.
	a.Store.SetOmniText("evening rounds")
	tt.Frame()
	run(func() bool { return a.Store.Search.Status == store.SearchReady && a.Store.Config.Values != nil })
	tt.Frame()
	jobs := len(a.Store.Jobs)
	if err := tt.Click("Download Evening Rounds"); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	for _, want := range []string{"Download “Evening Rounds”", "Lossless", "Dolby Atmos"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in the sheet: %q", want, tt.Texts())
		}
	}
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "In your library") {
		t.Errorf("a song not downloaded asks about the library: %q", tt.Texts())
	}
	if len(a.Store.Search.Starting) != 0 {
		t.Fatal("the download started before the sheet's answer")
	}
	if err := tt.Click("Download"); err != nil {
		t.Fatal(err)
	}
	run(func() bool { return len(a.Store.Jobs) > jobs })
	tt.Frame()
	if tt.HasText("Dolby Atmos") {
		t.Errorf("the sheet stayed: %q", tt.Texts())
	}
}
