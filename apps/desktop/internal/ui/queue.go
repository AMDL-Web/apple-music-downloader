package ui

import (
	"fmt"
	"slices"
	"strings"

	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/format"
	"github.com/egoist/mygo/ui"
)

// heroJob is the job whose palette lights the queue: the one downloading
// now, else the one that finished last. The album page asks for it too,
// while it flies in over the queue, for the light it turns from.
func (a *App) heroJob() *store.Job {
	if j := a.Store.Now(); j != nil {
		return j
	}
	if f := a.Store.Finished(); len(f) > 0 {
		return f[0]
	}
	return nil
}

// backdrop lights the top of the main column with the palette of what the
// screen shows: the album page's, home's link or first result, the queue
// hero's. A screen without one eases the light out. While the album page
// comes in or goes out, the light turns with it, from the screen under it
// to the page's, over the whole column at once.
func (a *App) backdrop(c *ui.Context, k *Tokens) {
	b := paletteBackdrop(k, a.screenPalette(c, a.Screen), a.Screen == ScreenHome)
	if f := &a.fly; f.moving {
		under := a.Screen
		if under == ScreenAlbum {
			under = f.under
		}
		from := paletteBackdrop(k, a.screenPalette(c, under), under == ScreenHome)
		b = paletteBackdrop(k, a.screenPalette(c, ScreenAlbum), false)
		b.From, b.Mix = &from, f.light()
	}
	b.Show(c)
}

// screenPalette is the palette of what s shows, if any.
func (a *App) screenPalette(c *ui.Context, s Screen) Palette {
	switch s {
	case ScreenAlbum:
		return a.pagePalette(c)
	case ScreenHome:
		return a.homePalette()
	case ScreenQueue:
		if j := a.heroJob(); j != nil {
			// A narrow window's queue hero shows the still cover.
			return jobPalette(j, !a.compact && a.motionShown(c, j.MotionArtworkURL))
		}
	}
	return Palette{}
}

// paletteBackdrop lights the top of the main column with pal, or eases the
// light out without one. Home's band reaches past the discs' blur, so the
// light fades out above the results instead of ending in a line; the
// queue's and the album page's end under their opaque lists.
func paletteBackdrop(k *Tokens, pal Palette, home bool) widgets.Backdrop {
	strength, height := float32(0.55), float32(420)
	switch {
	case home && k.Dark:
		strength, height = 0.45, 560
	case home:
		strength, height = 0.22, 560
	case !k.Dark:
		strength = 0.28
	}
	b := widgets.Backdrop{Ground: k.Ground, Strength: strength, Height: height}
	if pal.OK {
		b.Light, b.Deep = pal.Bg, pal.Bg.Mix(ui.Hex("#05081f"), 0.65)
	}
	return b
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

// heroPart names one of a hero's colors as it eases (heroColors.eased).
type heroPart int

// eased eases h from the colors a hero showed before, as the backdrop eases
// its light: the text follows a new palette, or the theme's colors without
// one, rather than switching at once. The queue's hero and the album page's
// head share them, as they share the album they open on.
func (h heroColors) eased(c *ui.Context) heroColors {
	r := c.Root()
	for i, col := range []*ui.Color{&h.title, &h.meta, &h.label, &h.accent, &h.ink} {
		*col = r.AnimateColor(heroPart(i), *col, ui.Smooth)
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
	// Each count keeps the room of two digits, so a count going from 9 to 10
	// moves no tab.
	room := []string{"All", "Active · 00", "Done · 00", "Failed · 00"}
	for i, t := range tabs {
		if len(t) > len(room[i]) {
			room[i] = t
		}
	}
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
	top := ui.Row(c).Padding(14, 18, 10, 18).Gap(12).AlignItems(ui.Center).Background(k.Ground)
	// The list's top, which the album page's lower panel flies from.
	a.flyEdge(top)
	top.Children(func() {
		display(c, "Queue", 22).SingleLine()
		segmentedRoom(c, k, "tabs", &a.Tab, tabs, room, map[int]ui.Color{int(store.TabFailed): k.Bad})
		ui.Spacer(c)
		if !compact && !upNext && len(jobs) > 0 {
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
	// The list fades under the bar above it (fadeEdges), over the ground,
	// not the backdrop behind the page.
	var list ui.Element
	ui.Box(c.Key("queue-area")).Grow(1).MinHeight(0).Background(k.Ground).Children(func() {
		st := a.scrollState(c, "queue")
		list = ui.List(c.Key("queue"), &a.queue, head+len(earlier), func(i int) {
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
		}).Absolute().Top(0).Left(0).Right(0).Bottom(0).ItemKey(func(i int) any {
			switch {
			case i == 0 && upNext:
				return "up-next"
			case i == 1 && head == 2:
				return "earlier"
			}
			return earlier[i-head].ID
		}).TrackScroll(st)
		a.fadeEdges(list, st)
	})
	a.settleQueueDrag(c, list)
}

// jobRow is one job of the queue: cover, names, stage bar with counts,
// status pill, and a menu.
func (a *App) jobRow(c *ui.Context, k *Tokens, j *store.Job, compact bool, place func(row ui.Element)) {
	s := a.Store
	pal := jobPalette(j, false)
	accent := AccentFor(pal, k)
	running := j.Status == domain.JobRunning
	if j.Status == domain.JobCompleted {
		// The feed lists jobs without their tracks, which the quality is
		// read from.
		s.WantTracks(j)
	}
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
			a.openingFrom("row:" + j.ID)
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
		cover := widgets.Cover{Image: a.Art.Get(j.ArtworkURL, 88), Placeholder: pal.Bg, Size: 44, Radius: radiusInner, Color: color, Dim: dim}.Show(c)
		a.flySourceCover(cover, "row:"+j.ID, radiusInner, color, dim, 0)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, j.Name()).FontSize(13.5).FontWeight(600).Ellipsis("…").SingleLine()
			ui.Text(c, a.jobSubtitle(j)).FontSize(12).TextColor(k.Muted).Ellipsis("…").SingleLine()
		})
		ui.Column(c).Width(220).Gap(6).Children(func() {
			bar := widgets.StageBar{Stages: j.Stages, Color: accent, Track: k.Track, Running: running, Still: a.scrolling(c),
				Failed: j.Status == domain.JobFailed, Bad: k.Bad, Finished: j.Status == domain.JobCompleted, Done: k.Fg.Alpha(0.85)}
			bar.Show(c, "bar")
			ui.Row(c).Children(func() {
				mono(c, a.jobCount(j), 11).TextColor(k.Muted)
				ui.Spacer(c)
				note, col := rowNote(j, k, a.jobQualityText(j))
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
	parts := []string{j.By()}
	if j.By() != j.Type {
		parts = append(parts, j.Kind())
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

func rowNote(j *store.Job, k *Tokens, quality string) (string, ui.Color) {
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
	return quality, k.Muted
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
