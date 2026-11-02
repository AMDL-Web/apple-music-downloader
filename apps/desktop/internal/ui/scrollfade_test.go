package ui

import (
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// A list's edge with more beyond it fades out to whatever is behind the
// list, here a blue the list never names: the bottom while more is below,
// the top once it scrolled.
func TestAListFadesAtTheEdgesWithMoreBeyond(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.EdgeFades = true
	red, blue := ui.RGB(220, 30, 30), ui.RGB(30, 60, 220)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().FillHeight().Background(blue).Children(func() {
			a.scrollArea(c, "t", 0, func() {
				ui.Box(c).FillWidth().Height(2000).Background(red)
			}).Grow(1).FillWidth()
		})
	}, 300, 300)
	tt.Advance(time.Second)
	near := func(px color.RGBA, c ui.Color) bool {
		return absDiff(px.R, c.R) <= 12 && absDiff(px.G, c.G) <= 12 && absDiff(px.B, c.B) <= 12
	}
	at := func(y int) color.RGBA {
		tt.Frame()
		img := tt.Image()
		s := float32(img.Bounds().Dy()) / 300
		return img.RGBAAt(int(150*s), int(float32(y)*s))
	}
	if top, mid, bottom := at(1), at(150), at(299); !near(top, red) || !near(mid, red) || !near(bottom, blue) {
		t.Errorf("at the top, the list shows %v at its top, %v amid, %v at its bottom", top, mid, bottom)
	}
	tt.Wheel(150, 150, 0, 400)
	tt.Advance(time.Second)
	if top, mid := at(1), at(150); !near(top, blue) || !near(mid, red) {
		t.Errorf("scrolled to %v, the list shows %v at its top, %v amid", a.scrolls["t"].Y, top, mid)
	}
}
