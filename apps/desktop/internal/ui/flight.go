package ui

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The album page comes in over the screen it was opened from and goes back
// out over it: while it moves both are built, the page above. The cover of
// what was clicked, and from the queue's hero its label, title, artist and
// tags too, fly from where that screen shows them to their places on the
// page (shared elements); the rest of the page fades in as it rises a
// little, and the screen under it fades out. Everything follows one value,
// q, from 0 (the screen under) to 1 (the page), on a spring, which Back
// turns round from wherever it is. While the desktop asks for less motion,
// q is a short fade, and nothing flies.

// flightSpring is how q moves: critically damped, so nothing overshoots,
// 95% of the way in 0.29 s and 99% in 0.4 s; it comes to rest within a
// thousandth of the way, under half a DIP of the longest flight.
var flightSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 380 * time.Millisecond}}

const (
	// flightFade is how long q takes while the desktop asks for less
	// motion.
	flightFade = 150 * time.Millisecond
	// flightRise is how far what only the page has rises as it comes in.
	flightRise = 16
	// underGone is the q by which the screen under the page has faded out,
	// and panelShown the q by which the page's opaque lower panel has faded
	// in over that screen's lists, so the two lists hardly mix.
	underGone  = 0.5
	panelShown = 0.2
)

// flightKey is q's key on the window's root.
type flightKey struct{}

// flightPart is a part of the page that can fly from the screen under it.
type flightPart int

const (
	partCover flightPart = iota
	partKicker
	partTitle
	partArtist
	partTags
	flightParts
)

// pairing is how a part of the page shows during a flight.
type pairing uint8

const (
	// unpaired: nothing under the page matches it, and it fades in.
	unpaired pairing = iota
	// flies from its match, which hides meanwhile.
	flies
	// crosses from its match, which says something else or wraps
	// otherwise: it fades in as it moves while its match fades out.
	crosses
)

// flightSrc is a part of the element the page opened from: its box in the
// window as the frame before showed it, what it says, and its font size,
// or a cover's corner radius.
type flightSrc struct {
	box  ui.Rect
	text string
	size float32
	ok   bool
}

// flight is the album page's way in and out.
type flight struct {
	// next names the element the next OpenAlbum or OpenJob opens from
	// (openingFrom); from is the one the page opened from, and under the
	// screen it came in over.
	next, from string
	under      Screen
	// q is how far the page is in, in this frame; moving is set while the
	// page comes in or goes out, and still while it fades in place, as the
	// desktop asks for less motion.
	q             float32
	moving, still bool
	// instant is set by a change of screen that does not fly: q goes to
	// its end at once.
	instant bool
	// ready is set once the page's parts were matched with what is under
	// it (flyPage), which they are in the frame after the page is first
	// laid out: q waits for it, so that the frame the page first shows has
	// its parts where their matches are, and those hidden. matched is set
	// in a frame that matched them.
	ready, matched bool
	// fadeFrom, fadeTo and fadeAt are q's fade while the desktop asks for
	// less motion: from fadeFrom at fadeAt to fadeTo.
	fadeFrom, fadeTo float32
	fadeAt           time.Time
	// src are the parts of the element the page opened from, as this frame
	// builds them under the page; paired is how each part of the page
	// showed in the frame before, which tells its match whether to hide.
	src             [flightParts]flightSrc
	paired, pairWas [flightParts]pairing
	// closeDetail is set while Back waits for the page to leave before it
	// closes the page's download, which the page shows until it is gone.
	closeDetail bool
	// back is what the title bar's Back button says, kept as it goes out.
	back string
}

// openingFrom names the element the next OpenAlbum or OpenJob opens from,
// whose cover flies to the page's: "hero:" and the job's ID for the queue's
// hero, "wall:", "row:", "hit:" and "song:" for the others.
func (a *App) openingFrom(from string) { a.fly.next = from }

// flightQ moves q towards the page while the album screen is shown, else
// towards the screen under it, and notes where it is.
func (a *App) flightQ(c *ui.Context, root ui.Element) {
	f := &a.fly
	f.still = c.Preferences().ReduceMotion
	inPage := a.Screen == ScreenAlbum
	f.ready = f.ready || f.matched || f.still || !a.PageMotion
	f.matched = false
	target := float32(0)
	if inPage && f.ready {
		target = 1
	}
	f.pairWas, f.paired = f.paired, [flightParts]pairing{}
	f.src = [flightParts]flightSrc{}
	// A click names what it opens from in the frame it opens; one that
	// downloads instead leaves nothing for a later open.
	f.next = ""
	now := c.Now()
	switch {
	case !a.PageMotion || f.instant:
		f.instant = false
		root.Jump(flightKey{}, target)
		f.q, f.fadeFrom, f.fadeTo = target, target, target
	case f.still:
		q := f.fadeTo
		if k := float32(now.Sub(f.fadeAt)) / float32(flightFade); k < 1 {
			q = f.fadeFrom + (f.fadeTo-f.fadeFrom)*k
		}
		if target != f.fadeTo {
			f.fadeFrom, f.fadeTo, f.fadeAt = q, target, now
		}
		if q != target {
			c.AnimationFrame()
		}
		root.Jump(flightKey{}, target)
		f.q = q
	default:
		f.q = root.AnimateTo(flightKey{}, target, flightSpring)
		f.fadeFrom, f.fadeTo = target, target
	}
	f.moving = f.q != target || f.q > 0 && f.q < 1 || inPage && !f.ready
	if !f.moving && f.q == 0 {
		f.ready = false
		if f.closeDetail {
			f.closeDetail = false
			a.Store.CloseDetail()
		}
	}
}

// screenLayer is a screen's layer over the main column, by which it keeps
// its identity whichever screen is built beside it.
type screenLayer Screen

// screens builds the screen shown, and while the album page moves, the
// page over the screen it came from.
func (a *App) screens(c *ui.Context, k *Tokens, page float32) {
	f := &a.fly
	inPage := a.Screen == ScreenAlbum
	under := a.Screen
	if inPage {
		under = f.under
	}
	layer := func(s Screen) ui.Element {
		return ui.Column(c.Key(screenLayer(s))).Absolute().Top(0).Left(0).Right(0).Bottom(0)
	}
	if under != ScreenAlbum && (!inPage || f.moving) {
		l := layer(under)
		if f.moving {
			l.Opacity(1 - unit(f.q/underGone))
		}
		l.Children(func() { a.screen(c, k, under, page) })
	}
	if inPage || f.moving {
		l := layer(ScreenAlbum)
		if !f.ready {
			// Laid out, then matched with what is under it, before it
			// shows; the screen under it shows as it was meanwhile.
			l.Invisible()
		}
		l.Children(func() { a.albumScreen(c, k, page) })
	}
	if f.moving {
		// Neither screen takes the pointer while the page moves; Back and
		// Escape still turn it round.
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0)
	}
}

// screen builds s.
func (a *App) screen(c *ui.Context, k *Tokens, s Screen, page float32) {
	switch s {
	case ScreenSettings:
		a.settings(c, k)
	case ScreenAlbum:
		a.albumScreen(c, k, page)
	case ScreenQueue:
		a.queueScreen(c, k, a.compact)
	default:
		a.home(c, k, page)
	}
}

// flySource marks el as part of the element from on the screen under the
// page: while the page moves, the page's part flies from it, and it hides
// as long as that part flies.
func (a *App) flySource(el ui.Element, from string, part flightPart, text string, size float32) {
	f := &a.fly
	if !f.moving || f.still || from == "" || from != f.from {
		return
	}
	f.src[part] = flightSrc{box: el.Bounds(), text: text, size: size, ok: true}
	if f.pairWas[part] == flies {
		el.Invisible()
	}
}

// flyPage shows el, a part of the album page, as this frame of a flight
// has it: from where its match under the page shows, scaled from the
// match's size (a text's font size, a cover's side) to its own, which is
// size, and a cover's corners from the match's radius; or, with no match,
// fading in as it rises.
func (a *App) flyPage(el ui.Element, part flightPart, text string, size float32) {
	f := &a.fly
	if !f.moving {
		return
	}
	dst, src := el.LayoutBounds(), f.src[part]
	if dst.W > 0 {
		f.matched = true
	}
	p := unpaired
	if src.ok && dst.W > 0 && src.box.W > 0 {
		p = flies
		if part == partKicker || part == partTags || text != src.text || part == partTitle && wrapsApart(src, dst, size) {
			p = crosses
		}
	}
	if f.still {
		p = unpaired
	}
	f.paired[part] = p
	if p == unpaired {
		a.flyRise(el)
		return
	}
	q := f.q
	s := float32(1)
	switch part {
	case partCover:
		s = lerp(src.box.W, dst.W, q) / dst.W
	case partTitle, partArtist:
		s = lerp(src.size/size, 1, q)
	}
	x, y := lerp(src.box.X, dst.X, q)-dst.X, lerp(src.box.Y, dst.Y, q)-dst.Y
	el.TransformOrigin(0, 0).Scale(s, s).Translate(x, y)
	if part == partCover && s > 0 {
		el.Radius(lerp(src.size, size, q) / s)
	}
	if p == crosses {
		el.Opacity(unit((q - 0.15) / 0.5))
	}
}

// flyRise shows el, a part only the album page has, as this frame of a
// flight has it: fading in as it rises into place, or in place while the
// desktop asks for less motion.
func (a *App) flyRise(el ui.Element) {
	if a.fly.moving {
		a.flyFade(el)
		if !a.fly.still {
			el.Translate(0, flightRise*(1-a.fly.q))
		}
	}
}

// flyPanel shows the album page's opaque lower panel as this frame of a
// flight has it: rising into place, and opaque early on, over the lists of
// the screen under it.
func (a *App) flyPanel(el ui.Element) {
	if a.fly.moving {
		el.Opacity(unit(a.fly.q / panelShown))
		if !a.fly.still {
			el.Translate(0, flightRise*(1-a.fly.q))
		}
	}
}

// flyFade fades el in with the album page, in place.
func (a *App) flyFade(el ui.Element) {
	if a.fly.moving {
		el.Opacity(unit((a.fly.q - 0.2) / 0.8))
	}
}

// wrapsApart reports whether a title shows on more lines than its match,
// or fewer: then it crosses rather than flies, as one wrapped otherwise
// would change shape at once. Both are display text, whose lines are
// LineHeight 1.05 of the size.
func wrapsApart(src flightSrc, dst ui.Rect, size float32) bool {
	lines := func(h, size float32) int { return int(h/(size*1.05) + 0.5) }
	return lines(src.box.H, src.size) != lines(dst.H, size)
}

func lerp(a, b, k float32) float32 { return a + (b-a)*k }

// unit clamps v to [0, 1].
func unit(v float32) float32 { return max(0, min(v, 1)) }
