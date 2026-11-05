//go:build !race

package ui

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// coverFilm is the album page of a download opened from nothing on the
// screen under it, or going back to Home, one image per frame at 60 Hz with
// the cover's box in it. With no match, the page's own cover fades in, in
// place.
func coverFilm(t *testing.T, theme string, out bool) (frames []*image.RGBA, covers []ui.Rect, settled *image.RGBA, home ui.Rect) {
	t.Helper()
	a, _ := newTestApp(remoteSettings())
	a.PageMotion, a.Theme, a.Screen = true, theme, ScreenHome
	openFixture(a, detailFixture(time.Now()))
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	for range 90 {
		tt.Advance(filmFrame)
	}
	if !out {
		a.Back()
		tt.Frame()
		for range 90 {
			tt.Advance(filmFrame)
		}
		a.OpenJob(a.Store.Now().ID)
	} else {
		a.Back()
	}
	tt.Frame()
	if !a.fly.moving {
		t.Fatalf("no flight: screen %v", a.Screen)
	}
	for range 60 {
		tt.Advance(filmFrame)
		frames = append(frames, cloneImage(tt.Image()))
		r, _ := tt.Find("Artwork")
		covers = append(covers, r)
		if !a.fly.moving {
			break
		}
	}
	for range 30 {
		tt.Advance(filmFrame)
	}
	settled = cloneImage(tt.Image())
	home, _ = tt.Find("Artwork")
	return frames, covers, settled, home
}

func cloneImage(src *image.RGBA) *image.RGBA {
	d := image.NewRGBA(src.Bounds())
	draw.Draw(d, d.Bounds(), src, src.Bounds().Min, draw.Src)
	return d
}

func luma(img *image.RGBA, x, y int) float64 {
	p := img.RGBAAt(x, y)
	return 0.2126*float64(p.R) + 0.7152*float64(p.G) + 0.0722*float64(p.B)
}

// Coming in, the page's cover is its rounded box and its shadow in every
// frame: nothing is drawn in the box's corners outside the rounding, where
// the screen behind shows, and the shadow is never cut to the box and then
// let out in one frame.
func TestACoverWithNoMatchFadesInWithItsShadow(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			frames, covers, settled, home := coverFilm(t, theme, false)
			var worstCorner, worstShadow, worstPage, landing float64
			var cornerAt, shadowAt, pageAt int
			prev, prevPage := math.NaN(), math.NaN()
			for i, img := range append(frames, settled) {
				if i == len(frames) {
					covers = append(covers, home)
				}
				r := covers[i]
				if r.W == 0 {
					continue
				}
				x0, y0, x1, y1 := int(r.X), int(r.Y), int(r.X+r.W)-1, int(r.Y+r.H)-1
				// A corner pixel outside the rounding against the pixel just
				// across the box's edge: the same ground and shadow, whatever
				// the box is doing.
				for _, c := range [][4]int{{x0 + 1, y0 + 1, x0 + 1, y0 - 2}, {x1 - 1, y0 + 1, x1 - 1, y0 - 2}} {
					if d := math.Abs(luma(img, c[0], c[1]) - luma(img, c[2], c[3])); d > worstCorner {
						worstCorner, cornerAt = d, i
					}
				}
				// The shadow beside the cover, from frame to frame.
				mid := (y0 + y1) / 2
				band := meanLuma(img, image.Rect(x0-24, mid-20, x0-6, mid+20))
				if !math.IsNaN(prev) {
					if d := math.Abs(band - prev); d > worstShadow {
						worstShadow, shadowAt = d, i
					}
					if i == len(frames)-1 {
						landing = math.Abs(band - prev)
					}
				}
				prev = band
				if page := meanLuma(img, img.Bounds()); !math.IsNaN(prevPage) {
					if d := math.Abs(page - prevPage); d > worstPage {
						worstPage, pageAt = d, i
					}
					prevPage = page
				} else {
					prevPage = page
				}
			}
			where := fmt.Sprintf("(%d frames)", len(frames))
			if worstCorner > 4 {
				t.Errorf("a frame draws %.1f levels off across the cover's corner, at frame %d of %s: its box shows outside the rounding", worstCorner, cornerAt, where)
			}
			if worstShadow > 12 || landing > 3 {
				t.Errorf("the cover's shadow steps %.1f levels in a frame, at frame %d, and %.1f in the last of %s: it pops in rather than fades", worstShadow, shadowAt, landing, where)
			}
			if worstPage > 8 {
				t.Errorf("the whole window changes %.1f levels in a frame, at frame %d of %s", worstPage, pageAt, where)
			}
		})
	}
}
