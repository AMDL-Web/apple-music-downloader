//go:build !race

package ui

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// riseShow is how strongly r shows in img against ground, the room
// between the field and the tabs: the sum, over r, of how
// far each pixel is from it.
func riseShow(img *image.RGBA, r image.Rectangle, ground image.Rectangle) float32 {
	g := areaLuma(img, ground)
	var mean float32
	for _, v := range g {
		mean += v
	}
	mean /= float32(len(g))
	var sum float32
	for _, v := range areaLuma(img, r) {
		sum += max(v-mean, mean-v)
	}
	return sum
}

// The first key docks home's field at the top, and the tabs and the
// skeleton below it come in as it rises, on its curve: never after it as a
// second step, never at once at full strength.
func TestTheFirstSearchComesInWithTheRise(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			a, _ := demoApp(t)
			a.Screen, a.PageMotion, a.Theme = ScreenHome, true, theme
			tt := ui.NewTester(a.View, 1280, 800)
			tt.SetScale(1)
			tt.Advance(time.Second)
			const field = "Paste links or search Apple Music"
			at, ok := tt.Find(field)
			if !ok {
				t.Fatal("no field on home")
			}
			y0 := at.Y
			// The demo's answer is held back: the read is still out as the
			// field rises, so what shows below it is the skeleton.
			a.Store.SetOmniText("o")
			tt.Frame()
			type shot struct {
				y             float32
				tabs          bool
				img           *image.RGBA
				row, skeleton image.Rectangle
				ground        image.Rectangle
			}
			var film []shot
			for range 24 {
				f, _ := tt.Find(field)
				s := shot{y: f.Y, img: tt.Image()}
				if r, ok := tt.Find("Overview"); ok {
					s.tabs = true
					s.row = image.Rect(int(r.X)-16, int(r.Y)-8, int(r.X)+440, int(r.Y)+25)
					s.skeleton = image.Rect(int(r.X)-16, int(r.Y)+40, int(r.X)+940, int(r.Y)+300)
					s.ground = image.Rect(int(r.X), int(r.Y)-24, int(r.X)+900, int(r.Y)-16)
				}
				film = append(film, s)
				tt.Advance(time.Second / 60)
			}
			end := film[len(film)-1]
			if end.y >= y0 {
				t.Fatalf("the field stays at %v of %v", end.y, y0)
			}
			bare := 0
			for i, s := range film {
				if rise := (y0 - s.y) / (y0 - end.y); rise > 0.02 && !s.tabs {
					t.Errorf("frame %d: the field has risen %.2f of the way with nothing below it", i, rise)
					bare++
				}
			}
			if bare > 0 {
				return
			}
			// The tabs show as the whole of it does; the skeleton's blocks
			// breathe (skeleton) and only have to show.
			full := riseShow(end.img, end.row, end.ground)
			for i, s := range film {
				rise := (y0 - s.y) / (y0 - end.y)
				if rise <= 0.02 {
					continue
				}
				show := riseShow(s.img, s.row, s.ground) / full
				if d := show - rise; d > 0.15 || d < -0.15 {
					t.Errorf("frame %d: the field has risen %.2f of the way, and the tabs show at %.2f", i, rise, show)
				}
				if sk := riseShow(s.img, s.skeleton, s.ground) / riseShow(end.img, end.skeleton, end.ground); sk < 0.5*rise {
					t.Errorf("frame %d: the field has risen %.2f of the way, and the skeleton shows at %.2f", i, rise, sk)
				}
			}
		})
	}
}
