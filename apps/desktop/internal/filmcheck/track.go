package filmcheck

import (
	"fmt"
	"image"
	"math"
)

// Ink is what of a region's pixels is drawn, in one frame: how many, and
// their bounds, in pixels from the region's corner. Clipped says ink
// touches the region's top or bottom edge (Y) or its left or right edge
// (X), where its bound is the region's, not the element's.
type Ink struct {
	N                        int
	Top, Bottom, Left, Right int
	ClipY, ClipX             bool
}

// InkOf measures ink in the w×h grey image pix (one byte a pixel, rows
// stride apart): a pixel is ink when ink says so of its level.
func InkOf(pix []uint8, stride, w, h int, ink func(uint8) bool) Ink {
	k := Ink{Top: h, Left: w, Bottom: -1, Right: -1}
	for y := range h {
		row := pix[y*stride : y*stride+w]
		for x, v := range row {
			if !ink(v) {
				continue
			}
			k.N++
			k.Top, k.Bottom = min(k.Top, y), max(k.Bottom, y)
			k.Left, k.Right = min(k.Left, x), max(k.Right, x)
		}
	}
	if k.N == 0 {
		return Ink{}
	}
	k.ClipY = k.Top == 0 || k.Bottom == h-1
	k.ClipX = k.Left == 0 || k.Right == w-1
	return k
}

// Step is a move of an element that a smooth motion does not make.
type Step struct {
	// Frame is the frame it moved in, from the frame before.
	Frame int
	// Kind is "snap", a move out of rest that rests again at once, or
	// "jump", a move far larger than the frames on either side.
	Kind string
	// DX and DY are the move, in pixels; Rest is how long it had held
	// still before, in seconds, for a snap.
	DX, DY, Rest float64
}

func (s Step) String() string {
	if s.Kind == "snap" {
		return fmt.Sprintf("snap at frame %d: moved (%+.0f, %+.0f) px after resting %.0f ms", s.Frame, s.DX, s.DY, s.Rest*1000)
	}
	return fmt.Sprintf("jump at frame %d: moved (%+.0f, %+.0f) px, over three times the frames on either side", s.Frame, s.DX, s.DY)
}

// minInk is the least ink that is an element rather than noise.
const minInk = 12

// Steps finds, in where an element's ink was frame by frame (times in
// seconds), a snap: it rested at least rest seconds,
// moved at least least pixels in one frame and rests again in the
// next; and a jump: a move of at least least+1 pixels over three times
// the moves of the frames on either side. A frame whose ink differs by
// over a tenth from the frame before's shows something else (a fade, a
// morph, another element), so no move is measured across it; nor along an
// axis where the ink is clipped.
func Steps(ink []Ink, times []float64, rest, least float64) []Step {
	n := len(ink)
	type move struct {
		ok     bool
		dx, dy float64
	}
	mv := make([]move, n)
	for i := 1; i < n; i++ {
		a, b := ink[i-1], ink[i]
		if a.N < minInk || b.N < minInk || math.Abs(float64(a.N-b.N)) > 0.1*float64(max(a.N, b.N)) {
			continue
		}
		m := move{ok: true}
		if !a.ClipY && !b.ClipY {
			m.dy = (float64(b.Top-a.Top) + float64(b.Bottom-a.Bottom)) / 2
		}
		if !a.ClipX && !b.ClipX {
			m.dx = (float64(b.Left-a.Left) + float64(b.Right-a.Right)) / 2
		}
		mv[i] = m
	}
	size := func(i int) float64 {
		if i <= 0 || i >= n || !mv[i].ok {
			return math.NaN()
		}
		return math.Max(math.Abs(mv[i].dx), math.Abs(mv[i].dy))
	}
	still := func(i int) bool { s := size(i); return !math.IsNaN(s) && s <= 0.5 }
	var out []Step
	for i := 2; i < n; i++ {
		s := size(i)
		if math.IsNaN(s) || s < least {
			continue
		}
		if still(i-1) && (i+1 >= n || still(i+1)) {
			k := i - 1
			for k > 1 && still(k) {
				k--
			}
			// The element is where it rests from frame k on.
			if held := times[i] - times[k]; held >= rest*0.99 {
				out = append(out, Step{Frame: i, Kind: "snap", DX: mv[i].dx, DY: mv[i].dy, Rest: held})
				continue
			}
		}
		before, after := size(i-1), size(i+1)
		if s >= least+1 && !math.IsNaN(before) && !math.IsNaN(after) && s > 3*math.Max(before, after) {
			out = append(out, Step{Frame: i, Kind: "jump", DX: mv[i].dx, DY: mv[i].dy})
		}
	}
	return out
}

// inkWindow is the side of the squares a film's elements are tracked in,
// half over each other, so that a line of text lies whole in one of them.
const inkWindow = 2 * Tile

// filmRest is how long a part of a film rests before a move counts as a
// snap: three frames at 60 Hz. A recording's two frames at 120 Hz are less,
// but a film's two-frame holds come with parts that rise in steps, while
// the settle-then-snap the maintainer saw held for 190 ms.
const filmRest = 3.0 / 60

// inkLevel is how far from its window's ground a pixel's luminance is to
// be ink, in levels: text and edges, not shadows and gradients.
const inkLevel = 60

func (f *Film) windows() (cols, rows int) {
	b := f.bounds
	return max((b.Dx()-inkWindow)/Tile+1, 1), max((b.Dy()-inkWindow)/Tile+1, 1)
}

func (f *Film) windowRect(w int) image.Rectangle {
	cols, _ := f.windows()
	x, y := (w%cols)*Tile, (w/cols)*Tile
	return image.Rect(x, y, x+inkWindow, y+inkWindow).Add(f.bounds.Min).Intersect(f.bounds)
}

// windowInks measures the ink of every window of img against the window's
// median luminance, light text on dark ground and dark on light alike.
func (f *Film) windowInks(img *image.RGBA) []Ink {
	b := img.Bounds()
	grey := make([]uint8, b.Dx()*b.Dy())
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			grey[y*b.Dx()+x] = uint8(luma(img, b.Min.X+x, b.Min.Y+y) + 0.5)
		}
	}
	cols, rows := f.windows()
	out := make([]Ink, cols*rows)
	for w := range out {
		r := f.windowRect(w).Sub(b.Min)
		var hist [256]int
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for _, v := range grey[y*b.Dx()+r.Min.X : y*b.Dx()+r.Max.X] {
				hist[v]++
			}
		}
		half, med := r.Dx()*r.Dy()/2, 0
		for c := 0; c < 256; c++ {
			if half -= hist[c]; half < 0 {
				med = c
				break
			}
		}
		out[w] = InkOf(grey[r.Min.Y*b.Dx()+r.Min.X:], b.Dx(), r.Dx(), r.Dy(), func(v uint8) bool {
			d := int(v) - med
			return d > inkLevel || d < -inkLevel
		})
	}
	return out
}

// moves finds the snaps and jumps (Steps) of what each window shows,
// leaving out windows over animated tiles and the film's first frame,
// which its input moved.
func (f *Film) moves(animated map[int]bool) {
	if f.n < 3 {
		return
	}
	cols, rows := f.windows()
	times := make([]float64, f.n)
	for i := range times {
		times[i] = float64(i) / 60
	}
	type key struct {
		frame int
		kind  string
	}
	boxes := map[key]image.Rectangle{}
	worst := map[key]Step{}
	series := make([]Ink, f.n)
	for w := 0; w < cols*rows; w++ {
		r := f.windowRect(w)
		if !r.Overlaps(f.Content) || f.overAnimated(r, animated) {
			continue
		}
		for i := range series {
			series[i] = f.inks[i][w]
		}
		for _, s := range Steps(series, times, filmRest, 2) {
			if s.Frame < 2 || !f.shifted[s.Frame][w] {
				continue
			}
			k := key{s.Frame, s.Kind}
			boxes[k] = boxes[k].Union(r)
			if w, ok := worst[k]; !ok || math.Hypot(s.DX, s.DY) > math.Hypot(w.DX, w.DY) {
				worst[k] = s
			}
		}
	}
	for k, box := range boxes {
		s := worst[k]
		f.found = append(f.found, Finding{Kind: k.kind, Frame: k.frame, Frames: 1, Box: box, Detail: s.String()})
	}
}

func (f *Film) overAnimated(r image.Rectangle, animated map[int]bool) bool {
	for t := range animated {
		if f.tileRect(t).Overlaps(r) {
			return true
		}
	}
	return false
}

// translations tells, for each window of frame i, whether what it shows
// moved there as one from frame i-1: its ink's bounds moved, and the
// window matches frame i-1 moved by as much far better than unmoved. A
// figure whose digits change, or a fade, moves its ink's bounds too, but
// matches no moved copy of the frame before.
func (f *Film) translations(i int) []bool {
	cols, rows := f.windows()
	out := make([]bool, cols*rows)
	if i == 0 {
		return out
	}
	a, b := f.ring[(i-1)%len(f.ring)], f.ring[i%len(f.ring)]
	for w := range out {
		p, q := f.inks[i-1][w], f.inks[i][w]
		if p.N < minInk || q.N < minInk || math.Abs(float64(p.N-q.N)) > 0.1*float64(max(p.N, q.N)) {
			continue
		}
		dx, dy := 0, 0
		if !p.ClipX && !q.ClipX {
			dx = int(math.Round(float64(q.Left-p.Left+q.Right-p.Right) / 2))
		}
		if !p.ClipY && !q.ClipY {
			dy = int(math.Round(float64(q.Top-p.Top+q.Bottom-p.Bottom) / 2))
		}
		if dx == 0 && dy == 0 {
			continue
		}
		r := f.windowRect(w)
		moved, still := shiftedMAD(a, b, r, dx, dy), shiftedMAD(a, b, r, 0, 0)
		out[w] = moved < Differ/2 && moved < still/3
	}
	return out
}

// shiftedMAD is the mean difference in luminance between b over r and a
// over r moved back by dx, dy, over the pixels both hold.
func shiftedMAD(a, b *image.RGBA, r image.Rectangle, dx, dy int) float64 {
	s, n := 0.0, 0
	for y := r.Min.Y; y < r.Max.Y; y += 2 {
		for x := r.Min.X; x < r.Max.X; x += 2 {
			p := image.Pt(x-dx, y-dy)
			if !p.In(a.Bounds()) {
				continue
			}
			s += math.Abs(luma(b, x, y) - luma(a, p.X, p.Y))
			n++
		}
	}
	if n == 0 {
		return math.Inf(1)
	}
	return s / float64(n)
}
