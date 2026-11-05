package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// A page growing from a search tile brings its ground at once: in the first
// frame that moves, the tile's words just under its cover are already under
// the page's opaque ground, with under a quarter of the detail they show at
// rest, rather than the cover flying on its own before the page shows.
func TestAGrowingPagesGroundShowsInItsFirstFrame(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		a, albums := searchApp(t)
		a.PageMotion, a.Theme = true, theme
		tt := ui.NewTester(a.View, 1280, 800)
		tt.SetScale(1)
		for range 10 {
			tt.Advance(filmFrame)
		}
		// The tile's cover is named after its album.
		cover, ok := tt.Find(albums[0].Name)
		if !ok {
			t.Fatalf("%s: no tile", theme)
		}
		under := pixels(ui.Rect{X: cover.X, Y: cover.Y + cover.H + 3, W: cover.W, H: 12})
		rest := lumaStd(tt.Image(), under)
		if rest < 8 {
			t.Fatalf("%s: no words under the cover at rest (detail %.1f)", theme, rest)
		}
		if err := tt.Click(albums[0].Name); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Advance(filmFrame)
		if !a.fly.moving || !a.fly.grow {
			t.Fatalf("%s: the page is not growing in the first frame: moving %v, grow %v", theme, a.fly.moving, a.fly.grow)
		}
		if at := lumaStd(tt.Image(), under); at > rest/4 {
			t.Errorf("%s: in the first frame (q %.3f) the words under the cover show with detail %.1f of %.1f at rest: the page's ground is not there yet", theme, a.fly.q, at, rest)
		}
	}
}
