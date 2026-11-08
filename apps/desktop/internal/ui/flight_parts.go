package ui

import "github.com/egoist/mygo/ui"

// flyRise shows el, a part only the album page has, as this frame of a
// flight has it: fading in as it rises into place, or in place while the
// desktop asks for less motion.
func (a *App) flyRise(el ui.Element) {
	if a.fly.growing() {
		el.Opacity(a.lateShown())
		return
	}
	if a.fly.moving {
		a.flyFade(el)
		if !a.fly.still {
			el.Translate(0, flightRise*(1-a.fly.travel()))
		}
	}
}

// flyRiseCover shows the album page's cover, when nothing under the page
// matches it, as flyRise shows a part, but not blurred: a blurred layer is
// cut to the cover's box, which would clip its shadow to a rectangle and
// let the whole shadow pop out as the layer goes.
func (a *App) flyRiseCover(el ui.Element) {
	if f := &a.fly; f.growing() {
		el.Opacity(a.lateShown())
	} else if f.moving {
		el.Opacity(unit((f.q - f.partsFrom()) / (1 - f.partsFrom())))
		if !f.still {
			el.Translate(0, flightRise*(1-f.travel()))
		}
	}
}

// flyEdge marks el, the top of the list of the screen under the page,
// which the page's lower panel flies from and back to.
func (a *App) flyEdge(el ui.Element) {
	if f := &a.fly; f.moving && !f.still {
		f.edge, f.edgeOK = el.Bounds().Y, true
	}
}

// flyPanel shows the album page's opaque lower panel as this frame of a
// flight has it: its top, a sharp edge, flies from the top of the list
// under the page (flyEdge), or, with none, rises with the page; its ground
// turns opaque early, over that list, before its rows show (flyRows), and
// turns from the screens' to its own, so that it does not show as a layer
// of its own while it fades.
func (a *App) flyPanel(el ui.Element, k *Tokens) {
	f := &a.fly
	if !f.moving || f.growing() {
		return
	}
	shown := f.groundShown()
	el.Background(k.Ground.Mix(detailGround(k), f.light()).Alpha(shown)).
		BorderColor(k.Line.Alpha(shown * unit((f.q-panelLined)/(1-panelLined))))
	if f.still {
		return
	}
	t := f.travel()
	y := flightRise * (1 - t)
	if dst := el.LayoutBounds(); f.edgeOK && dst.H > 0 {
		y = lerp(f.edge, dst.Y, t) - dst.Y
	}
	el.Translate(0, y)
}

// groundShown is how much of the lower panel's ground shows (cross): all of
// it once the panel is half in, as the screen under it starts to thin out.
func (f *flight) groundShown() float32 { return unit(2 * f.cross()) }

// flyRows shows el, the rows in the album page's lower panel, as this frame
// of a flight has it: fading in over the panel's ground once that is whole
// (groundShown), so no row is drawn over the screen under the page.
func (a *App) flyRows(el ui.Element) {
	if f := &a.fly; f.growing() {
		el.Opacity(a.lateShown())
	} else if f.moving {
		f.fade(el, unit(2*f.cross()-1))
	}
}

// bandRise is how far the hero's figures and bar rise as they come in and
// sink as they go: further than flightRise, as they come in all at once.
const bandRise = 30

// flyBand shows el, the head's figures and bar, as this frame of a flight
// has it: fading in with the page's parts (flyFade) as it rises bandRise
// into place, and the same way back, sinking as far as it has faded, so
// it moves for as long as it shows.
func (a *App) flyBand(el ui.Element) {
	f := &a.fly
	if f.growing() || !f.moving {
		a.flyRise(el)
		return
	}
	from := f.partsFrom()
	op := unit((f.q - from) / (1 - from))
	f.fade(el, op)
	if !f.still {
		el.Translate(0, bandRise*(1-op))
	}
}

// flyFade fades el in with the album page, in place, once the screen under
// it is gone.
func (a *App) flyFade(el ui.Element) {
	if f := &a.fly; f.growing() {
		el.Opacity(a.lateShown())
	} else if f.moving {
		from := f.partsFrom()
		f.fade(el, unit((f.q-from)/(1-from)))
	}
}

// flyFadeClear fades el in with the album page as flyFade does, but not
// before the screen under the page is gone (cross): el is a control that
// screen shows too, as the page's Pause is the queue hero's, which would
// otherwise show twice as the two cross. Going out the page's parts are
// gone before the screen under shows again (outParts).
func (a *App) flyFadeClear(el ui.Element) {
	f := &a.fly
	if f.growing() || !f.moving || f.out {
		a.flyFade(el)
		return
	}
	from := max(f.partsFrom(), inCross)
	f.fade(el, unit((f.q-from)/(1-from)))
}

// fade shows el, which fades in or out in a flight, at opacity op, and
// blurred as far as it has faded, up to flightBlur: it goes soft as it goes.
// Whole, or gone, it is not blurred, which costs a layer; nor while the
// desktop asks for less motion.
func (f *flight) fade(el ui.Element, op float32) ui.Element {
	el.Opacity(op)
	if op > 0 && op < 1 && !f.still && !f.grow {
		el.Blur(flightBlur * (1 - op))
	}
	return el
}
