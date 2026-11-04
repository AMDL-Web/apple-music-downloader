package ui

import (
	"fmt"
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
// one that finished last, the covers of those before it, then the queue:
// what runs and waits, then amdl's records and the stations' runs, one row
// each, newest activity first.
func (a *App) queueScreen(c *ui.Context, k *Tokens, compact bool) {
	s := a.Store
	if s.Conn.Phase == client.Offline {
		a.offlineBanner(c, k)
	}
	tab := store.Tab(a.Tab)
	if s.Loaded {
		s.OpenQueue(tab)
		if a.tabShown != a.Tab {
			a.tabShown = a.Tab
			s.ShowQueueTab(tab)
		}
	}
	// Loaded waits for the queue's counts and lists as for the feed, so
	// neither pops in after the first frame.
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
	tabs := []string{"All", fmt.Sprintf("Active · %d", s.QueueCount(store.TabActive)), fmt.Sprintf("Done · %d", s.QueueCount(store.TabDone)),
		fmt.Sprintf("Failed · %d", s.QueueCount(store.TabFailed))}
	// Each count keeps the room of two digits, so a count going from 9 to 10
	// moves no tab.
	room := []string{"All", "Active · 00", "Done · 00", "Failed · 00"}
	for i, t := range tabs {
		if len(t) > len(room[i]) {
			room[i] = t
		}
	}
	// All and Active lead with Up next, in the order the jobs run; the rows
	// below follow, newest activity first.
	var running, queued []*store.Job
	if tab == store.TabAll || tab == store.TabActive {
		for _, j := range s.Jobs {
			if j.Status == domain.JobRunning {
				running = append(running, j)
			}
		}
		queued = s.Queued()
	}
	rows, more := s.QueueRows(tab)
	l := s.Queue.List(tab)
	var adds []domain.PendingAdd
	if tab == store.TabFailed {
		adds = s.Queue.Adds
	}
	upNext := len(running)+len(queued) > 0
	top := ui.Row(c).Padding(14, 18, 10, 18).Gap(12).AlignItems(ui.Center).Background(k.Ground)
	// The list's top, which the album page's lower panel flies from.
	a.flyEdge(top)
	top.Children(func() {
		display(c, "Queue", 22).SingleLine()
		segmentedRoom(c, k, "tabs", &a.Tab, tabs, room, map[int]ui.Color{int(store.TabFailed): k.Bad})
		ui.Spacer(c)
		// As tall as a button whatever it holds, so Done's Download missing
		// moves nothing.
		ui.Row(c).Height(30).Gap(12).AlignItems(ui.Center).MinWidth(0).Shrink(1).Children(func() {
			switch {
			case tab == store.TabDone:
				a.missingBar(c, k)
			case !compact && !upNext && len(rows) > 0:
				ui.Text(c, "Newest first").FontSize(12).TextColor(k.Muted)
			}
		})
	})
	if !upNext && len(rows) == 0 && len(adds) == 0 {
		switch {
		case tab != store.TabActive && l.Status == store.SearchFailed:
			queueFailed(c, k, l.Err, func() { s.RetryQueue(tab) })
		case tab != store.TabActive && l.Status != store.SearchReady:
			// The first read is out; what it brings takes the room.
		default:
			a.emptyQueue(c, k, latest != nil)
		}
		return
	}
	// The list's items: Up next as one, the failed adds as one, the
	// heading of the rest, then the rest one by one, and the row that says
	// a next page failed.
	type part int
	const (
		partUpNext part = iota
		partAdds
		partEarlier
		partRow
		partMore
	)
	var parts []part
	if upNext {
		parts = append(parts, partUpNext)
	}
	if len(adds) > 0 {
		parts = append(parts, partAdds)
	}
	if upNext && len(rows) > 0 {
		parts = append(parts, partEarlier)
	}
	head := len(parts)
	failedMore := more && l.More == store.PageFailed
	n := head + len(rows)
	if failedMore {
		n++
	}
	// The list fades under the bar above it (fadeEdges), over the ground,
	// not the backdrop behind the page.
	var list ui.Element
	ui.Box(c.Key("queue-area")).Grow(1).MinHeight(0).Background(k.Ground).Children(func() {
		st := a.scrollState(c, "queue")
		list = ui.List(c.Key("queue"), &a.queue, n, func(i int) {
			switch {
			case i >= head+len(rows):
				ui.Row(c.Key("more")).Height(44).Gap(10).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Could not load more.").FontSize(12).TextColor(k.Bad)
					if button(c, k, buttonGhost, "Try again", nil).Clicked() {
						s.MoreQueue(tab)
					}
				})
			case i >= head:
				a.queueRow(c, k, rows[i-head], compact)
			case parts[i] == partUpNext:
				a.upNext(c, k, running, queued, compact)
			case parts[i] == partAdds:
				a.failedAdds(c, k, adds)
			default:
				ui.Row(c).Padding(18, 18, 6, 18).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Earlier").FontSize(12).FontWeight(650).TextColor(k.Muted)
					ui.Spacer(c)
					if !compact {
						ui.Text(c, "Newest first").FontSize(12).TextColor(k.Faint)
					}
				})
			}
		}).Absolute().Top(0).Left(0).Right(0).Bottom(0).ItemKey(func(i int) any {
			switch {
			case i >= head+len(rows):
				return "more"
			case i >= head:
				return rows[i-head].Key()
			case parts[i] == partUpNext:
				return "up-next"
			case parts[i] == partAdds:
				return "adds"
			}
			return "earlier"
		}).TrackScroll(st)
		a.fadeEdges(list, st)
		a.noteView(list)
		// The next page is read as the end of the list comes near.
		if more && l.More == store.PageIdle && st.MaxY-st.Y < loadAhead {
			s.MoreQueue(tab)
		}
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
		cover := a.rowCover(c, k, j.ArtworkURL, pal, color, dim)
		a.flySourceCover(cover, "row:"+j.ID, radiusInner, color, dim, 0)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, j.Name()).FontSize(13.5).FontWeight(600).Ellipsis("…").SingleLine()
			a.rowSub(c, k, a.jobSubtitle(j), a.region(j.Storefront))
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
			ui.Row(c).Width(pillColumn).AlignItems(ui.Center).Children(func() { statusPill(c, k, j) })
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

// jobMenu is what can be done with j in its state: a record's run takes
// its record's menu, a station's run its own.
func (a *App) jobMenu(m *ui.Menu, j *store.Job) { a.rowMenu(m, a.rowRecord(j), j) }

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

// askDeleteRun deletes what a run's page shows: a run of a record is that
// record's row in the queue, so it removes the record with its runs, as the
// row's Remove from amdl does; a run of no record goes alone.
func (a *App) askDeleteRun(j *store.Job) {
	if j.ResourceID == "" {
		a.askDelete(j)
		return
	}
	a.removing = &store.Record{Ref: domain.ResourceRef{Kind: j.ResourceKind, ID: j.ResourceID}, Name: j.Name()}
	a.removeOpen = true
}

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

// cancelWord is what stopping a run is called: Pause for a record's run,
// which pauses the record, Cancel for a station's.
func cancelWord(j *store.Job) string {
	if j.ResourceID != "" {
		return "Pause"
	}
	return "Cancel"
}

// retryWord is what starting a stopped run again is called: Resume for a
// record's cancelled run, which its pause cancelled, else Retry.
func retryWord(j *store.Job) string {
	if j.ResourceID != "" && j.Status == domain.JobCancelled {
		return "Resume"
	}
	return "Retry"
}
