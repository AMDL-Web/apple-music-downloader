package widgets

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// A screen without a palette has a transparent light; the mix towards it
// fades the other screen's light without darkening it, as a fade on screen
// does, and ends exactly on either side.
func TestTheMixedLightKeepsItsHue(t *testing.T) {
	pink := ui.RGBA(240, 80, 160, 0.6)
	half := premix(ui.Transparent, pink, 0.5)
	if half.R != pink.R || half.G != pink.G || half.B != pink.B {
		t.Errorf("half way from no light to %v is %v, another hue", pink, half)
	}
	if d := int(half.A) - int(pink.A)/2; d < -1 || d > 1 {
		t.Errorf("half way from no light to alpha %d is alpha %d", pink.A, half.A)
	}
	blue := ui.RGBA(40, 60, 200, 0.6)
	if got := premix(blue, pink, 0); got != blue {
		t.Errorf("the mix starts at %v, not %v", got, blue)
	}
	if got := premix(blue, pink, 1); got != pink {
		t.Errorf("the mix ends at %v, not %v", got, pink)
	}
}

// The light's slow gradients show no steps: across the deep disc's edge,
// on a dark ground, no level holds for more than a few pixels, as levels
// quantized without a dither hold for tens.
func TestTheLightShowsNoSteps(t *testing.T) {
	bg := ui.Hex("#3a4a8c")
	b := Backdrop{Ground: ui.Hex("#111219"), Light: bg, Deep: bg.Mix(ui.Hex("#05081f"), 0.65), Strength: 0.55, Height: 420}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(b.Ground).Children(func() { b.Show(c) })
	}, 900, 420)
	tt.Advance(time.Second)
	img := tt.Image()
	for ch := range 3 {
		longest, run, prev := 0, 0, -1
		for x := 300; x < 880; x++ {
			v := int(img.Pix[img.PixOffset(x, 40)+ch])
			if v == prev {
				run++
			} else {
				run = 1
			}
			longest, prev = max(longest, run), v
		}
		if longest > 8 {
			t.Errorf("channel %d holds a level for %d pixels across the deep disc's edge", ch, longest)
		}
	}
}
