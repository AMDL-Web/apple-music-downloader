//go:build !race

// The album page comes and goes frame by frame on the Tester's clock, which
// the race detector slows many times over with no concurrency to check.

package ui

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// resultsApp is home over the demo backend showing its results for "glass
// weather", with the album page's motion on.
func resultsApp(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	a, run := demoApp(t)
	a.PageMotion = true
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenHome)
	tt := ui.NewTester(a.View, 1280, 800)
	// Boxes are in DIPs whatever the scale; fewer pixels draw faster.
	tt.SetScale(0.5)
	tt.Type("glass weather")
	run(func() bool {
		tt.Advance(refresh)
		return s.Search.Status == store.SearchReady && s.Search.Result != nil
	})
	run(func() bool { tt.Advance(refresh); return a.Art.Get(s.Search.ShownHits()[0].ArtworkURL, 512) != nil })
	tt.Advance(time.Second)
	return a, tt
}

// An album opened, left and opened again comes in at one click with
// nothing else drawing meanwhile, as when what it shows is cached: the
// store's answers wait here.
func TestAnAlbumLeftOpensAgainAtOneClick(t *testing.T) {
	a, tt := resultsApp(t)
	for i := 1; i <= 2; i++ {
		if err := tt.Click("Glass Weather"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(time.Second)
		if a.Screen != ScreenAlbum || a.fly.q != 1 || a.fly.moving {
			t.Fatalf("open %d: a second after one click the page is %v in, moving %v", i, a.fly.q, a.fly.moving)
		}
		tt.Key(0, ui.KeyEscape)
		tt.Advance(time.Second)
		if a.Screen != ScreenHome || a.fly.moving {
			t.Fatalf("back %d: a second after Escape the screen is %v, moving %v", i, a.Screen, a.fly.moving)
		}
	}
}

// Back on the results, the album the page showed is where the keys go on
// from without a ring round its tile; the keys choosing it ring it.
func TestTheAlbumBackFromIsNotRinged(t *testing.T) {
	a, tt := resultsApp(t)
	r, ok := tt.Find("Glass Weather")
	if !ok {
		t.Fatalf("no tile in %q", tt.Texts())
	}
	tt.Move(4, 400)
	tt.Advance(time.Second)
	rest := ringBand(tt, r)
	if err := tt.Click("Glass Weather"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Key(0, ui.KeyEscape)
	tt.Move(4, 400)
	tt.Advance(time.Second)
	if a.searchSel != store.HitKey(a.Store.Search.Album.Hit) {
		t.Fatalf("back on the results the keys go on from %q", a.searchSel)
	}
	if d := bandDiff(rest, ringBand(tt, r)); d > 0 {
		t.Errorf("back from its page, %d pixels round the album's tile changed", d)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyUp)
	tt.Advance(time.Second)
	if a.searchSel != store.HitKey(a.Store.Search.Album.Hit) {
		t.Fatalf("the keys chose %q", a.searchSel)
	}
	if d := bandDiff(rest, ringBand(tt, r)); d == 0 {
		t.Errorf("chosen by the keys, the album's tile shows no ring")
	}
}

// ringBand is the window's pixels in a band just outside r, where a focus
// ring draws.
func ringBand(tt *ui.Tester, r ui.Rect) *image.RGBA {
	img := tt.Image()
	s := float32(img.Bounds().Dx()) / 1280
	box := image.Rect(int((r.X-6)*s), int((r.Y-6)*s), int((r.X+r.W+6)*s), int((r.Y+r.H+6)*s))
	out := image.NewRGBA(box)
	inner := image.Rect(int((r.X+2)*s), int((r.Y+2)*s), int((r.X+r.W-2)*s), int((r.Y+r.H-2)*s))
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if !(image.Point{x, y}).In(inner) {
				out.Set(x, y, img.At(x, y))
			}
		}
	}
	return out
}

// bandDiff counts the pixels of two bands that differ by more than a few
// levels in a channel.
func bandDiff(a, b *image.RGBA) int {
	n := 0
	for i := 0; i+3 < len(a.Pix) && i+3 < len(b.Pix); i += 4 {
		for c := range 3 {
			if d := int(a.Pix[i+c]) - int(b.Pix[i+c]); d > 6 || d < -6 {
				n++
				break
			}
		}
	}
	return n
}
