package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/region"
	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/format"
)

// OpenAlbum shows the page of the record an album or playlist hit stands
// for, which reads its songs; Back returns to the screen it was opened from.
// The page is its record's from wherever it opens: its state, track counts
// and region come from the record, and a run of it only adds the figures
// that are live while it works.
func (a *App) OpenAlbum(h client.SearchHit) { a.openAlbumFrom(h, store.PageSource{}) }

// openAlbumFrom is OpenAlbum reading the songs from from.
func (a *App) openAlbumFrom(h client.SearchHit, from store.PageSource) {
	s := a.Store
	ref := domain.ResourceRef{Kind: store.PageKind(h), ID: h.ID}
	job := s.AlbumJob(ref.Kind, ref.ID)
	// A run is the page only of an album whose record wants it whole: of one
	// downloaded in part, the run holds just the songs added by hand, queued
	// or finished (1 of 1), so the page shows the album and its record's
	// share, with the rest to add.
	if s.Job(job) != nil {
		if mark, _ := s.HitMark(h); mark == store.MarkPartial {
			job = ""
		}
	}
	a.openPage(ref, job)
	s.OpenAlbumFrom(h, from)
	if job != "" {
		s.OpenDetail(job)
	} else {
		s.CloseDetail()
	}
	a.push(ScreenAlbum)
}

// OpenJob shows the page of the record run id is a run of: an album's or a
// playlist's with its songs, else the run's own page. Back returns to the
// screen it was opened from.
func (a *App) OpenJob(id string) {
	s := a.Store
	h, album := s.JobAlbum(s.Job(id))
	a.openPage(domain.ResourceRef{Kind: store.PageKind(h), ID: h.ID}, id)
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
func (a *App) openPage(album domain.ResourceRef, job string) {
	f := &a.fly
	f.from, f.next, f.closeDetail = f.next, "", false
	if a.Screen != ScreenAlbum {
		// Setup leaves the window to the page at once.
		f.under, f.instant = a.Screen, a.Screen == ScreenSetup
		f.grow = growsFrom(f.from) && a.PageMotion && !f.still && !f.instant
		f.growFrom, f.growRadius = ui.Rect{}, 0
	}
	if key := album.ID + "\x00" + job; key != a.pageKey {
		a.pageKey = key
		if st := a.scrolls["album"]; st != nil {
			st.Y, a.scrollSeen["album"] = 0, 0
		}
	}
	a.albumSeen = ""
	if album.ID != "" {
		a.albumSeen = a.Store.AlbumJob(album.Kind, album.ID)
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
	if id := s.AlbumJob(p.Kind(), p.ID); id != a.albumSeen {
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
			a.searchSel, a.searchReveal, a.searchKeyed = store.HitKey(h), false, false
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
	case ScreenLibrary:
		return "Library"
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

// pagePalette lights the album screen: the motion cover's palette while
// that plays, else its download's, else the album's.
func (a *App) pagePalette(c *ui.Context) Palette {
	s := a.Store
	if master, pal := a.pageMotion(); pal.OK && a.motionShown(c, master) {
		return pal
	}
	if j := s.Detail(); j != nil {
		if pal := jobPalette(j, false); pal.OK {
			return pal
		}
	}
	return albumOf(&s.Search.Album).pal
}

// pageMotion is the album screen's motion cover and its palette: its
// download's, else the album's, which an album not downloaded yet is read
// with.
func (a *App) pageMotion() (string, Palette) {
	s := a.Store
	if j := s.Detail(); j != nil && j.MotionArtworkURL != "" {
		return j.MotionArtworkURL, jobPalette(j, true)
	}
	al := albumOf(&s.Search.Album)
	return al.motion, al.motionPal
}

// albumScreen is the one page of an album's or a playlist's record, or of a
// run that belongs to no record. The head is the cover and title over the
// palette, four figures and, while a run holds the page, its bar, and what
// can be done; the lower panel lists the songs, with the run's activity
// beside them.
func (a *App) albumScreen(c *ui.Context, k *Tokens, page float32) {
	s := a.Store
	// Going out, the page shows what it showed.
	in := a.Screen == ScreenAlbum
	if in {
		a.followAlbumJob()
	}
	p, j := &s.Search.Album, s.Detail()
	s.WatchRun(a.pageNeedsRun(p, j))
	if p.ID == "" && j == nil {
		if in {
			a.Back()
		}
		return
	}
	s.WatchConfig()
	l := pageHero(page, a.heroCompact(c))
	master, _ := a.pageMotion()
	video := a.motionPlayer(c, master)
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
		// The rows come and go once the panel's ground is whole
		// (flyRows), so it never stands empty over the screen under it.
		rows := ui.Row(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch)
		a.flyRows(rows)
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

// pageHead is the album screen's head: the record's names once read, else
// what its run holds; the run's cover, which stays while the album is read
// under another address of the same picture. Its kicker is the type alone;
// the four figures are pageFigures.
func (a *App) pageHead(c *ui.Context, k *Tokens, l heroLayout, p *store.AlbumPage, j *store.Job, pal Palette, hc heroColors, video *ui.VideoPlayer) heroSpec {
	album := p.ID != ""
	f := albumOf(p)
	h := heroSpec{colors: hc}
	var kind string
	switch {
	case album && p.Kind() == domain.ResourcePlaylist:
		kind, h.title, h.artist = "Playlist", f.title, f.artist
	case album:
		kind, h.title = store.AlbumKind(f.title)
		h.artist = f.artist
	default:
		kind, h.title, h.artist = j.Kind(), j.Name(), j.By()
	}
	h.kicker = kind
	artwork := f.artwork
	if j != nil && j.ArtworkURL != "" {
		artwork = j.ArtworkURL
	}
	h.cover = func(size float32) ui.Element {
		tone, dim, shadow, jump := a.flyCover()
		return widgets.Cover{Image: a.Art.Get(artwork, 512), Placeholder: pal.Bg, Size: size, Radius: radiusCover, Color: tone, Dim: dim, Jump: jump,
			Shadow: shadow, Video: video}.Show(c).Label("Artwork")
	}
	// What the download made of it, once known, stands for what the album
	// offers.
	var it *domain.JobItem
	if j != nil {
		it = firstWithSpec(j)
	}
	var hints []string
	if it != nil {
		h.quality, h.hi = format.LongQuality(it.Codec, it.BitDepth, it.SampleRate), isHiRes(format.Quality(it.Codec, it.BitDepth, it.SampleRate, 0))
	} else if hints = qualityHints(f.traits); len(hints) > 0 {
		h.quality, h.hi, hints = hints[0], hints[0] == "Hi-Res Lossless", hints[1:]
	}
	if len(hints) > 0 || f.explicit {
		h.tags = func() {
			for _, q := range hints {
				tag(c, k, q, q == "Hi-Res Lossless", hc.meta)
			}
			if f.explicit {
				explicitBadge(c, k)
			}
		}
	}
	figs := a.pageFigures(c, l, p, j, f)
	h.band = func() {
		ui.Row(c).Gap(heroGap).MaxWidth(heroBand).Children(func() {
			for _, x := range figs {
				heroStat(c, k, hc, l, x.name, x.value, x.unit)
			}
		})
		if j != nil {
			bar := ui.Column(c.Key("bar")).FillWidth().MaxWidth(heroBand)
			if a.PageMotion && !a.fly.moving {
				bar.Transition(a.barMotion(bar))
			}
			bar.Children(func() {
				albumProgress(c, k, j, "album", 8, hc.title, hc.title.Alpha(0.16), hc.accent, hc.meta, 12, true)
			})
		}
	}
	return h
}

// heroBand is the widest the head's figures and bar run, in a wide window,
// and heroGap the room between figures.
const (
	heroBand = 900
	heroGap  = 12
)

// figure is one figure of the head's band: its name, value and unit.
type figure struct{ name, value, unit string }

// pageFigures are the four figures under the head's title, in four equal
// columns whatever the state: the fourth is always the tracks. The record
// decides the state: with no run live, an album or playlist shows what it
// is (when it came out, its genre, its length) until its record is whole,
// and then what it made (its size, how long it took, its region). A run
// adds only what is live while it works: the bytes moved, its speed and
// the time it has left.
func (a *App) pageFigures(c *ui.Context, l heroLayout, p *store.AlbumPage, j *store.Job, f albumFacts) []figure {
	s := a.Store
	size := func(n int64) figure {
		if n <= 0 {
			return figure{"Size", "—", ""}
		}
		num, unit, _ := strings.Cut(format.Bytes(n), " ")
		return figure{"Size", num, " " + unit}
	}
	took := func(d time.Duration) figure {
		if d <= 0 {
			return figure{"Total time", "—", ""}
		}
		return figure{"Total time", format.Duration(d), ""}
	}
	where := a.regionFigure(c, l, a.pageRegion(p, j))
	count := func(done, total int) figure {
		if total > 0 && done >= total {
			return figure{"Tracks", strconv.Itoa(total), ""}
		}
		return figure{"Tracks", fmt.Sprintf("%d / %d", done, max(total, done)), ""}
	}
	switch {
	case j != nil && !j.Status.IsTerminal():
		b := j.Transferred()
		out := []figure{size(b), {"Speed", "—", ""}, {"Left", "—", ""}}
		if sp := j.ShownSpeed(c.Now()); sp > 0 {
			v, u := format.Scale(sp)
			out[1] = figure{"Speed", format.Number(v, 1), " " + u + "/s"}
		}
		if r := format.Remaining(j.Remaining); r != "" {
			out[2] = figure{"Left", r, ""}
		}
		done, total := jobTracks(j)
		return append(out, count(done, total))
	case j != nil:
		var d time.Duration
		if j.StartedAt != nil && j.FinishedAt != nil {
			d = j.FinishedAt.Sub(*j.StartedAt)
		}
		done, total := jobTracks(j)
		return []figure{size(j.Bytes()), took(d), where, count(done, total)}
	case p.ID == "":
		return []figure{size(0), took(0), where, count(0, 0)}
	}
	mark, _ := s.HitMark(p.Hit)
	total := max(f.tracks, p.Hit.TrackCount)
	r := s.HitRecord(p.Hit)
	if mark == store.MarkDone && r != nil {
		return []figure{size(p.Run.Size), took(p.Run.Took), where, count(r.TracksDone, store.MarkTotal(r, total))}
	}
	n := figure{"Tracks", strconv.Itoa(total), ""}
	if mark == store.MarkPartial && r != nil {
		n = count(r.TracksDone, store.MarkTotal(r, total))
	}
	length := figure{"Length", "—", ""}
	if m := int(f.length.Round(time.Minute) / time.Minute); m > 0 {
		length = figure{"Length", strconv.Itoa(m), " min"}
	}
	if p.Kind() == domain.ResourcePlaylist {
		artists := "—"
		if f.artists > 0 {
			artists = strconv.Itoa(f.artists)
		}
		return []figure{{"Artists", artists, ""}, length, where, n}
	}
	return []figure{{"Released", firstNonEmptyStr(f.year, "—"), ""}, {"Genre", firstNonEmptyStr(f.genre, "—"), ""}, length, n}
}

// jobTracks is how many of a run's tracks are on disk and how many it
// holds, from its album bar, or its counters before the tracks arrive.
func jobTracks(j *store.Job) (done, total int) {
	al := j.Album()
	if al.Total == 0 {
		return j.Done(), j.TotalItems
	}
	return al.OnDisk, al.Total
}

// pageNeedsRun reports a page whose record is whole while no run of it is
// in the store, which has the record's size and time to read (WatchRun).
func (a *App) pageNeedsRun(p *store.AlbumPage, j *store.Job) bool {
	if j != nil || p.ID == "" || p.Status != store.SearchReady {
		return false
	}
	mark, _ := a.Store.HitMark(p.Hit)
	return mark == store.MarkDone
}

// pageRegion is the region the page's record was read in, by name: a run's
// storefront, else the record's (read with its size), else the one the page
// was read from, else
// the default account's; "—" when none is known.
func (a *App) pageRegion(p *store.AlbumPage, j *store.Job) string {
	sf := ""
	if j != nil {
		sf = j.Storefront
	}
	if sf == "" {
		sf = p.Run.Storefront
	}
	if sf == "" {
		sf = p.Storefront
	}
	if rs := a.Store.SearchRegions(); sf == "" && len(rs) > 0 {
		sf = rs[0]
	}
	return sf
}

// regionFigure is the Region figure for storefront sf: its name where the
// name fits its column at the figure's size, else its code in capitals, and
// "—" for none; never an ellipsis.
func (a *App) regionFigure(c *ui.Context, l heroLayout, sf string) figure {
	if sf == "" {
		return figure{"Region", "—", ""}
	}
	size, band := float32(22), min(float32(heroBand), l.width)
	if l.compact {
		size = 18
	}
	name := region.Name(sf)
	if w, _ := c.MeasureText(0, ui.Span{Text: name, Font: MonoFont, Size: size, Weight: 600}); w > (band-3*heroGap)/4 {
		name = strings.ToUpper(sf)
	}
	return figure{"Region", name, ""}
}

// pageActions are what the record's state lets be done, at the title bar's
// end, as a toolbar's: a download of it, or the rest of it, or a download
// again once it is whole; and, while a run of it is in the store, that
// run's folder, cancel or retry, and delete once it ended. They act only
// while live: the page going out shows them as they were.
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
	if j == nil && h.URL == "" {
		// A playlist only the Apple library has downloads song by song.
		return
	}
	if j == nil {
		switch mark, _ := s.HitMark(h); {
		case mark == store.MarkDone:
			again()
		case mark == store.MarkPartial:
			if clicked(artButton(c, "Download the rest", hc.title, hc.ink).Label("Download the rest of "+h.Name)) && !starting {
				a.download(h)
			}
		case mark == store.MarkActive:
			// The record names a run the store does not hold.
			pill(c, k, pillRun, icDown, "In the queue")
		case mark == store.MarkPaused:
			if clicked(button(c, k, buttonOnArt, "Resume", nil).Label("Resume " + h.Name)) {
				a.download(h)
			}
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
		if clicked(button(c, k, buttonOnArt, cancelWord(j), nil)) {
			s.Cancel(j.ID)
		}
	case domain.JobFailed, domain.JobCancelled:
		if clicked(button(c, k, buttonOnArt, retryWord(j), nil)) {
			s.Retry(j.ID)
		}
	case domain.JobCompleted:
		if p.ID != "" {
			again()
		}
	}
	if j.Status.IsTerminal() && clicked(button(c, k, buttonOnArt, "Delete", nil).Label("Delete this download")) {
		a.askDeleteRun(j)
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
