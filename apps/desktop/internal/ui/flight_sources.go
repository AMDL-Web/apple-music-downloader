package ui

import (
	"github.com/egoist/mygo/ui"
)

// flySource marks el as part of the element from on the screen under the
// page: while the page moves, the page's part flies from it, and it hides
// as long as that part flies or morphs.
func (a *App) flySource(el ui.Element, from string, part flightPart, text string, size float32) {
	f := &a.fly
	if !f.moving || f.still || from == "" || from != f.from {
		return
	}
	f.src[part] = flightSrc{box: el.Bounds(), text: text, size: size, ok: true, tone: 1}
	if p := f.pairWas[part]; p == flies || p == morphs || p == swaps {
		el.Invisible()
	}
}

// flySourceLabel marks el, a label of the element from on the screen under
// the page saying text in color, as flySource does, for the page's label
// to swap with (flyLabel); part is partKicker for that label, partTracks
// for a tag of the track count, which flies into it.
func (a *App) flySourceLabel(el ui.Element, from string, part flightPart, text string, color ui.Color) {
	a.flySource(el, from, part, text, 0)
	if src := &a.fly.src[part]; src.ok {
		src.color = color
	}
}

// flySourceText builds a text of the element from on the screen under the
// page with build, set as span and in color, and marks it as flySource
// does: build also builds the copy of it that morphs into the page's
// (flightCopies).
func (a *App) flySourceText(c *ui.Context, build func() ui.Element, span ui.Span, from string, part flightPart, color ui.Color) ui.Element {
	el := build()
	a.flySource(el, from, part, span.Text, span.Size)
	if src := &a.fly.src[part]; src.ok {
		src.build, src.color = build, color
		src.ink, _ = c.MeasureText(src.box.W, span)
	}
	return el
}

// flySourceTag marks el, the quality tag of the element from on the screen
// under the page, saying text, gilt when hi, in fg: the page's flies from
// it as one box (flyTags).
func (a *App) flySourceTag(el ui.Element, from, text string, hi bool, fg ui.Color) {
	a.flySource(el, from, partTag, text, 0)
	if src := &a.fly.src[partTag]; src.ok {
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
// hides (flightCopies).
func (a *App) flySourceBadge(el ui.Element, from string, part flightPart, build func() ui.Element) {
	if !a.fliesFrom(from) {
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
	a.flySource(el, from, partCover, "", radius)
	if src := &a.fly.src[partCover]; src.ok {
		src.tone, src.dim, src.shadow = tone, dim, shadow
	}
}
