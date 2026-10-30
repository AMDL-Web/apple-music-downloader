package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/client"
	"amdl/internal/format"
)

// searchRowHeight is a hit row's height; loading rows take the same, so the
// list keeps its place when the answer lands.
const searchRowHeight = 62

// confirmFor is how long "Download all" waits for its second click.
const confirmFor = 4 * time.Second

// searchText is what the field searches for, and whether it is too long to.
func (a *App) searchText() (term string, tooLong bool) {
	o := &a.Store.Omni
	return store.SearchTerm(o.Text, len(o.Links) > 0)
}

// searchScreen is the new download screen for words: the catalog's songs,
// albums and artists, or one artist's albums, the chosen hit large beside
// them and Mole's bar to download it. The keyboard stays in the title bar's
// field: the arrows move through the rows and Return acts on the chosen one.
func (a *App) searchScreen(c *ui.Context, k *Tokens, compact bool, term string, tooLong bool) {
	s := a.Store
	s.WantSearch(term)
	q := &s.Search
	hits := q.ShownHits()
	sel := a.chosenHit(hits)
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Padding(24, 28).Gap(14).Children(func() {
			if compact {
				a.compactSearchField(c, k)
			}
			a.searchHeader(c, k, tooLong)
			list := ui.Scroll(c.Key("search-rows")).Grow(1)
			if q.Stale {
				list.Opacity(0.45)
			}
			list.Children(func() {
				switch {
				case tooLong:
					searchMessage(c, k, icSearch, "Too long to search", fmt.Sprintf("Search looks at the first %d characters; shorten the words.", store.SearchMaxTerm))
				case q.Artist.ID != "":
					a.artistAlbums(c, k, sel)
				default:
					a.searchResults(c, k, sel)
				}
			})
			a.qualityRow(c, k)
			a.hitBar(c, k, sel)
		})
		if !compact {
			a.hitPreview(c, k, sel, hits)
		}
	})
}

// compactSearchField is the field the narrow window's title bar folds into
// a button, here where its words show.
func (a *App) compactSearchField(c *ui.Context, k *Tokens) {
	s := a.Store
	box := ui.Row(c).Height(30).PaddingX(10).Gap(8).Radius(9).AlignItems(ui.Center).Background(k.Raise2).Border(1, k.Line)
	box.Children(func() {
		ui.Icon(c, icSearch).FontSize(14).TextColor(k.Faint)
		a.omniTxt = s.Omni.Text
		in := ui.TextInputBase(c.Key("omni-compact"), &a.omniTxt).Grow(1).Bind(&a.omni).FontSize(12.5).Label("Search Apple Music")
		if in.Focused() {
			box.Border(1, k.Accent)
		}
		if in.Changed() {
			s.SetOmniText(a.omniTxt)
		}
		if in.Submitted() {
			a.actOnHit()
		}
	})
}

// chosenHit is the hit the preview and the bar act on: the one picked with
// the arrows or a click, while it is still shown.
func (a *App) chosenHit(hits []client.SearchHit) *client.SearchHit {
	for i := range hits {
		if store.HitKey(hits[i]) == a.searchSel {
			return &hits[i]
		}
	}
	return nil
}

// searchKeys are the search's keys. They are window shortcuts, built from
// the view's root (shortcuts), so they work while the keyboard is in the
// title bar's field: a shortcut declared deeper only hears its own subtree.
func (a *App) searchKeys(c *ui.Context, hits []client.SearchHit, sel *client.SearchHit) {
	s := a.Store
	move := func(to func(i, n int) int) func() {
		return func() {
			if len(hits) == 0 {
				return
			}
			i := -1
			for j := range hits {
				if store.HitKey(hits[j]) == a.searchSel {
					i = j
				}
			}
			a.searchSel = store.HitKey(hits[min(max(to(i, len(hits)), 0), len(hits)-1)])
			a.searchReveal = true
		}
	}
	c.OnShortcut(0, ui.KeyDown, move(func(i, n int) int { return i + 1 }))
	c.OnShortcut(0, ui.KeyUp, move(func(i, n int) int {
		if i < 0 {
			return n - 1
		}
		return i - 1
	}))
	c.OnShortcut(0, ui.KeyHome, move(func(int, int) int { return 0 }))
	c.OnShortcut(0, ui.KeyEnd, move(func(_, n int) int { return n - 1 }))
	// Return in the field arrives as its submit (omniField); this one is for
	// a click that took the keyboard out of it.
	c.OnShortcut(0, ui.KeyEnter, a.actOnHit)
	c.OnShortcut(0, ui.KeyEscape, func() {
		switch {
		case s.Search.Artist.ID != "":
			a.searchSel = store.HitKey(s.Search.Artist.Artist)
			a.searchReveal = true
			s.OpenArtist("")
		case sel != nil:
			a.searchSel = ""
		}
	})
}

// actOnHit is Return: the first press picks the first row, then a song or
// an album downloads and an artist opens.
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
	if h.Type == "artist" {
		a.openArtist(*h)
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

// searchHeader says what the list shows: what was found, or the artist
// with Back and Download all. It keeps one height in every state.
func (a *App) searchHeader(c *ui.Context, k *Tokens, tooLong bool) {
	s := a.Store
	q := &s.Search
	ui.Row(c).Height(30).Gap(10).AlignItems(ui.Center).Children(func() {
		if ar := &q.Artist; ar.ID != "" {
			if button(c, k, buttonGhost, "Results", icBack).Label("Back to the results").Clicked() {
				a.searchSel, a.searchReveal = store.HitKey(ar.Artist), true
				s.OpenArtist("")
				return
			}
			name := ar.Artist.Name
			if name == "" {
				name = "Artist"
			}
			ui.Text(c, name).FontSize(15).FontWeight(650).SingleLine().Shrink(1).Ellipsis("…")
			if ar.Status == store.SearchReady {
				mono(c, albumCount(ar), 11).TextColor(k.Faint).SingleLine()
			}
			ui.Spacer(c)
			if ar.Status == store.SearchReady {
				a.downloadAll(c, k, ar)
			}
			return
		}
		label(c, k, "Apple Music")
		if !tooLong {
			mono(c, searchCountLine(q), 11).TextColor(k.Faint).SingleLine().Shrink(1).Ellipsis("…")
		}
	})
}

// albumCount is the albums read of an artist, "+" while more are left.
func albumCount(ar *store.ArtistPages) string {
	n := len(ar.Albums.Items)
	if ar.Albums.NextOffset > 0 {
		return fmt.Sprintf("%d+ albums", n)
	}
	return plural(n, "album")
}

// searchCountLine sums up an answer: "5 songs · 5 albums · 1 artist".
func searchCountLine(q *store.Search) string {
	switch {
	case q.Status == store.SearchLoading:
		return "Searching…"
	case q.Status != store.SearchReady || q.Result == nil:
		return ""
	}
	var parts []string
	for _, g := range store.SearchGroups {
		hg := hitGroupOf(q.Result, g)
		if hg == nil || len(hg.Items) == 0 {
			continue
		}
		n := fmt.Sprint(len(hg.Items))
		if hg.NextOffset > 0 {
			n += "+"
		}
		word := strings.TrimSuffix(g, "s")
		if n != "1" {
			word = g
		}
		parts = append(parts, n+" "+word)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ") + " for “" + q.Term + "”"
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
	if button(c, k, kind, text, icDown).Label("Download every album of the artist").Clicked() {
		if armed {
			a.confirmAll = ""
			s.SubmitHit(ar.Artist)
		} else {
			a.confirmAll, a.confirmAt = ar.ID, c.Now()
		}
	}
}

// searchResults are the groups of hits, or the state that stands for them.
func (a *App) searchResults(c *ui.Context, k *Tokens, sel *client.SearchHit) {
	s := a.Store
	q := &s.Search
	switch {
	case q.Status == store.SearchFailed:
		searchFailed(c, k, q.Err, s.RetrySearch)
		return
	case q.Result == nil:
		skeletonGroups(c, k)
		return
	case len(q.ShownHits()) == 0:
		searchMessage(c, k, icSearch, "Nothing found for “"+q.Term+"”", "Try fewer words, or the name as Apple Music writes it.")
		return
	}
	ui.Column(c).Gap(18).Children(func() {
		for _, g := range store.SearchGroups {
			hg := hitGroupOf(q.Result, g)
			if hg == nil || len(hg.Items) == 0 {
				continue
			}
			ui.Column(c.Key(g)).Gap(8).Children(func() {
				label(c, k, strings.ToUpper(g[:1])+g[1:])
				for i, h := range hg.Items {
					a.hitRow(c, k, h, fmt.Sprint(g, i), sel)
				}
				morePage(c, k, g, hg.NextOffset > 0, q.More[g], func() { s.MoreSearch(g) })
			})
		}
	})
}

// artistAlbums is the open artist's albums, or the state that stands for
// them.
func (a *App) artistAlbums(c *ui.Context, k *Tokens, sel *client.SearchHit) {
	s := a.Store
	ar := &s.Search.Artist
	switch ar.Status {
	case store.SearchFailed:
		searchFailed(c, k, ar.Err, s.RetryArtist)
		return
	case store.SearchLoading:
		ui.Column(c).Gap(8).Children(func() { skeletonRows(c, k, 6) })
		return
	}
	if len(ar.Albums.Items) == 0 {
		searchMessage(c, k, icLibrary, "No albums", "Apple Music lists no albums for this artist here.")
		return
	}
	ui.Column(c).Gap(8).Children(func() {
		for i, h := range ar.Albums.Items {
			a.hitRow(c, k, h, fmt.Sprint("album", i), sel)
		}
		morePage(c, k, "artist", ar.Albums.NextOffset > 0, ar.More, s.MoreArtist)
	})
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

// hitRow is one hit: its cover at the size drawn, its title and facts, the
// library or queue mark, and its action.
func (a *App) hitRow(c *ui.Context, k *Tokens, h client.SearchHit, key string, sel *client.SearchHit) {
	s := a.Store
	hk := store.HitKey(h)
	on := sel != nil && store.HitKey(*sel) == hk
	row := ui.ButtonBase(c.Key(key)).Height(searchRowHeight).PaddingX(12).Gap(12).Radius(10).AlignItems(ui.Center).Justify(ui.Start).
		Background(k.Raise).Border(1, k.Line).Label(h.Name)
	if on {
		row.Border(1, k.Accent).Shadow(0, 0, 0, 3, k.AccentSoft)
		if a.searchReveal {
			row.ScrollIntoView()
			a.searchReveal = false
		}
	}
	if row.Clicked() {
		a.searchSel = hk
	}
	row.Cursor(ui.CursorPointer).Children(func() {
		pal := hitPalette(h)
		radius := float32(6)
		if h.Type == "artist" {
			radius = 22
		}
		widgets.Cover{Image: a.Art.Get(h.ArtworkURL, 88), Placeholder: pal.Bg, Size: 44, Radius: radius, Color: 1}.Show(c)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, h.Name).FontWeight(600).Ellipsis("…").SingleLine().Shrink(1)
				if h.ContentRating == "explicit" {
					explicitBadge(c, k)
				}
			})
			mono(c, strings.Join(hitFacts(h), " · "), 11).TextColor(k.Faint).Ellipsis("…").SingleLine()
		})
		for _, q := range qualityHints(h.AudioTraits) {
			tag(c, k, q, q == "Hi-Res Lossless", k.Muted)
		}
		mark, job := s.HitMark(h)
		switch mark {
		case store.MarkDone:
			if markButton(c, k, "In library", pillOK, icCheck).Clicked() {
				a.OpenJob(job)
			}
		case store.MarkActive:
			if markButton(c, k, "In the queue", pillRun, icDown).Clicked() {
				a.OpenJob(job)
			}
		}
		switch {
		case h.Type == "artist":
			if button(c, k, buttonPlain, "Albums", nil).Label("Show the artist's albums").Clicked() {
				a.openArtist(h)
			}
		case s.Search.Starting[hk]:
			pill(c, k, pillWait, nil, "Starting…")
		case mark != store.MarkActive:
			if button(c, k, buttonPlain, "", icDown).Label("Download " + h.Name).Clicked() {
				a.searchSel = hk
				s.SubmitHit(h)
			}
		}
	})
}

// markButton is a hit's library or queue mark, which opens its job.
func markButton(c *ui.Context, k *Tokens, text string, kind pillKind, ic *ui.SVG) ui.Element {
	b := ui.ButtonBase(c.Key("mark")).Radius(999).Label(text + ": open the download")
	b.Cursor(ui.CursorPointer).Children(func() { pill(c, k, kind, ic, text) })
	return b
}

func explicitBadge(c *ui.Context, k *Tokens) {
	ui.Box(c).Size(15, 15).Radius(3).Background(k.Faint.Alpha(0.35)).Center().Children(func() {
		ui.Text(c, "E").FontSize(9.5).FontWeight(700).TextColor(k.Fg)
	})
}

// hitFacts is a hit's line of facts: a song's artist, album and length; an
// album's artist, year and track count; an artist's genre.
func hitFacts(h client.SearchHit) []string {
	var out []string
	add := func(s string) {
		if s != "" {
			out = append(out, s)
		}
	}
	switch h.Type {
	case "song":
		add(h.ArtistName)
		add(h.AlbumName)
		if h.DurationMS > 0 {
			add(format.Duration(time.Duration(h.DurationMS) * time.Millisecond))
		}
	case "album":
		add(h.ArtistName)
		if len(h.ReleaseDate) >= 4 {
			add(h.ReleaseDate[:4])
		}
		if h.TrackCount > 0 {
			add(plural(h.TrackCount, "track"))
		}
	default:
		add(firstNonEmptyStr(h.Genre, "Artist"))
	}
	return out
}

// qualityHints names what a hit's audio traits promise, best lossless
// first, then Atmos, as the web console does.
func qualityHints(traits []string) []string {
	has := map[string]bool{}
	for _, t := range traits {
		has[t] = true
	}
	var out []string
	switch {
	case has["hi-res-lossless"]:
		out = append(out, "Hi-Res Lossless")
	case has["lossless"]:
		out = append(out, "Lossless")
	}
	if has["atmos"] {
		out = append(out, "Dolby Atmos")
	}
	return out
}

func hitPalette(h client.SearchHit) Palette {
	return ParsePalette(h.ArtworkBgColor, h.ArtworkTextColor1, h.ArtworkTextColor2, h.ArtworkTextColor3, h.ArtworkTextColor4)
}

// hitBar is Mole's bar for the chosen hit: what it is on the left, the one
// button on the right.
func (a *App) hitBar(c *ui.Context, k *Tokens, sel *client.SearchHit) {
	s := a.Store
	ui.Row(c).Padding(12, 14, 12, 16).Gap(14).Radius(16).Background(k.Raise2).AlignItems(ui.Center).Children(func() {
		if sel == nil {
			ui.Column(c).Shrink(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, "Nothing chosen").FontSize(13).FontWeight(650).SingleLine()
				mono(c, "↑ ↓ to choose · Return to download", 11).TextColor(k.Muted)
			})
			ui.Spacer(c)
			button(c, k, buttonPill, "Download", nil).Disabled(true).Opacity(0.45)
			return
		}
		h := *sel
		pal := hitPalette(h)
		radius := float32(6)
		if h.Type == "artist" {
			radius = 15
		}
		widgets.Cover{Image: a.Art.Get(h.ArtworkURL, 64), Placeholder: pal.Bg, Size: 30, Radius: radius, Color: 1}.Show(c)
		mark, _ := s.HitMark(h)
		ui.Column(c).Shrink(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, h.Name).FontSize(13).FontWeight(650).SingleLine().Ellipsis("…")
			note := strings.ToUpper(h.Type[:1]) + h.Type[1:]
			switch mark {
			case store.MarkDone:
				note += " · in your library"
			case store.MarkActive:
				note += " · in the queue"
			}
			mono(c, note, 11).TextColor(k.Muted)
		})
		ui.Spacer(c)
		text, ready := "Download "+h.Type, mark != store.MarkActive
		switch {
		case h.Type == "artist":
			text, ready = "Show albums", true
		case s.Search.Starting[store.HitKey(h)]:
			text, ready = "Starting…", false
		case mark == store.MarkActive:
			text = "In the queue"
		}
		b := button(c, k, buttonPill, text, nil).Disabled(!ready)
		if !ready {
			b.Opacity(0.45)
		}
		if b.Clicked() {
			a.actOnHit()
		}
	})
}

// hitPreview shows the chosen hit large on its own colors; before one is
// chosen, the top result.
func (a *App) hitPreview(c *ui.Context, k *Tokens, sel *client.SearchHit, hits []client.SearchHit) {
	top := sel == nil
	if top && len(hits) > 0 {
		sel = &hits[0]
	}
	if sel == nil {
		a.previewPane(c, k, nil, icSearch, "Choose a result to see it here.")
		return
	}
	h := *sel
	p := &preview{
		Kind: h.Type, Title: h.Name, By: h.ArtistName, ReleaseDate: h.ReleaseDate, Genre: h.Genre,
		ArtworkURL: h.ArtworkURL, Pal: hitPalette(h), Round: h.Type == "artist",
	}
	if h.TrackCount > 0 {
		p.Tags = append(p.Tags, plural(h.TrackCount, "track"))
	}
	if h.DurationMS > 0 {
		p.Tags = append(p.Tags, format.Duration(time.Duration(h.DurationMS)*time.Millisecond))
	}
	if h.ContentRating == "explicit" {
		p.Tags = append(p.Tags, "Explicit")
	}
	p.Hi = qualityHints(h.AudioTraits)
	if h.AppleDigitalMaster {
		p.Tags = append(p.Tags, "Apple Digital Master")
	}
	if h.Type == "song" && h.AlbumName != "" {
		p.Note = "From " + h.AlbumName
	}
	switch m, _ := a.Store.HitMark(h); m {
	case store.MarkDone:
		p.Note = "Already in your library."
	case store.MarkActive:
		p.Note = "Already in the queue."
	}
	if top {
		p.Note = "Top result · ↓ to choose it"
	}
	a.previewPane(c, k, p, nil, "")
}

// searchMessage stands for the list when it has nothing to show.
func searchMessage(c *ui.Context, k *Tokens, ic *ui.SVG, title, help string) {
	ui.Column(c).Height(260).Center().Gap(8).Children(func() {
		ui.Icon(c, ic).FontSize(26).TextColor(k.Faint)
		ui.Text(c, title).FontSize(15).FontWeight(650).TextAlign(ui.Center)
		ui.Text(c, help).FontSize(12.5).TextColor(k.Muted).MaxWidth(420).TextAlign(ui.Center)
	})
}

// searchFailed stands for the list when its read failed, with a retry.
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

// skeletonGroups stand for the first answer of a term while it loads.
func skeletonGroups(c *ui.Context, k *Tokens) {
	ui.Column(c).Gap(18).Children(func() {
		for i, n := range []int{store.SearchFirst, store.SearchFirst, 2} {
			ui.Column(c.Key(i)).Gap(8).Children(func() {
				ui.Box(c).Width(64).Height(10).Radius(3).Background(k.Track)
				skeletonRows(c, k, n)
			})
		}
	})
}

func skeletonRows(c *ui.Context, k *Tokens, n int) {
	for i := range n {
		ui.Row(c.Key(i)).Height(searchRowHeight).PaddingX(12).Gap(12).Radius(10).AlignItems(ui.Center).Background(k.Raise).Border(1, k.Line).Children(func() {
			ui.Box(c).Size(44, 44).Radius(6).Background(k.Track)
			ui.Column(c).Grow(1).Gap(7).Children(func() {
				ui.Box(c).Width(float32(160 + (i*37)%90)).Height(11).Radius(3).Background(k.Track)
				ui.Box(c).Width(float32(220 + (i*53)%120)).Height(9).Radius(3).Background(k.Track.Alpha(0.6))
			})
		})
	}
}
