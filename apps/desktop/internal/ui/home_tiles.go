package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/format"
)

// The result grids, in DIPs.
const (
	// tileGap is the room between tiles, and tileMin the narrowest tile
	// before a row holds one fewer; a group that tileSnug more fits on one
	// row takes its tiles down to that width, so no tile sits alone below.
	tileGap  = 16
	tileMin  = 160
	tileSnug = 120
	// songMin is the narrowest song card before a row holds one fewer.
	songMin = 300
	// songHeight is a song card's height around its padTile-inset cover.
	songHeight = 64
)

// tileColumns is how many tiles a row width wide holds, at least three.
func tileColumns(width float32) int { return tileFit(width, tileMin) }

func tileFit(width, min float32) int { return max(3, int((width+tileGap)/(min+tileGap))) }

// tileSize is the width of count tiles' tile when a row of them fills
// width; count 0 is a row of any number.
func tileSize(width float32, count int) float32 {
	n := tileColumns(width)
	if count > n && count <= tileFit(width, tileSnug) {
		n = count
	}
	return (width - float32(n-1)*tileGap) / float32(n)
}

// songGap is the room between song cards.
const songGap = 12

// songColumns is how many song cards a row width wide holds.
func songColumns(width float32) int { return max(1, int((width+songGap)/(songMin+songGap))) }

// tileGrid lays hits out as tiles of their covers, rows filling width.
// Only hits that are whole, all there are, take tiles down to fit one row
// (tileSize): a first page that would fit one row grows to the columns'
// size when the next arrives, as its tiles move to their places.
func (a *App) tileGrid(c *ui.Context, k *Tokens, key string, hits []client.SearchHit, whole bool, sel *client.SearchHit, width float32) {
	count := 0
	if whole {
		count = len(hits)
	}
	size := tileSize(width, count)
	ui.Row(c).Width(width).Wrap().GapX(tileGap).GapY(24).Children(func() {
		for i, h := range hits {
			a.hitTile(c, k, h, a.tileKey(key, i), sel, size)
		}
	})
}

// hitTile is an album's, a playlist's or an artist's tile: the cover large,
// its title and one line of facts under it. An album's cover opens its page
// and a playlist's downloads it, or opens the download a mark names; both
// carry the best quality and one control, which downloads it or says it is
// in the queue or the library (tileControl). An artist's opens its albums.
func (a *App) hitTile(c *ui.Context, k *Tokens, h client.SearchHit, key string, sel *client.SearchHit, size float32) {
	hk, from := a.hitKey(h), a.hitFrom(h)
	on := sel != nil && a.hitKey(*sel) == hk
	round := h.Type == "artist"
	radius := float32(radiusCover)
	if round {
		radius = radiusFull
	}
	ui.Column(c.Key(key)).Width(size).Gap(10).Transition(a.hitMotion(c)).Children(func() {
		art := ui.Box(c.Key("art")).Size(size, size)
		hover := art.Hovered()
		art.Children(func() {
			open := tileOpen(c, size, radius, h.Name)
			if on {
				if a.searchKeyed {
					focusRing(open, radius)
				}
				if a.searchReveal && !a.viewSwapping(c) {
					open.ScrollIntoView()
					a.searchReveal = false
				}
			}
			if open.Clicked() {
				switch h.Type {
				case "artist":
					a.openArtist(h)
				case "album":
					a.openingFrom("hit:" + hk)
					a.OpenAlbum(h)
				default:
					a.openingFrom("hit:" + hk)
					a.openOrDownload(h)
				}
			}
			// The cover flies, not the button: its shadow stays with the
			// screen, which fades it out and back in as the page comes and
			// goes, so the tile has it when the cover lands (flyCover).
			open.Cursor(ui.CursorPointer).Children(func() {
				cover := widgets.Cover{Image: a.Art.Get(h.ArtworkURL, int(2*size)), Placeholder: hitPalette(h).Bg, Size: size, Radius: radius, Color: 1}.Show(c)
				a.flySource(cover, from, partCover, "", radius)
			})
			if round {
				return
			}
			if hints := a.tileHints(h, size); hints != "" {
				hint := qualityHint(c, k, hints).Absolute().Left(8).Bottom(8).PassThrough()
				if a.fliesFrom(from) {
					a.flySourceBadge(hint, from, partHint, func() ui.Element { return qualityHint(c, k, hints) })
				}
			}
			a.tileControl(c, k, h, hover || on)
		})
		texts := ui.Column(c).Gap(3)
		if round {
			texts.AlignItems(ui.Center)
		}
		texts.Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).MaxWidth(size).Children(func() {
				tileTitle(c, hitName(h), 1).Shrink(1)
				if h.ContentRating == "explicit" {
					explicitBadge(c, k)
				}
			})
			tileSub(c, k, a.tileLine(h), size)
		})
	})
}

// tileOpen is a tile's cover, a button size square rounded at radius,
// labeled label, which drops the tile's shadow; its children are the cover.
func tileOpen(c *ui.Context, size, radius float32, label string) ui.Element {
	return ui.ButtonBase(c.Key("open")).Size(size, size).Radius(radius).Label(label).
		Shadow(0, 12, 28, -14, ui.RGBA(0, 0, 0, 0.5))
}

// tileTitle is a tile's title under its cover, on up to lines lines.
func tileTitle(c *ui.Context, name string, lines int) ui.Element {
	t := ui.Text(c, name).FontSize(13).FontWeight(600).Ellipsis("…")
	if lines <= 1 {
		return t.SingleLine()
	}
	return t.MaxLines(lines)
}

// tileSub is a tile's line of facts under its title, when it has one.
func tileSub(c *ui.Context, k *Tokens, sub string, size float32) {
	if sub != "" {
		ui.Text(c, sub).FontSize(12).TextColor(k.Muted).SingleLine().Ellipsis("…").MaxWidth(size)
	}
}

// qualityHint is a tile's glass pill of its best quality.
func qualityHint(c *ui.Context, k *Tokens, s string) ui.Element {
	return ui.Row(c).Height(22).PaddingX(8).Radius(radiusFull).AlignItems(ui.Center).Material(glass.Glass{}).
		Children(func() { hintText(c, s, k.Fg) })
}

// hintText is what a quality hint says, in ink.
func hintText(c *ui.Context, s string, ink ui.Color) ui.Element {
	return ui.Text(c, s).FontSize(11).FontWeight(650).TextColor(ink).SingleLine()
}

// tileControlSize is the round control in an album tile's corner.
const tileControlSize = 32

// tileControl is an album tile's one control, in its bottom corner: a
// download button that shows while the tile is pointed at or chosen; the
// queue's mark while a download of the album runs, which opens its page;
// the library's check, which turns into a download button while pointed at,
// to fetch the tracks missing from disk (those on disk are skipped).
func (a *App) tileControl(c *ui.Context, k *Tokens, h client.SearchHit, shown bool) {
	s := a.Store
	hk, from := a.hitKey(h), a.hitFrom(h)
	mark, _ := s.HitMark(h)
	startedAt, starting := s.Search.Starting[hk]
	// The glyph changes only when the start is slow, as a pill's does.
	wait := waited(c, starting, startedAt)
	b := tileCorner(c)
	pointed := b.Hovered()
	look := markLook{ic: icDown, fg: k.Fg, glass: true}
	switch {
	case wait:
		b.Label("Starting the download of " + h.Name)
		look.ic = nil
	case mark == store.MarkActive:
		b.Label(h.Name + " is in the queue").Tooltip("In the queue")
		look = markLook{bg: k.Accent, fg: k.AccentInk, ic: icDown}
	case mark == store.MarkPaused:
		b.Label(h.Name + " is paused: resume").Tooltip("Paused")
		look = markLook{bg: k.Raise2, fg: k.Fg, ic: icPause, interactive: true}
	case mark == store.MarkPartial:
		r := s.HitRecord(h)
		b.Label(fmt.Sprintf("%s: %d of %d tracks downloaded, download the rest", h.Name, r.TracksDone, r.TracksWanted))
		if pointed {
			b.Tooltip(fmt.Sprintf("%d of %d downloaded", r.TracksDone, r.TracksWanted))
		}
		look.interactive = true
	case mark == store.MarkDone:
		// The check gives way to the download again button under the
		// pointer, crossing over (doneCheck) rather than at once.
		b.Label(h.Name + " is in your library: download again")
		if pointed {
			b.Tooltip("Download again")
		}
		look.interactive = true
		look.check = b.AnimateWith("check", map[bool]float32{true: 0, false: 1}[pointed], 120*time.Millisecond, ui.EaseOut)
		look.ok = k.OK
	default:
		b.Label("Download " + h.Name)
		look.interactive = true
	}
	// Only the download button waits for the pointer; a mark, and the
	// button of an album partly downloaded, stays.
	look.show(b, mark != store.MarkNone || shown || starting, mark == store.MarkNone)
	if a.fliesFrom(from) && look.op >= 0.01 {
		a.flySourceBadge(b, from, partMark, func() ui.Element { return look.copy(c) })
	}
	if b.Clicked() && !starting {
		switch {
		case mark == store.MarkActive && h.Type == "album":
			a.openingFrom("hit:" + hk)
			a.OpenAlbum(h)
		case mark == store.MarkActive:
			a.openOrDownload(h)
		default:
			a.download(h)
		}
	}
	look.fill(c, b.Cursor(ui.CursorPointer))
}

// tileCorner is the round control in a tile's bottom corner, over its
// cover.
func tileCorner(c *ui.Context) ui.Element {
	return ui.ButtonBase(c.Key("control")).Size(tileControlSize, tileControlSize).Radius(radiusFull).Center().
		Absolute().Right(8).Bottom(8)
}

// markLook is how a tile's control looks: on glass, interactive while it
// is a button, or on bg with a shadow; its glyph, ic or a wait's ellipsis,
// in fg; and how opaque it is.
type markLook struct {
	bg                 ui.Color
	glass, interactive bool
	ic                 *ui.SVG
	fg                 ui.Color
	op                 float32
	// check is how much an album in the library shows its check over the
	// button, on ok.
	check float32
	ok    ui.Color
}

// show styles b as m looks and sets how opaque it is: shown, at once, or
// easing to shown or gone where it waits for the pointer (wait).
func (m *markLook) show(b ui.Element, shown, wait bool) {
	m.style(b)
	m.op = 1
	if wait {
		m.op = b.AnimateWith("shown", map[bool]float32{true: 1, false: 0}[shown], 120*time.Millisecond, ui.EaseOut)
	}
	b.Opacity(m.op).Hidden(m.op < 0.01)
}

// fill draws m's glyph and check in b.
func (m markLook) fill(c *ui.Context, b ui.Element) {
	b.Children(func() {
		m.glyph(c)
		m.doneCheck(c)
	})
}

func (m markLook) style(el ui.Element) {
	if m.glass {
		el.Material(glass.Glass{Interactive: m.interactive})
		return
	}
	el.Background(m.bg).Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.3))
}

func (m markLook) glyph(c *ui.Context) {
	if m.ic == nil {
		ui.Text(c, "…").FontSize(13).FontWeight(700).TextColor(m.fg)
		return
	}
	ui.Icon(c, m.ic).FontSize(15).TextColor(m.fg)
}

// doneCheck is the check of an album in the library, as much as it shows,
// over the control.
func (m markLook) doneCheck(c *ui.Context) {
	if m.check <= 0.001 {
		return
	}
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(radiusFull).Center().Background(m.ok).
		Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.3)).Opacity(m.check).
		Children(func() { ui.Icon(c, icCheck).FontSize(15).TextColor(ui.RGB(255, 255, 255)) })
}

// copy builds the control again as it looks, for the flight to carry.
func (m markLook) copy(c *ui.Context) ui.Element {
	el := ui.Box(c).Size(tileControlSize, tileControlSize).Radius(radiusFull).Center().Opacity(m.op)
	m.style(el)
	el.Children(func() {
		m.glyph(c)
		m.doneCheck(c)
	})
	return el
}

// songGrid lays songs out as cards, rows filling width.
func (a *App) songGrid(c *ui.Context, k *Tokens, hits []client.SearchHit, sel *client.SearchHit, width float32) {
	cols := songColumns(width)
	cw := (width - songGap*float32(cols-1)) / float32(cols)
	ui.Row(c).Width(width).Wrap().Gap(songGap).Children(func() {
		for i, h := range hits {
			a.songCard(c, k, h, a.tileKey("songs", i), sel, cw)
		}
	})
}

// songCard is a song: its cover, title, artist and album, and its mark or
// download button.
func (a *App) songCard(c *ui.Context, k *Tokens, h client.SearchHit, key string, sel *client.SearchHit, width float32) {
	s := a.Store
	hk := a.hitKey(h)
	on := sel != nil && a.hitKey(*sel) == hk
	card := ui.Row(c.Key(key)).Width(width).Height(songHeight).Padding(padTile).PaddingEnd(10).Gap(12).Radius(radiusTile).
		AlignItems(ui.Center).Background(k.Raise).Border(1, k.Line).Transition(a.hitMotion(c))
	if card.Hovered() {
		card.Background(k.Raise2)
	}
	if on {
		if a.searchKeyed {
			focusRing(card, radiusTile)
		}
		if a.searchReveal {
			card.ScrollIntoView()
			a.searchReveal = false
		}
	}
	card.Children(func() {
		cover := widgets.Cover{Image: a.Art.Get(h.ArtworkURL, 96), Placeholder: hitPalette(h).Bg, Size: songHeight - 2*padTile, Radius: radiusControl, Color: 1}.Show(c)
		a.flySource(cover, "song:"+hk, partCover, "", radiusControl)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, h.Name).FontSize(13).FontWeight(600).Ellipsis("…").SingleLine().Shrink(1)
				if h.ContentRating == "explicit" {
					explicitBadge(c, k)
				}
			})
			ui.Text(c, a.songLine(h)).FontSize(12).TextColor(k.Muted).Ellipsis("…").SingleLine()
		})
		mark, job := s.HitMark(h)
		startedAt, startingOn := s.Search.Starting[hk]
		switch {
		case mark == store.MarkDone:
			if markButton(c, k, "In library", pillOK, icCheck).Clicked() {
				a.openingFrom("song:" + hk)
				a.openSongPlace(h, job)
			}
		case mark == store.MarkPaused:
			if markButton(c, k, "Paused", pillWait, icPause).Label("Paused: resume").Clicked() {
				a.download(h)
			}
		case mark == store.MarkActive:
			if markButton(c, k, "In the queue", pillRun, icDown).Clicked() {
				a.openingFrom("song:" + hk)
				a.OpenJob(job)
			}
		case waited(c, startingOn, startedAt):
			pill(c, k, pillWait, nil, "Starting…")
		default:
			dl := ui.ButtonBase(c.Key("download")).Size(32, 32).Radius(radiusFull).Center().Background(k.Raise2).Label("Download " + h.Name)
			if dl.Hovered() {
				dl.Background(k.Track)
			}
			if dl.Clicked() {
				a.download(h)
			}
			dl.Cursor(ui.CursorPointer).Children(func() { ui.Icon(c, icDown).FontSize(15).TextColor(k.Fg) })
		}
	})
}

// markButton is a hit's library or queue mark, which opens its job.
func markButton(c *ui.Context, k *Tokens, text string, kind pillKind, ic *ui.SVG) ui.Element {
	b := ui.ButtonBase(c.Key("mark")).Radius(radiusFull).Label(text + ": open the download")
	b.Cursor(ui.CursorPointer).Children(func() { pill(c, k, kind, ic, text) })
	return b
}

func explicitBadge(c *ui.Context, k *Tokens) {
	ui.Box(c).Size(15, 15).Radius(radiusInner).Background(k.Faint.Alpha(0.35)).Center().Children(func() {
		ui.Text(c, "E").FontSize(9.5).FontWeight(700).TextColor(k.Fg)
	})
}

// tileFacts is a tile's line of facts: an album's artist and year, an
// artist's genre.
func tileFacts(h client.SearchHit) []string {
	var out []string
	if h.Type == "artist" {
		if h.Genre != "" {
			out = append(out, h.Genre)
		}
		return out
	}
	if h.ArtistName != "" {
		out = append(out, h.ArtistName)
	}
	if kind, _ := store.AlbumKind(h.Name); h.Type == "album" && kind != "Album" {
		out = append(out, kind)
	}
	if len(h.ReleaseDate) >= 4 {
		out = append(out, h.ReleaseDate[:4])
	}
	return out
}

// hitName is what a hit is called: an album's title without its kind's
// suffix (store.AlbumKind), as everywhere else.
func hitName(h client.SearchHit) string {
	if h.Type == "album" {
		_, name := store.AlbumKind(h.Name)
		return name
	}
	return h.Name
}

// hitFacts is a song card's line of facts: its artist, album and length.
func hitFacts(h client.SearchHit) []string {
	var out []string
	_, album := store.AlbumKind(h.AlbumName)
	for _, v := range []string{h.ArtistName, album} {
		if v != "" {
			out = append(out, v)
		}
	}
	if h.DurationMS > 0 {
		out = append(out, format.Duration(time.Duration(h.DurationMS)*time.Millisecond))
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

// shortHints is the chip on a tile's cover: the best quality in short, and
// Atmos beside it on a tile wide enough.
func shortHints(traits []string, size float32) string {
	short := map[string]string{"Hi-Res Lossless": "Hi-Res", "Lossless": "Lossless", "Dolby Atmos": "Atmos"}
	hints := qualityHints(traits)
	if len(hints) > 1 && size < 180 {
		hints = hints[:1]
	}
	for i, h := range hints {
		hints[i] = short[h]
	}
	return strings.Join(hints, " · ")
}

func hitPalette(h client.SearchHit) Palette {
	return ParsePalette(h.ArtworkBgColor, h.ArtworkTextColor1, h.ArtworkTextColor2, h.ArtworkTextColor3, h.ArtworkTextColor4)
}

// skeletonGroups stand for the first answer of a term while it loads: the
// overview's albums and songs at the sizes and counts they will take.
func skeletonGroups(c *ui.Context, k *Tokens, width float32) {
	ui.Column(c).Gap(32).Children(func() {
		ui.Column(c.Key("albums")).Gap(14).Children(func() {
			skeleton(c, k, 72, 14).Margin(8, 0)
			skeletonTiles(c, k, width, 0)
		})
		ui.Column(c.Key("songs")).Gap(14).Children(func() {
			skeleton(c, k, 64, 14).Margin(8, 0)
			cols := songColumns(width)
			cw := (width - songGap*float32(cols-1)) / float32(cols)
			ui.Row(c).Width(width).Wrap().Gap(songGap).Children(func() {
				for i := range overviewCount("songs", width) {
					ui.Row(c.Key(i)).Width(cw).Height(songHeight).Padding(padTile).Gap(12).Radius(radiusTile).AlignItems(ui.Center).
						Background(k.Raise).Border(1, k.Line).Children(func() {
						skeleton(c, k, songHeight-2*padTile, songHeight-2*padTile).Radius(radiusControl)
						ui.Column(c).Grow(1).Gap(8).Children(func() {
							skeleton(c, k, float32(110+(i*37)%60), 10)
							skeleton(c, k, float32(150+(i*53)%70), 8)
						})
					})
				}
			})
		})
	})
}

// skeletonTiles is a row of tiles while they load; n 0 fills the row.
func skeletonTiles(c *ui.Context, k *Tokens, width float32, n int) {
	size := tileSize(width, 0)
	if fit := tileColumns(width); n == 0 || n > fit {
		n = fit
	}
	ui.Row(c).Gap(tileGap).Children(func() {
		for i := range n {
			ui.Column(c.Key(i)).Width(size).Gap(10).Children(func() {
				skeleton(c, k, size, size).Radius(radiusCover)
				skeleton(c, k, size*0.7, 10)
				skeleton(c, k, size*0.45, 8)
			})
		}
	})
}

// openSongPlace opens what a song in the library opens: the run this app
// finished for it, else its album's page.
func (a *App) openSongPlace(h client.SearchHit, job string) {
	if job != "" {
		a.OpenJob(job)
		return
	}
	if r := a.Store.HitRecord(h); r != nil && r.Kind == domain.ResourceAlbum {
		a.OpenAlbum(client.SearchHit{Type: "album", ID: r.ID, Name: h.AlbumName, ArtistName: h.ArtistName, ArtworkURL: h.ArtworkURL,
			ArtworkBgColor: h.ArtworkBgColor, ArtworkTextColor1: h.ArtworkTextColor1, ArtworkTextColor2: h.ArtworkTextColor2,
			ArtworkTextColor3: h.ArtworkTextColor3, ArtworkTextColor4: h.ArtworkTextColor4, Resource: r})
	}
}
