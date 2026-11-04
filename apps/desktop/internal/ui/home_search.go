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

// searchBody is home's answer to words: the tabs, Overview and the four
// groups, over the first of the catalog's albums, songs, playlists and
// artists, or one group of them page after page; or one artist's albums,
// or the state that stands for them. A group shown alone with no hits
// answers inside its tab, never as the whole search, so the tabs stay to
// leave it by; "Nothing found" is for every group empty.
func (a *App) searchBody(c *ui.Context, k *Tokens, width float32) {
	s := a.Store
	q := &s.Search
	sel := a.chosenHit(q.ShownHits())
	if a.groupsMoved {
		a.groupsAt, a.groupsMoved = c.Now(), false
	}
	if since := c.Now().Sub(a.groupsAt); since < groupEnter {
		c.After(groupEnter - since)
	}
	focus := q.Focus
	a.followView(c, q.Artist.ID)
	body := ui.Column(c).Width(width)
	if q.Stale {
		body.Opacity(0.5)
	}
	// The results and an artist's albums are views of their own: one
	// fades out where it is, then the search goes to its top and the other
	// rises in (followView), so the two never show over each other.
	view := "results"
	if q.Artist.ID != "" {
		view = "artist"
	}
	body.Children(func() {
		ui.Column(c.Key(view)).FillWidth().Gap(32).Transition(a.viewMotion(c)).Children(func() {
			switch {
			case q.Artist.ID != "":
				a.artistView(c, k, sel, width)
			case q.Status == store.SearchFailed:
				a.regionRow(c, k)
				searchFailed(c, k, q.Err, s.RetrySearch)
			case q.Result == nil:
				a.groupTabs(c, k)
				skeletonGroups(c, k, width)
			case store.NoHits(q.Result):
				a.regionRow(c, k)
				searchMessage(c, k, icSearch, "Nothing found for “"+q.Term+"”", "")
			default:
				a.groupTabs(c, k)
				a.searchGroups(c, k, focus, sel, width)
			}
		})
	})
}

// viewSwap is how long the view going out takes to fade, after which the
// one coming in rises in.
var (
	viewSwap     = groupFade.Duration
	viewRiseLate = ui.Animation{Duration: groupEase.Duration, Ease: ui.EaseOut, Delay: viewSwap}
)

// followView notes the search switching between the results and artist's
// albums, and once the view going out has faded, scrolls to what the one
// coming in shows first: an artist's top, or the results' tile of the
// artist left (searchReveal, in hitTile).
func (a *App) followView(c *ui.Context, artist string) {
	if artist != a.viewArtist {
		a.viewArtist, a.viewAt, a.viewScroll = artist, c.Now(), true
	}
	if a.viewSwapping(c) {
		c.After(viewSwap - c.Now().Sub(a.viewAt))
		return
	}
	if a.viewScroll {
		a.viewScroll = false
		if st := a.scrolls["home-body"]; st != nil && artist != "" {
			st.Y = 0
		}
	}
}

// viewSwapping reports whether the view going out still fades.
func (a *App) viewSwapping(c *ui.Context) bool {
	return a.PageMotion && c.Now().Sub(a.viewAt) < viewSwap
}

// viewMotion is the Transition of the search's view: the one coming in
// rises in once the one going out has faded; that one fades out.
func (a *App) viewMotion(c *ui.Context) ui.ElementTransition {
	if !a.PageMotion {
		return ui.ElementTransition{}
	}
	if a.viewSwapping(c) {
		return ui.ElementTransition{Animation: &viewRiseLate, Enter: &riseIn, Exit: &fadeOut}
	}
	return ui.ElementTransition{Animation: &groupFade, Exit: &fadeOut}
}

// groupRow is the height of a group's title row and of the tabs; loadAhead
// is how far from the end of a group shown alone its next page is read.
const groupRow, loadAhead = 30, 600

func groupTitle(g string) string { return strings.ToUpper(g[:1]) + g[1:] }

// The motion between the overview and a group alone, when the window
// moves the album page too (App.PageMotion), in turns so that nothing
// passes over what is still there. Into a tab, the other groups and the
// group's title fade (groupFade); then the group glides to the top
// (groupEase, after groupClear) and the hits past the overview's fade in
// where they land. Out to the overview, the group glides to its place
// first and the other groups fade in after it. Things fade in only for
// groupEnter after a tab changes, so a new answer in the same tab shows
// at once.
var (
	groupEase  = ui.Animation{Duration: 320 * time.Millisecond, Ease: ui.EaseInOut}
	groupFade  = ui.Animation{Duration: 160 * time.Millisecond, Ease: ui.EaseOut}
	groupClear = groupFade.Duration - 20*time.Millisecond
	groupEnter = groupClear + groupEase.Duration + groupFade.Duration + 50*time.Millisecond

	groupGlideLate = ui.Animation{Duration: groupEase.Duration, Ease: groupEase.Ease, Delay: groupClear}
	groupFadeLate  = ui.Animation{Duration: groupFade.Duration, Ease: groupFade.Ease, Delay: groupEase.Duration}
	hitFadeLate    = ui.Animation{Duration: groupFade.Duration, Ease: groupFade.Ease, Delay: groupClear + groupEase.Duration/2}
	landFadeLate   = ui.Animation{Duration: groupFade.Duration, Ease: groupFade.Ease, Delay: groupClear + groupEase.Duration}
	fadeOut        = ui.Motion{}
	riseIn         = ui.Motion{Y: 12}
)

// tabChange reports whether the search changed tab within groupEnter, and
// whether into a group's rather than out to the overview.
func (a *App) tabChange(c *ui.Context) (changing, into bool) {
	if c.Now().Sub(a.groupsAt) >= groupEnter {
		return false, false
	}
	return true, a.Store.Search.Focus != ""
}

// groupMotion is the Transition of a group of the search: one leaving
// fades out; one coming into the overview fades in after the group that
// stays has moved; the one that stays glides, after the others clear the
// way into a tab. It is none without page motion.
func (a *App) groupMotion(c *ui.Context, g string, leaving bool) ui.ElementTransition {
	if !a.PageMotion {
		return ui.ElementTransition{}
	}
	changing, into := a.tabChange(c)
	switch {
	case leaving:
		return ui.ElementTransition{Animation: &groupFade, Exit: &fadeOut}
	case changing && !into && g != a.groupsFrom:
		return ui.ElementTransition{Animation: &groupFadeLate, Exit: &fadeOut, Enter: &fadeOut}
	}
	t := ui.ElementTransition{Animation: &groupEase, Position: true, Exit: &fadeOut}
	if changing && into {
		t.Animation, t.Enter = &groupGlideLate, &fadeOut
	}
	return t
}

// titleMotion is the Transition of a group's title, or of the row under a
// group alone, which fade out at once and in once the group has landed.
func (a *App) titleMotion(c *ui.Context) ui.ElementTransition {
	if !a.PageMotion {
		return ui.ElementTransition{}
	}
	t := ui.ElementTransition{Animation: &groupFade, Exit: &fadeOut}
	if changing, into := a.tabChange(c); changing {
		t.Enter, t.Animation = &fadeOut, &groupFadeLate
		if into {
			t.Animation = &landFadeLate
		}
	}
	return t
}

// hitMotion is the Transition of a hit of the search: it settles as its
// row lays out again and fades out when its group no longer shows it;
// one appearing in a tab just opened rises in once its group is there.
func (a *App) hitMotion(c *ui.Context) ui.ElementTransition {
	if !a.PageMotion {
		return ui.ElementTransition{}
	}
	t := ui.ElementTransition{Animation: &groupFade, Position: true, Size: true, Exit: &fadeOut}
	if changing, into := a.tabChange(c); changing {
		t.Enter = &riseIn
		if into {
			t.Animation = &hitFadeLate
		}
	}
	return t
}

// searchGroups are the overview, every group with hits cut to what one row
// of tiles or rows of song cards hold at width (overviewCount) under its
// title, which opens its tab; or the focused group alone, its next page
// read as the end comes near. The frame shows focus, the tab it started
// with: one a click in it opens is the next frame's, and the groups that
// one leaves are given the quicker fade they go with.
func (a *App) searchGroups(c *ui.Context, k *Tokens, focus string, sel *client.SearchHit, width float32) {
	s := a.Store
	q := &s.Search
	type built struct {
		g  string
		el ui.Element
	}
	var groups [4]built
	n := 0
	for _, g := range store.SearchGroups {
		hg := store.HitGroup(q.Result, g)
		alone := focus == g
		if focus != "" && !alone || focus == "" && (hg == nil || len(hg.Items) == 0) {
			continue
		}
		el := ui.Column(c.Key(g)).Gap(14).Children(func() {
			if !alone {
				n := overviewCount(g, width)
				q.SetOverview(g, n)
				a.groupHead(c, k, g)
				a.groupGrid(c, k, g, hg.Items[:min(n, len(hg.Items))], false, sel, width)
				return
			}
			switch {
			case hg != nil && len(hg.Items) > 0:
				a.groupGrid(c, k, g, hg.Items, hg.NextOffset == 0, sel, width)
			case hg != nil && hg.NextOffset > 0:
				skeletonTiles(c, k, width, 5)
			default:
				searchMessage(c, k, icSearch, "No "+g+" for “"+q.Term+"”", "")
			}
			if hg != nil && hg.NextOffset > 0 {
				a.groupMore(c, k, g)
			}
		})
		groups[n] = built{g, el}
		n++
	}
	for _, b := range groups[:n] {
		b.el.Transition(a.groupMotion(c, b.g, q.Focus != focus && q.Focus != "" && b.g != q.Focus))
	}
}

// overviewCount is how many hits of group the overview shows at width: a
// row of tiles, or two rows of song cards, four when a row holds one.
func overviewCount(g string, width float32) int {
	if g == "songs" {
		return max(4, 2*songColumns(width))
	}
	return tileColumns(width)
}

// groupHead is a group's title in the overview, which opens its tab.
func (a *App) groupHead(c *ui.Context, k *Tokens, g string) {
	head := ui.ButtonBase(c.Key("head")).Height(groupRow).Gap(4).AlignItems(ui.Center).AlignSelf(ui.Start).
		Label("See all " + g).Transition(a.titleMotion(c))
	col := k.Faint
	if head.Hovered() {
		col = k.Fg
	}
	if head.Clicked() {
		a.focusGroup(g)
	}
	head.Cursor(ui.CursorPointer).Children(func() {
		display(c, groupTitle(g), 18)
		ui.Icon(c, icForward).FontSize(15).TextColor(col)
	})
}

// groupMore is the row under a group alone while its next page loads or
// after it failed; it keeps one height whether it loads, failed or waits.
func (a *App) groupMore(c *ui.Context, k *Tokens, g string) {
	s := a.Store
	q := &s.Search
	ui.Row(c.Key("more")).Height(groupRow).Gap(10).Justify(ui.Center).AlignItems(ui.Center).Transition(a.titleMotion(c)).Children(func() {
		switch q.More[g] {
		case store.PageLoading:
			ui.Text(c, "Loading more…").FontSize(12).TextColor(k.Muted)
		case store.PageFailed:
			ui.Text(c, "Could not load more.").FontSize(12).TextColor(k.Bad)
			if button(c, k, buttonGhost, "Try again", nil).Clicked() {
				s.MoreSearch(g)
			}
		}
	})
	if q.More[g] == store.PageIdle && !q.Stale {
		if st := a.scrolls["home-body"]; st == nil || st.MaxY-st.Y < loadAhead {
			s.MoreSearch(g)
		}
	}
}

// groupGrid lays a group's hits out: songs as cards, the rest as tiles,
// whole when they are all the group has (tileGrid). It moves as its group
// does when the title over it goes.
func (a *App) groupGrid(c *ui.Context, k *Tokens, g string, hits []client.SearchHit, whole bool, sel *client.SearchHit, width float32) {
	ui.Column(c.Key("grid")).Transition(a.groupMotion(c, g, false)).Children(func() {
		if g == "songs" {
			a.songGrid(c, k, hits, sel, width)
			return
		}
		a.tileGrid(c, k, g, hits, whole, sel, width)
	})
}

// focusGroup shows group alone from its top, or the overview for "".
func (a *App) focusGroup(group string) {
	if a.Store.Search.Focus == group {
		return
	}
	a.groupsFrom = a.Store.Search.Focus
	a.Store.FocusGroup(group)
	a.searchSel = ""
	a.groupsMoved = true
	if st := a.scrolls["home-body"]; st != nil {
		// Into a tab from near the top, the page glides there as the group
		// does. Out to the overview, or from further down, it starts at
		// its top and only the groups move: a glide up while the group
		// moves down would carry it and the tabs in from above the window.
		if a.PageMotion && group != "" && st.Y <= groupGlideMax {
			st.ScrollTo(0, 0, groupEase)
		} else {
			st.Y = 0
		}
	}
}

// groupGlideMax is how far down, in DIPs, the search glides back to its
// top as a tab opens; from further it jumps there.
const groupGlideMax = 400

// groupTabs are Overview and the four groups as pills, the one shown
// filled, and the search's region at the right (searchRegion).
func (a *App) groupTabs(c *ui.Context, k *Tokens) {
	q := &a.Store.Search
	ui.Row(c.Key("tabs-row")).FillWidth().Height(groupRow).Gap(8).AlignItems(ui.Center).Children(func() {
		a.groupPills(c, k, q)
		ui.Spacer(c)
		a.searchRegion(c, k)
	})
}

// regionRow is the region alone at the right of where the tabs go, for a
// search with no results to show: another region may have them.
func (a *App) regionRow(c *ui.Context, k *Tokens) {
	if len(a.Store.SearchRegions()) < 2 {
		return
	}
	ui.Row(c.Key("tabs-row")).FillWidth().Height(groupRow).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		a.searchRegion(c, k)
	})
}

// groupPills are the tabs of groupTabs.
func (a *App) groupPills(c *ui.Context, k *Tokens, q *store.Search) {
	ui.Row(c.Key("tabs")).Height(groupRow).Gap(8).AlignItems(ui.Center).Children(func() {
		for _, g := range append([]string{""}, store.SearchGroups...) {
			on := g == q.Focus
			name := "Overview"
			if g != "" {
				name = groupTitle(g)
			}
			t := ui.ButtonBase(c.Key("tab-" + g)).Height(groupRow).PaddingX(14).Radius(radiusFull).AlignItems(ui.Center).Label(name)
			bg, fg := k.Wash, k.Fg
			switch {
			case on:
				bg, fg = k.Pill, k.PillInk
			case t.Hovered():
				bg = k.Track
			}
			t.Background(bg)
			if t.Clicked() {
				a.focusGroup(g)
			}
			t.Cursor(ui.CursorPointer).Children(func() {
				ui.Text(c, name).FontSize(13.5).FontWeight(600).TextColor(fg).SingleLine()
			})
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
			a.searchSel, a.searchReveal, a.searchKeyed = store.HitKey(ar.Artist), true, false
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
		searchMessage(c, k, icLibrary, "No albums", "")
		return
	}
	ui.Column(c).Gap(14).Children(func() {
		a.tileGrid(c, k, "album", ar.Albums.Items, ar.Albums.NextOffset == 0, sel, width)
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
	a.searchReveal, a.searchKeyed = true, true
}

// searchKeys are the search's window keys, for when the keyboard is out of
// the field (fieldKey takes them in it): the arrows, Home and End, Return,
// and Escape, which leaves an artist, then a group shown alone, then
// forgets the chosen hit, then clears the field.
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
			a.searchReveal, a.searchKeyed = true, true
			s.OpenArtist("")
		case s.Search.Focus != "":
			a.focusGroup("")
		case a.searchSel != "":
			a.searchSel = ""
		default:
			a.clearField()
		}
	})
}

// actOnHit is Return: the first press picks the first hit, then an album,
// a playlist or an artist opens, as a click does, and a song downloads.
func (a *App) actOnHit() {
	s := a.Store
	hits := s.Search.ShownHits()
	h := a.chosenHit(hits)
	if h == nil {
		if len(hits) > 0 {
			a.searchSel, a.searchReveal, a.searchKeyed = store.HitKey(hits[0]), true, true
		}
		return
	}
	switch h.Type {
	case "artist":
		a.openArtist(*h)
		return
	case "album", "playlist":
		a.openingFrom("hit:" + store.HitKey(*h))
		a.OpenAlbum(*h)
		return
	}
	if m, _ := s.HitMark(*h); m != store.MarkActive {
		a.download(*h)
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

// downloadAll starts every album of the artist, after a second click: it
// can be hundreds of tracks.
func (a *App) downloadAll(c *ui.Context, k *Tokens, ar *store.ArtistPages) {
	s := a.Store
	if m, _ := s.HitMark(ar.Artist); m == store.MarkActive {
		pill(c, k, pillRun, icDown, "In the queue")
		return
	}
	// A start that answers within a frame keeps the button as it was.
	since, starting := s.Search.Starting[store.HitKey(ar.Artist)]
	if waited(c, starting, since) {
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
			// The button stays armed until the answer replaces it.
			a.download(ar.Artist)
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
		if help != "" {
			ui.Text(c, help).FontSize(12.5).TextColor(k.Muted).MaxWidth(420).TextAlign(ui.Center)
		}
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
