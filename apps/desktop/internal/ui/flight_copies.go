package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// flightCopies builds over the album page the copy of each text of it
// that morphs and shows in its match's lines (flyText): its match built
// again where it is, as wide, then moved and scaled towards the page's
// text, its color turning to the page's; and the quality tag's box as it
// morphs (flyTags), from its match's place, size, gilt and color to the
// page's, the one's text fading out in it as the other's fades in. Where
// they have not moved, they are their matches, drawn as those are.
func (a *App) flightCopies(c *ui.Context, k *Tokens) {
	f := &a.fly
	if !f.moving {
		return
	}
	over := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough()
	o, t := over.LayoutBounds(), f.travel()
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
				for i, s := range []string{src.text, dst.text} {
					op := 1 - unit((t-0.2)/0.4)
					if i == 1 {
						op = unit((t - 0.4) / 0.4)
					}
					// As wide as the wider tag, so that the box cuts the
					// text rather than the text ending in an ellipsis.
					ui.Row(c.Key(i)).Absolute().Left(6).Top(0).Bottom(0).Width(max(src.box.W, dst.box.W)).AlignItems(ui.Center).Opacity(op).
						Children(func() { tagText(c, s, ink).SingleLine() })
				}
			})
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

// sameText reports whether a text of the page and its match say the same,
// or one begins with the other, as a title with its " - Single" does.
func sameText(a, b string) bool {
	return a != "" && b != "" && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a))
}
