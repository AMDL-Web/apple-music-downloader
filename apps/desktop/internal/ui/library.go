package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// libraryForm is the Library screen's own state: its tab, filter and Apple
// list as segmented controls hold them, the list's place, the record whose
// removal waits for its dialog, and the Apple playlists opened.
type libraryForm struct {
	tab, filter, apple int
	list               ui.ListState
	removing           *store.Record
	removeOpen         bool
	opened             map[string]bool
}

// libraryFilters are the filters a record list offers, in the order of its
// segmented control.
var libraryFilters = []string{store.FilterAll, store.FilterComplete, store.FilterPartial, store.FilterNone}

// libraryFlags are the flag filters, shown while a record carries them.
var libraryFlags = []struct{ flag, word string }{
	{store.FilterFailed, "Failed"}, {store.FilterGivenUp, "Given up"}, {store.FilterMissing, "Missing"}, {store.FilterPaused, "Paused"},
}

// libraryScreen is amdl's records: albums and playlists with their state,
// filters, the disk check, and the Apple Music account library to add
// from.
func (a *App) libraryScreen(c *ui.Context, k *Tokens, compact bool) {
	s := a.Store
	l := &s.Library
	if !l.Open() {
		s.OpenLibrary()
	}
	f := &a.lib
	if s.Conn.Phase == client.Offline {
		a.offlineBanner(c, k)
	}
	top := ui.Row(c).Padding(14, 18, 10, 18).Gap(12).AlignItems(ui.Center).Background(k.Ground)
	top.Children(func() {
		display(c, "Library", 22).SingleLine()
		prev := f.tab
		segmented(c, k, "tabs", &f.tab, []string{"Albums", "Playlists", "Apple Music"}, nil)
		if f.tab != prev {
			s.SetLibraryTab(store.LibraryTab(f.tab))
		}
		ui.Spacer(c)
		if l.Tab == store.LibraryApple {
			a.appleSyncBar(c, k, compact)
		} else {
			a.checkBar(c, k, compact)
		}
	})
	if l.Tab == store.LibraryApple {
		a.appleList(c, k)
		return
	}
	a.recordFilters(c, k)
	a.recordList(c, k, compact)
}

// checkBar is the disk check: how the last went, and a button to check
// again, or to download the files it found missing.
func (a *App) checkBar(c *ui.Context, k *Tokens, compact bool) {
	s := a.Store
	check := s.Records.Check
	running := check != nil && check.FinishedAt == nil
	if text := checkText(check); text != "" && !compact {
		ui.Text(c, text).FontSize(12).TextColor(k.Muted).SingleLine().Ellipsis("…").Shrink(1)
	}
	if missing := a.flagCount(store.FilterMissing); missing > 0 {
		if button(c, k, buttonPlain, "Download missing", icDown).Clicked() {
			s.ResetFlagged(store.FilterMissing)
		}
	}
	if buttonOff(c, k, buttonPlain, "Check files", icDisk, running).Label("Check the files on disk").Clicked() && !running {
		s.CheckDisk(nil, false)
	}
}

// checkText is a disk check in a few words; "" before the first.
func checkText(check *domain.CheckRun) string {
	switch {
	case check == nil:
		return ""
	case check.FinishedAt == nil:
		return fmt.Sprintf("Checking %d files…", check.Checked)
	case check.AbortedReason == domain.CheckAbortedTooManyMissing:
		return "Check stopped: more than half the files were missing"
	case check.AbortedReason != "":
		return "Check stopped"
	case check.Missing > 0:
		return fmt.Sprintf("%d missing of %d", check.Missing, check.Checked)
	}
	return fmt.Sprintf("%d files checked %s", check.Checked, ago(*check.FinishedAt))
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

// counts are the summary's counts of the open tab's kind.
func (a *App) counts() *client.RecordCounts {
	l := &a.Store.Library
	if l.Summary == nil {
		return nil
	}
	if l.Tab == store.LibraryPlaylists {
		return &l.Summary.Playlists
	}
	return &l.Summary.Albums
}

func (a *App) flagCount(flag string) int {
	if n := a.counts(); n != nil {
		return n.Flags[flag]
	}
	return 0
}

// recordFilters are the state filters with their counts, then the flags
// some record carries.
func (a *App) recordFilters(c *ui.Context, k *Tokens) {
	s := a.Store
	l := &s.Library
	f := &a.lib
	n := a.counts()
	count := func(word string, v int) string {
		if n == nil {
			return word
		}
		return fmt.Sprintf("%s · %d", word, v)
	}
	var total, done, part, none int
	if n != nil {
		total, done, part, none = n.Total, n.States[domain.RecordComplete], n.States[domain.RecordPartial], n.States[domain.RecordNone]
	}
	labels := []string{count("All", total), count("Downloaded", done), count("Partly", part), count("Not yet", none)}
	room := []string{"All · 000", "Downloaded · 000", "Partly · 000", "Not yet · 000"}
	for i, t := range labels {
		if len(t) > len(room[i]) {
			room[i] = t
		}
	}
	ui.Row(c).Padding(0, 18, 10, 18).Gap(8).AlignItems(ui.Center).Background(k.Ground).Children(func() {
		prev := f.filter
		segmentedRoom(c, k, "filter", &f.filter, labels, room, nil)
		if f.filter != prev {
			s.SetLibraryFilter(libraryFilters[f.filter], l.Flag)
		}
		for _, fl := range libraryFlags {
			v := a.flagCount(fl.flag)
			on := l.Flag == fl.flag
			if v == 0 && !on {
				continue
			}
			b := ui.ButtonBase(c.Key("flag-" + fl.flag)).Height(26).PaddingX(10).Radius(radiusFull).Center().Label(fl.word + " filter")
			fg, bg := k.Muted, k.Track
			if fl.flag != store.FilterPaused {
				fg = k.Bad
			}
			switch {
			case on:
				bg, fg = k.AccentSoft, k.Accent
			case b.Hovered():
				bg = k.Track.Mix(k.Fg, 0.08)
			}
			b.Background(bg)
			if b.Clicked() {
				flag := fl.flag
				if on {
					flag = ""
				}
				s.SetLibraryFilter(l.Filter, flag)
			}
			b.Cursor(ui.CursorPointer).Children(func() {
				ui.Text(c, fmt.Sprintf("%s · %d", fl.word, v)).FontSize(12).FontWeight(600).TextColor(fg).SingleLine()
			})
		}
	})
}

// recordList is the records the filter selects, after the adds that could
// not be read.
func (a *App) recordList(c *ui.Context, k *Tokens, compact bool) {
	s := a.Store
	l := &s.Library
	failed := l.Failed
	if l.Tab != store.LibraryAlbums {
		failed = nil
	}
	switch {
	case l.Status == store.SearchFailed && len(l.Rows) == 0:
		searchFailed(c, k, l.Err, func() { s.OpenLibrary() })
		return
	case l.Status != store.SearchReady && len(l.Rows) == 0:
		return
	case len(l.Rows) == 0 && len(failed) == 0:
		word := "No albums"
		if l.Tab == store.LibraryPlaylists {
			word = "No playlists"
		}
		if l.Filter != store.FilterAll || l.Flag != "" {
			word = "Nothing here"
		}
		searchMessage(c, k, icLibrary, word, "")
		return
	}
	head := 0
	if len(failed) > 0 {
		head = 1
	}
	ui.Box(c.Key("library-area")).Grow(1).MinHeight(0).Background(k.Ground).Children(func() {
		st := a.scrollState(c, "library")
		list := ui.List(c.Key("library"), &a.lib.list, head+len(l.Rows), func(i int) {
			if i < head {
				a.failedAdds(c, k, failed)
				return
			}
			a.recordRow(c, k, l.Rows[i-head], compact)
		}).Absolute().Top(0).Left(0).Right(0).Bottom(0).ItemKey(func(i int) any {
			if i < head {
				return "failed"
			}
			return l.Rows[i-head].Ref
		}).TrackScroll(st)
		a.fadeEdges(list, st)
	})
}

// failedAdds are the links the backend could not read into records, each
// with Try again and Remove.
func (a *App) failedAdds(c *ui.Context, k *Tokens, failed []domain.PendingAdd) {
	s := a.Store
	ui.Column(c).Padding(4, 18, 10, 18).Gap(6).Children(func() {
		ui.Text(c, fmt.Sprintf("Couldn’t add · %d", len(failed))).FontSize(12).FontWeight(650).TextColor(k.Bad)
		for _, p := range failed {
			ui.Row(c.Key(p.ID)).Padding(8, 12).Gap(12).Radius(radiusControl).Background(k.BadSoft).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, fmt.Sprintf("%s %s", pendingWord(p.Kind), p.CatalogID)).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…")
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

// recordRow is one record: cover, names, its state, and what can be done
// with it.
func (a *App) recordRow(c *ui.Context, k *Tokens, r store.Record, compact bool) {
	key := fmt.Sprint(r.Ref.Kind, ":", r.Ref.ID)
	pal := ParsePalette(r.Artwork.Colors.Bg, r.Artwork.Colors.Text1, r.Artwork.Colors.Text2, r.Artwork.Colors.Text3, r.Artwork.Colors.Text4)
	row := ui.Row(c.Key(key)).Padding(10, 18).Gap(14).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(k.Line).Focusable().Label(recordName(r))
	if row.Hovered() {
		row.Background(k.Track.Alpha(0.5))
	}
	if row.Clicked() {
		a.openRecord(r)
	}
	row.ContextMenu(func(m *ui.Menu) { a.recordMenu(m, r) })
	row.Children(func() {
		color := float32(0.35)
		if r.State == domain.RecordComplete {
			color = 1
		}
		widgets.Cover{Image: a.Art.Get(r.Artwork.URL, 88), Placeholder: pal.Bg, Size: 44, Radius: radiusInner, Color: color}.Show(c)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, recordName(r)).FontSize(13.5).FontWeight(600).Ellipsis("…").SingleLine()
			if sub := recordSub(r); sub != "" {
				ui.Text(c, sub).FontSize(12).TextColor(k.Muted).Ellipsis("…").SingleLine()
			}
		})
		if !compact {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				if r.TracksFailed > 0 {
					chip(c, k, fmt.Sprintf("%d failed", r.TracksFailed), k.Bad)
				}
				if r.TracksGivenUp > 0 {
					chip(c, k, fmt.Sprintf("%d given up", r.TracksGivenUp), k.Bad)
				}
				if r.TracksMissing > 0 {
					chip(c, k, fmt.Sprintf("%d missing", r.TracksMissing), k.Warn)
				}
			})
		}
		ui.Row(c).Width(130).Justify(ui.End).AlignItems(ui.Center).Children(func() { recordPill(c, k, r) })
		more := ui.ButtonBase(c.Key("more")).Size(28, 28).Radius(radiusControl).Center().Label("More actions")
		col := k.Faint
		if more.Hovered() {
			more.Background(k.Track)
			col = k.Fg
		}
		more.PopoverMenuButton(func(m *ui.Menu) { a.recordMenu(m, r) })
		more.Cursor(ui.CursorPointer).Children(func() { ui.Icon(c, icMore).FontSize(16).TextColor(col) })
	})
}

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

// recordSub is a record's second line: its artist or curator, and its
// tracks, with those not in this region.
func recordSub(r store.Record) string {
	parts := []string{}
	if r.By != "" {
		parts = append(parts, r.By)
	}
	if r.TracksWanted > 0 {
		parts = append(parts, plural(r.TracksWanted, "track"))
	}
	if r.WithheldCount > 0 {
		parts = append(parts, fmt.Sprintf("%d not in your region", r.WithheldCount))
	}
	return joinParts(parts)
}

func joinParts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " · "
		}
		out += p
	}
	return out
}

// recordPill is a record's state: in the queue wins, then paused, then
// gone from Apple, then how much is downloaded.
func recordPill(c *ui.Context, k *Tokens, r store.Record) {
	switch {
	case r.ActiveJobID != "":
		pill(c, k, pillRun, icDown, "In the queue")
	case r.Paused && r.State != domain.RecordComplete:
		pill(c, k, pillWait, icPause, "Paused")
	case r.Catalog == domain.CatalogGone:
		pill(c, k, pillWarn, icWarn, "Gone from Apple")
	case r.Catalog == domain.CatalogNotInStorefront:
		pill(c, k, pillWarn, icWarn, "Not in your region")
	case r.State == domain.RecordComplete:
		pill(c, k, pillOK, icCheck, "Downloaded")
	case r.State == domain.RecordPartial:
		pill(c, k, pillWait, nil, fmt.Sprintf("%d of %d", r.TracksDone, r.TracksWanted))
	case !r.ResolvedAt:
		pill(c, k, pillWait, icClock, "Not read yet")
	default:
		pill(c, k, pillWait, nil, "Not downloaded")
	}
}

// openRecord opens what a record row opens: an album's page, a playlist's
// run while one is queued or running.
func (a *App) openRecord(r store.Record) {
	if r.Ref.Kind == domain.ResourceAlbum {
		a.OpenAlbum(recordHit(r))
		return
	}
	if r.ActiveJobID != "" {
		a.OpenJob(r.ActiveJobID)
	}
}

// recordHit is an album record as the hit its page opens from.
func recordHit(r store.Record) client.SearchHit {
	c := r.Artwork.Colors
	return client.SearchHit{Type: "album", ID: r.Ref.ID, Name: r.Name, ArtistName: r.By, ArtworkURL: r.Artwork.URL,
		ArtworkBgColor: c.Bg, ArtworkTextColor1: c.Text1, ArtworkTextColor2: c.Text2, ArtworkTextColor3: c.Text3, ArtworkTextColor4: c.Text4,
		TrackCount: r.TracksWanted, Resource: r.Mark()}
}

// recordMenu is what can be done with a record in its state.
func (a *App) recordMenu(m *ui.Menu, r store.Record) {
	s := a.Store
	name := recordName(r)
	active := r.ActiveJobID != ""
	if r.Ref.Kind == domain.ResourceAlbum && m.Item("Open").Chosen() {
		a.openRecord(r)
	}
	switch {
	case active:
		if m.Item("Pause").Chosen() {
			s.Pause(r.Ref)
		}
	case r.Paused:
		if m.Item("Resume").Chosen() {
			s.Resume(r.Ref)
		}
	case r.State != domain.RecordComplete:
		if m.Item("Download").Chosen() {
			s.DownloadRecord(r.Ref, name)
		}
	}
	if !active && r.TracksDone > 0 && m.Item("Download again").Chosen() {
		s.Reset(r.Ref, nil, nil, name)
	}
	if r.TracksDone > 0 && m.Item("Check files").Chosen() {
		ref := r.Ref
		s.CheckDisk(&ref, false)
	}
	if !active {
		m.Separator()
		if m.Item("Remove from amdl…").Chosen() {
			rr := r
			a.lib.removing, a.lib.removeOpen = &rr, true
		}
	}
}

// removeDialog confirms a record's removal, which keeps its files.
func (a *App) removeDialog(c *ui.Context, k *Tokens) {
	r := a.lib.removing
	if r == nil {
		return
	}
	ui.DialogBase(c.Key("remove"), &a.lib.removeOpen, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(440).Padding(padCard).Gap(12).Radius(radiusCard).Background(k.Raise).Border(1, k.Line2).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.4))
		ui.Text(c, "Remove “"+clip(recordName(*r), 48)+"” from amdl?").FontSize(15).FontWeight(650).MaxLines(2).Ellipsis("…")
		ui.Text(c, "The files stay. Adding it again downloads it again over them.").FontSize(12.5).TextColor(k.Muted).LineHeight(1.5)
		ui.Row(c).Gap(8).Justify(ui.End).Padding(6, 0, 0, 0).Children(func() {
			if button(c, k, buttonPlain, "Cancel", nil).Clicked() {
				a.lib.removeOpen = false
			}
			ok := button(c, k, buttonPrimary, "Remove", nil)
			ok.Background(k.Bad)
			if ok.Clicked() {
				a.Store.RemoveRecord(r.Ref, recordName(*r))
				a.lib.removeOpen = false
			}
		})
	})
	if !a.lib.removeOpen {
		a.lib.removing = nil
	}
}

// LibraryTab shows the Library's tab t, as its segmented control would.
func (a *App) LibraryTab(t store.LibraryTab) {
	a.lib.tab = int(t)
	a.Store.SetLibraryTab(t)
}
