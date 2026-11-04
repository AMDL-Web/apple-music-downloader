//go:build !race

package ui

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The queue's recent covers stay drawn in every frame after the hero's cover
// is clicked: while the page is laid out hidden, before it flies, the
// screen under it shows as it was (screens), its recent covers too. The
// maintainer's recording of 2026-11-04 21:51 shows them gone for the one
// frame after the click. The Tester runs the frames of one instant
// together, so each is read as the next one starts.
func TestTheRecentCoversStayInEveryFrameAfterTheHerosClick(t *testing.T) {
	a, run := demoApp(t)
	a.PageMotion = true
	a.Theme = "light"
	s := a.Store
	run(func() bool { return s.Loaded && (s.Now() != nil || len(s.Finished()) > 0) })
	a.Go(ScreenQueue)
	type point struct{ x, y int }
	var (
		tt       *ui.Tester
		wall     []point
		rest     [][3]uint8
		watching bool
		frames   int
		bad      string
		state    string
		shown    float32 = 1
	)
	view := func(c *ui.Context) {
		// Past the start of the way the covers sink as they should.
		if watching && bad == "" && shown > 0.99 {
			// The frame before, as the window showed it.
			frames++
			img := tt.Image()
			for i, p := range wall {
				px := img.RGBAAt(p.x, p.y)
				if max(absDiff(px.R, rest[i][0]), absDiff(px.G, rest[i][1]), absDiff(px.B, rest[i][2])) > 3 {
					bad = state
					break
				}
			}
		}
		a.View(c)
		shown = 1
		if a.fly.moving {
			shown = a.fly.wallShown()
		}
		state = fmt.Sprintf("moving %v, page ready %v, q %.3f, wallShown %.3f", a.fly.moving, a.fly.ready, a.fly.q, shown)
	}
	tt = ui.NewTester(view, 1280, 800)
	tt.SetScale(1)
	tt.Frame()
	j := a.heroJob()
	if j == nil {
		t.Fatal("the queue has no hero")
	}
	for _, r := range s.Finished() {
		if r.ID == j.ID {
			continue
		}
		if b, ok := tt.Find(r.Name()); ok && b.W < 120 {
			wall = append(wall, point{int(b.X + b.W/2), int(b.Y + b.H/2)})
		}
	}
	if len(wall) == 0 {
		t.Fatalf("no recent cover beside the hero in %q", tt.Texts())
	}
	for _, p := range wall {
		px := tt.Image().RGBAAt(p.x, p.y)
		rest = append(rest, [3]uint8{px.R, px.G, px.B})
	}
	watching = true
	if err := tt.Click("Open " + j.Name()); err != nil {
		t.Fatal(err)
	}
	for range 6 {
		tt.Advance(refresh)
	}
	watching = false
	if bad != "" {
		t.Errorf("a frame after the click drew the recent covers otherwise than at rest; in it: %s", bad)
	}
	if frames < 2 {
		t.Fatalf("read %d frames after the click: the test measures nothing", frames)
	}
}
