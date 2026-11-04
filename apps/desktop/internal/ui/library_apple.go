package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/region"
	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// appleSyncBar is the Apple library's lists and its last sync, with Sync
// now.
func (a *App) appleSyncBar(c *ui.Context, k *Tokens, compact bool) {
	s := a.Store
	st := s.Library.AppleEntry()
	syncing := st != nil && st.Current != nil
	if !compact {
		if st != nil && st.LastSync != nil && st.LastSync.FinishedAt != nil {
			ui.Text(c, "Synced "+ago(*st.LastSync.FinishedAt)).FontSize(12).TextColor(k.Muted).SingleLine()
		}
	}
	if buttonOff(c, k, buttonPlain, "Sync now", icSync, syncing).Label("Sync your Apple Music library").Clicked() && !syncing {
		s.SyncApple()
	}
}

// libraryPadX is the room either side of the Library's lists.
const libraryPadX = 18

// appleList is the Apple library's list of the chosen kind: albums and
// playlists as a grid of their covers, songs as rows.
func (a *App) appleList(c *ui.Context, k *Tokens) {
	s := a.Store
	l := &s.Library
	f := &a.lib
	ui.Row(c).Padding(0, libraryPadX, 10, libraryPadX).Gap(8).AlignItems(ui.Center).Background(k.Ground).Children(func() {
		prev := f.apple
		segmented(c, k, "apple", &f.apple, []string{"Albums", "Songs", "Playlists"}, nil)
		if f.apple != prev {
			s.SetAppleKind(store.AppleKinds[f.apple])
			f.kindAt = c.Now()
		}
		ui.Spacer(c)
		a.appleAccounts(c, k)
	})
	switch {
	case l.AppleStatus == store.SearchFailed:
		searchMessage(c, k, icWarn, "Your Apple Music library is not available", l.AppleErr)
		return
	case l.AppleStatus != store.SearchReady:
		return
	case len(l.Apple.Items) == 0:
		searchMessage(c, k, icLibrary, "Nothing here", "")
		return
	}
	items := l.Apple.Items
	if l.AppleKind == "songs" {
		a.appleSongs(c, k, items)
		return
	}
	a.appleGrid(c, k, items)
}

// appleListState is the place of the Apple list of kind, which each kind
// keeps of its own.
func (a *App) appleListState(kind string) *ui.ListState {
	if a.lib.appleLists == nil {
		a.lib.appleLists = map[string]*ui.ListState{}
	}
	st := a.lib.appleLists[kind]
	if st == nil {
		st = &ui.ListState{}
		a.lib.appleLists[kind] = st
	}
	return st
}

// appleSongs is the Apple library's songs, a row each, built as they show.
func (a *App) appleSongs(c *ui.Context, k *Tokens, items []domain.AppleLibraryItem) {
	ui.Box(c.Key("apple-area-songs")).Grow(1).MinHeight(0).Background(k.Ground).Transition(a.appleMotion(c)).Children(func() {
		st := a.scrollState(c, "apple-songs")
		list := ui.List(c.Key("apple-songs"), a.appleListState("songs"), len(items), func(i int) {
			a.appleRow(c, k, items[i])
		}).Absolute().Top(0).Left(0).Right(0).Bottom(0).ItemKey(func(i int) any { return items[i].ID }).TrackScroll(st)
		a.fadeEdges(list, st)
		a.noteView(list)
	})
}

// swapFadeLate fades a kind's list in once the one it replaces has faded
// out (groupFade), as the search's tabs go from one group to another.
var swapFadeLate = ui.Animation{Duration: groupFade.Duration, Ease: groupFade.Ease, Delay: groupFade.Duration}

// appleMotion is the Transition of the Apple list of a kind: the one left
// fades out, and the one chosen fades in after it, so the two never show
// through each other. It is none without page motion.
func (a *App) appleMotion(c *ui.Context) ui.ElementTransition {
	if !a.PageMotion {
		return ui.ElementTransition{}
	}
	t := ui.ElementTransition{Animation: &groupFade, Enter: &fadeOut, Exit: &fadeOut}
	if c.Now().Sub(a.lib.kindAt) < 2*groupFade.Duration {
		t.Animation = &swapFadeLate
	}
	return t
}

// appleGridGapY is the room between two rows of the Apple grid, as between
// home's rows of tiles.
const appleGridGapY = 24

// appleGrid lays the Apple library's albums or playlists out as home's
// tiles, rows filling the page. The list builds only the rows in view.
func (a *App) appleGrid(c *ui.Context, k *Tokens, items []domain.AppleLibraryItem) {
	kind := a.Store.Library.AppleKind
	width := a.pageWidth(c) - 2*libraryPadX
	cols, size := tileColumns(width), tileSize(width, 0)
	rows := (len(items) + cols - 1) / cols
	ui.Box(c.Key("apple-area-" + kind)).Grow(1).MinHeight(0).Background(k.Ground).Transition(a.appleMotion(c)).Children(func() {
		st := a.scrollState(c, "apple-"+kind)
		list := ui.List(c.Key("apple-"+kind), a.appleListState(kind), rows, func(r int) {
			ui.Row(c).Padding(0, libraryPadX, appleGridGapY, libraryPadX).Gap(tileGap).AlignItems(ui.Start).Children(func() {
				for _, it := range items[r*cols : min(len(items), (r+1)*cols)] {
					a.appleTile(c, k, kind, it, size)
				}
			})
		}).Absolute().Top(0).Left(0).Right(0).Bottom(0).ItemKey(func(r int) any { return items[r*cols].ID }).TrackScroll(st)
		a.fadeEdges(list, st)
		a.noteView(list)
	})
}

// appleLibraryOnly reports whether it is a playlist with no catalog link,
// whose tracks are added one by one.
func appleLibraryOnly(kind string, it domain.AppleLibraryItem) bool {
	return kind == "playlists" && it.CatalogID == ""
}

// appleTile is an Apple album or playlist as home's tiles show one: its
// cover large, which opens an album's page or a library-only playlist's
// tracks, its title on up to two lines, its artist or curator, and its one
// control in the cover's corner (appleControl).
func (a *App) appleTile(c *ui.Context, k *Tokens, kind string, it domain.AppleLibraryItem, size float32) {
	ui.Column(c.Key(it.ID)).Width(size).Gap(10).Children(func() {
		art := ui.Box(c.Key("art")).Size(size, size)
		hover := art.Hovered()
		art.Children(func() {
			open := tileOpen(c, size, radiusCover, it.Name)
			from := a.appleFrom(kind, it)
			if act := a.appleOpen(kind, it); act != nil {
				if open.Clicked() {
					// The page grows out of this cover (flight_grow.go); a
					// playlist's tracks open in place and fly nothing.
					a.openingFrom(from)
					act()
				}
				open.Cursor(ui.CursorPointer)
			}
			open.Children(func() {
				cover := widgets.Cover{Image: a.Art.Get(it.ArtworkURL, int(2*size)), Placeholder: k.Track, Size: size, Radius: radiusCover, Color: 1}.Show(c)
				a.flySource(cover, from, partCover, "", radiusCover)
				if it.ArtworkURL == "" {
					ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().PassThrough().Children(func() {
						ui.Icon(c, icNote).FontSize(size / 4).TextColor(k.Faint)
					})
				}
			})
			a.appleControl(c, k, kind, it, hover)
		})
		ui.Column(c).Gap(3).Children(func() {
			tileTitle(c, it.Name, 2).MaxWidth(size)
			tileSub(c, k, it.ArtistName, size)
		})
	})
}

// appleOpen is what a click on an Apple tile's cover does: an album's opens
// its page, and a playlist's opens its page with its songs as the library
// copy has them; nil for a cover that opens nothing.
func (a *App) appleOpen(kind string, it domain.AppleLibraryItem) func() {
	switch {
	case kind == "albums" && it.CatalogID != "":
		return func() { a.openAppleAlbum(it) }
	case kind == "playlists":
		return func() { a.openApplePlaylist(it) }
	}
	return nil
}

// openApplePlaylist opens a playlist of the Apple library on its page, its
// songs read from the library copy: a playlist the catalog has downloads
// whole through its catalog link as its hit's, one only the library has
// (appleLibraryOnly) song by song.
func (a *App) openApplePlaylist(it domain.AppleLibraryItem) {
	h, ok := a.appleHit("playlists", it)
	if !ok {
		h = client.SearchHit{Type: "playlist", ID: it.ID, Name: it.Name, ArtistName: it.ArtistName, ArtworkURL: it.ArtworkURL, TrackCount: it.TrackCount}
	}
	a.openAlbumFrom(h, store.PageSource{Playlist: it.ID, Account: a.Store.Library.AppleAccount})
}

// appleHit is the search hit an Apple album or playlist with a catalog page
// stands for, carrying its record's mark as the last read had it; false for
// an item amdl cannot download as a hit.
func (a *App) appleHit(kind string, it domain.AppleLibraryItem) (client.SearchHit, bool) {
	if it.CatalogID == "" || it.URL == "" {
		return client.SearchHit{}, false
	}
	typ, rk := "album", domain.ResourceAlbum
	if kind == "playlists" {
		typ, rk = "playlist", domain.ResourcePlaylist
	}
	return client.SearchHit{Type: typ, ID: it.CatalogID, Name: it.Name, ArtistName: it.ArtistName, ArtworkURL: it.ArtworkURL, URL: it.URL,
		TrackCount: it.TrackCount, Resource: a.Store.Library.Mark(rk, it.CatalogID)}, true
}

// appleFrom is the name a flight from an Apple tile's cover goes by: its
// hit's, as a search tile's, so that its mark's clicks (tileControl) open
// from it too, else its own.
func (a *App) appleFrom(kind string, it domain.AppleLibraryItem) string {
	if h, ok := a.appleHit(kind, it); ok {
		return a.hitFrom(h)
	}
	return "lib:" + it.ID
}

func (a *App) openAppleAlbum(it domain.AppleLibraryItem) {
	a.OpenAlbum(client.SearchHit{Type: "album", ID: it.CatalogID, Name: it.Name, ArtistName: it.ArtistName, ArtworkURL: it.ArtworkURL,
		Resource: a.Store.RecordMark(domain.ResourceAlbum, it.CatalogID)})
}

// appleControl is an Apple tile's control, as a search tile's: the check
// of an item amdl keeps, which opens an album; a library-only playlist's
// button that shows its tracks; else Add, shown while the tile is pointed
// at, and what the add became.
func (a *App) appleControl(c *ui.Context, k *Tokens, kind string, it domain.AppleLibraryItem, hover bool) {
	s := a.Store
	l := &s.Library
	done := markLook{bg: k.OK, fg: ui.RGB(255, 255, 255), ic: icCheck}
	if h, ok := a.appleHit(kind, it); ok {
		// The record's own mark, as a search tile's: queued, paused, partly
		// downloaded (its click adds the rest) or downloaded.
		if mark, _ := s.HitMark(h); mark != store.MarkNone {
			a.tileControl(c, k, h, hover)
			return
		}
	}
	switch {
	case it.CatalogID != "" && l.Kept[it.CatalogID]:
		b := tileCorner(c).Tooltip("In Library")
		if kind == "albums" {
			b.Label("In Library: open it").Cursor(ui.CursorPointer)
			if b.Clicked() {
				a.openingFrom(a.appleFrom(kind, it))
				a.openAppleAlbum(it)
			}
		} else {
			b.Label("In Library")
		}
		done.show(b, true, false)
		done.fill(c, b)
		return
	case appleLibraryOnly(kind, it), it.URL == "":
		// Added song by song, on its page.
		return
	}
	st, sent := l.Added[it.URL]
	b := tileCorner(c)
	look, wait := markLook{glass: true, fg: k.Fg, ic: icPlus}, false
	switch {
	case sent && st == "" && waited(c, true, a.lib.addedAt[it.URL]):
		// The button changes only when the add is slow, as a pill does.
		b.Label("Adding " + it.Name)
		look.ic = nil
	case sent && st == "":
		b.Label("Add " + it.Name).Cursor(ui.CursorPointer)
		look.interactive = true
	case st == domain.SubmitAccepted:
		b.Label(it.Name + " is in the queue").Tooltip("In the queue")
		look = markLook{bg: k.Accent, fg: k.AccentInk, ic: icDown}
		// As a search tile's mark of a download under way, it opens the page.
		if kind == "albums" && it.CatalogID != "" {
			b.Cursor(ui.CursorPointer)
			if b.Clicked() {
				a.openingFrom(a.appleFrom(kind, it))
				a.openAppleAlbum(it)
			}
		}
	case st == domain.SubmitPending:
		b.Label("Adding " + it.Name).Tooltip("Adding")
		look = markLook{bg: k.Raise2, fg: k.Fg, ic: icClock}
	case st == domain.SubmitAlreadyDownloaded:
		b.Label(it.Name + " is in your library").Tooltip("In Library")
		look = done
	default:
		b.Label("Add " + it.Name).Cursor(ui.CursorPointer)
		look.interactive, wait = true, true
		if b.Clicked() {
			if a.lib.addedAt == nil {
				a.lib.addedAt = map[string]time.Time{}
			}
			a.lib.addedAt[it.URL] = c.Now()
			s.AddApple(it.URL, it.Name)
		}
	}
	look.show(b, hover, wait)
	look.fill(c, b)
}

// appleAccounts switches between the accounts whose Apple library amdl
// copies, by region, once there are two of them.
func (a *App) appleAccounts(c *ui.Context, k *Tokens) {
	l := &a.Store.Library
	on := l.AppleAccounts()
	if len(on) < 2 {
		return
	}
	labels, names := appleAccountLabels(on)
	sel := 0
	for i, e := range on {
		if e.AccountID == l.AppleAccount {
			sel = i
		}
	}
	prev := sel
	segmentedTips(c, k, "apple-account", &sel, labels, labels, nil, names)
	if sel != prev {
		a.Store.SetAppleAccount(on[sel].AccountID)
	}
}

// appleAccountLabels are the segments' words, the region's name, and each
// one's Apple ID for its tooltip. Accounts of one region are told apart by
// their Apple IDs.
func appleAccountLabels(on []client.AppleLibraryAccountStatus) (labels, names []string) {
	seen := map[string]int{}
	for _, e := range on {
		seen[region.Name(e.Region)]++
	}
	for i, e := range on {
		text := region.Name(e.Region)
		if text == "" || seen[text] > 1 {
			text = region.Label(e.Name, "", fmt.Sprintf("Account %d", i+1))
		}
		labels, names = append(labels, text), append(names, e.Name)
	}
	return labels, names
}

// appleRow is an Apple library song: its cover, title, artist and album,
// and its Add.
func (a *App) appleRow(c *ui.Context, k *Tokens, it domain.AppleLibraryItem) {
	ui.Row(c.Key(it.ID)).Padding(8, libraryPadX).Gap(12).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(k.Line).Children(func() {
		widgets.Cover{Image: a.Art.Get(it.ArtworkURL, 80), Placeholder: k.Track, Size: 40, Radius: radiusInner, Color: 1}.Show(c)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, it.Name).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…")
			var parts []string
			for _, p := range []string{it.ArtistName, it.AlbumName} {
				if p != "" {
					parts = append(parts, p)
				}
			}
			if sub := joinParts(parts); sub != "" {
				ui.Text(c, sub).FontSize(12).TextColor(k.Muted).SingleLine().Ellipsis("…")
			}
		})
		a.appleAdd(c, k, it.URL, it.Name)
	})
}

// appleAdd is an Apple item's Add, or what its add became.
func (a *App) appleAdd(c *ui.Context, k *Tokens, url, name string) {
	if url == "" {
		return
	}
	s := a.Store
	st, sent := s.Library.Added[url]
	switch {
	case sent && st == "":
		pill(c, k, pillWait, nil, "Adding…")
	case st == domain.SubmitAccepted:
		pill(c, k, pillRun, icDown, "In the queue")
	case st == domain.SubmitPending:
		pill(c, k, pillWait, icClock, "Adding")
	case st == domain.SubmitAlreadyDownloaded:
		pill(c, k, pillOK, icCheck, "In Library")
	default:
		if button(c, k, buttonPlain, "Add", icPlus).Label("Add " + name).Clicked() {
			s.AddApple(url, name)
		}
	}
}

// AppleKind shows the Apple library's list of kind, one of
// store.AppleKinds.
func (a *App) AppleKind(kind string) {
	for i, k := range store.AppleKinds {
		if k == kind {
			a.lib.apple = i
		}
	}
	a.Store.SetAppleKind(kind)
}
