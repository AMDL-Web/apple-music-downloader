package ui

import (
	"image"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The page of the download in the sidebar's card grows out of the card and
// shrinks back into it: the page's cover is seen over the sidebar above
// the card as it leaves, and the card, which stays in its place, is just
// as it was the moment the page has landed back on it.
func TestThePageGrowsFromTheSidebarsCardAndBack(t *testing.T) {
	a, run := demoApp(t)
	a.PageMotion, a.Theme = true, "light"
	s := a.Store
	run(func() bool { return s.Loaded && s.Now() != nil })
	a.Go(ScreenHome)
	tt := ui.NewTester(a.View, 1300, 838)
	tt.SetScale(1)
	for range 30 {
		tt.Advance(50 * time.Millisecond)
	}
	card, ok := tt.Find("Downloading now")
	if !ok {
		t.Fatal("no card")
	}
	above := image.Rect(int(card.X), int(card.Y)-120, int(card.X+card.W), int(card.Y)-4)
	dark := func(img *image.RGBA, r image.Rectangle) (n int) {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if luma(img, x, y) < 90 {
					n++
				}
			}
		}
		return n
	}
	if err := tt.Click("Downloading now"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	seen := 0
	var last *image.RGBA
	film := func(dir string) {
		for i := range 120 {
			tt.Advance(filmFrame)
			img := tt.Image()
			if i < 8 {
				seen = max(seen, dark(img, above))
			}
			last = cloneImage(img)
			if !a.fly.moving {
				return
			}
		}
		t.Fatalf("%s: still moving after 2 s", dir)
	}
	film("in")
	if seen < 200 {
		t.Errorf("the page's cover shows over the sidebar in %d pixels as it leaves the card: it does not grow out of it", seen)
	}
	tt.Advance(time.Second)
	a.Back()
	tt.Frame()
	film("out")
	tt.Advance(time.Second)
	box := image.Rect(int(card.X), int(card.Y), int(card.X+card.W), int(card.Y+card.H))
	if d := math.Abs(meanLuma(last, box) - meanLuma(tt.Image(), box)); d > 3 {
		t.Errorf("the card changes %.1f levels after the page has landed on it", d)
	}
}
