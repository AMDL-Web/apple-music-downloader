package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/client"
)

// confirmFor is how long "Download all" waits for its second click.
const confirmFor = 4 * time.Second

// searchText is what the field searches for, and whether it is too long to.
func (a *App) searchText() (term string, tooLong bool) {
	o := &a.Store.Omni
	return store.SearchTerm(o.Text, len(o.Links) > 0)
}

// searchBody is home's answer to words: the catalog's albums, songs and
// artists, or one artist's albums, or the state that stands for them.
func (a *App) searchBody(c *ui.Context, k *Tokens, width float32) {
	s := a.Store
	q := &s.Search
	sel := a.chosenHit(q.ShownHits())
	body := ui.Column(c).Width(width).Gap(32)
	if q.Stale {
		body.Opacity(0.5)
	}
	body.Children(func() {
		switch {
		case q.Artist.ID != "":
			a.artistView(c, k, sel, width)
		case q.Status == store.SearchFailed:
			searchFailed(c, k, q.Err, s.RetrySearch)
		case q.Result == nil:
			skeletonGroups(c, k, width)
		case len(q.ShownHits()) == 0:
			searchMessage(c, k, icSearch, "Nothing found for “"+q.Term+"”", "Try fewer words, or the name as Apple Music writes it.")
		default:
			for _, g := range store.SearchGroups {
				hg := hitGroupOf(q.Result, g)
				if hg == nil || len(hg.Items) == 0 {
					continue
				}
				ui.Column(c.Key(g)).Gap(14).Children(func() {
					ui.Row(c).Height(30).AlignItems(ui.Center).Children(func() {
						display(c, strings.ToUpper(g[:1])+g[1:], 18)
						ui.Spacer(c)
						morePage(c, k, g, hg.NextOffset > 0, q.More[g], func() { s.MoreSearch(g) })
					})
					if g == "songs" {
						a.songGrid(c, k, hg.Items, sel, width)
					} else {
						a.tileGrid(c, k, g, hg.Items, sel, width)
					}
				})
			}
		}
	})
}

// artistView is the open artist: its round cover, name and Download all
// over its albums.
func (a *App) artistView(c *ui.Context, k *Tokens, sel *client.SearchHit, width float32) {
	s := a.Store
	ar := &s.Search.Artist
	ui.Column(c).Gap(16).Children(func() {
		if button(c, k, buttonGhost, "Results", icBack).Label("Back to the results").AlignSelf(ui.Start).Clicked() {
			a.searchSel, a.searchReveal = store.HitKey(ar.Artist), true
			s.OpenArtist("")
			return
		}
		ui.Row(c).Gap(20).AlignItems(ui.Center).Children(func() {
			h := ar.Artist
			widgets.Cover{Image: a.Art.Get(h.ArtworkURL, 192), Placeholder: hitPalette(h).Bg, Size: 96, Radius: radiusFull, Color: 1}.Show(c)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
				name := firstNonEmptyStr(h.Name, "Artist")
				display(c, name, 26).SingleLine().Ellipsis("…")
				facts := []string{}
				if h.Genre != "" {
					facts = append(facts, h.Genre)
				}
				if ar.Status == store.SearchReady {
					facts = append(facts, albumCount(ar))
				}
				ui.Text(c, strings.Join(facts, " · ")).FontSize(13).TextColor(k.Muted).SingleLine()
			})
			if ar.Status == store.SearchReady {
				a.downloadAll(c, k, ar)
			}
		})
	})
	switch ar.Status {
	case store.SearchFailed:
		searchFailed(c, k, ar.Err, s.RetryArtist)
		return
	case store.SearchLoading:
		skeletonTiles(c, k, width, 5)
		return
	}
	if len(ar.Albums.Items) == 0 {
		searchMessage(c, k, icLibrary, "No albums", "Apple Music lists no albums for this artist here.")
		return
	}
	ui.Column(c).Gap(14).Children(func() {
		a.tileGrid(c, k, "album", ar.Albums.Items, sel, width)
		morePage(c, k, "artist", ar.Albums.NextOffset > 0, ar.More, s.MoreArtist)
	})
}

// chosenHit is the hit Return acts on: the one picked with the arrows or a
// click, while it is still shown.
func (a *App) chosenHit(hits []client.SearchHit) *client.SearchHit {
	for i := range hits {
		if store.HitKey(hits[i]) == a.searchSel {
			return &hits[i]
		}
	}
	return nil
}

// moveHit picks the hit by steps after the chosen one, in reading order;
// before any is chosen, Down picks the first and Up the last.
func (a *App) moveHit(by int) {
	hits := a.Store.Search.ShownHits()
	if len(hits) == 0 {
		return
	}
	i := -1
	for j := range hits {
		if store.HitKey(hits[j]) == a.searchSel {
			i = j
		}
	}
	switch {
	case i < 0 && by < 0:
		i = len(hits) - 1
	case i < 0:
		i = 0
	default:
		i += by
	}
	a.searchSel = store.HitKey(hits[min(max(i, 0), len(hits)-1)])
	a.searchReveal = true
}

// searchKeys are the search's window keys, for when the keyboard is out of
// the field (fieldKey takes them in it): the arrows, Home and End, Return,
// and Escape,
// which leaves an artist, then forgets the chosen hit, then clears the
// field.
func (a *App) searchKeys(c *ui.Context) {
	s := a.Store
	c.OnShortcut(0, ui.KeyDown, func() { a.moveHit(1) })
	c.OnShortcut(0, ui.KeyUp, func() { a.moveHit(-1) })
	c.OnShortcut(0, ui.KeyHome, func() { a.searchSel = ""; a.moveHit(1) })
	c.OnShortcut(0, ui.KeyEnd, func() { a.searchSel = ""; a.moveHit(-1) })
	c.OnShortcut(0, ui.KeyEnter, a.actOnHit)
	c.OnShortcut(0, ui.KeyEscape, func() {
		switch {
		case s.Search.Artist.ID != "":
			a.searchSel = store.HitKey(s.Search.Artist.Artist)
			a.searchReveal = true
			s.OpenArtist("")
		case a.searchSel != "":
			a.searchSel = ""
		default:
			a.clearField()
		}
	})
}

// actOnHit is Return: the first press picks the first hit, then an album
// or an artist opens, as a click does, and a song downloads.
func (a *App) actOnHit() {
	s := a.Store
	hits := s.Search.ShownHits()
	h := a.chosenHit(hits)
	if h == nil {
		if len(hits) > 0 {
			a.searchSel, a.searchReveal = store.HitKey(hits[0]), true
		}
		return
	}
	switch h.Type {
	case "artist":
		a.openArtist(*h)
		return
	case "album":
		a.OpenAlbum(*h)
		return
	}
	if m, _ := s.HitMark(*h); m != store.MarkActive {
		s.SubmitHit(*h)
	}
}

func (a *App) openArtist(h client.SearchHit) {
	a.searchSel, a.searchReveal = "", false
	a.Store.OpenArtist(h.ID)
}

// albumCount is the albums read of an artist, "+" while more are left.
func albumCount(ar *store.ArtistPages) string {
	n := len(ar.Albums.Items)
	if ar.Albums.NextOffset > 0 {
		return fmt.Sprintf("%d+ albums", n)
	}
	return plural(n, "album")
}

func hitGroupOf(r *client.SearchResult, g string) *client.HitGroup {
	switch g {
	case "songs":
		return r.Songs
	case "albums":
		return r.Albums
	}
	return r.Artists
}

// downloadAll starts every album of the artist, after a second click: it
// can be hundreds of tracks.
func (a *App) downloadAll(c *ui.Context, k *Tokens, ar *store.ArtistPages) {
	s := a.Store
	if m, _ := s.HitMark(ar.Artist); m == store.MarkActive {
		pill(c, k, pillRun, icDown, "In the queue")
		return
	}
	if s.Search.Starting[store.HitKey(ar.Artist)] {
		pill(c, k, pillWait, nil, "Starting…")
		return
	}
	armed := a.confirmAll == ar.ID && c.Now().Sub(a.confirmAt) < confirmFor
	text, kind := "Download all", buttonPlain
	if armed {
		text, kind = "Start "+albumCount(ar)+"?", buttonPrimary
		c.After(confirmFor - c.Now().Sub(a.confirmAt))
	}
	if button(c, k, kind, text, icDown).Height(34).Label("Download every album of the artist").Clicked() {
		if armed {
			a.confirmAll = ""
			s.SubmitHit(ar.Artist)
		} else {
			a.confirmAll, a.confirmAt = ar.ID, c.Now()
		}
	}
}

// morePage is a group's "Show more", its loading line or its retry, one
// height in every state.
func morePage(c *ui.Context, k *Tokens, key string, more bool, st store.PageState, load func()) {
	if !more {
		return
	}
	ui.Row(c.Key(key + "-more")).Height(30).Gap(10).AlignItems(ui.Center).Children(func() {
		switch st {
		case store.PageLoading:
			ui.Text(c, "Loading more…").FontSize(12).TextColor(k.Muted)
		case store.PageFailed:
			ui.Text(c, "Could not load more.").FontSize(12).TextColor(k.Bad)
			if button(c, k, buttonGhost, "Try again", nil).Clicked() {
				load()
			}
		default:
			if button(c, k, buttonGhost, "Show more", nil).Clicked() {
				load()
			}
		}
	})
}

// searchMessage stands for the results when there are none to show.
func searchMessage(c *ui.Context, k *Tokens, ic *ui.SVG, title, help string) {
	ui.Column(c).Height(260).Center().Gap(8).Children(func() {
		ui.Icon(c, ic).FontSize(26).TextColor(k.Faint)
		ui.Text(c, title).FontSize(15).FontWeight(650).TextAlign(ui.Center)
		ui.Text(c, help).FontSize(12.5).TextColor(k.Muted).MaxWidth(420).TextAlign(ui.Center)
	})
}

// searchFailed stands for the results when their read failed, with a retry.
func searchFailed(c *ui.Context, k *Tokens, why string, retry func()) {
	ui.Column(c).Height(260).Center().Gap(8).Children(func() {
		ui.Icon(c, icWarn).FontSize(26).TextColor(k.Bad)
		ui.Text(c, "Apple Music did not answer").FontSize(15).FontWeight(650)
		ui.Text(c, why).FontSize(12.5).TextColor(k.Muted).MaxWidth(420).TextAlign(ui.Center).MaxLines(3).Ellipsis("…")
		if button(c, k, buttonPlain, "Try again", nil).Clicked() {
			retry()
		}
	})
}
