//go:build !race

package ui

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// moved says which element showing text in both frames changed its box, or
// returns "". A lift, an element scaled up about its center a few percent
// by a transform, moves nothing around it and is allowed; the texts of a
// menu or a tooltip the pointer opens are not compared.
func moved(now, rest frameSnap) string {
	for text, r := range rest.rects {
		o, ok := now.rects[text]
		if !ok || (abs32(r.X-o.X) <= 0.5 && abs32(r.Y-o.Y) <= 0.5 && abs32(r.W-o.W) <= 0.5 && abs32(r.H-o.H) <= 0.5) {
			continue
		}
		cx, cy, ox, oy := r.X+r.W/2, r.Y+r.H/2, o.X+o.W/2, o.Y+o.H/2
		sw, sh := o.W/r.W, o.H/r.H
		if r.W > 0 && r.H > 0 && abs32(cx-ox) <= 0.5 && abs32(cy-oy) <= 0.5 && abs32(sw-sh) < 0.002 && sw < 1.05 {
			continue
		}
		return fmt.Sprintf("the box of %q: %v vs %v", text, r, o)
	}
	return ""
}

// hoverSweep rests the pointer on every element of the screen in turn and
// reports each one whose box, or any other element's, moved against the
// pointer at rest. Hover may recolor, never move. It only moves the
// pointer: pressing starts drags and clicks change the screen.
func hoverSweep(t *testing.T, tt *ui.Tester) {
	t.Helper()
	const restX, restY = 3, 3
	tt.Move(restX, restY)
	tt.Frame()
	rest := snapFrame(tt)
	seen := map[ui.Rect]bool{}
	for _, text := range rest.texts {
		r, ok := rest.rects[text]
		if !ok || seen[r] || r.W <= 0 || r.H <= 0 {
			continue
		}
		seen[r] = true
		x, y := r.X+r.W/2, r.Y+r.H/2
		if x < 0 || y < 0 || x >= 1279 || y >= 799 {
			continue
		}
		tt.Move(x, y)
		tt.Frame()
		if d := moved(snapFrame(tt), rest); d != "" {
			t.Errorf("the pointer on %q moves things: %s", text, d)
		}
	}
	tt.Move(restX, restY)
	tt.Frame()
}

func TestPointerNeverMovesTheQueue(t *testing.T) {
	a, run := demoApp(t)
	a.Screen = ScreenQueue
	run(func() bool { return a.Store.Loaded })
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	a.Tab = int(store.TabAll)
	tt.Frame()
	hoverSweep(t, tt)
}

func TestPointerNeverMovesSettings(t *testing.T) {
	a, tt, _ := hooksApp(t)
	for _, page := range []string{"quality", "lyrics_artwork", "hooks"} {
		a.GoSettings(page)
		for range 4 {
			tt.Frame()
		}
		hoverSweep(t, tt)
	}
}
