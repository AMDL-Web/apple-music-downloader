package ui

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// A switch, a segmented control choosing what a screen shows (the search's
// tabs, the Library's kinds, the queue's filters), changes it one way on
// every screen, in the time the capsule's fill slides (segmentSlide): what
// goes fades out at once (switchOut); what comes fades in where it lands
// once that is gone (switchIn), so the two never show through each other;
// what both show glides to its new place with the fill (switchGlide). The
// fades ease out, as a spring from rest would start too slowly and leave
// frames that show neither. switchIn waits until switchOut's cubic has
// left under a twentieth: (1-55/90)³ ≈ 0.06.
var (
	switchOut   = ui.Animation{Duration: 90 * time.Millisecond, Ease: ui.EaseOut}
	switchIn    = ui.Animation{Duration: 200 * time.Millisecond, Ease: ui.EaseOut, Delay: 55 * time.Millisecond}
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

// switchMotion is the Transition of el, what a switch made at at shows as
// a whole, keyed by the choice and the state it is in: the Library's list
// of a kind, the queue's list of a filter, or what stands for either while
// there is none. Only el's first frame is given switchIn, which it enters
// with: one chosen again before the switch settles goes out with
// switchOut. Outside a switch, what comes shows at once. It is none
// without page motion.
func (a *App) switchMotion(c *ui.Context, el ui.Element, at time.Time) ui.ElementTransition {
	switch {
	case !a.PageMotion:
		return ui.ElementTransition{}
	case switching(c, at) && fresh(el):
		return ui.ElementTransition{Animation: &switchIn, Enter: &fadeOut, Exit: &fadeOut}
	}
	return ui.ElementTransition{Animation: &switchOut, Exit: &fadeOut}
}

// fresh reports whether el is new in this frame: no frame before laid it
// out.
func fresh(el ui.Element) bool { return el.LayoutBounds().W <= 0 }
