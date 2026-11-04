package ui

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// A switch, a segmented control choosing what a screen shows (the search's
// tabs, the Library's kinds, the queue's filters), changes it one way on
// every screen, in the time the capsule's fill slides (segmentSlide): what
// goes fades out at once (switchOut); what comes fades in where it lands
// as that is all but gone (switchIn), so the two never show through each
// other; what both show glides to its new place with the fill
// (switchGlide). The fades ease out, as a spring from rest would start too
// slowly and leave frames that show neither.
var (
	switchOut   = ui.Animation{Duration: 90 * time.Millisecond, Ease: ui.EaseOut}
	switchIn    = ui.Animation{Duration: 200 * time.Millisecond, Ease: ui.EaseOut, Delay: 40 * time.Millisecond}
	switchGlide = segmentSlide
	// switchSettle is how long after a switch what comes in is still
	// given switchIn: past it, the screen's parts go out with switchOut
	// at the next switch.
	switchSettle = segmentSlide.Spring.Duration + 100*time.Millisecond
)

// switching reports whether a switch made at at is still under way, and
// asks for the frame that ends it, which records what goes out next.
func switching(c *ui.Context, at time.Time) bool {
	since := c.Now().Sub(at)
	if since < 0 || since >= switchSettle {
		return false
	}
	c.After(switchSettle - since)
	return true
}

// fresh reports whether el is new in this frame: no frame before laid it
// out.
func fresh(el ui.Element) bool { return el.LayoutBounds().W <= 0 }
