package widgets

import (
	"testing"

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
