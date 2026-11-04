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
