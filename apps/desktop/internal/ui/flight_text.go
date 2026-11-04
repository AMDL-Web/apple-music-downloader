package ui

import (
	"github.com/egoist/mygo/ui"
)

// flyLabel shows el, the album page's label saying text in color, as this
// frame of a flight has it: it swaps with its match under the page; with
// no match, it rises in. What the match's element has with no place on the
// page, as the queue hero's track count, fades with its screen where it
// is.
func (a *App) flyLabel(el ui.Element, text string, color ui.Color) {
	f := &a.fly
	if !f.moving {
		return
	}
	dst, src := el.LayoutBounds(), f.src[partKicker]
	if dst.W > 0 {
		f.matched = true
	}
	if f.still || !src.ok || dst.W <= 0 || src.box.W <= 0 {
		a.flyRise(el)
		return
	}
	f.paired[partKicker] = swaps
	f.label = flightLabel{box: dst, text: text, color: color}
	el.Invisible()
}

// tagFlies reports whether the album page's quality tag flies, as one box,
// from its match under the page in this frame (flyTags).
func (a *App) tagFlies() bool {
	f := &a.fly
	return f.moving && !f.still && f.src[partTag].ok
}

// flyTags shows the album page's row of tags as this frame of a flight has
// it: its first, quality, which says text, gilt when hi, in fg, morphs from
// its match under the page as one box, which flightCopies draws, its text
// changing inside it, and the rest of the row moves with it; with no
// match, the row rises.
func (a *App) flyTags(row, quality ui.Element, text string, hi bool, fg ui.Color) {
	f := &a.fly
	if !f.moving {
		return
	}
	src := f.src[partTag]
	dst := ui.Rect{}
	if text != "" {
		dst = quality.LayoutBounds()
	}
	if dst.W > 0 {
		f.matched = true
	}
	if !a.tagFlies() || dst.W <= 0 || src.box.W <= 0 {
		a.flyRise(row)
		return
	}
	f.paired[partTag] = morphs
	f.tag = flightTag{box: dst, text: text, hi: hi, fg: fg}
	quality.Invisible()
	t := f.travel()
	row.Translate(lerp(src.box.X, dst.X, t)-dst.X, lerp(src.box.Y, dst.Y, t)-dst.Y)
}

// flyText shows el, a text of the album page set as span in color, at
// most room wide, as this frame of a flight has it: when its match under
// the page says the same, or begins the same, it morphs, shown here in its
// own lines and as a copy of its match before (flightCopies); otherwise as
// flyPage shows a part.
func (a *App) flyText(c *ui.Context, el ui.Element, part flightPart, span ui.Span, color ui.Color, room float32) {
	f := &a.fly
	if !f.moving {
		return
	}
	text, size := span.Text, span.Size
	dst, src := el.LayoutBounds(), f.src[part]
	if f.still || !src.ok || src.build == nil || dst.W <= 0 || src.box.W <= 0 || !sameText(src.text, text) {
		a.flyPage(el, part, text, size)
		return
	}
	f.matched = true
	f.paired[part] = morphs
	f.texts[part] = flightText{box: dst, size: size, color: color}
	t := f.travel()
	in := a.Screen == ScreenAlbum
	// The lines it left grow with it: past the page's room, it takes the
	// lines it goes to sooner.
	x := lerp(src.box.X, dst.X, t)
	w := src.ink * lerp(1, size/src.size, t)
	if f.own[part] {
		ink, _ := c.MeasureText(dst.W, span)
		w = ink * lerp(src.size/size, 1, t)
	}
	if over := x+w > dst.X+room+0.5; in && (over || t >= 1-textSwap) || !in && (over || t <= textSwap) {
		f.own[part] = in
	}
	if !f.own[part] {
		el.Invisible()
		return
	}
	s := lerp(src.size/size, 1, t)
	el.TransformOrigin(0, 0).Scale(s, s).Translate(lerp(src.box.X, dst.X, t)-dst.X, lerp(src.box.Y, dst.Y, t)-dst.Y).
		TextColor(src.color.Mix(color, t))
}
