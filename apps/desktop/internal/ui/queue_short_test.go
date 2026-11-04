package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// In a short window the queue's hero takes its compact form, as in a narrow
// one, so the list under it keeps the room: the 820×560 window, whose
// sidebar closes and leaves the page wide, as much as a narrow one.
func TestAShortWindowCompactsTheQueueHero(t *testing.T) {
	a, run := demoApp(t)
	run(func() bool { return a.Store.Loaded && a.Store.Now() != nil })
	a.Go(ScreenQueue)
	tt := ui.NewTester(a.View, 1280, 800)
	cover := func() ui.Rect {
		for range 10 {
			run(func() bool { return true })
			tt.Advance(50 * time.Millisecond)
		}
		r, ok := tt.Find("Open " + a.Store.Now().Name())
		if !ok {
			t.Fatalf("no hero cover: %q", tt.Texts())
		}
		return r
	}
	if r := cover(); r.H != heroCover {
		t.Fatalf("in a 1280×800 window the hero's cover is %v, not %d high", r, heroCover)
	}
	tt.SetSize(820, 560)
	if r := cover(); r.H != heroCoverCompact {
		t.Errorf("in an 820×560 window the hero's cover is %v, not the compact %d", r, heroCoverCompact)
	}
}

// In the light theme the hero's light ends inside the queue's bar, by an
// inward ramp, not at a hard line along its top edge: down a column clear
// of the bar's words, no row differs from the one above by more than a
// few levels.
func TestTheQueueBarLetsTheLightFadeIn(t *testing.T) {
	a, run := demoApp(t)
	a.Theme = "light"
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
