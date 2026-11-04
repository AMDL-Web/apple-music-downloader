//go:build !race

package ui

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// switchFilm clicks the segment showing click and films the switch at 60
// Hz, frames frames: for each, how much of area shows, as the pixels of
// text and covers on the dark theme's ground (ink), after the frame before
// the click.
func switchFilm(t *testing.T, tt *ui.Tester, click string, area image.Rectangle, frames int) (before int, film []int) {
	t.Helper()
	tt.Frame()
	before = ink(tt.Image(), area)
	if err := tt.Click(click); err != nil {
		t.Fatal(err)
	}
	for range frames {
		tt.Advance(filmFrame)
		tt.Frame()
		film = append(film, ink(tt.Image(), area))
	}
	return before, film
}

// ink counts the bright pixels of r, text and covers over a dark ground.
func ink(img *image.RGBA, r image.Rectangle) int {
	n := 0
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			o := img.PixOffset(x, y)
			if 0.2126*float32(img.Pix[o])+0.7152*float32(img.Pix[o+1])+0.0722*float32(img.Pix[o+2]) > 110 {
				n++
			}
		}
	}
	return n
}

// blankRun is the longest run of frames of film that show less than a
// tenth of the less of before and the last frame: the time a switch shows
// nothing.
func blankRun(before int, film []int) int {
	floor := min(before, film[len(film)-1]) / 10
	longest, run := 0, 0
	for _, n := range film {
		if n < floor {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// settledBy is the first frame of film from which it no longer changes by
// more than a hundredth of the last frame.
func settledBy(film []int) int {
	last := film[len(film)-1]
	at := len(film)
	for i := len(film) - 1; i >= 0; i-- {
		d := film[i] - last
		if d < 0 {
			d = -d
		}
		if d > last/100+1 {
			break
		}
		at = i
	}
	return at
}

// A change of the search's tab goes in one move, in the time the capsule's
// fill slides: the tab left fades out and the one chosen fades in at once
// after it, never a stretch of frames with neither past the two fades'
// crossing, three frames, whether the tab left had hits or only its "No
// playlists".
func TestASearchTabSwitchShowsNoGap(t *testing.T) {
	a, run := demoApp(t)
	a.Screen, a.PageMotion, a.Theme = ScreenHome, true, "dark"
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	s := a.Store
	s.SetOmniText("glass weather")
	tt.Frame()
	run(func() bool { tt.Frame(); return s.Search.Status == store.SearchReady })
	run(func() bool {
		tt.Advance(50 * time.Millisecond)
		return a.Art.Get(s.Search.ShownHits()[0].ArtworkURL, 512) != nil
	})
	tt.Advance(time.Second)
	tabs, ok := tt.Find("Overview")
	if !ok {
		t.Fatalf("no tabs: %q", tt.Texts())
	}
	area := image.Rect(int(tabs.X), int(tabs.Y+tabs.H+8), 1280-24, 800-24)
	for _, tab := range []string{"Albums", "Songs", "Playlists", "Overview"} {
		before, film := switchFilm(t, tt, tab, area, 40)
		if n := blankRun(before, film); n > 3 {
			t.Errorf("to %s: %d frames show neither tab (%v, before %d)", tab, n, film, before)
		}
		// The fill's spring, 300 ms, and a few frames for the fade's tail.
		if at := settledBy(film); at > 24 {
			t.Errorf("to %s: the switch settles at frame %d, past 400 ms (%v)", tab, at, film)
		}
		tt.Advance(time.Second)
	}
}

// switchShape is when a filmed switch's old content has faded below half
// of what it showed before (out) and its new content risen past half of
// what it ends on (in), in frames.
func switchShape(before int, film []int) (out, in int) {
	out, low := -1, 0
	for i, n := range film {
		if out < 0 && n < before/2 {
			out = i
		}
		if n < film[low] {
			low = i
		}
	}
	last := film[len(film)-1]
	in = -1
	for i := low; i < len(film); i++ {
		if film[i]-film[low] >= (last-film[low])/2 {
			in = i
			break
		}
	}
	return out, in
}

// startsWith is the first text of tt that starts with prefix, as a
// segment whose label carries a count.
func startsWith(t *testing.T, tt *ui.Tester, prefix string) string {
	t.Helper()
	for _, x := range tt.Texts() {
		if len(x) >= len(prefix) && x[:len(prefix)] == prefix {
			return x
		}
	}
	t.Fatalf("no %q…: %q", prefix, tt.Texts())
	return ""
}

// Changing what a screen shows with its segmented control moves alike on
// every screen: the search's tabs, the Library's kinds and the queue's
// filters fade the old content out and the new in on the same frames, with
// no stretch showing neither, and settle with the capsule's fill.
func TestEverySwitchMovesAlike(t *testing.T) {
	type shot struct {
		name        string
		before      int
		film        []int
		outAt, inAt int
	}
	var shots []shot
	film := func(name string, tt *ui.Tester, click string, area image.Rectangle) {
		before, f := switchFilm(t, tt, click, area, 40)
		out, in := switchShape(before, f)
		shots = append(shots, shot{name, before, f, out, in})
	}

	// The search, from Albums to Songs.
	a, run := demoApp(t)
	a.Screen, a.PageMotion, a.Theme = ScreenHome, true, "dark"
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	s := a.Store
	s.SetOmniText("glass weather")
	tt.Frame()
	run(func() bool { tt.Frame(); return s.Search.Status == store.SearchReady })
	run(func() bool {
		tt.Advance(50 * time.Millisecond)
		return a.Art.Get(s.Search.ShownHits()[0].ArtworkURL, 512) != nil
	})
	if err := tt.Click("Albums"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tabs, _ := tt.Find("Overview")
	film("search", tt, "Songs", image.Rect(int(tabs.X), int(tabs.Y+tabs.H+8), 1280-24, 800-24))

	// The Library, from Albums to Songs, each kind read once before.
	a, run = demoApp(t)
	a.PageMotion, a.Theme = true, "dark"
	run(func() bool { return a.Store.Loaded })
	a.Go(ScreenLibrary)
	tt = ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	l := &a.Store.Library
	read := func(kind string) {
		if kind != "Albums" || l.AppleStatus == store.SearchReady {
			if err := tt.Click(kind); err != nil {
				t.Fatal(err)
			}
		}
		run(func() bool { tt.Advance(50 * time.Millisecond); return l.AppleStatus == store.SearchReady })
		for range 20 {
			run(func() bool { return true })
			tt.Advance(50 * time.Millisecond)
		}
	}
	tt.Frame()
	for _, kind := range []string{"Albums", "Songs", "Albums"} {
		read(kind)
	}
	seg, _ := tt.Find("Albums")
	film("library", tt, "Songs", image.Rect(0+int(seg.X)-12, int(seg.Y+seg.H+12), 1280-24, 800-24))

	// The queue, from All to Done, each filter read once before.
	a, run = demoApp(t)
	a.PageMotion, a.Theme = true, "dark"
	run(func() bool { return a.Store.Loaded && a.heroJob() != nil })
	a.Go(ScreenQueue)
	tt = ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	settle := func() {
		for range 20 {
			run(func() bool { return true })
			tt.Advance(50 * time.Millisecond)
		}
	}
	settle()
	for _, f := range []string{"Done", "All"} {
		if err := tt.Click(startsWith(t, tt, f)); err != nil {
			t.Fatal(err)
		}
		settle()
	}
	all, _ := tt.Find("All")
	area := image.Rect(int(all.X)-12, int(all.Y+all.H+12), 1280-24, 800-24)
	film("queue", tt, startsWith(t, tt, "Done"), area)

	// A filter chosen while the last switch still runs goes out as quickly:
	// the list coming in fades out before the next shows, not through it.
	tt.Advance(time.Second)
	if err := tt.Click("All"); err != nil {
		t.Fatal(err)
	}
	for range 6 {
		tt.Advance(filmFrame)
		tt.Frame()
	}
	before, again := switchFilm(t, tt, startsWith(t, tt, "Failed"), area, 12)
	if shows := max(again[1], again[2]); shows > before/10 {
		t.Errorf("chosen again 100 ms into a switch, the list coming in still shows %d of %d two frames later (%v)", shows, before, again)
	}

	for _, sh := range shots {
		t.Logf("%s: out by frame %d, in by %d: %v", sh.name, sh.outAt, sh.inAt, sh.film)
		if n := blankRun(sh.before, sh.film); n > 3 {
			t.Errorf("%s: %d frames show neither (%v, before %d)", sh.name, n, sh.film, sh.before)
		}
		if at := settledBy(sh.film); at > 24 {
			t.Errorf("%s: settles at frame %d, past 400 ms (%v)", sh.name, at, sh.film)
		}
		if d := sh.outAt - shots[0].outAt; d < -1 || d > 1 || sh.inAt-shots[0].inAt < -1 || sh.inAt-shots[0].inAt > 1 {
			t.Errorf("%s: the old content fades by frame %d and the new by %d; the search's by %d and %d (%v, before %d)",
				sh.name, sh.outAt, sh.inAt, shots[0].outAt, shots[0].inAt, sh.film, sh.before)
		}
	}
}
