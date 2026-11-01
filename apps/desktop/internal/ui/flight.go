package ui

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// The album page comes in over the screen it was opened from and goes back
// out over it: while it moves both are built, the page above. The cover of
// what was clicked, and from the queue's hero its label, title, artist and
// tags too, fly from where that screen shows them to their places on the
// page (shared elements), a text as one copy of itself that changes size
// and place; the screen under it fades out, then the rest of the page fades
// in as it rises a little, and what moves moves only then (travel), so
// that nothing moves beside what stands still. The page's lower panel slides
// from the top of the list under it, and the light behind both turns from
// the one screen's to the other's (backdrop). Everything follows one value,
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
	// crossover is the q by which the screen under the page has faded out
	// and the page's opaque lower panel faded in, empty, over that screen's
	// list; the rest of the page fades in only from there (flyFade, a part
	// that crosses), so that nothing of the two screens shows at once but
	// what flies.
	crossover = 0.2
	// panelLined is the q from which the lower panel's top line shows, as
	// it lands.
	panelLined = 0.85
	// textSwap is how far from its end of the way a text that morphs
	// changes from its match's lines to the page's, or back: late, where
	// the two nearly coincide, and while it still moves.
	textSwap = 0.12
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
	partTag
	flightParts
)

// pairing is how a part of the page shows during a flight.
type pairing uint8

const (
	// unpaired: nothing under the page matches it, and it fades in.
	unpaired pairing = iota
	// flies from its match, which hides meanwhile.
	flies
	// morphs from its match, the same text in another size, place or lines,
	// which hides meanwhile: one copy of it changes from the one to the
	// other, in the lines of the side it left until near the other
	// (textSwap).
	morphs
	// crosses from its match, which says something else: it fades in as it
	// moves, once its match has faded out.
	crosses
)

// flightSrc is a part of the element the page opened from: its box in the
// window as the frame before showed it, what it says, and its font size,
// or a cover's corner radius; a text's or a tag's color, a text's widest
// line, and build, which builds it again (flightCopies); whether a tag is
// gilt.
type flightSrc struct {
	box   ui.Rect
	text  string
	size  float32
	color ui.Color
	ink   float32
	build func() ui.Element
	hi    bool
	ok    bool
}

// flightText is where a text of the page that morphs is laid out, its
// font size and color.
type flightText struct {
	box   ui.Rect
	size  float32
	color ui.Color
}

// flightTag is where the page's quality tag is laid out, what it says,
// whether it is gilt, and its color.
type flightTag struct {
	box  ui.Rect
	text string
	hi   bool
	fg   ui.Color
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
	// texts are the page's texts that morph, as this frame lays them out;
	// own tells whether each shows in the page's lines, rather than as a
	// copy in its match's.
	texts [flightParts]flightText
	own   [flightParts]bool
	// tag is the page's quality tag, as this frame lays it out, when it
	// morphs.
	tag flightTag
	// closeDetail is set while Back waits for the page to leave before it
	// closes the page's download, which the page shows until it is gone.
	closeDetail bool
	// back is what the title bar's Back button says, kept as it goes out.
	back string
	// edge is the top in the window of the list under the page, as this
	// frame builds it, when edgeOK: the lower panel's top flies from it.
	edge   float32
	edgeOK bool
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
	f.src, f.texts, f.tag, f.edgeOK = [flightParts]flightSrc{}, [flightParts]flightText{}, flightTag{}, false
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
	if !f.moving {
		// A text that morphs sets off in the lines of the side it leaves.
		for i := range f.own {
			f.own[i] = f.q == 1
		}
	}
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
			l.Opacity(1 - unit(f.q/crossover))
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
// as long as that part flies or morphs.
func (a *App) flySource(el ui.Element, from string, part flightPart, text string, size float32) {
	f := &a.fly
	if !f.moving || f.still || from == "" || from != f.from {
		return
	}
	f.src[part] = flightSrc{box: el.Bounds(), text: text, size: size, ok: true}
	if p := f.pairWas[part]; p == flies || p == morphs {
		el.Invisible()
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

// travel is how far what moves in a flight has gone, from 0 (where the
// screen under the page shows it) to 1 (where the page does): it moves
// only once that screen is gone (crossover), so that nothing moves beside
// what stands still, and sets off and comes to rest smoothly.
func (f *flight) travel() float32 {
	x := unit((f.q - crossover) / (1 - crossover))
	return x * x * (3 - 2*x)
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
	if src.ok && dst.W > 0 && src.box.W > 0 && !f.still {
		p = flies
		if part == partKicker || text != src.text {
			p = crosses
		}
	}
	f.paired[part] = p
	if p == unpaired {
		a.flyRise(el)
		return
	}
	t := f.travel()
	s := float32(1)
	switch part {
	case partCover:
		s = lerp(src.box.W, dst.W, t) / dst.W
	case partTitle, partArtist:
		s = lerp(src.size/size, 1, t)
	}
	el.TransformOrigin(0, 0).Scale(s, s).Translate(lerp(src.box.X, dst.X, t)-dst.X, lerp(src.box.Y, dst.Y, t)-dst.Y)
	if part == partCover && s > 0 {
		el.Radius(lerp(src.size, size, t) / s)
	}
	if p == crosses {
		el.Opacity(unit((f.q - crossover) / 0.45))
	}
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

// flyRise shows el, a part only the album page has, as this frame of a
// flight has it: fading in as it rises into place, or in place while the
// desktop asks for less motion.
func (a *App) flyRise(el ui.Element) {
	if a.fly.moving {
		a.flyFade(el)
		if !a.fly.still {
			el.Translate(0, flightRise*(1-a.fly.travel()))
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
// under the page (flyEdge), or, with none, rises with the page; it turns
// opaque early, over that list, before its rows show, and its ground turns
// from the screens' to its own, so that it does not show as a layer of its
// own while it fades.
func (a *App) flyPanel(el ui.Element, k *Tokens) {
	f := &a.fly
	if !f.moving {
		return
	}
	el.Background(k.Ground.Mix(detailGround(k), f.q)).Opacity(unit(f.q / crossover)).
		BorderColor(k.Line.Alpha(unit((f.q - panelLined) / (1 - panelLined))))
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

// flyFade fades el in with the album page, in place, once the screen under
// it is gone.
func (a *App) flyFade(el ui.Element) {
	if a.fly.moving {
		el.Opacity(unit((a.fly.q - crossover) / (1 - crossover)))
	}
}

func lerp(a, b, k float32) float32 { return a + (b-a)*k }

// unit clamps v to [0, 1].
func unit(v float32) float32 { return max(0, min(v, 1)) }
