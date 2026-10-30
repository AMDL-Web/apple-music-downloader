package widgets

import (
	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/domain"
)

// AlbumBar is a job's album bar (store.Album): the finished tracks fill
// solid, left to right in the order they finished and colored by outcome;
// the overall progress fills translucent behind them; and the bar is cut
// into one cell per track while each cell stays wide enough.
type AlbumBar struct {
	Album store.Album
	// The outcome colors fill a finished track's share; Working fills a
	// single track that is still working.
	Completed, Skipped, Failed, Cancelled, Working ui.Color
	// Live is the translucent overall fill, Track the empty bar.
	Live, Track ui.Color
	// Resolving draws moving stripes while the job has no tracks yet.
	Resolving bool
	Height    float32
}

// albumCellMin is the narrowest a cell may be: below it the bar is drawn
// whole, with the outcome colors as runs, rather than as slivers.
const albumCellMin, albumCellGap = 6, 2

// Show builds the bar.
func (b AlbumBar) Show(c *ui.Context, key any) ui.Element {
	h := b.Height
	if h == 0 {
		h = 6
	}
	a := b.Album
	el := ui.Box(c.Key(key)).Height(h).FillWidth()
	live := el.AnimateTo("live", float32(a.Live), stageAnimation)
	// The solid fill eases out to its new end, so a newly lit cell sweeps in.
	done := el.AnimateTo("done", float32(a.Done), stageAnimation)
	reduce := c.Preferences().ReduceMotion
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		radius := min(h/2, 2.5)
		n := a.Ticks
		if n > 0 && (r.W+albumCellGap)/float32(n)-albumCellGap < albumCellMin {
			n = 0
		}
		// Shares are laid along span; with cells it runs one gap past the
		// end, so each run ends in the gap after its last cell.
		span := r.W
		if n > 0 {
			span += albumCellGap
		}
		paint := func(clip ui.Rect) {
			p.Clip(clip, radius, func() {
				p.Fill(clip, b.Track, 0)
				if b.Resolving {
					p.Fill(clip, b.Working.Alpha(0.45), 0)
					stripes(p, clip, 0, reduce)
					return
				}
				if w := span * min(live, 1); w > 0 {
					p.Fill(ui.Rect{X: r.X, Y: r.Y, W: w, H: h}, b.Live, 0)
				}
				end := r.X + span*min(done, 1)
				x := r.X
				for _, run := range a.Runs {
					w := min(span*float32(run.Share), end-x)
					if w <= 0 {
						break
					}
					p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: h}, b.outcome(run.Status), 0)
					x += w
				}
			})
		}
		if n == 0 {
			paint(r)
			return
		}
		cell := span / float32(n)
		for i := range n {
			paint(ui.Rect{X: r.X + float32(i)*cell, Y: r.Y, W: cell - albumCellGap, H: h})
		}
	})
	return el
}

func (b AlbumBar) outcome(s domain.ItemStatus) ui.Color {
	switch s {
	case domain.ItemCompleted:
		return b.Completed
	case domain.ItemSkipped:
		return b.Skipped
	case domain.ItemFailed:
		return b.Failed
	case domain.ItemCancelled:
		return b.Cancelled
	}
	return b.Working
}
