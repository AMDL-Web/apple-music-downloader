package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
)

func regionAccounts(regions ...string) []client.Account {
	var out []client.Account
	for i, r := range regions {
		out = append(out, client.Account{ID: "a" + r, Name: r + "@icloud.com", Region: r, Position: i, Default: i == 0, State: client.AccountActive})
	}
	return out
}

// searchWithRegions is home with a search answered, over accounts in regions.
func searchWithRegions(theme string, w, h int, regions ...string) (*App, *ui.Tester) {
	a, _ := newTestApp(remoteSettings())
	a.Theme = theme
	s := a.Store
	s.Server.Accounts = regionAccounts(regions...)
	s.SetOmniText("yorushika")
	s.WantSearch("yorushika")
	s.Search.Result = &client.SearchResult{
		Albums: &client.HitGroup{Items: []client.SearchHit{
			{Type: "album", ID: "1", Name: "だから僕は音楽を辞めた", ArtistName: "ヨルシカ", ReleaseDate: "2019-04-10", TrackCount: 10},
			{Type: "album", ID: "2", Name: "エルマ", ArtistName: "ヨルシカ", ReleaseDate: "2020-07-29", TrackCount: 12},
		}},
		Songs:   &client.HitGroup{Items: []client.SearchHit{{Type: "song", ID: "3", Name: "春泥棒", ArtistName: "ヨルシカ", AlbumName: "盗作", DurationMS: 245_000}}},
		Artists: &client.HitGroup{Items: []client.SearchHit{{Type: "artist", ID: "4", Name: "ヨルシカ", Genre: "J-Pop"}}},
	}
	s.Search.Status = store.SearchReady
	tt := ui.NewTester(a.View, w, h)
	a.FocusField()
	tt.Frame()
	return a, tt
}

func TestRegionSwitchShowsOnlyAcrossTwoRegions(t *testing.T) {
	for _, tc := range []struct {
		regions []string
		shown   bool
	}{{nil, false}, {[]string{"us"}, false}, {[]string{"us", "us"}, false}, {[]string{"us", "jp"}, true}, {[]string{"jp", "us", "gb"}, true}} {
		_, tt := searchWithRegions("dark", 1280, 800, tc.regions...)
		if got := tt.HasText("Japan") || tt.HasText("United States"); got != tc.shown {
			t.Errorf("accounts in %v: switch shown %v, want %v: %q", tc.regions, got, tc.shown, tt.Texts())
		}
	}
}

func TestChoosingARegionSearchesThere(t *testing.T) {
	a, tt := searchWithRegions("dark", 1280, 800, "us", "jp")
	s := a.Store
	if s.SearchRegion() != "us" || s.Search.Region != "" {
		t.Fatalf("before a choice: shown %q chosen %q", s.SearchRegion(), s.Search.Region)
	}
	if err := tt.Click("Japan"); err != nil {
		t.Fatalf("no Japan segment: %v in %q", err, tt.Texts())
	}
	tt.Frame()
	if s.Search.Region != "jp" || s.SearchRegion() != "jp" {
		t.Errorf("after clicking Japan: chosen %q", s.Search.Region)
	}
}

// The region sits at the right end of the group tabs' row, its label on
// their line, appears only once there is a search, and stays reachable
// when the search found nothing.
func TestTheRegionRidesTheGroupTabs(t *testing.T) {
	_, tt := searchWithRegions("dark", 1280, 800, "us", "jp")
	tab, ok1 := tt.Find("Artists")
	jp, ok2 := tt.Find("Japan")
	if !ok1 || !ok2 {
		t.Fatalf("no tabs or region in %q", tt.Texts())
	}
	if d := (tab.Y + tab.H/2) - (jp.Y + jp.H/2); d > 1 || d < -1 || jp.X <= tab.X+tab.W {
		t.Errorf("region at %v, last tab at %v: not on the tabs' line at their right", jp, tab)
	}

	a, _ := newTestApp(remoteSettings())
	a.Store.Server.Accounts = regionAccounts("us", "jp")
	home := ui.NewTester(a.View, 1280, 800)
	home.Frame()
	if home.HasText("Japan") {
		t.Error("a region before any search")
	}

	a, tt = searchWithRegions("dark", 1280, 800, "us", "jp")
	a.Store.Search.Result = &client.SearchResult{}
	tt.Frame()
	if !tt.HasText("Japan") {
		t.Errorf("no region on a search that found nothing: %q", tt.Texts())
	}
}

func TestThreeRegionsAreAMenu(t *testing.T) {
	a, tt := searchWithRegions("dark", 1280, 800, "us", "jp", "gb")
	if tt.HasText("Japan") {
		t.Errorf("more than two regions laid out side by side: %q", tt.Texts())
	}
	if err := tt.Click("Region"); err != nil {
		t.Fatalf("no region menu: %v in %q", err, tt.Texts())
	}
	tt.Frame()
	if err := tt.Click("United Kingdom"); err != nil {
		t.Fatalf("no United Kingdom in the menu: %v in %q", err, tt.Texts())
	}
	tt.Frame()
	if a.Store.Search.Region != "gb" {
		t.Errorf("chosen %q after picking United Kingdom", a.Store.Search.Region)
	}
}
