//go:build !race

package ui

import (
	"image"
	"slices"
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
	f := switchFilmWatch(t, tt, click, area, image.Rectangle{}, frames)
	return f.before, f.ink
}

// switchShot is a filmed switch: the ink of its area before the click and
// in each frame, the ink of a part of it watched, and how opaque the old
// content and the new show in each frame (layers).
type switchShot struct {
	before        int
	ink, watched  []int
	old, incoming []float32
}

// switchFilmWatch is switchFilm that also films how much of watch shows,
// and how opaque the old content and the new show.
func switchFilmWatch(t *testing.T, tt *ui.Tester, click string, area, watch image.Rectangle, frames int) switchShot {
	t.Helper()
	tt.Frame()
	var f switchShot
	f.before = ink(tt.Image(), area)
	before := areaLuma(tt.Image(), area)
	if err := tt.Click(click); err != nil {
		t.Fatal(err)
	}
	var shown [][]float32
	for range frames {
		tt.Advance(filmFrame)
		tt.Frame()
		f.ink = append(f.ink, ink(tt.Image(), area))
		f.watched = append(f.watched, ink(tt.Image(), watch))
		shown = append(shown, areaLuma(tt.Image(), area))
	}
	after := shown[len(shown)-1]
	for _, v := range shown {
		o, n := layers(before, after, v)
		f.old, f.incoming = append(f.old, o), append(f.incoming, n)
	}
	return f
}

// areaLuma is the luminance of each pixel of r.
func areaLuma(img *image.RGBA, r image.Rectangle) []float32 {
	r = r.Intersect(img.Bounds())
	l := make([]float32, 0, r.Dx()*r.Dy())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			o := img.PixOffset(x, y)
			l = append(l, 0.2126*float32(img.Pix[o])+0.7152*float32(img.Pix[o+1])+0.0722*float32(img.Pix[o+2]))
		}
	}
	return l
}

// layers is how opaque the content before a switch (before) and after it
// (after) show in a frame of it, v: over the pixels where only one of the
// two has content on the dark ground and the other the ground, the median
// of how far v is from the ground there towards that content. -1 where
// too few pixels tell.
func layers(before, after, v []float32) (old, incoming float32) {
	const ground, content = 45, 60
	var o, n []float32
	for i := range v {
		switch {
		case after[i] < ground && before[i]-after[i] > content:
			o = append(o, (v[i]-after[i])/(before[i]-after[i]))
		case before[i] < ground && after[i]-before[i] > content:
			n = append(n, (v[i]-before[i])/(after[i]-before[i]))
		}
	}
	median := func(xs []float32) float32 {
		if len(xs) < 50 {
			return -1
		}
		slices.Sort(xs)
		return xs[len(xs)/2]
	}
	return median(o), median(n)
}

// handOff checks a filmed switch's hand-off from the old content to the
// new: no frame shows both above a tenth, which would show them through
// each other, and no more than two frames show neither.
func handOff(t *testing.T, name string, f switchShot) {
	t.Helper()
	neither := 0
	for i := range f.old {
		o, n := f.old[i], f.incoming[i]
		if o > 0.1 && n > 0.1 {
			t.Errorf("%s: frame %d shows the old content at %.2f and the new at %.2f through each other (old %.2f, new %.2f)", name, i, o, n, f.old, f.incoming)
			return
		}
		if o >= 0 && n >= 0 && o <= 0.1 && n <= 0.1 {
			neither++
		}
	}
	if neither > 2 {
		t.Errorf("%s: %d frames show neither the old content nor the new (old %.2f, new %.2f)", name, neither, f.old, f.incoming)
	}
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
// fill slides: the tab left fades out and the one chosen fades in as soon
// as it has gone, never through it nor after a stretch of frames with
// neither, whether the tab left had hits or only its "No playlists".
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
		// What the tab left says alone, as its "No playlists", goes as
		// quickly as a group does, before the overview shows over it.
		var said image.Rectangle
		if r, ok := tt.Find("No playlists for “glass weather”"); ok {
			said = image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
		}
		f := switchFilmWatch(t, tt, tab, area, said, 40)
		if !said.Empty() && f.watched[3] > 0 {
			t.Errorf("to %s: “No playlists” still shows as the next tab comes in (%v)", tab, f.watched)
		}
		// Into a tab from the overview, the group both show glides, which
		// layers cannot tell from a hand-off.
		if tab != "Albums" {
			handOff(t, "to "+tab, f)
		}
		// The fill's spring, 300 ms, and a few frames for the fade's tail.
		if at := settledBy(f.ink); at > 21 {
			t.Errorf("to %s: the switch settles at frame %d, past 350 ms (%v)", tab, at, f.ink)
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
// filters fade the old content out and the new in on the same frames,
// never through each other nor with a stretch showing neither, and settle
// with the capsule's fill.
func TestEverySwitchMovesAlike(t *testing.T) {
	type shot struct {
		name        string
		f           switchShot
		outAt, inAt int
	}
	var shots []shot
	film := func(name string, tt *ui.Tester, click string, area image.Rectangle) {
		f := switchFilmWatch(t, tt, click, area, image.Rectangle{}, 40)
		out, in := switchShape(f.before, f.ink)
		shots = append(shots, shot{name, f, out, in})
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
		t.Logf("%s: out by frame %d, in by %d: %v; old %.2f, new %.2f", sh.name, sh.outAt, sh.inAt, sh.f.ink, sh.f.old, sh.f.incoming)
		handOff(t, sh.name, sh.f)
		if at := settledBy(sh.f.ink); at > 21 {
			t.Errorf("%s: settles at frame %d, past 350 ms (%v)", sh.name, at, sh.f.ink)
		}
		if d := sh.outAt - shots[0].outAt; d < -1 || d > 1 || sh.inAt-shots[0].inAt < -1 || sh.inAt-shots[0].inAt > 1 {
			t.Errorf("%s: the old content fades by frame %d and the new by %d; the search's by %d and %d (%v, before %d)",
				sh.name, sh.outAt, sh.inAt, shots[0].outAt, shots[0].inAt, sh.f.ink, sh.f.before)
		}
	}
}
