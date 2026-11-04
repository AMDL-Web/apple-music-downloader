package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// An album page opened from a cover other than the queue hero's (a search
// tile, a row, the wall, a song) grows out of that cover: it is a surface
// whose rounded box goes from the cover's box to the main column's,
// with the page, title bar included, laid out at its full size from the
// box's top-left corner, so what is on the page moves with it. The screen
// under it stays whole, blurred and dimmed (motionTune), and nothing of the
// two washes out over the other. The cover flies on q like the box, so it
// stays inside it all the way; the page's ground, its light and its other
// parts come in late and together, in place on the surface (lateShown),
// and turn it opaque. Going out runs the same way back. The hero's page morphs
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

// groundGrown is how much of the growing page's ground shows: as much as
// its parts (lateShown). The box is larger than the cover from the first
// frame, so a ground ahead of the parts would lead the cover as an empty
// box over the screen under it.
func (a *App) groundGrown() float32 {
	if !a.fly.growing() {
		return 1
	}
	return a.lateShown()
}

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

// growBox is the surface's box in this frame, in the main column's
// coordinates, main being the main column's box in the window, and its
// corner radius: from the cover the page opened from (the last box it was
// seen at) to the whole column, on q.
func (f *flight) growBox(main ui.Rect) (ui.Rect, float32) {
	full := ui.Rect{W: main.W, H: main.H}
	if !f.growing() {
		return full, 0
	}
	if src := f.src[partCover]; src.ok && src.box.W > 0 {
		// The part of the cover its list shows: a tile cut by the bar over
		// its list grows from under that bar, as the cover is cut
		// (clipCover).
		top, bottom := f.flightClip(src.box)
		b := src.box
		b.Y, b.H = b.Y+top, max(0, b.H-top-bottom)
		f.growFrom, f.growRadius = b, src.size
	}
	from := f.growFrom
	if from.W <= 0 {
		// Nothing seen to grow from: the middle of the column.
		from = ui.Rect{X: main.X + main.W/2, Y: main.Y + main.H/2}
	}
	from.X, from.Y = from.X-main.X, from.Y-main.Y
	q := f.q
	r := ui.Rect{X: lerp(from.X, 0, q), Y: lerp(from.Y, 0, q), W: lerp(from.W, full.W, q), H: lerp(from.H, full.H, q)}
	return r, lerp(f.growRadius, 0, unit(q))
}

// growSurface builds the album page as the surface it grows on: over the
// main column, which is main in the window, its title bar and page in
// their own layer, and the page's light inside it.
func (a *App) growSurface(c *ui.Context, k *Tokens, sidebar bool, page float32) {
	f := &a.fly
	main := f.main
	box, radius := f.growBox(main)
	f.ridePrev, f.ride = f.ride, ui.Point{X: box.X, Y: box.Y}
	f.surface = ui.Rect{X: main.X + box.X, Y: main.Y + box.Y, W: box.W, H: box.H}
	t := unit(f.q)
	ground := a.groundGrown()
	s := ui.Box(c.Key("surface")).Absolute().Left(box.X).Top(box.Y).Size(box.W, box.H).Clip().Radius(radius).Background(k.Ground.Alpha(ground))
	if lift := 4 * t * (1 - t) * ground; f.growing() && lift > 0.01 {
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
		ui.Box(c).Absolute().Top(0).Left(0).Size(main.W, main.H).Children(func() {
			b := paletteBackdrop(k, a.screenPalette(c, ScreenAlbum), false)
			if f.growing() {
				plain := paletteBackdrop(k, Palette{}, false)
				b.From, b.Mix = &plain, t
			}
			// The light draws the ground under it too, so it comes in with
			// the surface's.
			light := ui.Box(c.Key("light")).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough()
			if ground < 1 {
				light.Opacity(ground)
			}
			light.Children(func() { b.Show(c) })
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
// the surface, smaller than the column the page is laid out for, does not
// cut its words and rows as a hard line. The band comes in with the part,
// and narrows to nothing as the surface reaches the column's edges, so
// nothing changes as it lands. Each part takes it on its own: an effect on
// a layer that holds the flying cover moves the cover off its way.
func (a *App) growEdges(el ui.Element) {
	f := &a.fly
	if !f.growing() {
		return
	}
	main, s := f.main, f.surface
	band := growBand * a.lateShown()
	bx, by := min(band, main.W-s.W), min(band, main.H-s.H)
	if bx < 0.5 && by < 0.5 {
		return
	}
	// Laid out where the surface carried the page in the frame before.
	b := el.LayoutBounds()
	right := s.X + s.W - (b.X - f.ridePrev.X + f.ride.X)
	bottom := s.Y + s.H - (b.Y - f.ridePrev.Y + f.ride.Y)
	if b.W <= 0 || right >= b.W+bx && bottom >= b.H+by {
		return
	}
	v := growEdge.NewValues().Float(growEdgeRight, right).Float(growEdgeBottom, bottom).
		Float(growEdgeBandX, max(0, bx)).Float(growEdgeBandY, max(0, by))
	el.Effect(growEdge.Effect(shader.Options{Values: v}))
}

// underGone is how far the screen under the growing page has gone back,
// blurred and dimmed: as far as the page has come, and only as far as its
// ground shows (groundGrown), so that going back the screen under is clear
// as the page goes into its cover rather than over q's last frames after.
func (a *App) underGone() float32 { return unit(a.fly.q) * a.groundGrown() }

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
// is where the frame before laid it out less how far the surface had
// carried it (ridePrev); this frame lays it out where the surface carries
// it now (ride).
func (f *flight) growCover(el ui.Element, src flightSrc, dst ui.Rect, size float32) {
	at := ui.Rect{X: dst.X - f.ridePrev.X, Y: dst.Y - f.ridePrev.Y, W: dst.W, H: dst.H}
	f.cover = at
	t := f.lead()
	s := lerp(src.box.W, dst.W, t) / dst.W
	el.TransformOrigin(0, 0).Scale(s, s).
		Translate(lerp(src.box.X, at.X, t)-(at.X+f.ride.X), lerp(src.box.Y, at.Y, t)-(at.Y+f.ride.Y))
	if s > 0 {
		el.Radius(lerp(src.size, size, t) / s)
		f.clipCover(el, src.box, t, s)
	}
}
