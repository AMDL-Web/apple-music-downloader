package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// An album page opened from a cover other than the queue hero's (a search
// tile, a row, the wall, a song) grows out of that cover: the page, title
// bar included, is laid out at its full size and scaled and placed so that
// its own cover is the flying cover, which goes on q from the cover clicked
// to its place on the page; it shows through an opaque surface whose
// rounded box is the flying cover's until the page's parts start to show
// and then opens from it to the whole page as they fade in (lateShown).
// Whatever of the surface shows is the page, and nothing under it shows
// through it. The screen under it stays whole, blurred and dimmed as far
// as the surface has opened (motionTune). The page's light comes in from
// the plain ground. Going out runs the same way back. The hero's page morphs
// into place instead (flight.go).

// growLand is the q at which a page going back into its cover has landed.
const growLand = 0.002

// growsFrom reports whether a page opened from from grows out of its cover.
func growsFrom(from string) bool { return from != "" && !strings.HasPrefix(from, "hero:") }

// growing reports whether the page grows in this frame.
func (f *flight) growing() bool { return f.grow && f.moving && !f.still }

// lateShown is how much of the page's own parts show as it grows: none
// until q reaches the tune's Text, all of it a little after.
func (a *App) lateShown() float32 {
	from := a.tune.Text
	return smoothstep(from, min(1, from+0.35), a.fly.q)
}

// opened is how far the growing surface has opened from the flying cover
// to the whole page: as far as the page's parts show (lateShown), so that
// no more of its ground shows than they fill.
func (a *App) opened() float32 { return a.lateShown() }

func smoothstep(a, b, x float32) float32 {
	if b <= a {
		if x >= b {
			return 1
		}
		return 0
	}
	t := unit((x - a) / (b - a))
	return t * t * (3 - 2*t)
}

// growBox is, in this frame and in the main column's coordinates, main
// being the main column's box in the window, the surface's box and corner
// radius, and the page's: its box, at its scale, which puts its own cover
// where the flying cover is. The flying cover goes on q from the cover the
// page opened from (the last box it was seen at) to the page's (the last
// place it was laid out); the surface opens that far (open) from the part
// of it its list shows to the page's box.
func (f *flight) growBox(main ui.Rect, open float32) (box ui.Rect, radius float32, page ui.Rect) {
	full := ui.Rect{W: main.W, H: main.H}
	if !f.growing() {
		return full, 0, full
	}
	if src := f.src[partCover]; src.ok && src.box.W > 0 {
		f.growFrom, f.growRadius = src.box, src.size
		// The part of the cover its list shows: a tile cut by the bar over
		// its list grows from under that bar, as the cover is cut
		// (clipCover).
		f.growClipTop, f.growClipBottom = f.flightClip(src.box)
	}
	from, to, q := f.growFrom, f.growTo, f.q
	if from.W <= 0 || to.W <= 0 {
		// Nothing seen to grow from, or the page not laid out yet: from
		// the middle of the column, at its size.
		from = ui.Rect{X: main.X + main.W/2, Y: main.Y + main.H/2}
		r := ui.Rect{X: lerp(from.X-main.X, 0, q), Y: lerp(from.Y-main.Y, 0, q), W: lerp(0, full.W, q), H: lerp(0, full.H, q)}
		return r, 0, r
	}
	cover := ui.Rect{X: lerp(from.X, to.X, q), Y: lerp(from.Y, to.Y, q), W: lerp(from.W, to.W, q), H: lerp(from.H, to.H, q)}
	cover.X, cover.Y = cover.X-main.X, cover.Y-main.Y
	scale := cover.W / to.W
	at := ui.Point{X: to.X - main.X, Y: to.Y - main.Y}
	page = ui.Rect{X: cover.X - scale*at.X, Y: cover.Y - scale*at.Y, W: scale * full.W, H: scale * full.H}
	shown := cover
	if cut := 1 - unit(q); cut > 0 {
		shown.Y += f.growClipTop * cut
		shown.H = max(0, shown.H-(f.growClipTop+f.growClipBottom)*cut)
	}
	box = ui.Rect{X: lerp(shown.X, page.X, open), Y: lerp(shown.Y, page.Y, open), W: lerp(shown.W, page.W, open), H: lerp(shown.H, page.H, open)}
	return box, lerp(lerp(f.growRadius, f.growToRadius, unit(q)), 0, open), page
}

// growSurface builds the album page as the surface it grows on: over the
// main column, which is main in the window, its title bar and page in
// their own layer, and the page's light inside it.
func (a *App) growSurface(c *ui.Context, k *Tokens, sidebar bool, page float32) {
	f := &a.fly
	main := f.main
	box, radius, pg := f.growBox(main, a.opened())
	f.ridePrev, f.ride = f.ride, ui.Point{X: pg.X, Y: pg.Y}
	f.surface = ui.Rect{X: main.X + box.X, Y: main.Y + box.Y, W: box.W, H: box.H}
	f.page = ui.Rect{X: main.X + pg.X, Y: main.Y + pg.Y, W: pg.W, H: pg.H}
	f.scale = 1
	if main.W > 0 {
		f.scale = pg.W / main.W
	}
	t := unit(f.q)
	s := ui.Box(c.Key("surface")).Absolute().Left(box.X).Top(box.Y).Size(box.W, box.H).Clip().Radius(radius).Background(k.Ground)
	if lift := 4 * t * (1 - t); f.growing() && lift > 0.01 {
		s.Shadow(0, 10*lift, 24*lift, 0, ui.RGBA(0, 0, 0, 0.18*lift))
	}
	if !f.ready {
		// Laid out, then matched with what is under it, before it shows.
		s.Invisible()
	}
	if f.moving {
		// The pointer goes to the screens' guard meanwhile (screens).
		s.PassThrough()
	}
	s.Children(func() {
		layer := ui.Box(c).Absolute().Left(pg.X-box.X).Top(pg.Y-box.Y).Size(main.W, main.H)
		if f.scale != 1 {
			layer.TransformOrigin(0, 0).Scale(f.scale, f.scale)
		}
		layer.Children(func() {
			b := paletteBackdrop(k, a.screenPalette(c, ScreenAlbum), false)
			if f.growing() {
				plain := paletteBackdrop(k, Palette{}, false)
				b.From, b.Mix = &plain, t
			}
			b.Flat = a.FlatBackdrop
			b.Show(c)
			ui.Column(c.Key("content")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() {
				a.titleBar(c, k, sidebar, true)
				ui.Box(c).Grow(1).MinHeight(0).Children(func() {
					ui.Column(c.Key(screenLayer(ScreenAlbum))).Absolute().Top(0).Left(0).Right(0).Bottom(0).
						Children(func() { a.albumScreen(c, k, page) })
				})
			})
		})
	})
}

// growBand is how far in from the growing surface's right and bottom edges
// the page's parts fade to nothing (growEdges).
const growBand = 2 * scrollFade

// growEdge fades what an element draws to nothing towards Right and Bottom,
// in its own DIPs, over BandX and BandY DIPs inward, 0 for an edge that
// stays.
var growEdge = shader.MustCompile(`package main

var Right float
var Bottom float
var BandX float
var BandY float

func ColorEffect(pos vec2, color vec4) vec4 {
	k := 1.0
	if BandX > 0 {
		k *= smoothstep(0, BandX, Right-pos.x)
	}
	if BandY > 0 {
		k *= smoothstep(0, BandY, Bottom-pos.y)
	}
	return color * k
}
`)

var (
	growEdgeRight  = growEdge.Uniform("Right")
	growEdgeBottom = growEdge.Uniform("Bottom")
	growEdgeBandX  = growEdge.Uniform("BandX")
	growEdgeBandY  = growEdge.Uniform("BandY")
)

// growEdges fades el, a part of the growing page that comes in late
// (lateShown), towards the surface's right and bottom edges (growBand), so
// the surface, smaller than the page, does not cut its words and rows as a
// hard line. The band comes in with the part, and narrows to nothing as
// the surface reaches the page's edges, so nothing changes as it lands.
// Each part takes it on its own: an effect on a layer that holds the
// flying cover moves the cover off its way.
func (a *App) growEdges(el ui.Element) {
	f := &a.fly
	if !f.growing() || f.scale <= 0 {
		return
	}
	s, p := f.surface, f.page
	band := growBand * a.lateShown()
	bx, by := min(band, p.X+p.W-s.X-s.W), min(band, p.Y+p.H-s.Y-s.H)
	if bx < 0.5 && by < 0.5 {
		return
	}
	// In the page's own DIPs, which the page's scale draws smaller: el is
	// laid out where the page was placed in the frame before.
	b := el.LayoutBounds()
	k := f.scale
	right := (s.X+s.W-p.X)/k - (b.X - f.main.X - f.ridePrev.X)
	bottom := (s.Y+s.H-p.Y)/k - (b.Y - f.main.Y - f.ridePrev.Y)
	bx, by = bx/k, by/k
	if b.W <= 0 || right >= b.W+bx && bottom >= b.H+by {
		return
	}
	v := growEdge.NewValues().Float(growEdgeRight, right).Float(growEdgeBottom, bottom).
		Float(growEdgeBandX, max(0, bx)).Float(growEdgeBandY, max(0, by))
	el.Effect(growEdge.Effect(shader.Options{Values: v}))
}

// underGone is how far the screen under the growing page has gone back,
// blurred and dimmed: as far as the surface has opened over it (opened),
// so that going back the screen under is clear as the page goes into its
// cover rather than over q's last frames after.
func (a *App) underGone() float32 { return a.opened() }

// growUnder shows the content column under the growing page, el, blurred
// as far as it has gone back (underGone).
func (a *App) growUnder(el ui.Element) {
	if !a.fly.growing() {
		return
	}
	if t := a.underGone(); a.tune.Blur > 0 && t > 0 {
		el.Blur(a.tune.Blur * t)
	}
}

// growDim dims the screen under the growing page (underGone).
func (a *App) growDim(c *ui.Context) {
	if f := &a.fly; f.growing() {
		ui.Box(c.Key("grow-dim")).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough().
			Background(ui.RGBA(0, 0, 0, a.tune.Dim*a.underGone()))
	}
}

// growCover shows el, the page's cover, laid out at dst, flying from src as
// the page grows: on q from src's box to its own place on the page, which
// is where the frame before laid it out less where the page was placed
// (ridePrev). The page is placed and scaled (growBox) to put it there
// already; what the page's own layout and scale leave, as the frame in
// which the page is first laid out, el's own scale and move make up.
func (f *flight) growCover(el ui.Element, src flightSrc, dst ui.Rect, size float32) {
	at := ui.Rect{X: dst.X - f.ridePrev.X, Y: dst.Y - f.ridePrev.Y, W: dst.W, H: dst.H}
	f.cover, f.growTo, f.growToRadius = at, at, size
	t := f.lead()
	s := lerp(src.box.W, dst.W, t) / dst.W
	k := max(f.scale, 0.01)
	// The page's origin in the window, about which it is scaled.
	o := ui.Point{X: f.main.X + f.ride.X, Y: f.main.Y + f.ride.Y}
	el.TransformOrigin(0, 0).Scale(s/k, s/k).
		Translate((lerp(src.box.X, at.X, t)-o.X)/k-(at.X-f.main.X), (lerp(src.box.Y, at.Y, t)-o.Y)/k-(at.Y-f.main.Y))
	if s > 0 {
		el.Radius(lerp(src.size, size, t) / s)
		f.clipCover(el, src.box, t, s)
	}
}
