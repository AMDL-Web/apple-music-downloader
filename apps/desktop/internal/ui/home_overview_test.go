package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
)

// overviewApp is home on the demo's answer to "a", in the overview.
func overviewApp(t *testing.T, motion bool) (*App, *ui.Tester, func(until func() bool)) {
	t.Helper()
	a, run := demoApp(t)
	a.Screen, a.PageMotion = ScreenHome, motion
	tt := ui.NewTester(a.View, 1280, 800)
	a.Store.SetOmniText("a")
	tt.Frame()
	run(func() bool { tt.Frame(); return a.Store.Search.Status == store.SearchReady })
	tt.Advance(time.Second)
	return a, tt, run
}

// The overview keeps each group to its row: the songs a tab reads further
// stay in the tab, and the overview shows the cards it showed before.
func TestTheOverviewKeepsEachGroupToItsRows(t *testing.T) {
	a, tt, run := overviewApp(t, false)
	q := &a.Store.Search
	shown := len(q.ShownHits())
	// The demo's names repeat across groups and the queue.
	before := map[string]bool{}
	for _, x := range tt.Texts() {
		before[x] = true
	}
	a.focusGroup("songs")
	tt.Frame()
	first := len(q.Result.Songs.Items)
	run(func() bool { tt.Frame(); return len(q.Result.Songs.Items) > first })
	a.focusGroup("")
	tt.Frame()
	songs := q.Result.Songs.Items
	// Two rows of song cards at most: the page is 1280 wide, so a row holds
	// two or three.
	for _, h := range songs[6:] {
		if !before[h.Name] && tt.HasText(h.Name) {
			t.Errorf("the overview shows %q, read by the Songs tab, of %d songs", h.Name, len(songs))
		}
	}
	if n := len(q.ShownHits()); n != shown {
		t.Errorf("the keys walk %d hits of the overview, %d before the Songs tab read more", n, shown)
	}
}

// A group's title opens its tab with the group's hits moving there from
// the overview, not appearing in place.
func TestAGroupsHitsMoveIntoItsTab(t *testing.T) {
	a, tt, _ := overviewApp(t, true)
	// A song whose name shows once: the demo's names repeat across groups
	// and the queue.
	count := map[string]int{}
	for _, x := range tt.Texts() {
		count[x]++
	}
	name := ""
	for _, h := range a.Store.Search.Result.Songs.Items[:4] {
		if count[h.Name] == 1 && name == "" {
			name = h.Name
		}
	}
	from, ok := tt.Find(name)
	if !ok {
		t.Fatalf("no first song %q in the overview: %q", name, tt.Texts())
	}
	a.focusGroup("songs")
	tt.Frame()
	// Partway: past the others' fade, before the group lands.
	tt.Advance(250 * time.Millisecond)
	mid, ok := tt.Find(name)
	if !ok {
		t.Fatalf("partway, the first song is gone: %q", tt.Texts())
	}
	tt.Advance(time.Second)
	to, ok := tt.Find(name)
	if !ok {
		t.Fatalf("in the Songs tab, the first song is gone: %q", tt.Texts())
	}
	if to.Y >= from.Y-20 {
		t.Fatalf("the Songs tab moved the first song from %v to %v, not up", from, to)
	}
	if mid.Y <= to.Y+2 || mid.Y >= from.Y-2 {
		t.Errorf("partway the first song is at %.1f, not between %.1f and %.1f", mid.Y, from.Y, to.Y)
	}
}

// A tab whose first page would fit one row lays it out at the columns'
// size while more pages are to come, the overview's: its tiles neither
// shrink to fit the row nor grow back when the next page arrives.
func TestATabsFirstPageKeepsItsTilesSize(t *testing.T) {
	a, albums := searchApp(t)
	q := &a.Store.Search
	q.Result.Albums = &client.HitGroup{Items: albums[:6], NextOffset: 6}
	tt := ui.NewTester(a.View, 1280, 800)
	tt.Frame()
	over, ok := tt.Find(albums[0].Name)
	if !ok {
		t.Fatalf("no tile %q in the overview: %q", albums[0].Name, tt.Texts())
	}
	a.focusGroup("albums")
	tt.Frame()
	tab, ok := tt.Find(albums[0].Name)
	if !ok {
		t.Fatalf("no tile %q in the Albums tab: %q", albums[0].Name, tt.Texts())
	}
	if tab.W != over.W {
		t.Errorf("with more to come, the Albums tab's tile is %.1f wide, the overview's %.1f", tab.W, over.W)
	}
}

// Going from one tab to another, the group left has faded out by the time
// the one chosen starts to show: the two never show through each other.
// The window builds a frame more than once, and the group left fades as
// the frame's last build laid it out.
func TestATabLeftIsGoneBeforeTheNextShows(t *testing.T) {
	a, tt, _ := overviewApp(t, true)
	a.Theme = "light"
	tt.SetScale(1)
	tt.Frame()
	if err := tt.Click("Albums"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(2 * time.Second)
	tt.Frame()
	// A point of the second row of album tiles, where the songs, a shorter
	// list, leave the page's ground.
	tile, ok := tt.Find("Late Bloom")
	if !ok {
		t.Fatalf("no second row of albums: %q", tt.Texts())
	}
	x, y := int(tile.X+tile.W/2), int(tile.Y-60)
	at := func() [3]uint8 {
		img := tt.Image()
		o := img.PixOffset(x, y)
		return [3]uint8{img.Pix[o], img.Pix[o+1], img.Pix[o+2]}
	}
	before := at()
	if err := tt.Click("Songs"); err != nil {
		t.Fatal(err)
	}
	var seen [][3]uint8
	for range 12 {
		tt.Advance(refresh)
		seen = append(seen, at())
	}
	tt.Advance(2 * time.Second)
	ground := at()
	if ground == before {
		t.Fatalf("the point %d,%d shows the same before and after: %v", x, y, ground)
	}
	// groupFade is 160 ms: by the eleventh frame the albums are gone.
	if got := seen[10]; got != ground {
		t.Errorf("183 ms after the click the albums still show: %v, the ground is %v (frames %v)", got, ground, seen)
	}
}
