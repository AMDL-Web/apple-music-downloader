package ui

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The album page comes in over the screen it was opened from and goes back
// out over it: while it moves both are built, the page above. The cover of
// what was clicked, and from the queue's hero its label, title, artist and
// tags too, fly from where that screen shows them to their places on the
// page (shared elements), a text as one copy of itself that changes size
// and place; the page's lower panel comes in over the screen under it,
// which thins out only once the panel is half in (cross), and the rest of
// the page fades in as it rises a little. The page's lower panel slides
// from the top of the list under it, and the light behind both turns from
// the one screen's to the other's (backdrop). Everything follows one value,
// q, from 0 (the screen under) to 1 (the page), on a spring, which Back
// turns round from wherever it is: the cover goes with q itself, so it sets
// off at once and leads, and the rest follows from crossover (travel).
// While the desktop asks for less motion, q is a short fade, and nothing
// flies.

// flightSpring is how q comes in: critically damped, so nothing
// overshoots, 95% of the way in 0.29 s and 99% in 0.4 s; it comes to rest
// within a thousandth of the way, under half a DIP of the longest flight.
var flightSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 380 * time.Millisecond}}

// flightBack is how q goes out: quicker than it comes in, and aimed
// flightPast below 0, so that the flight ends as q gets to 0, where the
// cover lands, in about 0.28 s, still moving under 1% of its way a frame
// rather than over the spring's slow tail.
var flightBack = ui.Animation{Spring: ui.SpringOptions{Duration: 320 * time.Millisecond}}

const (
	// flightFade is how long q takes while the desktop asks for less
	// motion.
	flightFade = 150 * time.Millisecond
	// flightRise is how far what only the page has rises as it comes in.
	flightRise = 16
	// flightBlur is how far, in DIPs, what fades in a flight is blurred
	// once it has all but faded (fade).
	flightBlur = 3
	// flightPast is how far below 0 q aims going out (flightBack).
	flightPast = 0.03
	// crossover is the q from which what flies, but the cover, moves
	// (travel), once the screens are crossing coming in (cross).
	crossover = 0.2
	// outLand is the q by which, going out, the screens have crossed back
	// and the light has turned (cross, light): the cover settles the last
	// of its way, about a fifth of it, over its own screen.
	outLand = 0.08
	// inCross and outCross are how much of q the screens take to cross
	// (cross): coming in from the start, over the first few frames, and
	// going out down to outLand.
	inCross, outCross = 0.45, 0.3
	// inParts and outParts are the q from which the page's own parts show
	// (partsFrom), as the screen under thins out, so the two overlap a
	// little and the window never stands blank between them.
	inParts, outParts = 0.3, 0.25
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
	// partHint and partMark are a search tile's quality hint and mark in
	// its cover's corners, which ride the flying cover and fade
	// (flySourceBadge): the page's own quality tag comes in apart.
	partHint
	partMark
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
	// swaps with its match, which says something else in its own style:
	// one copy moves from the match's place to its own, the match's words
	// fading out in it as its own fade in, which hides both meanwhile
	// (flightCopies).
	swaps
)

// flightSrc is a part of the element the page opened from: its box in the
// window as the frame before showed it, what it says, and its font size,
// or a cover's corner radius; a text's or a tag's color, a text's widest
// line, and build, which builds it again (flightCopies); whether a tag is
// gilt, and whether it is a tile's glass hint rather than a tag; a cover's
// tone (widgets.Cover's Color), dim and how much of a hero cover's shadow
// it drops, which the page's cover takes on its way (flySourceCover).
type flightSrc struct {
	box   ui.Rect
	text  string
	size  float32
	color ui.Color
	ink   float32
	build func() ui.Element
	hi    bool
	ok    bool

	tone, dim, shadow float32
}

// flightText is where a text of the page that morphs is laid out, its
// font size and color.
type flightText struct {
	box   ui.Rect
	size  float32
	color ui.Color
}

// flightLabel is where the page's label is laid out, what it says and its
// color, when it swaps with its match (flyLabel).
type flightLabel struct {
	box   ui.Rect
	text  string
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
	// out is set while the page goes out with motion, which crosses the
	// screens and ends sooner than it comes in (fade).
	out bool
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
	// morphs, and label its label, when it swaps.
	tag   flightTag
	label flightLabel
	// closeDetail is set while Back waits for the page to leave before it
	// closes the page's download, which the page shows until it is gone.
	closeDetail bool
	// back is what the title bar's Back button says, kept as it goes out.
	back string
	// cover is the page's cover as this frame lays it out, when it flies
	// from its match: the badges of that match ride it (flightCopies).
	cover ui.Rect
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
	f.out = target == 0 && !f.still && a.PageMotion
	f.pairWas, f.paired = f.paired, [flightParts]pairing{}
	f.src, f.texts, f.tag, f.label, f.edgeOK, f.cover = [flightParts]flightSrc{}, [flightParts]flightText{}, flightTag{}, flightLabel{}, false, ui.Rect{}
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
		aim, spring := target, flightSpring
		if f.out && f.q > 0 {
			// f.q is still the frame before's: at rest at 0, q aims at
			// 0 and asks for no frames.
			aim, spring = -flightPast, flightBack
		}
		f.q = root.AnimateTo(flightKey{}, aim, spring)
		f.fadeFrom, f.fadeTo = target, target
		if f.out && f.q <= 0 {
			// Going out, the cover has landed and the screens have crossed
			// (cross): the flight ends there, so the screen under takes the
			// pointer, and shows its tile's own shadow and marks, as the
			// cover lands.
			root.Jump(flightKey{}, 0)
			f.q = 0
		}
	}
	f.moving = f.q != target || f.q > 0 && f.q < 1 || inPage && !f.ready
	if inPage && !f.ready {
		// The page's parts match in a frame and q reads it in the next:
		// without asking for those, the page waits hidden, and the screen
		// under it deaf to the pointer, until something else draws, as
		// nothing does for an album opened again from what is cached.
		c.AnimationFrame()
	}
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

// cross is how far the screens have crossed to the page, 0 to 1: the
// page's lower panel comes in over the screen under it, which stays whole
// until the panel is half in and only then thins out (underShown). Two
// layers thinning out together over the light ground wash out to white,
// and a screen gone before the panel is there leaves it blank. Coming in
// it runs over inCross of q; going out over outCross above outLand, as the
// cover comes down to its place.
func (f *flight) cross() float32 {
	if f.out {
		return unit((f.q - outLand) / outCross)
	}
	return unit(f.q / inCross)
}

// underShown is how much the screen under the page shows (cross).
func (f *flight) underShown() float32 { return unit(2 * (1 - f.cross())) }

// partsFrom is the q from which the page's own parts show (flyFade).
func (f *flight) partsFrom() float32 {
	if f.out {
		return outParts
	}
	return inParts
}

// light is how far the light and the lower panel's ground have turned to
// the page's: q coming in, and going out all the way back by outLand, with
// the screens.
func (f *flight) light() float32 {
	if f.out {
		return unit((f.q - outLand) / (1 - outLand))
	}
	return f.q
}

// travel is how far what moves in a flight has gone, from 0 (where the
// screen under the page shows it) to 1 (where the page does): it sets off
// at crossover, once the page's lower panel is coming in over the screen
// under it, and sets off and comes to rest smoothly.
func (f *flight) travel() float32 {
	x := unit((f.q - crossover) / (1 - crossover))
	return x * x * (3 - 2*x)
}

// lead is how far the cover has gone, from 0 (where the screen under the
// page shows it) to 1 (where the page does): q itself, so that it sets off
// in the first frame, ahead of what travels, and lands as the flight ends;
// q's spring eases it in and out.
func (f *flight) lead() float32 { return f.q }

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
		if text != src.text {
			p = crosses
		}
	}
	f.paired[part] = p
	if p == unpaired {
		a.flyRise(el)
		return
	}
	t := f.travel()
	if part == partCover {
		t = f.lead()
	}
	s := float32(1)
	switch part {
	case partCover:
		f.cover = dst
		s = lerp(src.box.W, dst.W, t) / dst.W
	case partTitle, partArtist:
		s = lerp(src.size/size, 1, t)
	}
	el.TransformOrigin(0, 0).Scale(s, s).Translate(lerp(src.box.X, dst.X, t)-dst.X, lerp(src.box.Y, dst.Y, t)-dst.Y)
	if part == partCover && s > 0 {
		el.Radius(lerp(src.size, size, t) / s)
	}
	if p == crosses {
		f.fade(el, unit((f.q-f.partsFrom())/0.45))
	}
}

// flyCover is the album page's cover's tone, dim and shadow in this frame
// (widgets.Cover's Color, Dim and Shadow): its own, but on the way to or
// from the cover it flies from, as far between the two as it has come, a
// search tile's shadow staying with its screen (hitTile); and
// whether they change every frame, which the cover then shows at once
// rather than easing to.
func (a *App) flyCover() (tone, dim, shadow float32, jump bool) {
	f := &a.fly
	src := f.src[partCover]
	if !f.moving || f.still || !src.ok {
		return 1, 0, 1, false
	}
	t := f.lead()
	return lerp(src.tone, 1, t), lerp(src.dim, 0, t), lerp(src.shadow, 1, t), true
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
	el.Background(k.Ground.Mix(detailGround(k), f.light())).
		BorderColor(k.Line.Alpha(unit((f.q - panelLined) / (1 - panelLined))))
	f.fade(el, f.cross())
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
	if f := &a.fly; f.moving {
		from := f.partsFrom()
		f.fade(el, unit((f.q-from)/(1-from)))
	}
}

// fade shows el, which fades in or out in a flight, at opacity op, and
// blurred as far as it has faded, up to flightBlur: it goes soft as it goes.
// Whole, or gone, it is not blurred, which costs a layer; nor while the
// desktop asks for less motion.
func (f *flight) fade(el ui.Element, op float32) ui.Element {
	el.Opacity(op)
	if op > 0 && op < 1 && !f.still {
		el.Blur(flightBlur * (1 - op))
	}
	return el
}

func lerp(a, b, k float32) float32 { return a + (b-a)*k }

// unit clamps v to [0, 1].
func unit(v float32) float32 { return max(0, min(v, 1)) }
