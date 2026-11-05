// Package filmcheck finds, in a film of the desktop window (one frame per
// 60 Hz refresh), what the maintainer's motion rules forbid and an eye
// catches at once: a frame that flashes or loses an element and then
// returns, a part that snaps once the motion is over, a window gone blank
// between two screens, and a text that leaves for a frame.
//
// It compares frames tile by tile, so a small badge that pops is not lost in
// a whole-window average, and keeps only a few frames whole: a film of any
// length costs a handful of full frames of memory.
package filmcheck

import (
	"fmt"
	"image"
	"math"
	"sort"
	"strings"
	"unicode"
)

// Tile is the side of the squares frames are compared in, in pixels.
const Tile = 32

// The thresholds are mean absolute differences over a tile, in levels of
// 255 averaged over R, G and B. A line of text across a tile changes it by
// 15 to 40; antialiasing and dithering stay under 1.
const (
	// Differ is the change of a tile between two frames that an eye sees.
	Differ = 8.0
	// Agree is the most two frames may differ in a tile and still show the
	// same thing.
	Agree = 1.5
	// PopMin is the least step that counts as a snap.
	PopMin = 6.0
	// SeeShift is the least shift, in pixels along an axis, that an eye
	// sees as a part moving between two frames at 1x. A part that lands one
	// pixel off and snaps into place changes a tile by as much as 12
	// levels and is not seen.
	SeeShift = 2
	// searchShift is how far a tile's change is looked for as a shift.
	searchShift = 6
	// shiftFit is how much of a tile's change a shift must explain, as the
	// part left over, for the change to be called a shift.
	shiftFit = 0.4
	// maxGlitch is the longest run of frames that counts as a flicker
	// rather than a change of state.
	maxGlitch = 3
	// minTiles is the fewest tiles a flicker covers: one tile crossed by a
	// moving edge or capsule in a frame or two looks like one.
	minTiles = 2
	// minMovingTiles is minTiles while the app reports a motion, whose
	// flying copies cross small parts of the screen under them in a frame
	// or two: a flash then has to light up a sizeable part of the window.
	minMovingTiles = 12
	// quietTiles is the most other tiles that may change beside a flicker
	// on the side where the window holds still.
	quietTiles = 4
	// settleFrames is how long after a motion the app reports ends a part
	// may still be seen to snap.
	settleFrames = 8
)

// Finding is one thing wrong in a film.
type Finding struct {
	// Kind is "flicker" (a flash, or an element gone and back), "pop",
	// "snap" or "jump" (Steps),
	// "blank", "text" or "stale".
	Kind string `json:"kind"`
	// Frame is the index of the first frame that shows it.
	Frame int `json:"frame"`
	// Frames is how many frames it lasts.
	Frames int `json:"frames"`
	// Box is where, in the frame's pixels; empty for the whole frame.
	Box image.Rectangle `json:"box"`
	// Detail says what was measured.
	Detail string `json:"detail"`
}

func (f Finding) String() string {
	s := fmt.Sprintf("%s at frame %d", f.Kind, f.Frame)
	if f.Frames > 1 {
		s += fmt.Sprintf(" for %d frames", f.Frames)
	}
	if !f.Box.Empty() {
		s += fmt.Sprintf(" in %v", f.Box)
	}
	return s + ": " + f.Detail
}

// Film checks frames as they are added.
type Film struct {
	// Content is the part of the window whose contrast must not collapse
	// and where flickers and pops are looked for; empty means the whole
	// frame. The sidebar, which stays, is left out of it.
	Content image.Rectangle

	n        int
	bounds   image.Rectangle
	cols     int
	rows     int
	ring     [maxGlitch + 2]*image.RGBA
	steps    [][]float64     // per frame, per tile: change from the frame before
	aligns   []map[int]align // per frame: tiles that changed by PopMin or more, as a shift
	moving   []bool
	contrast []float64
	inks     [][]Ink  // per frame, per window (inkWindows): its ink
	shifted  [][]bool // per frame, per window: its ink moved as one from the frame before
	texts    []map[string]bool
	runs     []run
	found    []Finding
}

// run is a flicker before Finish weighs it against the whole film.
type run struct {
	start, end int // the agreeing frames on either side
	tiles      []int
	worst      float64
	bright     float64
}

// Add checks img, the next frame, with the texts it shows; moving says
// the app was still running a motion it reports (a page's flight) when it
// drew the frame. img is copied.
func (f *Film) Add(img *image.RGBA, texts []string, moving bool) {
	if f.n == 0 {
		f.bounds = img.Bounds()
		f.cols = (f.bounds.Dx() + Tile - 1) / Tile
		f.rows = (f.bounds.Dy() + Tile - 1) / Tile
		if f.Content.Empty() {
			f.Content = f.bounds
		}
	}
	if img.Bounds() != f.bounds {
		panic("filmcheck: frames of different sizes")
	}
	i := f.n
	cur := f.ring[i%len(f.ring)]
	if cur == nil {
		cur = image.NewRGBA(f.bounds)
		f.ring[i%len(f.ring)] = cur
	}
	copy(cur.Pix, img.Pix)
	step := make([]float64, f.cols*f.rows)
	if i > 0 {
		prev := f.ring[(i-1)%len(f.ring)]
		for t := range step {
			step[t] = f.mad(prev, cur, t)
		}
	}
	f.steps = append(f.steps, step)
	f.aligns = append(f.aligns, f.align(i, step))
	f.moving = append(f.moving, moving)
	f.contrast = append(f.contrast, contrastOf(cur, f.Content))
	f.inks = append(f.inks, f.windowInks(cur))
	f.shifted = append(f.shifted, f.translations(i))
	set := map[string]bool{}
	for _, s := range texts {
		if s = strings.TrimSpace(s); s != "" {
			set[s] = true
		}
	}
	f.texts = append(f.texts, set)
	f.n++
	f.glitches()
	f.textGlitches()
}

// Findings are what was found so far; Finish adds what needs the whole
// film.
func (f *Film) Findings() []Finding { return f.found }

// Frames is how many frames were added.
func (f *Film) Frames() int { return f.n }

// Finish checks what needs the whole film and returns every finding, by
// frame.
func (f *Film) Finish() []Finding {
	animated := f.animated()
	f.weighRuns(animated)
	for i := 2; i < f.n; i++ {
		f.pops(i, animated)
	}
	f.blanks()
	f.moves(animated)
	sort.SliceStable(f.found, func(a, b int) bool { return f.found[a].Frame < f.found[b].Frame })
	return f.found
}

func (f *Film) tileRect(t int) image.Rectangle {
	x, y := t%f.cols, t/f.cols
	return image.Rect(x*Tile, y*Tile, (x+1)*Tile, (y+1)*Tile).Add(f.bounds.Min).Intersect(f.bounds)
}

func (f *Film) inContent(t int) bool { return f.tileRect(t).Overlaps(f.Content) }

// mad is the mean absolute difference of a and b over tile t.
func (f *Film) mad(a, b *image.RGBA, t int) float64 {
	r := f.tileRect(t)
	s, n := 0, 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i, j := a.PixOffset(r.Min.X, y), b.PixOffset(r.Min.X, y)
		for x := r.Min.X; x < r.Max.X; x++ {
			for k := range 3 {
				d := int(a.Pix[i+k]) - int(b.Pix[j+k])
				if d < 0 {
					d = -d
				}
				s += d
			}
			i, j, n = i+4, j+4, n+3
		}
	}
	if n == 0 {
		return 0
	}
	return float64(s) / float64(n)
}

func tileLuma(img *image.RGBA, r image.Rectangle) float64 {
	s, n := 0.0, 0.0
	for y := r.Min.Y; y < r.Max.Y; y += 2 {
		for x := r.Min.X; x < r.Max.X; x += 2 {
			s += luma(img, x, y)
			n++
		}
	}
	return s / max(n, 1)
}

func luma(img *image.RGBA, x, y int) float64 {
	i := img.PixOffset(x, y)
	return 0.2126*float64(img.Pix[i]) + 0.7152*float64(img.Pix[i+1]) + 0.0722*float64(img.Pix[i+2])
}

// contrastOf is the spread of luminance over r: high where a screen shows
// its content, low where it is blank ground.
func contrastOf(img *image.RGBA, r image.Rectangle) float64 {
	var sum, sq, n float64
	for y := r.Min.Y; y < r.Max.Y; y += 3 {
		for x := r.Min.X; x < r.Max.X; x += 3 {
			l := luma(img, x, y)
			sum, sq, n = sum+l, sq+l*l, n+1
		}
	}
	if n == 0 {
		return 0
	}
	m := sum / n
	return math.Sqrt(max(sq/n-m*m, 0))
}

// glitches notes, ending at the newest frame, a run of one to maxGlitch
// frames in which tiles differ from the frames on both sides while those
// two agree: something flashed, or left and came back. Finish weighs it.
func (f *Film) glitches() {
	end := f.n - 1
	for k := 1; k <= maxGlitch; k++ {
		start := end - k - 1
		if start < 0 {
			return
		}
		a, b := f.ring[start%len(f.ring)], f.ring[end%len(f.ring)]
		r := run{start: start, end: end, worst: math.Inf(1)}
		for t := range f.steps[end] {
			// The run begins and ends with a change.
			if f.steps[start+1][t] < Differ || f.steps[end][t] < Differ || !f.inContent(t) {
				continue
			}
			if f.mad(a, b, t) > Agree {
				continue
			}
			all := true
			low := math.Inf(1)
			for j := start + 1; j < end; j++ {
				m := f.ring[j%len(f.ring)]
				d := min(f.mad(a, m, t), f.mad(m, b, t))
				if d < Differ {
					all = false
					break
				}
				low = min(low, d)
			}
			if !all {
				continue
			}
			r.tiles = append(r.tiles, t)
			r.worst = min(r.worst, low)
			tr := f.tileRect(t)
			r.bright += tileLuma(f.ring[(start+1)%len(f.ring)], tr) - (tileLuma(a, tr)+tileLuma(b, tr))/2
		}
		if len(r.tiles) >= minTiles {
			f.runs = append(f.runs, r)
		}
	}
}

// animated are the tiles that change in over two frames of five, as a
// running download's stripes: they are always changing, so their changes
// tell nothing. Stripes a page covered while it flew show only after it
// lands, so the frames after the last motion the app reports are counted
// apart too, down to the faint changes of light stripes on a light bar:
// there nothing else moves.
func (f *Film) animated() map[int]bool {
	out := map[int]bool{}
	count := func(from int, least float64) {
		if f.n-from < 6 {
			return
		}
		for t := range f.steps[0] {
			c := 0
			for i := from; i < f.n; i++ {
				if f.steps[i][t] > least {
					c++
				}
			}
			if c*5 > (f.n-from)*2 {
				out[t] = true
			}
		}
	}
	count(1, Differ/2)
	for i := f.n - 1; i > 0; i-- {
		if f.moving[i-1] {
			count(i+1, Agree)
			break
		}
	}
	return out
}

// quiet reports whether, at step i, at most quietTiles tiles of the content
// change, leaving out the animated ones and those of skip.
func (f *Film) quiet(i int, animated map[int]bool, skip []int) bool {
	if i < 1 || i >= f.n {
		return false
	}
	c := 0
	for t, s := range f.steps[i] {
		if s > Differ/2 && f.inContent(t) && !animated[t] && !containsInt(skip, t) {
			c++
		}
	}
	return c <= quietTiles
}

// weighRuns keeps the runs that are flickers: of at least minTiles tiles
// that are not always changing, beside a window that holds still on one
// side at least. Content in motion crosses a tile that way by chance, as a
// page growing over a ground of its own color or a field gliding up.
func (f *Film) weighRuns(animated map[int]bool) {
	for _, r := range f.runs {
		var tiles []int
		for _, t := range r.tiles {
			if !animated[t] {
				tiles = append(tiles, t)
			}
		}
		least := minTiles
		for i := r.start; i <= r.end; i++ {
			if f.moving[i] {
				least = minMovingTiles
			}
		}
		if len(tiles) < least || (!f.quiet(r.start, animated, tiles) && !f.quiet(r.end+1, animated, tiles)) {
			continue
		}
		how := "darker"
		if r.bright > 0 {
			how = "brighter"
		}
		f.found = append(f.found, Finding{Kind: "flicker", Frame: r.start + 1, Frames: r.end - r.start - 1, Box: f.union(tiles),
			Detail: fmt.Sprintf("%d tiles differ by %.0f+ levels (%s) from frames %d and %d, which agree", len(tiles), r.worst, how, r.start, r.end)})
	}
}

// pops finds tiles that snap at frame i, within settleFrames of the end of
// a motion the app reports: on the frame it ends, a change of over 1.5
// times the most of the five frames before (a shadow or badge that lands
// late); after it, a change out of stillness that holds still after (a
// part that appears whole).
func (f *Film) pops(i int, animated map[int]bool) {
	if f.moving[i] {
		return
	}
	ended := -1
	for j := i - 1; j >= max(i-settleFrames, 0); j-- {
		if f.moving[j] {
			ended = j + 1
			break
		}
	}
	if ended < 0 {
		return
	}
	handOver := ended == i
	var hit []int
	var shifts []align
	worst := 0.0
	for t, s := range f.steps[i] {
		if s < PopMin || animated[t] || !f.inContent(t) {
			continue
		}
		before := 0.0
		for j := max(i-5, 1); j < i; j++ {
			if handOver || j >= i-3 {
				before = max(before, f.steps[j][t])
			}
		}
		if handOver {
			if s <= 1.5*before {
				continue
			}
		} else {
			after := 0.0
			for j := i + 1; j < min(i+4, f.n); j++ {
				after = max(after, f.steps[j][t])
			}
			if s < 3*before || after > s/3 || i+1 >= f.n {
				continue
			}
		}
		if a, ok := f.aligns[i][t]; ok && a.shifted {
			if abs(a.dx) < SeeShift && abs(a.dy) < SeeShift {
				continue // a part one pixel off: nothing an eye sees
			}
			shifts = append(shifts, a)
		}
		hit = append(hit, t)
		worst = max(worst, s)
	}
	if len(hit) == 0 {
		return
	}
	how := fmt.Sprintf("%d frames after the motion ended, out of stillness, then hold still", i-ended)
	if handOver {
		how = "on the frame the motion ends, over 1.5 times the frames before"
	}
	detail := fmt.Sprintf("%d tiles change by up to %.0f levels %s", len(hit), worst, how)
	if len(shifts) > 0 {
		big := shifts[0]
		for _, a := range shifts {
			if abs(a.dx)+abs(a.dy) > abs(big.dx)+abs(big.dy) {
				big = a
			}
		}
		if len(shifts) == len(hit) {
			detail = fmt.Sprintf("%d tiles move by up to (%+d, %+d) px %s", len(hit), big.dx, big.dy, how)
		} else {
			detail += fmt.Sprintf("; %d of them are a move of up to (%+d, %+d) px", len(shifts), big.dx, big.dy)
		}
	}
	f.found = append(f.found, Finding{Kind: "pop", Frame: i, Frames: 1, Box: f.union(hit), Detail: detail})
}

// textGlitches finds a text that leaves for up to two frames and comes
// back. Texts with digits are left out: a speed or a clock may come back
// to a value it showed.
func (f *Film) textGlitches() {
	end := f.n - 1
	for k := 1; k <= 2; k++ {
		start := end - k - 1
		if start < 0 {
			return
		}
		var gone []string
		for s := range f.texts[start] {
			if !f.texts[end][s] || strings.IndexFunc(s, unicode.IsDigit) >= 0 {
				continue
			}
			missing := true
			for j := start + 1; j < end; j++ {
				if f.texts[j][s] {
					missing = false
				}
			}
			if missing {
				gone = append(gone, s)
			}
		}
		if len(gone) > 0 {
			sort.Strings(gone)
			f.found = append(f.found, Finding{Kind: "text", Frame: start + 1, Frames: k,
				Detail: fmt.Sprintf("%s left and came back", clipList(gone, 4))})
		}
	}
}

// blanks finds frames of a motion the app reports whose content lost over
// half the contrast of both the first and the last frame: the window
// between two screens showing neither. A switch of tabs fades the old
// content out before the new comes in, by design, so frames out of such a
// motion are not looked at.
func (f *Film) blanks() {
	if f.n < 3 {
		return
	}
	floor := 0.5 * min(f.contrast[0], f.contrast[f.n-1])
	low := func(i int) bool { return f.contrast[i] < floor && (f.moving[i] || f.moving[i-1]) }
	for i := 1; i < f.n-1; i++ {
		if !low(i) {
			continue
		}
		j := i
		for j+1 < f.n-1 && low(j+1) {
			j++
		}
		f.found = append(f.found, Finding{Kind: "blank", Frame: i, Frames: j - i + 1, Box: f.Content,
			Detail: fmt.Sprintf("contrast %.1f against %.1f at the start and %.1f at the end", f.contrast[i], f.contrast[0], f.contrast[f.n-1])})
		i = j
	}
}

func (f *Film) union(ts []int) image.Rectangle {
	var r image.Rectangle
	for _, t := range ts {
		r = r.Union(f.tileRect(t))
	}
	return r
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func clipList(s []string, n int) string {
	q := make([]string, 0, min(len(s), n))
	for _, x := range s[:min(len(s), n)] {
		if r := []rune(x); len(r) > 40 {
			x = string(r[:40]) + "…"
		}
		q = append(q, fmt.Sprintf("%q", x))
	}
	out := strings.Join(q, ", ")
	if len(s) > n {
		out += fmt.Sprintf(" and %d more", len(s)-n)
	}
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// align is how a tile's change from the frame before is best explained as
// a shift: the content now at (x, y) was at (x-dx, y-dy) before.
type align struct {
	dx, dy  int
	shifted bool // the shift leaves under shiftFit of the change
}

// align measures, for frame i, every tile that changed by PopMin or more
// from the frame before, how far a shift of up to searchShift pixels
// explains it. Content that slid in from beside the tile counts: the
// frame before is read whole.
func (f *Film) align(i int, step []float64) map[int]align {
	if i == 0 {
		return nil
	}
	prev, cur := f.ring[(i-1)%len(f.ring)], f.ring[i%len(f.ring)]
	var out map[int]align
	for t, s := range step {
		if s < PopMin || !f.inContent(t) {
			continue
		}
		r := f.tileRect(t)
		best, bx, by := s, 0, 0
		for dy := -searchShift; dy <= searchShift; dy++ {
			for dx := -searchShift; dx <= searchShift; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				if m := shiftedMad(prev, cur, r, dx, dy, best); m < best {
					best, bx, by = m, dx, dy
				}
			}
		}
		if out == nil {
			out = map[int]align{}
		}
		out[t] = align{dx: bx, dy: by, shifted: (bx != 0 || by != 0) && best < shiftFit*s}
	}
	return out
}

// shiftedMad is the mean absolute difference over r between cur and prev
// moved by (dx, dy); it stops early once it can no longer beat limit.
func shiftedMad(prev, cur *image.RGBA, r image.Rectangle, dx, dy int, limit float64) float64 {
	b := prev.Bounds()
	s, n := 0, 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		py := y - dy
		if py < b.Min.Y || py >= b.Max.Y {
			continue
		}
		for x := r.Min.X; x < r.Max.X; x++ {
			px := x - dx
			if px < b.Min.X || px >= b.Max.X {
				continue
			}
			i, j := cur.PixOffset(x, y), prev.PixOffset(px, py)
			for k := range 3 {
				d := int(cur.Pix[i+k]) - int(prev.Pix[j+k])
				if d < 0 {
					d = -d
				}
				s += d
			}
			n += 3
		}
	}
	if n < 3*r.Dx()*r.Dy()/2 {
		return limit
	}
	return float64(s) / float64(n)
}
