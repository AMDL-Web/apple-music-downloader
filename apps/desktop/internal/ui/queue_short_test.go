package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// A short window keeps three rows of the queue's list in view: the hero
// takes its compact form, and the recent covers go, only as far as that
// needs. The compact cover lines up with the words beside it at the top
// and the bottom, and a page wide enough keeps the percent with Pause and
// Details.
func TestAShortWindowKeepsThreeRowsOfTheQueue(t *testing.T) {
	a, run := demoApp(t)
	run(func() bool { return a.Store.Loaded && a.Store.Now() != nil })
	a.Go(ScreenQueue)
	tt := ui.NewTester(a.View, 1280, 800)
	find := func(s string) ui.Rect {
		t.Helper()
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("no %q: %q", s, tt.Texts())
		}
		return r
	}
	for _, sz := range [][2]int{{1280, 800}, {1280, 690}, {820, 560}} {
		tt.SetSize(sz[0], sz[1])
		for range 10 {
			run(func() bool { return true })
			tt.Advance(50 * time.Millisecond)
		}
		// The third job of Up next.
		if r := find("夏夜信号"); r.Y+r.H > float32(sz[1]) {
			t.Errorf("%v: the third row ends at %.0f, past the window", sz, r.Y+r.H)
		}
		find("Pause")
		find("Details")
		cover := find("Open " + a.Store.Now().Name())
		if cover.X > 300 {
			t.Errorf("%v: the hero's cover is not at its left: %v", sz, cover)
		}
		if sz[1] >= 690 {
			if cover.H != heroCover {
				t.Errorf("%v: the hero's cover is %v, not the full %d", sz, cover, heroCover)
			}
			continue
		}
		// The hero has no label: its title is the top of its words, and
		// the first element showing the job's name.
		top, bottom := find(a.Store.Now().Name()), find(a.heroProgressText(a.Store.Now()))
		if d := top.Y - cover.Y; d < -1 || d > 1 {
			t.Errorf("%v: the compact cover's top is at %.0f, the words' at %.0f", sz, cover.Y, top.Y)
		}
		if d := bottom.Y + bottom.H - cover.Y - cover.H; d < -1 || d > 1 {
			t.Errorf("%v: the compact cover's bottom is at %.0f, the words' at %.0f", sz, cover.Y+cover.H, bottom.Y+bottom.H)
		}
	}
}

// In the light theme the hero's light ends inside the queue's bar, by an
// inward ramp, not at a hard line along its top edge: down a column clear
// of the bar's words, no row differs from the one above by more than a
// few levels.
func TestTheQueueBarLetsTheLightFadeIn(t *testing.T) {
	a, run := demoApp(t)
	a.Theme, a.FlatBackdrop = "light", false
	run(func() bool { return a.Store.Loaded && a.Store.Now() != nil })
	a.Go(ScreenQueue)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	for range 20 {
		run(func() bool { return true })
		tt.Advance(100 * time.Millisecond)
	}
	all, ok := tt.Find("All")
	if !ok {
		t.Fatalf("no filters: %q", tt.Texts())
	}
	img := tt.Image()
	lum := func(x, y int) float32 {
		o := img.PixOffset(x, y)
		return 0.2126*float32(img.Pix[o]) + 0.7152*float32(img.Pix[o+1]) + 0.0722*float32(img.Pix[o+2])
	}
	for _, x := range []int{1000, 1150, 1260} {
		for y := int(all.Y) - 40; y < int(all.Y+all.H)+20; y++ {
			if d := lum(x, y+1) - lum(x, y); d > 4 || d < -4 {
				t.Errorf("at x %d, row %d differs from row %d by %.0f levels: a hard edge", x, y+1, y, d)
				break
			}
		}
	}
}
