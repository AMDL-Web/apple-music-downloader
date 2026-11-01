package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/format"
)

// OpenAlbum shows an album hit's page, which reads its songs, with its
// download while the feed holds one; Back returns to the screen it was
// opened from.
func (a *App) OpenAlbum(h client.SearchHit) {
	s := a.Store
	job := s.AlbumJob(h.ID)
	a.openPage(h.ID, job)
	s.OpenAlbum(h)
	if job != "" {
		s.OpenDetail(job)
	} else {
		s.CloseDetail()
	}
	a.push(ScreenAlbum)
}

// OpenJob shows a download's page: an album's is its album's page, with
// the album's songs. Back returns to the screen it was opened from.
func (a *App) OpenJob(id string) {
	s := a.Store
	h, album := s.JobAlbum(s.Job(id))
	a.openPage(h.ID, id)
	if album {
		s.OpenAlbum(h)
	} else {
		s.CloseAlbum()
	}
	s.OpenDetail(id)
	a.push(ScreenAlbum)
}

// openPage notes the album and the download the page opens on: another
// pair opens at the top of its songs, and a download of the album that
// starts later takes the page (followAlbumJob).
func (a *App) openPage(album, job string) {
	f := &a.fly
	f.from, f.next, f.closeDetail = f.next, "", false
	if a.Screen != ScreenAlbum {
		// Setup leaves the window to the page at once.
		f.under, f.instant = a.Screen, a.Screen == ScreenSetup
	}
	if key := album + "\x00" + job; key != a.pageKey {
		a.pageKey = key
		if st := a.scrolls["album"]; st != nil {
			st.Y, a.scrollSeen["album"] = 0, 0
		}
	}
	a.albumSeen = ""
	if album != "" {
		a.albumSeen = a.Store.AlbumJob(album)
	}
}

// followAlbumJob moves the page to the album's download when that changes:
// one started from the page, or the next one after the shown one was
// deleted. The download the page opened on stays beside a newer one that
// already ran.
func (a *App) followAlbumJob() {
	s := a.Store
	p := &s.Search.Album
	if p.ID == "" {
		return
	}
	if id := s.AlbumJob(p.ID); id != a.albumSeen {
		a.albumSeen = id
		if id != "" {
			s.OpenDetail(id)
		}
	}
}

// revealAlbum chooses, back on home, the album the page showed, so the
// keys go on from it. The results keep the place they were left at, where
// it was in view when it opened.
func (a *App) revealAlbum() {
	h := a.Store.Search.Album.Hit
	for _, x := range a.Store.Search.ShownHits() {
		if store.HitKey(x) == store.HitKey(h) {
			a.searchSel, a.searchReveal = store.HitKey(h), false
			return
		}
	}
}

// backLabel names the screen Back returns to.
func (a *App) backLabel() string {
	s := a.Store
	switch a.backTo() {
	case ScreenHome:
		if s.Search.Artist.ID != "" && s.Search.Artist.Artist.Name != "" {
			return clip(s.Search.Artist.Artist.Name, 28)
		}
		if s.Search.Term != "" {
			return "Results"
		}
		return "Home"
	case ScreenSettings:
		return "Settings"
	}
	return "Queue"
}

// clip cuts s to n characters, an ellipsis last.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// pagePalette lights the album screen: its download's palette, the motion
// cover's while that plays, else the album's.
func (a *App) pagePalette(c *ui.Context) Palette {
	s := a.Store
	if j := s.Detail(); j != nil {
		if pal := jobPalette(j, a.motionShown(c, j.MotionArtworkURL)); pal.OK {
			return pal
		}
	}
	return albumOf(&s.Search.Album).pal
}

// albumScreen is the one page of an album and of a download: an album with
// its download while there is one, or a download that is not an album's.
// The head is the cover and title over the palette, the download's figures
// and bar while there is one, else the album's, and what can be done; the
// lower panel lists the songs, with the download's activity beside them.
func (a *App) albumScreen(c *ui.Context, k *Tokens, page float32) {
	s := a.Store
	// Going out, the page shows what it showed.
	in := a.Screen == ScreenAlbum
	if in {
		a.followAlbumJob()
	}
	p, j := &s.Search.Album, s.Detail()
	if p.ID == "" && j == nil {
		if in {
			a.Back()
		}
		return
	}
	s.WatchConfig()
	l := pageHero(c, page)
	var video *ui.VideoPlayer
	if j != nil {
		// At least the queue hero's size, so that the motion cover flying
		// between them plays on in the same player.
		video = a.motionPlayer(c, j.MotionArtworkURL, max(l.cover, heroCover))
	}
	pal := a.pagePalette(c)
	hc := heroColorsFor(pal, k)
	if in {
		hc = hc.eased(c)
	}
	// The head is as tall whatever its title (collectionHero), so the lower
	// panel is placed under it, and built first: absolute layers paint in
	// the order they are built, and the head's parts fly over the panel.
	// Opaque, so the backdrop, which lights the head, ends under it.
	lower := ui.Row(c).Absolute().Top(l.height()).Left(0).Right(0).Bottom(0).AlignItems(ui.Stretch).
		Background(detailGround(k)).BorderWidth(1, 0, 0, 0).BorderColor(k.Line).Label("Tracks")
	a.flyPanel(lower, k)
	accent := hc.accent
	lower.Children(func() {
		rows := ui.Row(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch)
		a.flyFade(rows)
		rows.Children(func() {
			a.pageTracks(c, k, j, accent, stageWidth(page))
			if j != nil {
				activity := float32(280)
				if page < 1000 {
					activity = 240
				}
				a.activity(c, k, j, activity)
			}
		})
	})
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Children(func() {
		a.collectionHero(c, k, l, a.pageHead(c, k, l, p, j, pal, hc, video))
	})
	a.flightCopies(c, k)
}

// pageHead is the album screen's head: the album's names once read, else
// what the download holds; the download's cover, which stays while the
// album is read under another address of the same picture.
func (a *App) pageHead(c *ui.Context, k *Tokens, l heroLayout, p *store.AlbumPage, j *store.Job, pal Palette, hc heroColors, video *ui.VideoPlayer) heroSpec {
	s := a.Store
	album := p.ID != ""
	f := albumOf(p)
	h := heroSpec{colors: hc}
	var kind string
	tracks := f.tracks
	if album {
		kind, h.title = store.AlbumKind(f.title)
		h.artist = f.artist
	} else {
		kind, h.title, h.artist = j.Kind(), j.Name(), j.By()
	}
	if j != nil && (!album || tracks == 0) {
		tracks = j.TotalItems
	}
	if tracks > 0 {
		kind += " · " + plural(tracks, "track")
	}
	h.kicker = kind
	artwork := f.artwork
	if j != nil && j.ArtworkURL != "" {
		artwork = j.ArtworkURL
	}
	h.cover = func(size float32) ui.Element {
		return widgets.Cover{Image: a.Art.Get(artwork, 512), Placeholder: pal.Bg, Size: size, Radius: radiusCover, Color: 1, Shadow: true, Video: video}.Show(c).Label("Artwork")
	}
	// What the download made of it, once known, stands for what the album
	// offers.
	var it *domain.JobItem
	if j != nil {
		it = firstWithCodec(j)
	}
	var hints []string
	if it != nil {
		h.quality, h.hi = format.LongQuality(it.Codec, it.BitDepth, it.SampleRate), isHiRes(format.Quality(it.Codec, it.BitDepth, it.SampleRate, 0))
	} else if hints = qualityHints(f.traits); len(hints) > 0 {
		h.quality, h.hi, hints = hints[0], hints[0] == "Hi-Res Lossless", hints[1:]
	}
	// A download the feed no longer holds put it in the library.
	inLibrary := false
	if album && j == nil {
		mark, _ := s.HitMark(p.Hit)
		inLibrary = mark == store.MarkDone
	}
	if len(hints) > 0 || f.explicit || inLibrary {
		h.tags = func() {
			for _, q := range hints {
				tag(c, k, q, q == "Hi-Res Lossless", hc.meta)
			}
			if f.explicit {
				explicitBadge(c, k)
			}
			if inLibrary {
				pill(c, k, pillOK, icCheck, "In your library")
			}
		}
	}
	h.band = func() {
		ui.Row(c).Gap(12).MaxWidth(heroBand).Children(func() {
			if j != nil {
				jobFigures(c, k, l, j, hc)
			} else {
				albumFigures(c, k, l, f, hc)
			}
		})
		if j != nil {
			ui.Column(c).FillWidth().MaxWidth(heroBand).Children(func() {
				albumProgress(c, k, j, "album", 8, hc.title, hc.title.Alpha(0.16), hc.accent, hc.meta, 12)
			})
		}
	}
	return h
}

// heroBand is the widest the head's figures and bar run, in a wide window.
const heroBand = 900

// jobFigures are a download's figures under the title: what landed, which
// counts the bytes still arriving while it runs, how fast, how long it has
// left or when it finished, and when it started. How many tracks finished
// is the bar's to say.
func jobFigures(c *ui.Context, k *Tokens, l heroLayout, j *store.Job, hc heroColors) {
	b := j.Bytes()
	if !j.Status.IsTerminal() {
		b = j.Transferred()
	}
	if b > 0 {
		num, unit, _ := strings.Cut(format.Bytes(b), " ")
		heroStat(c, k, hc, l, "Size", num, " "+unit)
	} else {
		heroStat(c, k, hc, l, "Size", "—", "")
	}
	if sp := j.AvgSpeed(c.Now()); sp > 0 {
		v, u := format.Scale(sp)
		heroStat(c, k, hc, l, "Avg speed", format.Number(v, 1), " "+u+"/s")
	} else {
		heroStat(c, k, hc, l, "Avg speed", "—", "")
	}
	switch r := format.Remaining(j.Remaining); {
	case r != "":
		heroStat(c, k, hc, l, "Left", r, "")
	case j.FinishedAt != nil:
		heroStat(c, k, hc, l, "Finished", format.Clock(*j.FinishedAt), "")
	default:
		heroStat(c, k, hc, l, "Left", "—", "")
	}
	if j.StartedAt != nil {
		heroStat(c, k, hc, l, "Started", format.Clock(*j.StartedAt), "")
	} else {
		heroStat(c, k, hc, l, "Created", format.Clock(j.CreatedAt), "")
	}
}

// albumFigures are what an album is under its title, until a download
// holds it: when it came out, its genre and length.
func albumFigures(c *ui.Context, k *Tokens, l heroLayout, f albumFacts, hc heroColors) {
	heroStat(c, k, hc, l, "Released", firstNonEmptyStr(f.year, "—"), "")
	// A genre's name runs longer than a year or a length.
	heroStat(c, k, hc, l, "Genre", firstNonEmptyStr(f.genre, "—"), "").Grow(2)
	if m := int(f.length.Round(time.Minute) / time.Minute); m > 0 {
		heroStat(c, k, hc, l, "Length", fmt.Sprint(m), " min")
	} else {
		heroStat(c, k, hc, l, "Length", "—", "")
	}
}

// pageActions are what the page's state lets be done, at the title bar's
// end, as a toolbar's: a download of the album; or, once a download holds
// it, its folder, cancel or retry, a download again once it is in the
// library, and delete once it ended. They act only while live: the page
// going out shows them as they were.
func (a *App) pageActions(c *ui.Context, k *Tokens, p *store.AlbumPage, j *store.Job, hc heroColors, live bool) {
	s := a.Store
	h := p.Hit
	clicked := func(b ui.Element) bool { return b.Clicked() && live }
	// A start that answers within a frame keeps the buttons as they were.
	since, starting := s.Search.Starting[store.HitKey(h)]
	wait := p.ID != "" && waited(c, starting, since)
	again := func() {
		if clicked(buttonOff(c, k, buttonOnArt, "Download again", nil, wait).Label("Download "+h.Name+" again")) && !starting {
			a.download(h)
		}
	}
	if j == nil {
		switch mark, _ := s.HitMark(h); {
		case mark == store.MarkDone:
			again()
		case mark == store.MarkActive:
			// A download the feed does not hold, past the newest it lists.
			pill(c, k, pillRun, icDown, "In the queue")
		case wait:
			pill(c, k, pillWait, nil, "Starting…")
		default:
			if clicked(artButton(c, "Download", hc.title, hc.ink).Label("Download "+h.Name)) && !starting {
				a.download(h)
			}
		}
		return
	}
	if dir := a.revealDir(j); dir != "" && clicked(button(c, k, buttonOnArt, revealLabel, nil)) {
		a.Act.Reveal(dir)
	}
	switch j.Status {
	case domain.JobQueued, domain.JobRunning:
		if clicked(button(c, k, buttonOnArt, "Cancel", nil)) {
			s.Cancel(j.ID)
		}
	case domain.JobFailed, domain.JobCancelled:
		if clicked(button(c, k, buttonOnArt, "Retry", nil)) {
			s.Retry(j.ID)
		}
	case domain.JobCompleted:
		if p.ID != "" {
			again()
		}
	}
	if j.Status.IsTerminal() && clicked(button(c, k, buttonOnArt, "Delete", nil).Label("Delete this download")) {
		a.askDelete(j)
	}
}

// titleActions are the album page's buttons in the title bar, while the
// page shows.
func (a *App) titleActions(c *ui.Context, k *Tokens, live bool) {
	s := a.Store
	p, j := &s.Search.Album, s.Detail()
	if p.ID == "" && j == nil {
		return
	}
	hc := heroColorsFor(a.pagePalette(c), k)
	if live {
		hc = hc.eased(c)
	}
	a.pageActions(c, k, p, j, hc, live)
}
