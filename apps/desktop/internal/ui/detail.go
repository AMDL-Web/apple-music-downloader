package ui

import (
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/format"
	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/domain"
)

// detail is one job: the album in big type over its palette, its numbers,
// every track's stage bar, and what happened, newest first.
func (a *App) detail(c *ui.Context, k *Tokens) {
	j := a.Store.Detail()
	if j == nil {
		a.Go(ScreenQueue)
		return
	}
	video := a.motionPlayer(c, j.MotionArtworkURL, 248)
	pal := jobPalette(j, video != nil)
	hc := heroColorsFor(pal, k)
	ui.Row(c).Padding(26, 32, 22, 32).Gap(32).AlignItems(ui.End).Children(func() {
		cover := widgets.Cover{Image: a.Art.Get(j.ArtworkURL, 512), Placeholder: pal.Bg, Size: 248, Radius: 16, Color: 1, Shadow: true, Video: video}.Show(c)
		if j.MotionArtworkURL != "" {
			cover.Children(func() { motionBadge(c, "MOTION") })
		}
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(12).Children(func() {
			kind := strings.ToUpper(j.Type[:min(1, len(j.Type))]) + j.Type[min(1, len(j.Type)):]
			if j.TotalItems > 0 {
				kind += fmt.Sprintf(" · %d tracks", j.TotalItems)
			}
			label(c, k, kind).TextColor(hc.label)
			display(c, j.Name(), 52).TextColor(hc.title).MaxLines(2).Ellipsis("…")
			ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, j.By()).FontSize(17).TextColor(hc.title.Mix(hc.meta, 0.4)).SingleLine()
				if it := firstWithCodec(j); it != nil {
					q := format.LongQuality(it.Codec, it.BitDepth, it.SampleRate)
					tag(c, k, q, isHiRes(format.Quality(it.Codec, it.BitDepth, it.SampleRate, 0)), hc.meta)
				}
				if b := j.Bytes(); b > 0 {
					tag(c, k, fmt.Sprintf("%d tracks · %s", j.TotalItems, format.Bytes(b)), false, hc.meta)
				}
			})
			ui.Row(c).Gap(12).MaxWidth(620).Margin(8, 0, 0, 0).Children(func() {
				stat := func(name, value, unit string) {
					ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
						label(c, k, name).TextColor(hc.meta.Alpha(0.8))
						ui.Row(c).AlignItems(ui.Baseline).Children(func() {
							mono(c, value, 22).FontWeight(600).TextColor(hc.title)
							if unit != "" {
								mono(c, unit, 14).TextColor(hc.meta.Alpha(0.8))
							}
						})
					})
				}
				stat("Done", fmt.Sprint(j.Done()), fmt.Sprintf(" / %d", j.TotalItems))
				if sp := j.AvgSpeed(c.Now()); sp > 0 {
					v, u := format.Scale(sp)
					stat("Avg speed", format.Number(v, 1), " "+u+"/s")
				} else {
					stat("Avg speed", "—", "")
				}
				if r := format.Remaining(j.Remaining); r != "" {
					stat("Left", r, "")
				} else if j.FinishedAt != nil {
					stat("Finished", format.Clock(*j.FinishedAt), "")
				} else {
					stat("Left", "—", "")
				}
				if j.StartedAt != nil {
					stat("Started", format.Clock(*j.StartedAt), "")
				} else {
					stat("Created", format.Clock(j.CreatedAt), "")
				}
			})
			ui.Column(c).FillWidth().MaxWidth(620).Margin(6, 0, 0, 0).Children(func() {
				albumProgress(c, k, j, "album", 8, hc.title, hc.title.Alpha(0.16), hc.accent, hc.meta, 12)
			})
		})
		ui.Column(c).Gap(8).Children(func() {
			if a.Settings.Mode == mode.Standalone {
				if dir := savedDir(j); dir != "" && button(c, k, buttonOnArt, revealLabel, nil).Clicked() {
					a.Act.Reveal(dir)
				}
			}
			switch j.Status {
			case domain.JobQueued, domain.JobRunning:
				if button(c, k, buttonOnArt, "Cancel", nil).Clicked() {
					a.Store.Cancel(j.ID)
				}
			case domain.JobFailed, domain.JobCancelled:
				if button(c, k, buttonOnArt, "Retry", nil).Clicked() {
					a.Store.Retry(j.ID)
				}
			}
		})
	})
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Background(k.Ink.Alpha(0.72)).BorderWidth(1, 0, 0, 0).BorderColor(k.Line).Children(func() {
		a.trackList(c, k, j, hc.accent)
		a.activity(c, k, j)
	})
}

// albumProgress is a job's album bar with how many tracks finished under
// its left end and the overall percent under its right. ink is the text
// color the bar's live fill is drawn from.
func albumProgress(c *ui.Context, k *Tokens, j *store.Job, key string, h float32, ink, track, working, text ui.Color, size float32) {
	a := j.Album()
	lit, total := a.Lit, a.Total
	if total == 0 {
		// Before the tracks arrive the list feed still has the counts.
		lit, total = j.Done(), j.TotalItems
	}
	resolving := total == 0 && (j.Status == domain.JobQueued || j.Status == domain.JobRunning)
	ui.Column(c).Gap(5).Children(func() {
		widgets.AlbumBar{Album: a, Completed: k.OK, Skipped: k.Skip, Failed: k.Bad, Cancelled: k.Muted, Working: working,
			Live: ink.Alpha(0.38), Track: track, Resolving: resolving, Height: h}.Show(c, key)
		ui.Row(c).Gap(8).Children(func() {
			switch {
			case resolving:
				mono(c, "Resolving tracks", size).TextColor(text)
			default:
				mono(c, fmt.Sprintf("%d / %d finished", lit, total), size).TextColor(text)
				ui.Spacer(c)
				mono(c, fmt.Sprintf("%d%%", a.Percent), size).TextColor(text)
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

func (a *App) trackList(c *ui.Context, k *Tokens, j *store.Job, accent ui.Color) {
	ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Padding(10, 24, 0, 32).Children(func() {
		ui.Row(c).Gap(16).Padding(11, 4, 4, 4).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(28)
			label(c, k, "Track").Grow(1)
			ui.Row(c).Width(300).Gap(3).Children(func() {
				for i, w := range store.StageWeights {
					mono(c, store.StageLabels[i], 10).TextColor(k.Faint).Grow(float32(w)).Basis(0).ClipX()
				}
			})
			label(c, k, "State").Width(104).TextAlign(ui.End)
		})
		if len(j.Items) == 0 {
			ui.Text(c, "Tracks appear once the link resolves.").TextColor(k.Muted).Padding(12, 4)
			return
		}
		ui.Scroll(c.Key("tracks")).Grow(1).Children(func() {
			for i := range j.Items {
				a.trackRow(c, k, &j.Items[i], accent)
			}
		})
	})
}

func (a *App) trackRow(c *ui.Context, k *Tokens, it *domain.JobItem, accent ui.Color) {
	running := it.Status != domain.ItemQueued && it.Status != domain.ItemCompleted && it.Status != domain.ItemSkipped &&
		it.Status != domain.ItemFailed && it.Status != domain.ItemCancelled
	row := ui.Row(c.Key(it.ID)).Gap(16).Padding(11, 4).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(k.Line)
	if running {
		row.Background(k.AccentSoft.Alpha(0.45)).Radius(8)
	}
	row.Children(func() {
		mono(c, fmt.Sprint(it.Index), 12).TextColor(k.Faint).Width(28).TextAlign(ui.End)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, it.Title).FontWeight(600).Ellipsis("…").SingleLine()
			mono(c, trackSub(it), 11).TextColor(k.Muted).Ellipsis("…")
		})
		ui.Box(c).Width(300).Children(func() {
			widgets.StageBar{Stages: store.ItemStages(*it), Color: accent, Track: k.Track, Running: running,
				Indeterminate: store.Indeterminate(*it), Failed: it.Status == domain.ItemFailed, Bad: k.Bad,
				Finished: true, Done: k.Fg.Alpha(0.85)}.Show(c, "bar")
		})
		ui.Row(c).Width(104).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() { trackState(c, k, it) })
	})
}

func trackSub(it *domain.JobItem) string {
	q := strings.ToLower(format.Quality(it.Codec, it.BitDepth, it.SampleRate, it.Bitrate))
	switch {
	case it.Status == domain.ItemQueued:
		return "waiting for a download slot"
	case it.Attempt > 1 && it.MaxAttempts > 0 && it.Status != domain.ItemCompleted:
		return fmt.Sprintf("%s → retry %d of %d", q, it.Attempt, it.MaxAttempts)
	case it.FileSize > 0:
		return q + " · " + format.Bytes(it.FileSize)
	case it.StatusMessage != "" && it.Status == domain.ItemFailed:
		return it.StatusMessage
	}
	return q
}

func trackState(c *ui.Context, k *Tokens, it *domain.JobItem) {
	show := func(ic *ui.SVG, text string, col ui.Color) {
		ui.Icon(c, ic).FontSize(13).TextColor(col)
		ui.Text(c, text).FontSize(12).FontWeight(600).TextColor(col).SingleLine()
	}
	switch it.Status {
	case domain.ItemCompleted:
		show(icCheck, "Saved", k.OK)
	case domain.ItemSkipped:
		show(icCheck, "On disk", k.Skip)
	case domain.ItemFailed:
		show(icCross, "Failed", k.Bad)
	case domain.ItemCancelled:
		show(icCross, "Cancelled", k.Muted)
	case domain.ItemQueued, domain.ItemWaitingDownload:
		show(icClock, "Waiting", k.Muted)
	case domain.ItemDownloading:
		if it.Progress.Download > 0 {
			show(icDown, fmt.Sprintf("%d%%", int(it.Progress.Download*100)), k.Accent)
		} else {
			show(icDown, "Download", k.Accent)
		}
	case domain.ItemWaitingDecrypt, domain.ItemDecrypting:
		show(icLock, "Decrypt", k.Accent)
	default:
		show(icNote, "Finishing", k.Accent)
	}
}

func (a *App) activity(c *ui.Context, k *Tokens, j *store.Job) {
	ui.Column(c).Width(280).Padding(16, 20).Gap(4).BorderWidth(0, 0, 0, 1).BorderColor(k.Line).Children(func() {
		label(c, k, "Activity").Margin(0, 0, 6, 0)
		ui.Scroll(c.Key("activity")).Grow(1).Children(func() {
			if len(j.Activity) == 0 {
				ui.Text(c, "Nothing yet.").FontSize(12).TextColor(k.Muted)
			}
			for i, ev := range j.Activity {
				ui.Row(c.Key(i)).Gap(10).PaddingY(6).AlignItems(ui.Start).Children(func() {
					mono(c, format.Seconds(ev.At), 11).TextColor(k.Faint).Width(62)
					col := k.Fg
					switch ev.Kind {
					case store.ActivityWarn:
						col = k.Warn
					case store.ActivityFailed:
						col = k.Bad
					}
					ui.Text(c, ev.Text).FontSize(12).TextColor(col).Grow(1).Shrink(1)
				})
			}
		})
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
