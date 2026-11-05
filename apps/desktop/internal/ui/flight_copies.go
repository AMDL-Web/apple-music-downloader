package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// wallDrop is how far below their place the recent covers are as they go
// out and come back (wallShown).
const wallDrop = 14

// wallShown is how much the queue's recent covers show under a hero's
// page: coming in, they sink and fade over the first part of the way, as
// the page's parts start to show (inParts) and a little after; going out,
// they rise back into their row over the whole way the page's parts have
// left them, from outParts down, landing as the flight ends.
func (f *flight) wallShown() float32 {
	if f.out {
		return 1 - smoothstep(0, outParts, f.q)
	}
	return 1 - smoothstep(0, inParts+0.15, f.q)
}

// flightCopies builds over the album page the copy of each text of it
// that morphs and shows in its match's lines (flyText): its match built
// again where it is, as wide, then moved and scaled towards the page's
// text, its color turning to the page's; the quality tag's box as it
// morphs (flyTags), from its match's place, size, gilt and color to the
// page's, the one's text fading out in it as the other's fades in; and the
// label as it swaps (flyLabel). Where they have not moved, they are their
// matches, drawn as those are.
func (a *App) flightCopies(c *ui.Context, k *Tokens) {
	f := &a.fly
	if !f.moving {
		return
	}
	over := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough()
	o, t := over.LayoutBounds(), f.travel()
	if f.growing() {
		// Where the surface carries the layer in this frame, not where it
		// did in the frame before.
		o.X, o.Y = o.X-f.ridePrev.X+f.ride.X, o.Y-f.ridePrev.Y+f.ride.Y
	}
	over.Children(func() {
		if f.paired[partTag] == morphs {
			src, dst := f.src[partTag], f.tag
			gilt := func(hi bool) float32 {
				if hi {
					return 1
				}
				return 0
			}
			box, ink := tagBox(c.Key(partTag), k, lerp(gilt(src.hi), gilt(dst.hi), t), src.color.Mix(dst.fg, t))
			box.Absolute().Left(lerp(src.box.X, dst.box.X, t) - o.X).Top(lerp(src.box.Y, dst.box.Y, t) - o.Y).
				Width(lerp(src.box.W, dst.box.W, t)).Clip()
			box.Children(func() {
				// Word by word (tagMorph), as wide as the wider tag, so that
				// the box cuts the text rather than the text ending in an
				// ellipsis.
				ui.Box(c.Key("words")).Absolute().Left(6).Top(0).Bottom(0).Width(max(src.box.W, dst.box.W)).AlignItems(ui.Center).
					Children(func() { tagMorph(c, src.text, dst.text, t, ink) })
			})
		}
		a.flyBadges(c, o, f.lead())
		// The recent covers under a hero's page: at rest they are drawn
		// over the hero cover's shadow, so the page's cover, flying to or
		// from the hero, casts none on them either. They go and come back
		// on their own, slower than the screen under the page (wallShown).
		shown := f.wallShown()
		for i, w := range f.wall {
			el := ui.Box(c.Key(fmt.Sprint("wall", i))).Absolute().Left(w.box.X-o.X).Top(w.box.Y-o.Y).Size(w.box.W, w.box.H).Opacity(shown)
			if !f.still {
				el.Translate(0, wallDrop*(1-shown))
			}
			el.Children(w.build)
		}
		if f.paired[partKicker] == swaps {
			src, dst := f.src[partKicker], f.label
			at := func(el ui.Element, w float32) ui.Element {
				return el.Absolute().Left(lerp(src.box.X, dst.box.X, t) - o.X).Top(lerp(src.box.Y, dst.box.Y, t) - o.Y).Width(w)
			}
			// The words it leaves fade out as it sets off, its own fade in
			// as it lands, as the quality tag's do.
			at(label(c, k, src.text).TextColor(src.color), src.box.W).Opacity(1 - unit((t-0.2)/0.4))
			at(label(c, k, dst.text).TextColor(dst.color), dst.box.W).Opacity(unit((t - 0.4) / 0.4))
		}
		for part, src := range f.src {
			if f.paired[part] != morphs || f.own[part] || src.build == nil {
				continue
			}
			dst := f.texts[part]
			ui.Box(c.Key(flightPart(part))).Absolute().Top(0).Left(0).Children(func() {
				el := src.build().Absolute().Left(src.box.X - o.X).Top(src.box.Y - o.Y).Width(src.box.W).TextColor(src.color.Mix(dst.color, t))
				if t > 0 {
					s := lerp(1, dst.size/src.size, t)
					el.TransformOrigin(0, 0).Scale(s, s).Translate(t*(dst.box.X-src.box.X), t*(dst.box.Y-src.box.Y))
				}
			})
		}
	})
}

// flyBadges draws the badges of the cover the page flew from, its quality
// hint and its mark, each at its corner of the flying cover and as large as
// it was, fading out as the cover leaves it and, going out, in with the
// screen under it, whose own badges take over where these are when the
// flight ends; o is the copies' layer.
func (a *App) flyBadges(c *ui.Context, o ui.Rect, t float32) {
	f := &a.fly
	from := f.src[partCover].box
	if !f.src[partCover].ok || f.cover.W <= 0 || from.W <= 0 {
		return
	}
	at := ui.Rect{X: lerp(from.X, f.cover.X, t), Y: lerp(from.Y, f.cover.Y, t), W: lerp(from.W, f.cover.W, t), H: lerp(from.H, f.cover.H, t)}
	for _, part := range []flightPart{partHint, partMark} {
		src := f.src[part]
		if !src.ok || src.build == nil {
			continue
		}
		x := at.X + src.box.X - from.X
		if src.box.X+src.box.W/2 > from.X+from.W/2 {
			x = at.X + at.W - (from.X + from.W - src.box.X)
		}
		y := at.Y + at.H - (from.Y + from.H - src.box.Y)
		op := 1 - unit(t/0.35)
		switch {
		case f.growing():
			op = 1 - smoothstep(0, 0.3, t)
		case f.out:
			op = f.underShown()
		}
		f.fade(ui.Box(c.Key(part+flightParts)).Absolute().Left(x-o.X).Top(y-o.Y), op).
			Children(func() { src.build() })
	}
}

// sameText reports whether a text of the page and its match say the same,
// or one begins with the other, as a title with its " - Single" does.
func sameText(a, b string) bool {
	return a != "" && b != "" && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a))
}
