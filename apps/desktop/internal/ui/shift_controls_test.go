//go:build !race

package ui

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
)

// The controls that keep their size by construction, held to it: a change
// that gives one a state-dependent border, weight, icon or padding fails
// here.

func TestSegmentsKeepTheirSizeWhenChosen(t *testing.T) {
	themes(t, func(t *testing.T, dark bool) {
		labels := []string{"System", "Light", "Dark"}
		chosen := 0
		tt := bench(dark, func(c *ui.Context, k *Tokens) {
			ui.Row(c).Children(func() { segmented(c, k, "theme", &chosen, labels, nil) })
		})
		for _, l := range labels {
			holdsStill(t, tt, l, labels...)
		}
	})
}

func TestSearchTabsKeepTheirSizeWhenChosen(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	s := a.Store
	s.SetOmniText("one last kiss")
	s.WantSearch("one last kiss")
	hit := func(typ, id, name string) *client.HitGroup {
		return &client.HitGroup{Items: []client.SearchHit{{Type: typ, ID: id, Name: name, ArtistName: "Utada"}}}
	}
	s.Search.Result = &client.SearchResult{Albums: hit("album", "1", "One Last Kiss"), Songs: hit("song", "2", "Kiss"),
		Playlists: hit("playlist", "3", "Kisses"), Artists: hit("artist", "4", "Utada Hikaru")}
	s.Search.Status = store.SearchReady
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	a.FocusField()
	tt.Frame()
	a.focusGroup("albums")
	tt.Frame()
	tabs := []string{"Albums", "Songs", "Playlists", "Artists"}
	before := map[string]ui.Rect{}
	for _, l := range tabs {
		before[l] = findAny(t, tt, l)
	}
	for _, g := range []string{"songs", "playlists", "artists", "albums"} {
		a.focusGroup(g)
		tt.Frame()
		for _, l := range tabs {
			sameBox(t, "the "+l+" tab with "+g+" chosen", before[l], findAny(t, tt, l))
		}
	}
}

func TestSidebarItemsStayPutAcrossScreens(t *testing.T) {
	a, run := demoApp(t)
	a.Screen = ScreenQueue
	run(func() bool { return a.Store.Loaded })
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	items := []string{"Home", "Queue", "Settings"}
	before := map[string]ui.Rect{}
	for _, l := range items {
		before[l] = findAny(t, tt, l)
	}
	for _, s := range []Screen{ScreenHome, ScreenSettings, ScreenQueue} {
		a.Go(s)
		for range 3 {
			tt.Frame()
		}
		for _, l := range items {
			sameBox(t, "the sidebar's "+l+" on screen "+fmt.Sprint(s), before[l], findAny(t, tt, l))
		}
	}
}

func TestSettingsNavKeepsItsItemsWhenAPageIsChosen(t *testing.T) {
	_, tt, _ := hooksApp(t)
	pages := []string{"Account", "Quality", "Hooks", "Logs"}
	before := map[string]ui.Rect{}
	for _, l := range pages {
		before[l] = findAny(t, tt, l)
	}
	for _, l := range pages {
		if err := tt.Click(l); err != nil {
			t.Fatal(err)
		}
		for range 8 {
			tt.Frame()
		}
		for _, p := range pages {
			sameBox(t, "the nav item "+p+" with "+l+" chosen", before[p], findAny(t, tt, p))
		}
	}
}

func TestSettingsSwitchKeepsItsSize(t *testing.T) {
	_, tt, _ := hooksApp(t)
	// Switching hooks off changes what the section says; the switch and
	// the button beside it stay.
	toggle, add := findAny(t, tt, "Run hooks"), findAny(t, tt, "Add hook")
	for range 2 {
		if err := tt.Click("Run hooks"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		sameBox(t, "the switch", toggle, findAny(t, tt, "Run hooks"))
		sameBox(t, "Add hook", add, findAny(t, tt, "Add hook"))
	}
}
