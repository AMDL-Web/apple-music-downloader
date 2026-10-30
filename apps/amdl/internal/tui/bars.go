package tui

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"

	"amdl/internal/domain"
	"amdl/internal/progress"
)

var eighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// fill draws w cells filled to f with eighth-cell precision.
func fill(w int, f float64, on, off color.Color) string {
	if w <= 0 {
		return ""
	}
	e := int(math.Round(math.Max(0, math.Min(1, f)) * float64(w) * 8))
	full, part := e/8, e%8
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(on).Render(strings.Repeat("█", full)))
	rest := w - full
	if part > 0 && rest > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(on).Background(off).Render(eighths[part]))
		rest--
	}
	if rest > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(off).Render(strings.Repeat("█", rest)))
	}
	return b.String()
}

// stageWidths splits w cells by the stage weights, each stage at least one
// cell and download and decrypt sharing the rest.
func stageWidths(w int) [progress.StageCount]int {
	var out [progress.StageCount]int
	small := 0
	for i, x := range progress.Weights {
		if s := progress.Stage(i); s != progress.StageDownload && s != progress.StageDecrypt {
			out[i] = max(1, int(math.Round(x*float64(w))))
			small += out[i]
		}
	}
	left := max(2, w-small)
	dl, dc := progress.Weights[progress.StageDownload], progress.Weights[progress.StageDecrypt]
	out[progress.StageDownload] = int(math.Round(float64(left) * dl / (dl + dc)))
	out[progress.StageDecrypt] = left - out[progress.StageDownload]
	return out
}

// stageBar draws one track's seven stages: a one-cell gap between stages when
// the bar is wide enough, alternating shades when it is not. A failed track
// paints the stage it stopped in.
func stageBar(t theme, w int, it domain.JobItem, accent color.Color) string {
	st := progress.ItemStages(it)
	failedAt := -1
	if it.Status == domain.ItemFailed {
		failedAt = int(st.Current())
	}
	gaps := w >= 36
	inner := w
	if gaps {
		inner -= int(progress.StageCount) - 1
	}
	widths := stageWidths(inner)
	var b strings.Builder
	for i, cw := range widths {
		on, off := accent, t.faint
		if !gaps && i%2 == 1 {
			off = t.gap
		}
		f := st[i]
		if i == failedAt {
			on, f = t.bad, 1
		}
		if it.Status == domain.ItemSkipped {
			on = t.skipped
		}
		b.WriteString(fill(cw, f, on, off))
		if gaps && i < len(widths)-1 {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// albumBar draws the iOS app's album bar in w cells: one bar with a solid fill
// for the finished tracks, run by run in finish order and coloured by outcome,
// and a translucent fill out to the overall stage-weighted progress. With
// 2..progress.TickMax tracks and room for it the bar is cut into one cell
// per track.
func albumBar(t theme, w int, a progress.Album, accent color.Color) string {
	if w <= 0 {
		return ""
	}
	cells := a.Ticks
	cellW := 0
	if cells > 0 {
		cellW = (w + 1) / cells // each cell plus a one-column gap
		if cellW < 3 {
			cells = 0
		}
	}
	outcome := func(s domain.ItemStatus) color.Color {
		switch s {
		case domain.ItemSkipped:
			return t.skipped
		case domain.ItemFailed:
			return t.bad
		case domain.ItemCancelled:
			return t.dim
		}
		return accent
	}
	// colorAt is the color of the bar at fraction x of its length.
	colorAt := func(x float64) color.Color {
		edge := 0.0
		for _, r := range a.Runs {
			edge += r.Share
			if x < edge-1e-9 {
				return outcome(r.Status)
			}
		}
		if x < a.Live-1e-9 {
			return t.soft(accent)
		}
		return t.faint
	}
	var b strings.Builder
	if cells == 0 {
		for i := 0; i < w; i++ {
			b.WriteString(t.style(colorAt((float64(i) + 0.5) / float64(w))).Render("━"))
		}
		return b.String()
	}
	avail := w - (cells - 1)
	for c := 0; c < cells; c++ {
		lo := int(math.Round(float64(c) * float64(avail) / float64(cells)))
		hi := int(math.Round(float64(c+1) * float64(avail) / float64(cells)))
		for i := lo; i < hi; i++ {
			b.WriteString(t.style(colorAt((float64(i) + 0.5) / float64(avail))).Render("━"))
		}
		if c < cells-1 {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// albumLine is the album bar between its count and its percent:
// "3/12 ━━━━━━━━━━━ 42%".
func albumLine(t theme, w int, a progress.Album, accent color.Color, extra string) string {
	left := fmt.Sprintf("%d/%d", a.Lit, a.Total)
	right := fmt.Sprintf("%d%%", a.Percent)
	if extra != "" {
		right += "  " + extra
	}
	bw := w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if bw < 8 {
		return t.style(t.fg).Render(left) + " " + t.style(t.dim).Render(right)
	}
	return t.style(t.fg).Render(left) + " " + albumBar(t, bw, a, accent) + " " + t.style(t.dim).Render(right)
}
