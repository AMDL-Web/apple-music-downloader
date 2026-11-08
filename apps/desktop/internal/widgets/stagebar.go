// Package widgets draws the desktop app's own controls: the weighted stage
// bar, the album bar, covers that color in as they download, the
// album-palette backdrop and the motion-cover player.
package widgets

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/internal/progress"
)

// StageBar is one track's or one job's pipeline drawn as seven segments
// whose widths are progress.Weights.
type StageBar struct {
	Stages progress.Stages
	// Color fills done and running segments: the album's own color.
	Color ui.Color
	// Track is an empty segment.
	Track ui.Color
	// Failed paints the first unfinished segment in Bad, where a failed or
	// cancelled track stopped.
	Failed bool
	Bad    ui.Color
	// Finished paints a completed bar in Done, quieter than Color.
	Finished bool
	Done     ui.Color
	// Running draws moving stripes over the segment being worked on;
	// Indeterminate fills it with them when it has no fraction yet.
	Running, Indeterminate bool
	// Still holds the stripes in place, as the desktop's reduced motion
	// does: moving, they pace the window to 30 frames a second.
	Still       bool
	Height, Gap float32
}

// stageAnimation eases segment fills, so a retry that sends a track back to
// resolving visibly retreats instead of jumping.
var stageAnimation = ui.Smooth

// Show builds the bar. key separates the fill animations of bars built from
// the same call site, such as the rows of a list.
func (b StageBar) Show(c *ui.Context, key any) ui.Element {
	h, gap := b.Height, b.Gap
	if h == 0 {
		h = 6
	}
	if gap == 0 {
		gap = 2
	}
	el := ui.Box(c.Key(key)).Height(h).FillWidth()
	d := *ui.Local(el, "draw", newStageDraw)
	for i, v := range b.Stages {
		d.fill[i] = el.AnimateTo(i, float32(v), stageAnimation)
	}
	d.b, d.h, d.gap = b, h, gap
	d.current = b.Stages.Current()
	d.reduce = b.Still || c.Preferences().ReduceMotion
	el.Draw(d.fn)
	return el
}

// stageDraw is a StageBar's drawing as a frame builds it, kept with its
// element (ui.Local) so that a frame sets it rather than allocating a
// closure: drawing runs every frame its stripes move.
type stageDraw struct {
	b       StageBar
	h, gap  float32
	fill    [progress.StageCount]float32
	current progress.Stage
	reduce  bool
	phase   stripePhase
	fn      func(*ui.Painter, ui.Rect)
}

func newStageDraw() *stageDraw {
	d := &stageDraw{}
	d.fn = d.draw
	return d
}

func (d *stageDraw) draw(p *ui.Painter, r ui.Rect) {
	b, h, gap, current := &d.b, d.h, d.gap, d.current
	avail := r.W - gap*float32(progress.StageCount-1)
	x := r.X
	radius := min(h/2, 2.5)
	for i, w := range progress.Weights {
		sw := max(avail*float32(w), 3)
		seg := ui.Rect{X: x, Y: r.Y, W: sw, H: h}
		x += sw + gap
		p.Fill(seg, b.Track, radius)
		color := b.Color
		if b.Finished && current == progress.StageCount {
			color = b.Done
		}
		if b.Failed && progress.Stage(i) == current {
			p.Fill(seg, b.Bad, radius)
			continue
		}
		f := d.fill[i]
		if b.Indeterminate && b.Running && progress.Stage(i) == current {
			f = 1
			color = color.Alpha(0.45)
		}
		if f <= 0.001 {
			continue
		}
		done := ui.Rect{X: seg.X, Y: seg.Y, W: max(seg.W*min(f, 1), min(seg.W, h)), H: h}
		p.Fill(done, color, radius)
		if b.Running && progress.Stage(i) == current && current < progress.StageCount {
			stripes(p, done, radius, d.phase.shift(p.Now(), d.reduce))
			if !d.reduce {
				p.AnimationFrameRate(30)
			}
		}
	}
}

// stripePeriod is the distance between two stripes, and stripeInk their
// color.
const stripePeriod = 11.3

var stripeInk = ui.RGBA(255, 255, 255, 0.28)

// stripeBands is what fillStripes draws, set by stripes: one path serves
// every band, the painter flattening it as it fills it, and a func literal
// capturing the bands' place would be allocated on every frame they move.
// Drawing is on the UI thread only.
var stripeBands struct {
	p     *ui.Painter
	r     ui.Rect
	shift float32
	path  ui.Path
}

// stripePhase is how far a bar's stripes have moved: a period a second
// while they move, held where they are while still, and on from there
// after, so a flight or a scroll that stills them leaves no jump.
type stripePhase struct {
	at   float32
	last time.Time
}

func (s *stripePhase) shift(now time.Time, still bool) float32 {
	if !still && !s.last.IsZero() && now.After(s.last) {
		s.at = float32(math.Mod(float64(s.at)+stripePeriod*now.Sub(s.last).Seconds(), stripePeriod))
	}
	s.last = now
	return s.at
}

// stripes draws the diagonal stripes of a running segment, shift along.
func stripes(p *ui.Painter, r ui.Rect, radius, shift float32) {
	s := &stripeBands
	s.p, s.r, s.shift = p, r, shift
	p.Clip(r, radius, fillStripes)
	s.p = nil
}

func fillStripes() {
	s := &stripeBands
	r := s.r
	for x := r.X - r.H - stripePeriod + s.shift; x < r.X+r.W+r.H; x += stripePeriod {
		s.path.Reset().MoveTo(x, r.Y+r.H).LineTo(x+4, r.Y+r.H).LineTo(x+4+r.H, r.Y).LineTo(x+r.H, r.Y).Close()
		s.p.FillPath(&s.path, stripeInk)
	}
}
