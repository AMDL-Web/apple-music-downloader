package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/progress"
)

// queueRow is one row of the queue below Up next: a record, shown as its
// run while one is under way, or a station's run.
func (a *App) queueRow(c *ui.Context, k *Tokens, r store.QueueRow, compact bool) {
	switch {
	case r.Record == nil:
		a.jobRow(c, k, r.Run, compact, nil)
	case a.Store.LiveRun(r.Record) != nil:
		a.jobRow(c, k, a.Store.LiveRun(r.Record), compact, nil)
	default:
		a.recordRow(c, k, r.Record, compact)
	}
}

// recordRow is a record without a run under way, laid out as a job's row:
// cover, names, how much is downloaded, its state, and a menu.
func (a *App) recordRow(c *ui.Context, k *Tokens, r *store.Record, compact bool) {
	s := a.Store
	from := a.recordFrom(r.Ref)
	pal := ParsePalette(r.Artwork.Colors.Bg, r.Artwork.Colors.Text1, r.Artwork.Colors.Text2, r.Artwork.Colors.Text3, r.Artwork.Colors.Text4)
	last := s.LastRun(r.Ref)
	if last != nil && last.Status == domain.JobCompleted {
		s.WantTracks(last)
	}
	row := ui.Row(c.Key(from)).Padding(10, 18).Gap(14).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(k.Line).Focusable().Label(recordName(*r))
	if row.Hovered() {
		row.Background(k.Track.Alpha(0.5))
	}
	if row.Clicked() {
		a.showDetails(r, nil, from)
	}
	row.ContextMenu(func(m *ui.Menu) { a.rowMenu(m, r, nil) })
	row.Children(func() {
		color, dim := float32(0.35), float32(0.2)
		switch {
		case r.State == domain.RecordComplete:
			color, dim = 1, 0
		case failedOf(r):
			dim = 0.3
		}
		cover := a.rowCover(c, k, r.Artwork.URL, pal, color, dim)
		a.flySourceCover(cover, from, radiusInner, color, dim, 0)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, recordName(*r)).FontSize(13.5).FontWeight(600).Ellipsis("…").SingleLine()
			a.rowSub(c, k, a.recordSub(r), a.recordRegion(r))
		})
		ui.Column(c).Width(220).Gap(6).Children(func() {
			f := 0.0
			if r.TracksWanted > 0 {
				f = float64(r.TracksDone) / float64(r.TracksWanted)
			}
			var st progress.Stages
			for i := range st {
				st[i] = f
			}
			bar := widgets.StageBar{Stages: st, Color: AccentFor(pal, k), Track: k.Track, Failed: r.TracksFailed+r.TracksGivenUp > 0, Bad: k.Bad,
				Finished: r.State == domain.RecordComplete, Done: k.Fg.Alpha(0.85)}
			bar.Show(c, "bar")
			ui.Row(c).Children(func() {
				count := "—"
				if r.TracksWanted > 0 {
					count = a.texts.rowCount.of(rowCountKey{total: r.TracksWanted, done: r.TracksDone}, func(n rowCountKey) string { return fmt.Sprintf("%d/%d", n.done, n.total) })
				}
				mono(c, count, 11).TextColor(k.Muted)
				ui.Spacer(c)
				note, col := a.recordNote(r, k, last)
				mono(c, note, 11).TextColor(col).Ellipsis("…")
			})
		})
		if !compact {
			ui.Row(c).Width(pillColumn).AlignItems(ui.Center).Children(func() { a.recordPill(c, k, *r) })
		}
		more := ui.ButtonBase(c.Key("more")).Size(28, 28).Radius(radiusControl).Center().Label("More actions")
		col := k.Faint
		if more.Hovered() {
			more.Background(k.Track)
			col = k.Fg
		}
		more.PopoverMenuButton(func(m *ui.Menu) { a.rowMenu(m, r, nil) })
		more.Cursor(ui.CursorPointer).Children(func() { ui.Icon(c, icMore).FontSize(16).TextColor(col) })
	})
}

// rowCover is a row's cover slot, always drawn: the artwork over its
// palette's ground, or, without artwork, the ground (the track color
// without a palette) with a note, as the Apple grid shows a missing cover.
// It returns the cover, which the album page flies from.
func (a *App) rowCover(c *ui.Context, k *Tokens, url string, pal Palette, color, dim float32) ui.Element {
	ground := pal.Bg
	if !pal.OK {
		ground = k.Track
	}
	var cover ui.Element
	ui.Box(c).Size(44, 44).Shrink(0).Children(func() {
		cover = widgets.Cover{Image: a.Art.Get(url, 88), Placeholder: ground, Size: 44, Radius: radiusInner, Color: color, Dim: dim}.Show(c)
		if url == "" {
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().PassThrough().Children(func() {
				ui.Icon(c, icNote).FontSize(15).TextColor(k.Faint)
			})
		}
	})
	return cover
}

// pillColumn is the width of a row's state column: the pill in it is as
// wide as its words.
const pillColumn = 130

// recordFrom is the name of a record row, which its page flies from.
func (a *App) recordFrom(ref domain.ResourceRef) string {
	return a.texts.recordFrom.of(ref, func(r domain.ResourceRef) string { return "row:" + string(r.Kind) + ":" + r.ID })
}

// failedOf reports whether r has a failed, given-up or missing track.
func failedOf(r *store.Record) bool { return r.TracksFailed+r.TracksGivenUp+r.TracksMissing > 0 }

// recordSub is a record's second line: its artist or curator and its kind,
// as a job's row says them.
func (a *App) recordSub(r *store.Record) string {
	typ := "album"
	if r.Ref.Kind == domain.ResourcePlaylist {
		typ = "playlist"
	}
	return a.texts.rowSub.of(rowSubKey{by: r.By, typ: typ, title: r.Name}, func(k rowSubKey) string {
		return rowSubtitle(&store.Job{Job: domain.Job{ArtistName: k.by, Type: k.typ, Title: k.title, Status: domain.JobQueued}})
	})
}

type recordNoteKey struct{ failed, givenUp, missing, withheld int }

// recordNote is what a record's row says under its bar's right end: the
// tracks that failed, gave up or went missing, else the quality of its
// last run.
func (a *App) recordNote(r *store.Record, k *Tokens, last *store.Job) (string, ui.Color) {
	if failedOf(r) {
		col := k.Bad
		if r.TracksFailed+r.TracksGivenUp == 0 {
			col = k.Warn
		}
		return a.texts.recordNote.of(recordNoteKey{r.TracksFailed, r.TracksGivenUp, r.TracksMissing, 0}, func(n recordNoteKey) string {
			var parts []string
			for _, p := range []struct {
				n    int
				word string
			}{{n.failed, "failed"}, {n.givenUp, "given up"}, {n.missing, "missing"}} {
				if p.n > 0 {
					parts = append(parts, fmt.Sprintf("%d %s", p.n, p.word))
				}
			}
			return strings.Join(parts, " · ")
		}), col
	}
	if last != nil && last.Status == domain.JobCompleted {
		return a.jobQualityText(last), k.Muted
	}
	return "", k.Muted
}

// rowSub is a row's second line, after its region's tag when the accounts
// span regions.
func (a *App) rowSub(c *ui.Context, k *Tokens, sub, region string) {
	if region == "" {
		ui.Text(c, sub).FontSize(12).TextColor(k.Muted).Ellipsis("…").SingleLine()
		return
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
		regionTag(c, k, region)
		ui.Text(c, sub).FontSize(12).TextColor(k.Muted).Ellipsis("…").SingleLine().Shrink(1)
	})
}

// regionTag is a storefront as a small tag, as an audio fact's.
func regionTag(c *ui.Context, k *Tokens, region string) {
	el, ink := tagBox(c, k, 0, k.Muted)
	el.Height(15).PaddingX(5)
	el.Children(func() { mono(c, region, 9.5).FontWeight(650).TextColor(ink) })
}

// region is the tag of storefront sf, "" for the default account's, while
// the accounts span two regions or more; "" with one, where every row
// would say the same.
func (a *App) region(sf string) string {
	regions := a.Store.SearchRegions()
	if len(regions) < 2 {
		return ""
	}
	if sf == "" {
		sf = regions[0]
	}
	return a.texts.region.of(sf, strings.ToUpper)
}

func (a *App) recordRegion(r *store.Record) string { return a.region(r.Storefront) }

// recordName is a record's name, or its id while it was never read.
func recordName(r store.Record) string {
	if r.Name != "" {
		if r.Ref.Kind == domain.ResourceAlbum {
			_, name := store.AlbumKind(r.Name)
			return name
		}
		return r.Name
	}
	return "Not read yet · " + r.Ref.ID
}

// recordPill is a record's state: in the queue wins, then paused, then
// gone from Apple, then failed tracks, then how much is downloaded.
func (a *App) recordPill(c *ui.Context, k *Tokens, r store.Record) {
	switch {
	case r.ActiveJobID != "":
		pill(c, k, pillRun, icDown, "In the queue")
	case r.Paused && r.State != domain.RecordComplete:
		pill(c, k, pillWait, icPause, "Paused")
	case r.Catalog == domain.CatalogGone:
		pill(c, k, pillWarn, icWarn, "Gone from Apple")
	case r.Catalog == domain.CatalogNotInStorefront:
		pill(c, k, pillWarn, icWarn, "Not in your region")
	case r.TracksFailed+r.TracksGivenUp > 0:
		pill(c, k, pillBad, icCross, "Failed")
	case r.TracksMissing > 0:
		pill(c, k, pillWarn, icWarn, "Missing")
	case r.State == domain.RecordComplete:
		pill(c, k, pillOK, icCheck, "Done")
	case r.State == domain.RecordPartial:
		pill(c, k, pillWait, nil, a.texts.rowCount.of(rowCountKey{total: r.TracksWanted, done: r.TracksDone, percent: -1}, func(n rowCountKey) string {
			return fmt.Sprintf("%d of %d", n.done, n.total)
		}))
	case !r.ResolvedAt:
		pill(c, k, pillWait, icClock, "Not read yet")
	default:
		pill(c, k, pillWait, nil, "Not downloaded")
	}
}

// recordHit is an album record as the hit its page opens from.
func recordHit(r store.Record) client.SearchHit {
	c := r.Artwork.Colors
	return client.SearchHit{Type: "album", ID: r.Ref.ID, Name: r.Name, ArtistName: r.By, ArtworkURL: r.Artwork.URL,
		ArtworkBgColor: c.Bg, ArtworkTextColor1: c.Text1, ArtworkTextColor2: c.Text2, ArtworkTextColor3: c.Text3, ArtworkTextColor4: c.Text4,
		TrackCount: r.TracksWanted, Resource: r.Mark()}
}

// showDetails opens what shows a row: an album record's page, else the
// run of a playlist record (its live one, else its last), or a station's
// run. from names the cover the page flies from.
func (a *App) showDetails(r *store.Record, j *store.Job, from string) {
	if r != nil && r.Ref.Kind == domain.ResourceAlbum {
		a.openingFrom(from)
		a.OpenAlbum(recordHit(*r))
		return
	}
	if j == nil && r != nil {
		if j = a.Store.LiveRun(r); j == nil {
			j = a.Store.LastRun(r.Ref)
		}
	}
	if j != nil {
		a.openingFrom(from)
		a.OpenJob(j.ID)
	}
}

// rowRecord is the record of j, a record's run, as the queue last read it,
// or as j names it; nil for a station's run.
func (a *App) rowRecord(j *store.Job) *store.Record {
	if j.ResourceID == "" {
		return nil
	}
	ref := domain.ResourceRef{Kind: j.ResourceKind, ID: j.ResourceID}
	if r := a.Store.Queue.Find(ref); r != nil {
		return r
	}
	r := &store.Record{Ref: ref, Name: j.Title, By: j.By(), Storefront: j.Storefront, ResolvedAt: j.TotalItems > 0, ActiveJobID: j.ID}
	r.Artwork.URL = j.ArtworkURL
	if m := a.Store.RecordMark(ref.Kind, ref.ID); m != nil {
		r.Rollup = domain.Rollup{State: m.State, TracksDone: m.TracksDone, TracksWanted: m.TracksWanted}
		r.Paused = m.Paused
	}
	return r
}

// rowMenu is what can be done with a row in its state: a record r, with
// its run j when one is under way, or a station's run j.
func (a *App) rowMenu(m *ui.Menu, r *store.Record, j *store.Job) {
	s := a.Store
	if r != nil && j == nil {
		j = s.LiveRun(r)
	}
	live := j != nil && !j.Status.IsTerminal()
	from := ""
	if r != nil {
		from = a.recordFrom(r.Ref)
	} else if j != nil {
		from = "row:" + j.ID
	}
	if (r != nil && r.Ref.Kind == domain.ResourceAlbum) || j != nil || (r != nil && s.LastRun(r.Ref) != nil) {
		if m.Item("Show details").Chosen() {
			a.showDetails(r, j, from)
		}
	}
	if j != nil && j.Status == domain.JobQueued {
		if q := s.Queued(); len(q) > 1 {
			at := slices.Index(q, j)
			if at > 0 && m.Item("Run next").Chosen() {
				s.MoveJob(j.ID, 0)
			}
			if at >= 0 && at < len(q)-1 && m.Item("Run last").Chosen() {
				s.MoveJob(j.ID, len(q)-1)
			}
		}
	}
	name := ""
	if r != nil {
		name = recordName(*r)
	}
	switch {
	case live && r != nil:
		if m.Item("Pause").Chosen() {
			s.Pause(r.Ref)
		}
	case live:
		if m.Item("Cancel").Chosen() {
			s.Cancel(j.ID)
		}
	case r != nil && r.Paused && r.State != domain.RecordComplete:
		if m.Item("Resume").Chosen() {
			s.Resume(r.Ref)
		}
	case r != nil && failedOf(r):
		if m.Item("Retry").Chosen() {
			s.DownloadRecord(r.Ref, name)
		}
	case r != nil && r.State != domain.RecordComplete:
		if m.Item("Download").Chosen() {
			s.DownloadRecord(r.Ref, name)
		}
	case r == nil && j != nil && (j.Status == domain.JobFailed || j.Status == domain.JobCancelled):
		if m.Item("Retry").Chosen() {
			s.Retry(j.ID)
		}
	}
	if r != nil && !live && r.TracksDone > 0 && m.Item("Download again").Chosen() {
		s.Reset(r.Ref, nil, nil, name)
	}
	run := j
	if run == nil && r != nil {
		run = s.LastRun(r.Ref)
	}
	if run != nil {
		if dir := a.revealDir(run); dir != "" && m.Item(revealLabel).Chosen() {
			a.Act.Reveal(dir)
		}
	}
	m.Separator()
	if m.Item("Copy link").Chosen() {
		if r != nil {
			a.Act.CopyText(r.Link())
		} else {
			a.Act.CopyText(j.Input)
		}
	}
	switch {
	case r != nil && !live:
		m.Separator()
		if m.Item("Remove from amdl…").Chosen() {
			rr := *r
			a.removing, a.removeOpen = &rr, true
		}
	case r == nil && j != nil && j.Status.IsTerminal():
		m.Separator()
		if m.Item("Delete…").Chosen() {
			a.askDelete(j)
		}
	}
}

// removeDialog confirms a record's removal, which keeps its files.
func (a *App) removeDialog(c *ui.Context, k *Tokens) {
	r := a.removing
	if r == nil {
		return
	}
	ui.DialogBase(c.Key("remove"), &a.removeOpen, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(440).Padding(padCard).Gap(12).Radius(radiusCard).Background(k.Raise).Border(1, k.Line2).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.4))
		ui.Text(c, "Remove “"+clip(recordName(*r), 48)+"” from amdl?").FontSize(15).FontWeight(650).MaxLines(2).Ellipsis("…")
		ui.Text(c, "The files stay. Adding it again downloads it again over them.").FontSize(12.5).TextColor(k.Muted).LineHeight(1.5)
		ui.Row(c).Gap(8).Justify(ui.End).Padding(6, 0, 0, 0).Children(func() {
			if button(c, k, buttonPlain, "Cancel", nil).Clicked() {
				a.removeOpen = false
			}
			ok := button(c, k, buttonPrimary, "Remove", nil)
			ok.Background(k.Bad)
			if ok.Clicked() {
				a.Store.RemoveRecord(r.Ref, recordName(*r))
				a.removeOpen = false
			}
		})
	})
	if !a.removeOpen {
		a.removing = nil
	}
}

// missingBar heads Done while records have files a disk check found
// missing: a button to download them again.
func (a *App) missingBar(c *ui.Context, k *Tokens) {
	s := a.Store
	if sum := s.Queue.Summary; sum != nil && sum.Albums.Flags[store.FilterMissing]+sum.Playlists.Flags[store.FilterMissing] > 0 {
		if button(c, k, buttonPlain, "Download missing", icDown).Clicked() {
			s.ResetFlagged(store.FilterMissing)
		}
	}
}

// ago is how long ago t was, in the largest unit.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return t.Local().Format("2 Jan")
}

// failedAdds are the links the backend could not read into records, at
// the top of Failed, each with Try again and Remove.
func (a *App) failedAdds(c *ui.Context, k *Tokens, failed []domain.PendingAdd) {
	s := a.Store
	ui.Column(c).Padding(10, 18, 10, 18).Gap(6).Children(func() {
		ui.Text(c, fmt.Sprintf("Couldn’t add · %d", len(failed))).FontSize(12).FontWeight(650).TextColor(k.Bad)
		for _, p := range failed {
			ui.Row(c.Key(p.ID)).Padding(8, 12).Gap(12).Radius(radiusControl).Background(k.BadSoft).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						if region := a.region(p.Storefront); region != "" {
							regionTag(c, k, region)
						}
						ui.Text(c, fmt.Sprintf("%s %s", pendingWord(p.Kind), p.CatalogID)).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…").Shrink(1)
					})
					if p.LastError != "" {
						ui.Text(c, p.LastError).FontSize(12).TextColor(k.Muted).SingleLine().Ellipsis("…")
					}
				})
				if button(c, k, buttonPlain, "Try again", nil).Clicked() {
					s.RetryAdd(p.ID)
				}
				if button(c, k, buttonGhost, "Remove", nil).Label("Remove this add").Clicked() {
					s.DropAdd(p.ID)
				}
			})
		}
	})
}

// queueFailed stands for a tab's list when its first read failed, with a
// retry.
func queueFailed(c *ui.Context, k *Tokens, why string, retry func()) {
	ui.Column(c).Height(260).Center().Gap(8).Children(func() {
		ui.Icon(c, icWarn).FontSize(26).TextColor(k.Bad)
		ui.Text(c, "The queue could not be read").FontSize(15).FontWeight(650)
		ui.Text(c, why).FontSize(12.5).TextColor(k.Muted).MaxWidth(420).TextAlign(ui.Center).MaxLines(3).Ellipsis("…")
		if button(c, k, buttonPlain, "Try again", nil).Clicked() {
			retry()
		}
	})
}

func pendingWord(k domain.PendingAddKind) string {
	switch k {
	case domain.PendingSong:
		return "Song"
	case domain.PendingArtist:
		return "Artist"
	case domain.PendingPlaylist:
		return "Playlist"
	}
	return "Album"
}
