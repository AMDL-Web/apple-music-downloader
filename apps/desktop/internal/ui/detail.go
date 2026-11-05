package ui

import (
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/domain"
	"amdl/internal/format"
	"amdl/internal/progress"
)

// albumProgress is a job's album bar with how many tracks finished under
// its left end, the failed ones counted apart, and the overall percent
// under its right, which a job that failed or was cancelled leaves out:
// it no longer measures anything. ink is the text color the bar's live
// fill is drawn from.
func albumProgress(c *ui.Context, k *Tokens, j *store.Job, key string, h float32, ink, track, working, text ui.Color, size float32, page bool) {
	a := j.Album()
	done, failed, total := a.OnDisk, a.Failed, a.Total
	if total == 0 {
		// Before the tracks arrive the list feed still has the counts.
		done, failed, total = j.Done(), j.FailedItems, j.TotalItems
	}
	short := j.Status == domain.JobFailed || j.Status == domain.JobCancelled
	resolving := total == 0 && (j.Status == domain.JobQueued || j.Status == domain.JobRunning)
	ui.Column(c).Gap(5).Children(func() {
		widgets.AlbumBar{Album: a, Completed: k.OK, Skipped: k.Skip, Failed: k.Bad, Cancelled: k.Muted, Working: working,
			Live: ink.Alpha(0.38), Track: track, Resolving: resolving, Height: h}.Show(c, key)
		ui.Row(c).Gap(8).Children(func() {
			switch {
			case resolving:
				mono(c, "Resolving tracks", size).TextColor(text)
			default:
				// The album page's figures say the count: its bar says the
				// share, always.
				if !page {
					count := fmt.Sprintf("%d / %d finished", done, total)
					if failed > 0 {
						count += fmt.Sprintf(" · %d failed", failed)
					}
					mono(c, count, size).TextColor(text)
				}
				ui.Spacer(c)
				if !short || page {
					mono(c, fmt.Sprintf("%d%%", a.Percent), size).TextColor(text)
				}
			}
		})
	})
}

func firstWithCodec(j *store.Job) *domain.JobItem {
	for i := range j.Items {
		if j.Items[i].Codec != "" {
			return &j.Items[i]
		}
	}
	return nil
}

// savedDir is the folder a job's tracks went to, from the first track's
// output path, with the separator of the machine that wrote it: a remote
// backend's paths are its own, not this computer's.
func savedDir(j *store.Job) string {
	for _, it := range j.Items {
		if p := it.OutputPath; p != "" {
			if strings.Contains(p, "/") && !strings.Contains(p, `\`) {
				return path.Dir(p) + "/"
			}
			return filepath.Dir(p) + `\`
		}
	}
	return ""
}

// The track list's columns and rows, in DIPs.
const (
	// trackStateWidth is the state column's width, which its longest label
	// ("Preparing", "Cancelled") fits.
	trackStateWidth = 104
	// trackInset is the room between a row's hairline and its highlight,
	// so two highlighted rows stand apart; the highlight's own padding
	// keeps the row as tall as an idle one.
	trackInset = 3
	trackPadY  = 11 - trackInset
)

// pageWidth is the width of the page beside the sidebar.
func (a *App) pageWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	if a.SidebarShown() {
		w -= sidebarWidth
	}
	return w
}

// stageWidth is the width of the stage bars on a page width wide: all of
// 300 on a wide page, less as the page narrows, so the titles keep room
// beside the lengths and the activity.
func stageWidth(page float32) float32 {
	return min(max((page-640)*0.5, 100), 300)
}

// trackList lists a download's own tracks, as the album's songs are
// listed until the album is read, or where it is not an album's.
func (a *App) trackList(c *ui.Context, k *Tokens, items []domain.JobItem, accent ui.Color, bar float32) {
	still := a.listStill(c)
	a.scrollArea(c, "album", scrollGutter, func() {
		for i := range items {
			next := i+1 < len(items) && trackLit(&items[i+1])
			a.trackRow(c, k, &items[i], accent, bar, next, still)
		}
	}).Grow(1)
}

// itemRunning reports whether a track is in the pipeline now.
func itemRunning(it *domain.JobItem) bool {
	switch it.Status {
	case domain.ItemQueued, domain.ItemCompleted, domain.ItemSkipped, domain.ItemUnavailable, domain.ItemFailed, domain.ItemCancelled:
		return false
	}
	return true
}

// trackLit reports whether a track's row is highlighted: it is being
// worked on, not waiting for a slot.
func trackLit(it *domain.JobItem) bool {
	if !itemRunning(it) {
		return false
	}
	_, _, working := stateLabel(it)
	return working
}

// trackRow is one track: a hairline row whose content sits in a rounded
// highlight while the track is worked on. The highlight is inset from the
// hairline, so adjacent highlighted rows read as two, and the hairline
// beside one is hidden rather than removed, so a row keeps its height
// either way.
func (a *App) trackRow(c *ui.Context, k *Tokens, it *domain.JobItem, accent ui.Color, bar float32, nextLit, still bool) {
	lit := trackLit(it)
	line := k.Line
	if lit || nextLit {
		line = ui.Transparent
	}
	ui.Column(c.Key(it.ID)).PaddingY(trackInset).BorderWidth(0, 0, 1, 0).BorderColor(line).Children(func() {
		row := ui.Row(c).Gap(16).Padding(trackPadY, 4).AlignItems(ui.Center).Radius(radiusControl)
		if lit {
			row.Background(k.AccentSoft.Alpha(0.45))
		}
		row.Children(func() {
			mono(c, fmt.Sprint(it.Index), 12).TextColor(k.Faint).Width(trackNumber).TextAlign(ui.End)
			trackNames(c, k, it.Title, false, a.trackSubText(it))
			stageCell(c, k, it, accent, bar, still)
			trackDuration(c, k, a.songLength(it.DurationMS))
			ui.Row(c).Width(trackStateWidth).Height(trackControl).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() { trackState(c, k, it) })
		})
	})
}

// stageCell is a track's stage bar in a column bar wide, which grows while
// a download runs and shrinks away after it; it is empty for a song the
// download has not listed.
func stageCell(c *ui.Context, k *Tokens, it *domain.JobItem, accent ui.Color, bar float32, still bool) {
	if bar < 0.5 {
		return
	}
	ui.Box(c).Width(bar).Clip().Children(func() {
		if it != nil {
			widgets.StageBar{Stages: progress.ItemStages(*it), Color: accent, Track: k.Track, Running: itemRunning(it), Still: still,
				Indeterminate: progress.Indeterminate(*it), Failed: it.Status == domain.ItemFailed, Bad: k.Bad,
				Finished: true, Done: k.Fg.Alpha(0.85)}.Show(c, "bar")
		}
	})
}

func trackSub(it *domain.JobItem) string {
	q := strings.ToLower(format.Quality(it.Codec, it.BitDepth, it.SampleRate, it.Bitrate))
	// A queued track's wait is said by its state column (stateLabel).
	switch {
	case it.Attempt > 1 && it.MaxAttempts > 0 && it.Status != domain.ItemCompleted:
		if q == "" {
			return fmt.Sprintf("retry %d of %d", it.Attempt, it.MaxAttempts)
		}
		return fmt.Sprintf("%s → retry %d of %d", q, it.Attempt, it.MaxAttempts)
	case it.FileSize > 0:
		return q + " · " + format.Bytes(it.FileSize)
	case it.StatusMessage != "" && it.Status == domain.ItemFailed:
		return it.StatusMessage
	case it.Status == domain.ItemResolving:
		return "fetching metadata, cover, lyrics"
	}
	return q
}

// stateLabel is what the state column says of a track, its icon and
// whether it is in the accent (working) or muted (waiting): short words of
// one column width.
func stateLabel(it *domain.JobItem) (ic *ui.SVG, text string, working bool) {
	pct := func(f float64, word string) string {
		if f > 0 {
			return fmt.Sprintf("%d%%", int(f*100))
		}
		return word
	}
	switch it.Status {
	case domain.ItemQueued, domain.ItemWaitingDownload:
		return icClock, "Waiting", false
	case domain.ItemResolving:
		return icSearch, "Preparing", true
	case domain.ItemDownloading:
		return icDown, pct(it.Progress.Download, "Download"), true
	case domain.ItemWaitingDecrypt:
		return icLock, "Waiting", false
	case domain.ItemDecrypting:
		return icLock, pct(it.Progress.Decrypt, "Decrypt"), true
	case domain.ItemRemuxing:
		return icNote, "Remux", true
	case domain.ItemTagging:
		return icNote, "Tagging", true
	case domain.ItemSaving:
		return icNote, "Saving", true
	}
	return icNote, "Working", true
}

func trackState(c *ui.Context, k *Tokens, it *domain.JobItem) {
	show := func(ic *ui.SVG, text string, col ui.Color) {
		ui.Icon(c, ic).FontSize(13).TextColor(col)
		ui.Text(c, text).FontSize(12).FontWeight(600).FontFeatures("tnum").TextColor(col).SingleLine()
	}
	switch it.Status {
	case domain.ItemCompleted:
		show(icCheck, "Saved", k.OK)
	case domain.ItemSkipped:
		show(icCheck, "On disk", k.Skip)
	case domain.ItemUnavailable:
		show(icCross, "Not offered", k.Skip)
	case domain.ItemFailed:
		show(icCross, "Failed", k.Bad)
	case domain.ItemCancelled:
		show(icCross, "Cancelled", k.Muted)
	default:
		ic, text, working := stateLabel(it)
		col := k.Muted
		if working {
			col = k.Accent
		}
		show(ic, text, col)
	}
}

func (a *App) activity(c *ui.Context, k *Tokens, j *store.Job, width float32) {
	ui.Column(c).Width(width).Padding(16, 20).Gap(4).BorderWidth(0, 0, 0, 1).BorderColor(k.Line).Children(func() {
		label(c, k, "Activity").Margin(0, 0, 6, 0)
		a.scrollArea(c, "activity", scrollGutter, func() {
			for i, ev := range j.Activity {
				ui.Row(c.Key(i)).FillWidth().Gap(10).PaddingY(6).AlignItems(ui.Start).Children(func() {
					mono(c, format.Seconds(ev.At), 11).TextColor(k.Faint).Width(62).Shrink(0)
					col := k.Fg
					switch ev.Kind {
					case store.ActivityWarn:
						col = k.Warn
					case store.ActivityFailed:
						col = k.Bad
					}
					// Long lines, as a retry's error with its URL, wrap
					// inside the column.
					ui.Text(c, ev.Text).FontSize(12).LineHeight(1.35).TextColor(col).Grow(1).Shrink(1).MinWidth(0)
				})
			}
		}).Grow(1)
		if dir := savedDir(j); dir != "" {
			ui.Column(c).Gap(4).Padding(10, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(k.Line).Children(func() {
				label(c, k, "Saved to")
				ui.Text(c, dir).Font(MonoFont).FontSize(11).TextColor(k.Muted)
			})
		}
	})
}

// revealLabel names the file manager as the platform does.
var revealLabel = func() string {
	switch runtime.GOOS {
	case "darwin":
		return "Reveal in Finder"
	case "windows":
		return "Show in Explorer"
	}
	return "Show in folder"
}()

// detailInk is how much of the ink the album screen's lower half mixes into
// the ground, and detailGround the color that makes.
const detailInk = 0.72

func detailGround(k *Tokens) ui.Color { return k.Ground.Mix(k.Ink, detailInk) }
