package ui

import (
	"github.com/egoist/mygo/ui"
)

// flySource marks el as part of the element from on the screen under the
// page: while the page moves, the page's part flies from it, and it hides
// as long as that part flies or morphs. It reports whether el is the part's
// source in this frame: several elements can go by one name, as two tiles
// of one album, and only one is, the one where the source was in the frame
// before (sourceNearer); only that one hides.
func (a *App) flySource(el ui.Element, from string, part flightPart, text string, size float32) bool {
	return a.markSource(el, from, part, text, size, true)
}

// markSource is flySource, hiding el while its part flies only if hide.
func (a *App) markSource(el ui.Element, from string, part flightPart, text string, size float32, hide bool) bool {
	f := &a.fly
	if part == partCover && !f.moving && f.q == 0 && f.pick.W <= 0 && from != "" && from == f.from && a.Screen == ScreenAlbum {
		// The frame of the click: the tile clicked names the page as it
		// builds, and its cover is the first of that name built after.
		f.pick = el.Bounds()
	}
	if !f.moving || f.still || from == "" || from != f.from {
		return false
	}
	box := el.Bounds()
	if cur := f.src[part]; cur.ok && !f.sourceNearer(part, box, cur.box) {
		return false
	}
	f.src[part] = flightSrc{box: box, text: text, size: size, ok: true, tone: 1}
	if p := f.pairWas[part]; hide && (p == flies || p == morphs || p == swaps) && f.wasSource(part, box) {
		el.Invisible()
	}
	return true
}

// sourceNearer reports whether box, an element going by the name the page
// flies from, is a better source for part than cur, one before it: nearer
// where the source was in the frame before, or, in a flight's first frame
// or one after a screen built anew, whose elements had no place yet, nearer
// the cover clicked (pick); with neither, shown where cur is not, as a tile
// on the screen is against one of the same album scrolled out of it.
func (f *flight) sourceNearer(part flightPart, box, cur ui.Rect) bool {
	was := f.srcWas[part].box
	if !f.srcWas[part].ok || was.W <= 0 {
		was = ui.Rect{}
		if part == partCover {
			was = f.pick
		}
	}
	if was.W > 0 {
		return centerDist(box, was) < centerDist(cur, was)
	}
	return f.onScreen(box) && !f.onScreen(cur)
}

// wasSource reports whether box is where part's source was in the frame
// before: within half its size, closer than another element of the same
// name can be.
func (f *flight) wasSource(part flightPart, box ui.Rect) bool {
	was := f.srcWas[part]
	return was.ok && centerDist(box, was.box) <= max(2, min(was.box.W, was.box.H)/2)
}

// onScreen reports whether some of box shows: in the main column, and not
// cut off whole by the list that holds it (flightClip).
func (f *flight) onScreen(box ui.Rect) bool {
	m := f.main
	if box.X+box.W <= m.X || box.X >= m.X+m.W || box.Y+box.H <= m.Y || box.Y >= m.Y+m.H {
		return false
	}
	top, bottom := f.flightClip(box)
	return top+bottom < box.H
}

func centerDist(a, b ui.Rect) float32 {
	dx, dy := a.X+a.W/2-(b.X+b.W/2), a.Y+a.H/2-(b.Y+b.H/2)
	return dx*dx + dy*dy
}

// flySourceLabel marks el, a label of the element from on the screen under
// the page saying text in color, as flySource does, for the page's label
// to swap with (flyLabel), as part.
func (a *App) flySourceLabel(el ui.Element, from string, part flightPart, text string, color ui.Color) {
	if a.flySource(el, from, part, text, 0) {
		a.fly.src[part].color = color
	}
}

// flySourceText builds a text of the element from on the screen under the
// page with build, set as span and in color, and marks it as flySource
// does: build also builds the copy of it that morphs into the page's
// (flightCopies).
func (a *App) flySourceText(c *ui.Context, build func() ui.Element, span ui.Span, from string, part flightPart, color ui.Color) ui.Element {
	el := build()
	if a.flySource(el, from, part, span.Text, span.Size) {
		src := &a.fly.src[part]
		src.build, src.color = build, color
		src.ink, _ = c.MeasureText(src.box.W, span)
	}
	return el
}

// flySourceTag marks el, the quality tag of the element from on the screen
// under the page, saying text, gilt when hi, in fg: the page's flies from
// it as one box (flyTags).
func (a *App) flySourceTag(el ui.Element, from, text string, hi bool, fg ui.Color) {
	if a.flySource(el, from, partTag, text, 0) {
		src := &a.fly.src[partTag]
		src.color, src.hi = fg, hi
	}
}

// fliesFrom reports whether the album page is flying from or back to the
// element from in this frame, whose parts then mark themselves.
func (a *App) fliesFrom(from string) bool {
	f := &a.fly
	return f.moving && !f.still && from != "" && from == f.from
}

// flySourceBadge marks el, a badge on the cover of the element from, as
// part, which build builds again: a copy of it rides the flying cover's
// corner, fading out as the cover leaves and in as it lands, while el
// hides (flightCopies). Only a badge on the cover the page flies from is
// one (flySource): another element of the same name keeps its own.
func (a *App) flySourceBadge(el ui.Element, from string, part flightPart, build func() ui.Element) {
	if !a.fliesFrom(from) {
		return
	}
	f := &a.fly
	cover := f.srcWas[partCover]
	if !cover.ok {
		cover = f.src[partCover]
	}
	b := el.Bounds()
	if cx, cy := b.X+b.W/2, b.Y+b.H/2; cover.ok && (cx < cover.box.X || cx > cover.box.X+cover.box.W || cy < cover.box.Y || cy > cover.box.Y+cover.box.H) {
		return
	}
	a.fly.src[part] = flightSrc{box: el.Bounds(), build: build, ok: true}
	el.Invisible()
}

// flySourceCover marks el, the cover of the element from with corners of
// radius, as flySource does, with its tone and dim (widgets.Cover's Color
// and Dim) and how much of a hero cover's shadow it drops: the page's cover
// leaves and lands with them (flyCover), not with its own.
func (a *App) flySourceCover(el ui.Element, from string, radius, tone, dim, shadow float32) {
	if a.flySource(el, from, partCover, "", radius) {
		src := &a.fly.src[partCover]
		src.tone, src.dim, src.shadow = tone, dim, shadow
	}
}

// flySourceCoverShown is flySourceCover for a cover that stays in view
// beside the page, the sidebar card's: the page's cover leaves it and lands
// on it, and it shows all the while.
func (a *App) flySourceCoverShown(el ui.Element, from string, radius float32) {
	a.markSource(el, from, partCover, "", radius, false)
}

// flySourceCard marks el, a whole card of the element from with corners of
// radius, as what the page grows out of (flight.card), when it opens from
// from.
func (a *App) flySourceCard(el ui.Element, from string, radius float32) {
	if f := &a.fly; f.moving && !f.still && from != "" && from == f.from {
		f.card = flightSrc{box: el.Bounds(), size: radius, ok: true}
	}
}
