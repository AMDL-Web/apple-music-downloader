package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/format"
)

// heroJob is the job whose palette lights the queue: the one downloading
// now, else the one that finished last.
func (a *App) heroJob() *store.Job {
	if a.Screen != ScreenQueue {
		return nil
	}
	if j := a.Store.Now(); j != nil {
		return j
	}
	if f := a.Store.Finished(); len(f) > 0 {
		return f[0]
	}
	return nil
}

// backdrop lights the top of the main column with the hero's palette.
func (a *App) backdrop(c *ui.Context, k *Tokens) {
	if a.Screen == ScreenAlbum {
		paletteBackdrop(c, k, a.pagePalette(c))
		return
	}
	if a.Screen == ScreenHome {
		if pal := a.homePalette(); pal.OK {
			homeBackdrop(c, k, pal)
			return
		}
	}
	j := a.heroJob()
	if j == nil {
		return
	}
	// A narrow window's queue hero shows the still cover.
	pal := jobPalette(j, !a.compact && a.motionShown(c, j.MotionArtworkURL))
	paletteBackdrop(c, k, pal)
}

// paletteBackdrop lights the top of the main column with pal.
func paletteBackdrop(c *ui.Context, k *Tokens, pal Palette) {
	if !pal.OK {
		return
	}
	strength := float32(0.55)
	if !k.Dark {
		strength = 0.28
	}
	widgets.Backdrop{Light: pal.Bg, Deep: pal.Bg.Mix(ui.Hex("#05081f"), 0.65), Strength: strength, Height: 420}.Show(c)
}

// heroColors are the text colors over a hero lit by pal.
type heroColors struct {
	title, meta, label, accent ui.Color
	// ink is the album's deep color, for text on a title-colored button.
	ink ui.Color
}

func heroColorsFor(pal Palette, k *Tokens) heroColors {
	under := k.Ground
	if pal.OK {
		under = k.Ground.Mix(pal.Bg, 0.45)
	}
	h := heroColors{title: k.Fg, meta: k.Muted, label: k.Accent, accent: AccentFor(pal, k), ink: k.Ink}
	if pal.OK {
		h.title = Readable(pal.Text1, under, k)
		h.meta = Readable(pal.Text2, under, k)
		h.label = Readable(pal.Text3, under, k)
		if deep := pal.Bg.Mix(ui.Hex("#05081f"), 0.6); Contrast(deep, h.title) >= 4.5 {
			h.ink = deep
		}
	}
	return h
}

// jobQuality labels what a job's tracks are downloaded as, from the first
// track that has picked a codec.
func jobQuality(j *store.Job) string {
	for _, it := range j.Items {
		if it.Codec != "" {
			return format.Quality(it.Codec, it.BitDepth, it.SampleRate, it.Bitrate)
		}
	}
	return ""
}

// isHiRes gives a quality the gold tag: the design gilds every 24-bit ALAC,
// 24/48 included.
func isHiRes(q string) bool { return strings.HasPrefix(q, "ALAC 24/") }

// queueScreen is the main screen: the album downloading now, or else the
// one that finished last, the covers of those before it, then the queue.
func (a *App) queueScreen(c *ui.Context, k *Tokens, compact bool) {
	s := a.Store
	if s.Conn.Phase == client.Offline {
		a.offlineBanner(c, k)
	}
	if !s.Loaded {
		if s.Conn.Phase != client.Offline {
			a.loadingState(c, k)
		}
		return
	}
	finished := s.Finished()
	var latest *store.Job
	if j := s.Now(); j != nil {
		a.hero(c, k, j, compact)
	} else if len(finished) > 0 {
		latest = finished[0]
		a.latestHero(c, k, latest, compact)
	}
	a.recentWall(c, k, finished, latest, compact)
	tabs := []string{"All", fmt.Sprintf("Active · %d", s.Count(store.TabActive)), fmt.Sprintf("Done · %d", s.Count(store.TabDone)),
		fmt.Sprintf("Failed · %d", s.Count(store.TabFailed))}
	tab := store.Tab(a.Tab)
	jobs := s.Filter(tab, "")
	// All and Active lead with Up next, in the order the jobs run; the
	// finished ones follow, newest first.
	var running, queued, earlier []*store.Job
	if tab == store.TabAll || tab == store.TabActive {
		for _, j := range jobs {
			switch j.Status {
			case domain.JobRunning:
				running = append(running, j)
			case domain.JobQueued:
			default:
				earlier = append(earlier, j)
			}
		}
		queued = s.Queued()
	} else {
		earlier = jobs
	}
	upNext := len(running)+len(queued) > 0
	ui.Row(c).Padding(14, 18, 10, 18).Gap(12).AlignItems(ui.Center).Background(k.Ground).Children(func() {
		display(c, "Queue", 22).SingleLine()
		segmented(c, k, "tabs", &a.Tab, tabs, map[int]ui.Color{int(store.TabFailed): k.Bad})
		ui.Spacer(c)
		if !compact && !upNext {
			ui.Text(c, "Newest first").FontSize(12).TextColor(k.Muted)
		}
	})
	if len(jobs) == 0 {
		a.emptyQueue(c, k, latest != nil)
		return
	}
	// The list's items: Up next as one, the heading of the rest, then the
	// rest one by one.
	head := 0
	if upNext {
		head = 1
		if len(earlier) > 0 {
			head = 2
		}
	}
	list := ui.List(c.Key("queue"), &a.queue, head+len(earlier), func(i int) {
		switch {
		case i == 0 && upNext:
			a.upNext(c, k, running, queued, compact)
		case i == 1 && head == 2:
			ui.Row(c).Padding(18, 18, 6, 18).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Earlier").FontSize(12).FontWeight(650).TextColor(k.Muted)
				ui.Spacer(c)
				if !compact {
					ui.Text(c, "Newest first").FontSize(12).TextColor(k.Faint)
				}
			})
		default:
			a.jobRow(c, k, earlier[i-head], compact, nil)
		}
	}).Grow(1).Background(k.Ground).ItemKey(func(i int) any {
		switch {
		case i == 0 && upNext:
			return "up-next"
		case i == 1 && head == 2:
			return "earlier"
		}
		return earlier[i-head].ID
	}).TrackScroll(a.scrollState(c, "queue"))
	a.settleQueueDrag(c, list)
}

// hero is the "downloading now" band: the cover, the album in big type, its
// stage bar and the percent, all in the album's colors.
func (a *App) hero(c *ui.Context, k *Tokens, j *store.Job, compact bool) {
	size := float32(196)
	var video *ui.VideoPlayer
	if compact {
		size = 88
	} else {
		video = a.motionPlayer(c, j.MotionArtworkURL, size)
	}
	pal := jobPalette(j, video != nil)
	hc := heroColorsFor(pal, k)
	ui.Row(c).Padding(18, 28, 24, 28).Gap(28).AlignItems(ui.End).Children(func() {
		a.heroCover(c, j, func() {
			widgets.Cover{Image: a.Art.Get(j.ArtworkURL, int(size*2)), Placeholder: pal.Bg, Size: size, Radius: radiusCover, Color: 1, Shadow: true, Video: video}.Show(c)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(10).Children(func() {
			label(c, k, "Downloading now").TextColor(hc.label)
			title := display(c, j.Name(), 42).TextColor(hc.title).MaxLines(2).Ellipsis("…")
			if compact {
				title.FontSize(24).LetterSpacing(-0.48).MaxLines(1)
			}
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, j.By()).FontSize(15).TextColor(hc.meta).SingleLine().Shrink(1)
				if q := jobQuality(j); q != "" {
					tag(c, k, q, isHiRes(q), hc.meta)
				}
				if j.TotalItems > 0 {
					tag(c, k, plural(j.TotalItems, "track"), false, hc.meta)
				}
			})
			ui.Column(c).MaxWidth(600).Gap(6).Margin(6, 0, 0, 0).Children(func() {
				widgets.StageBar{Stages: j.Stages, Color: hc.accent, Track: k.Track, Height: 10, Gap: 3, Running: true, Still: a.scrolling(c)}.Show(c, "hero-bar")
				ui.Row(c).Children(func() {
					mono(c, a.heroProgressText(j), 11.5).TextColor(hc.meta)
					ui.Spacer(c)
					mono(c, a.heroRateText(c, j), 11.5).TextColor(hc.meta)
				})
			})
		})
		if compact {
			return
		}
		ui.Column(c).AlignItems(ui.End).Gap(10).Children(func() {
			ui.Row(c).AlignItems(ui.Baseline).Children(func() {
				display(c, fmt.Sprint(j.Percent()), 64).LetterSpacing(-1.9).TextColor(hc.title).FontFeatures("tnum")
				ui.Text(c, "%").FontSize(26).FontWeight(700).TextColor(hc.meta)
			})
			ui.Row(c).Gap(8).Children(func() {
				if button(c, k, buttonOnArt, "Cancel", nil).Clicked() {
					a.Store.Cancel(j.ID)
				}
				if artButton(c, "Details", hc.title, hc.ink).Clicked() {
					a.OpenJob(j.ID)
				}
			})
		})
	})
}

func (a *App) heroProgressText(j *store.Job) string {
	parts := []string{fmt.Sprintf("%d of %s", j.Done(), plural(j.TotalItems, "track"))}
	var down, dec int
	for _, it := range j.Items {
		switch it.Status {
		case domain.ItemDownloading:
			down++
		case domain.ItemDecrypting:
			dec++
		}
	}
	if down > 0 {
		parts = append(parts, fmt.Sprintf("downloading %d", down))
	}
	if dec > 0 {
		parts = append(parts, fmt.Sprintf("decrypting %d", dec))
	}
	if j.TotalItems == 0 {
		return "resolving the link"
	}
	return strings.Join(parts, " · ")
}

func (a *App) heroRateText(c *ui.Context, j *store.Job) string {
	var parts []string
	if sp := j.AvgSpeed(c.Now()); sp > 0 {
		parts = append(parts, "avg "+format.Rate(sp))
	}
	if r := format.Remaining(j.Remaining); r != "" {
		parts = append(parts, r+" left")
	}
	return strings.Join(parts, " · ")
}

// heroCover makes a hero's cover open the job's detail, as its Details
// button does, lifting a little under the pointer.
func (a *App) heroCover(c *ui.Context, j *store.Job, show func()) {
	b := ui.ButtonBase(c.Key("hero-cover")).Label("Open " + j.Name())
	lift := b.AnimateTo("lift", map[bool]float32{true: 1, false: 0}[b.Hovered()], ui.Snappy)
	if b.Clicked() {
		a.OpenJob(j.ID)
	}
	b.Cursor(ui.CursorPointer).Scale(1+0.02*lift, 1+0.02*lift).Children(show)
}

// jobRow is one job of the queue: cover, names, stage bar with counts,
// status pill, and a menu.
func (a *App) jobRow(c *ui.Context, k *Tokens, j *store.Job, compact bool, place func(row ui.Element)) {
	s := a.Store
	pal := jobPalette(j, false)
	accent := AccentFor(pal, k)
	running := j.Status == domain.JobRunning
	row := ui.Row(c.Key(j.ID)).Padding(10, 18).Gap(14).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(k.Line).Focusable().Label(j.Name())
	if s.Selection[j.ID] {
		row.Background(k.AccentSoft)
	} else if row.Hovered() {
		row.Background(k.Track.Alpha(0.5))
	}
	if running {
		row.InsetShadow(3, 0, 0, 0, accent)
	}
	if place != nil {
		place(row)
	}
	if row.Clicked() {
		if row.ClickModifiers()&ui.Cmd != 0 {
			s.Select(j.ID, true)
		} else {
			a.OpenJob(j.ID)
		}
	}
	row.ContextMenu(func(m *ui.Menu) { a.jobMenu(m, j) })
	row.Children(func() {
		color, dim := float32(0), float32(0.2)
		switch j.Status {
		case domain.JobRunning:
			color = 0.3 + 0.7*float32(j.Progress)
		case domain.JobCompleted:
			color = 1
		case domain.JobFailed, domain.JobCancelled:
			dim = 0.3
		}
		widgets.Cover{Image: a.Art.Get(j.ArtworkURL, 88), Placeholder: pal.Bg, Size: 44, Radius: radiusInner, Color: color, Dim: dim}.Show(c)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, j.Name()).FontSize(13.5).FontWeight(600).Ellipsis("…").SingleLine()
			ui.Text(c, rowSubtitle(j)).FontSize(12).TextColor(k.Muted).Ellipsis("…").SingleLine()
		})
		ui.Column(c).Width(220).Gap(6).Children(func() {
			bar := widgets.StageBar{Stages: j.Stages, Color: accent, Track: k.Track, Running: running, Still: a.scrolling(c),
				Failed: j.Status == domain.JobFailed, Bad: k.Bad, Finished: j.Status == domain.JobCompleted, Done: k.Fg.Alpha(0.85)}
			bar.Show(c, "bar")
			ui.Row(c).Children(func() {
				mono(c, rowCount(j), 11).TextColor(k.Muted)
				ui.Spacer(c)
				note, col := rowNote(j, k)
				mono(c, note, 11).TextColor(col).Ellipsis("…")
			})
		})
		if !compact {
			// The column keeps its width whatever the state; the pill in it
			// is as wide as its words.
			ui.Row(c).Width(110).AlignItems(ui.Center).Children(func() { statusPill(c, k, j) })
		}
		// PopoverMenu would open only for the secondary button.
		more := ui.ButtonBase(c.Key("more")).Size(28, 28).Radius(radiusControl).Center().Label("More actions")
		col := k.Faint
		if more.Hovered() {
			more.Background(k.Track)
			col = k.Fg
		}
		more.PopoverMenuButton(func(m *ui.Menu) { a.jobMenu(m, j) })
		more.Cursor(ui.CursorPointer).Children(func() { ui.Icon(c, icMore).FontSize(16).TextColor(col) })
	})
}

func rowSubtitle(j *store.Job) string {
	kind := strings.ToUpper(j.Type[:min(1, len(j.Type))]) + j.Type[min(1, len(j.Type)):]
	parts := []string{j.By()}
	if j.By() != j.Type {
		parts = append(parts, kind)
	}
	if j.Status == domain.JobCompleted && j.FinishedAt != nil {
		parts = append(parts, "finished "+format.Clock(*j.FinishedAt))
	}
	return strings.Join(parts, " · ")
}

func rowCount(j *store.Job) string {
	if j.TotalItems == 0 {
		return "—"
	}
	s := fmt.Sprintf("%d/%d", j.Done(), j.TotalItems)
	if j.Status == domain.JobRunning {
		s += fmt.Sprintf(" · %d%%", j.Percent())
	}
	return s
}

func rowNote(j *store.Job, k *Tokens) (string, ui.Color) {
	switch j.Status {
	case domain.JobQueued:
		return "waiting for a worker", k.Muted
	case domain.JobFailed:
		e := j.Error
		if e == "" {
			e = fmt.Sprintf("%d failed", j.FailedItems)
		}
		return e, k.Bad
	case domain.JobCancelled:
		return "cancelled", k.Muted
	}
	return jobQuality(j), k.Muted
}

func statusPill(c *ui.Context, k *Tokens, j *store.Job) {
	switch j.Phase() {
	case store.PhaseQueued:
		pill(c, k, pillWait, icClock, "Queued")
	case store.PhaseResolving:
		pill(c, k, pillRun, icSearch, "Resolving")
	case store.PhaseDownloading:
		pill(c, k, pillRun, icDown, "Downloading")
	case store.PhaseDecrypting:
		pill(c, k, pillRun, icLock, "Decrypting")
	case store.PhaseFinishing:
		pill(c, k, pillRun, icNote, "Tagging")
	case store.PhaseDone:
		pill(c, k, pillOK, icCheck, "Done")
	case store.PhaseFailed:
		pill(c, k, pillBad, icCross, "Failed")
	case store.PhaseCancelled:
		pill(c, k, pillWait, icCross, "Cancelled")
	}
}

// jobMenu is what can be done with j in its state: a job under way is
// cancelled, a finished one deleted.
func (a *App) jobMenu(m *ui.Menu, j *store.Job) {
	if m.Item("Show details").Chosen() {
		a.OpenJob(j.ID)
	}
	switch j.Status {
	case domain.JobQueued, domain.JobRunning:
		if q := a.Store.Queued(); j.Status == domain.JobQueued && len(q) > 1 {
			at := slices.Index(q, j)
			if at > 0 && m.Item("Run next").Chosen() {
				a.Store.MoveJob(j.ID, 0)
			}
			if at < len(q)-1 && m.Item("Run last").Chosen() {
				a.Store.MoveJob(j.ID, len(q)-1)
			}
		}
		if m.Item("Cancel").Chosen() {
			a.Store.Cancel(j.ID)
		}
	case domain.JobFailed, domain.JobCancelled:
		if m.Item("Retry").Chosen() {
			a.Store.Retry(j.ID)
		}
	}
	if dir := a.revealDir(j); dir != "" && m.Item(revealLabel).Chosen() {
		a.Act.Reveal(dir)
	}
	m.Separator()
	if m.Item("Copy link").Chosen() {
		a.Act.CopyText(j.Input)
	}
	if j.Status.IsTerminal() {
		m.Separator()
		if m.Item("Delete…").Chosen() {
			a.askDelete(j)
		}
	}
}

// revealDir is the folder of j's tracks on this computer, or "" when they
// are on another machine or nowhere yet.
func (a *App) revealDir(j *store.Job) string {
	if a.Settings.Mode != mode.Standalone {
		return ""
	}
	return savedDir(j)
}

// askDelete opens the dialog that deletes j.
func (a *App) askDelete(j *store.Job) { a.deleting, a.deleteOpen = j, true }

// deleteDialog confirms a job's delete, which keeps its files.
func (a *App) deleteDialog(c *ui.Context, k *Tokens) {
	j := a.deleting
	if j == nil {
		return
	}
	ui.DialogBase(c.Key("delete"), &a.deleteOpen, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(440).Padding(padCard).Gap(12).Radius(radiusCard).Background(k.Raise).Border(1, k.Line2).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.4))
		ui.Text(c, "Delete “"+clip(j.Name(), 48)+"”?").FontSize(15).FontWeight(650).MaxLines(2).Ellipsis("…")
		ui.Text(c, "The download and its history leave the queue. Files already saved stay on disk.").FontSize(12.5).TextColor(k.Muted).LineHeight(1.5)
		ui.Row(c).Gap(8).Justify(ui.End).Padding(6, 0, 0, 0).Children(func() {
			if button(c, k, buttonPlain, "Cancel", nil).Clicked() {
				a.deleteOpen = false
			}
			ok := button(c, k, buttonPrimary, "Delete", nil)
			ok.Background(k.Bad)
			if ok.Clicked() {
				a.Store.Delete(j.ID)
				a.deleteOpen = false
			}
		})
	})
	if !a.deleteOpen {
		a.deleting = nil
	}
}
