package ui

import (
	"fmt"

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

// appleList is the Apple library's list of the chosen kind: each item
// amdl keeps marked, any other with Add; a playlist without a catalog link
// opens its tracks, each added on its own.
func (a *App) appleList(c *ui.Context, k *Tokens) {
	s := a.Store
	l := &s.Library
	f := &a.lib
	ui.Row(c).Padding(0, 18, 10, 18).Gap(8).AlignItems(ui.Center).Background(k.Ground).Children(func() {
		prev := f.apple
		segmented(c, k, "apple", &f.apple, []string{"Albums", "Songs", "Playlists"}, nil)
		if f.apple != prev {
			s.SetAppleKind(store.AppleKinds[f.apple])
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
	a.scrollArea(c, "apple", scrollGutter, func() {
		for _, it := range items {
			a.appleRow(c, k, it)
		}
	}).Grow(1).Background(k.Ground)
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

func (a *App) appleRow(c *ui.Context, k *Tokens, it domain.AppleLibraryItem) {
	s := a.Store
	l := &s.Library
	kind := l.AppleKind
	libraryOnly := kind == "playlists" && it.CatalogID == ""
	ui.Column(c.Key(it.ID)).BorderWidth(0, 0, 1, 0).BorderColor(k.Line).Children(func() {
		ui.Row(c).Padding(8, 18).Gap(12).AlignItems(ui.Center).Children(func() {
			widgets.Cover{Image: a.Art.Get(it.ArtworkURL, 80), Placeholder: k.Track, Size: 40, Radius: radiusInner, Color: 1}.Show(c)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, it.Name).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…")
				var parts []string
				if it.ArtistName != "" {
					parts = append(parts, it.ArtistName)
				}
				if kind == "songs" && it.AlbumName != "" {
					parts = append(parts, it.AlbumName)
				}
				if kind == "albums" && it.TrackCount > 0 {
					parts = append(parts, plural(it.TrackCount, "song"))
				}
				if sub := joinParts(parts); sub != "" {
					ui.Text(c, sub).FontSize(12).TextColor(k.Muted).SingleLine().Ellipsis("…")
				}
			})
			switch {
			case it.CatalogID != "" && (kind == "albums" || kind == "playlists") && l.Kept[it.CatalogID]:
				if kind == "albums" {
					if markButton(c, k, "In Library", pillOK, icCheck).Label("In Library: open it").Clicked() {
						a.OpenAlbum(client.SearchHit{Type: "album", ID: it.CatalogID, Name: it.Name, ArtistName: it.ArtistName, ArtworkURL: it.ArtworkURL,
							Resource: s.RecordMark(domain.ResourceAlbum, it.CatalogID)})
					}
				} else {
					pill(c, k, pillOK, icCheck, "In Library")
				}
			case libraryOnly:
				open := a.lib.opened[it.ID]
				if button(c, k, buttonPlain, "Tracks", icChevron).Label("Show its tracks").Clicked() {
					if a.lib.opened == nil {
						a.lib.opened = map[string]bool{}
					}
					a.lib.opened[it.ID] = !open
					s.OpenApplePlaylist(it.ID)
				}
			default:
				a.appleAdd(c, k, it.URL, it.Name)
			}
		})
		if libraryOnly && a.lib.opened[it.ID] {
			ui.Column(c).Padding(0, 18, 8, 70).Children(func() {
				for _, t := range l.Tracks[it.ID] {
					ui.Row(c.Key(t.Position)).Height(34).Gap(10).AlignItems(ui.Center).Children(func() {
						mono(c, fmt.Sprint(t.Position+1), 11).TextColor(k.Faint).Width(22)
						ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
							ui.Text(c, t.Name).FontSize(12.5).SingleLine().Ellipsis("…")
						})
						a.appleAdd(c, k, t.URL, t.Name)
					})
				}
			})
		}
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
