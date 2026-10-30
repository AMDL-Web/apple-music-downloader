// Package widgets draws the desktop app's own controls: the weighted stage
// bar, the album bar, covers that color in as they download, the
// album-palette backdrop and the motion-cover player.
package widgets

import (
	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// StageBar is one track's or one job's pipeline drawn as seven segments
// whose widths are store.StageWeights.
type StageBar struct {
	Stages store.Stages
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
	Height, Gap            float32
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
	var fill [store.StageCount]float32
	for i, v := range b.Stages {
		fill[i] = el.AnimateTo(i, float32(v), stageAnimation)
	}
	current := b.Stages.Current()
	reduce := c.Preferences().ReduceMotion
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		avail := r.W - gap*float32(store.StageCount-1)
		x := r.X
		radius := min(h/2, 2.5)
		for i, w := range store.StageWeights {
			sw := max(avail*float32(w)/100, 3)
			seg := ui.Rect{X: x, Y: r.Y, W: sw, H: h}
			x += sw + gap
			p.Fill(seg, b.Track, radius)
			color := b.Color
			if b.Finished && current == store.StageCount {
				color = b.Done
			}
			if b.Failed && store.Stage(i) == current {
				p.Fill(seg, b.Bad, radius)
				continue
			}
			f := fill[i]
			if b.Indeterminate && b.Running && store.Stage(i) == current {
				f = 1
				color = color.Alpha(0.45)
			}
			if f <= 0.001 {
				continue
			}
			done := ui.Rect{X: seg.X, Y: seg.Y, W: max(seg.W*min(f, 1), min(seg.W, h)), H: h}
			p.Fill(done, color, radius)
			if b.Running && store.Stage(i) == current && current < store.StageCount {
				stripes(p, done, radius, reduce)
			}
		}
	})
	return el
}

// stripes draws the moving diagonal stripes of a running segment.
func stripes(p *ui.Painter, r ui.Rect, radius float32, still bool) {
	const period = 11.3
	shift := float32(0)
	if !still {
		ms := p.Now().UnixMilli() % 1000
		shift = period * float32(ms) / 1000
		p.AnimationFrameRate(30)
	}
	ink := ui.RGBA(255, 255, 255, 0.28)
	p.Clip(r, radius, func() {
		for x := r.X - r.H - period + shift; x < r.X+r.W+r.H; x += period {
			var band ui.Path
			band.MoveTo(x, r.Y+r.H).LineTo(x+4, r.Y+r.H).LineTo(x+4+r.H, r.Y).LineTo(x+r.H, r.Y).Close()
			p.FillPath(&band, ink)
		}
	})
}
