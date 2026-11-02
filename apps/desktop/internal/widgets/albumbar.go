package widgets

import (
	"github.com/egoist/mygo/ui"

	"amdl/internal/domain"
	"amdl/internal/progress"
)

// AlbumBar is a job's album bar (progress.Album): the finished tracks fill
// solid, left to right in the order they finished and colored by outcome;
// the overall progress fills translucent behind them; and the bar is cut
// into one cell per track while each cell stays wide enough.
type AlbumBar struct {
	Album progress.Album
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
	el := ui.Box(c.Key(key)).Height(h).FillWidth()
	d := *ui.Local(el, "draw", newAlbumDraw)
	d.live = el.AnimateTo("live", float32(b.Album.Live), stageAnimation)
	// The solid fill eases out to its new end, so a newly lit cell sweeps in.
	d.done = el.AnimateTo("done", float32(b.Album.Done), stageAnimation)
	d.b, d.h = b, h
	d.reduce = c.Preferences().ReduceMotion
	el.Draw(d.fn)
	return el
}

// albumDraw is an AlbumBar's drawing as a frame builds it, kept with its
// element as stageDraw is; its cell and span are the cell drawn and the
// width the shares are laid along, which fillCell reads.
type albumDraw struct {
	b             AlbumBar
	h, live, done float32
	reduce        bool
	p             *ui.Painter
	r, cell       ui.Rect
	span          float32
	fn            func(*ui.Painter, ui.Rect)
	fillFn        func()
}

func newAlbumDraw() *albumDraw {
	d := &albumDraw{}
	d.fn, d.fillFn = d.draw, d.fillCell
	return d
}

func (d *albumDraw) draw(p *ui.Painter, r ui.Rect) {
	radius := min(d.h/2, 2.5)
	n := d.b.Album.Ticks
	if n > 0 && (r.W+albumCellGap)/float32(n)-albumCellGap < albumCellMin {
		n = 0
	}
	// Shares are laid along span; with cells it runs one gap past the
	// end, so each run ends in the gap after its last cell.
	d.p, d.r, d.span = p, r, r.W
	if n > 0 {
		d.span += albumCellGap
	}
	if n == 0 {
		d.cell = r
		p.Clip(r, radius, d.fillFn)
	} else {
		cell := d.span / float32(n)
		for i := range n {
			d.cell = ui.Rect{X: r.X + float32(i)*cell, Y: r.Y, W: cell - albumCellGap, H: d.h}
			p.Clip(d.cell, radius, d.fillFn)
		}
	}
	d.p = nil
}

// fillCell fills d.cell, under its clip.
func (d *albumDraw) fillCell() {
	p, b, r, clip, h := d.p, &d.b, d.r, d.cell, d.h
	p.Fill(clip, b.Track, 0)
	if b.Resolving {
		p.Fill(clip, b.Working.Alpha(0.45), 0)
		stripes(p, clip, 0, d.reduce)
		return
	}
	if w := d.span * min(d.live, 1); w > 0 {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: w, H: h}, b.Live, 0)
	}
	end := r.X + d.span*min(d.done, 1)
	x := r.X
	for _, run := range b.Album.Runs {
		w := min(d.span*float32(run.Share), end-x)
		if w <= 0 {
			break
		}
		p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: h}, b.outcome(run.Status), 0)
		x += w
	}
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
