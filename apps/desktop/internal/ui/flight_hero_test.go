//go:build !race

package ui

import (
	"image"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// lumaStd is how much the luma varies over r: a word or a control drawn
// there, none on a plain ground.
func lumaStd(img *image.RGBA, r image.Rectangle) float64 {
	s, s2, n := 0.0, 0.0, 0.0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			l := luma(img, x, y)
			s, s2, n = s+l, s2+l*l, n+1
		}
	}
	m := s / n
	return math.Sqrt(max(0, s2/n-m*m))
}

func pixels(r ui.Rect) image.Rectangle {
	return image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
}

// Opening the album page from the queue hero, the page's Pause in its title
// bar does not show while the hero, with its own Pause, still does: in no
// frame, watched at a quarter of the speed, do both the hero's percent,
// which fades with its Pause, and the title bar's Pause show.
func TestTheHerosPauseAndThePagesNeverShowTogether(t *testing.T) {
	a, tt, j := flightApp(t)
	tt.SetScale(1)
	for range 30 {
		tt.Advance(50 * time.Millisecond)
	}
	pct, ok := tt.Find("33")
	if !ok {
		t.Fatalf("no percent in the hero: %q", tt.Texts())
	}
	hero := pixels(pct)
	rest := cloneImage(tt.Image())
	a.tune.load()
	a.tune.Slow = 0.25
	if err := tt.Click("Open " + j.Name()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	var frames []*image.RGBA
	for range 240 {
		tt.Advance(filmFrame)
		if !a.fly.moving {
			break
		}
		frames = append(frames, cloneImage(tt.Image()))
	}
	tt.Advance(time.Second)
	if a.Screen != ScreenAlbum || a.fly.moving {
		t.Fatalf("the page did not land: screen %v", a.Screen)
	}
	pause, ok := tt.Find("Pause")
	if !ok || pause.Y > 60 {
		t.Fatalf("no Pause in the page's title bar: %v", pause)
	}
	bar := pixels(pause)
	landed := tt.Image()
	shown := func(img *image.RGBA, r image.Rectangle, off, on *image.RGBA) float64 {
		lo, hi := lumaStd(off, r), lumaStd(on, r)
		return (lumaStd(img, r) - lo) / (hi - lo)
	}
	for i, f := range frames {
		h, b := shown(f, hero, landed, rest), shown(f, bar, rest, landed)
		if h > 0.005 && b > 0.005 {
			t.Errorf("frame %d: the hero shows %.1f%% and the page's Pause %.1f%% at once", i, 100*h, 100*b)
		}
	}
}
