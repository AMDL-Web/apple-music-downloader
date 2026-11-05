//go:build !race

package ui

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// rowStep is the largest change of luminance from one row of img to the
// next at x, between rows top and bottom.
func rowStep(img *image.RGBA, x, top, bottom int) (at int, step float32) {
	lum := func(y int) float32 {
		o := img.PixOffset(x, y)
		return 0.2126*float32(img.Pix[o]) + 0.7152*float32(img.Pix[o+1]) + 0.0722*float32(img.Pix[o+2])
	}
	for y := top; y < bottom; y++ {
		if d := lum(y+1) - lum(y); max(d, -d) > step {
			at, step = y+1, max(d, -d)
		}
	}
	return at, step
}

// The queue's light fades out as the Library comes in, over the title bar
// and the Library alike: the band the title bar leaves empty and the
// Library below it are one ground, with no edge between them.
func TestTheQueueLightFadesOverTheLibraryUncut(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			a, _, run := bulkApp(t, 60, 1280)
			a.Theme, a.PageMotion = theme, true
			// The sidebar shows the Library once an account with it is read.
			run(func() bool { return a.Store.Loaded && a.Store.Now() != nil && a.Store.AppleLibraryOn() })
			a.Go(ScreenQueue)
			tt := ui.NewTester(a.View, 1280, 800)
			tt.SetScale(1)
			for range 20 {
				n := 0
				run(func() bool { n++; return n > 3 })
				tt.Advance(100 * time.Millisecond)
			}
			if err := tt.Click("Library"); err != nil {
				t.Fatal(err)
			}
			// Past the queue's content fading out, the rows from the top to
			// the Library's first covers hold nothing at these columns but
			// the ground and the light leaving it.
			tt.Advance(150 * time.Millisecond)
			for i := range 30 {
				img := tt.Image()
				for _, x := range []int{700, 850} {
					if at, step := rowStep(img, x, 4, 140); step > 3 {
						t.Errorf("frame %d, x %d: row %d differs from the row above by %.0f levels: an edge across the light", i, x, at, step)
						return
					}
				}
				tt.Advance(time.Second / 60)
			}
		})
	}
}
