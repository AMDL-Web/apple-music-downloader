package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/format"
)

// The album screen's track list, in DIPs. The album's songs and a
// download's own tracks share the rows' metrics (trackInset, trackPadY,
// trackStateWidth), so the list keeps its place when the album is read.
const (
	// trackHeight is a skeleton row while the songs load.
	trackHeight = 48
	// trackControl is the round control at a row's end.
	trackControl = 28
	// trackNumber is the number's column, and trackLead a row's room before
	// its title: its padding, number and gap.
	trackNumber = 28
	trackLead   = 4 + trackNumber + 16
	// trackLength is the length's column.
	trackLength = 44
	// trackNamesHeight is a title over the line under it: 17 for the title,
	// 2 between, 14 for the line, as the type sets them.
	trackNamesHeight = 33
)

// pageTracks lists the page's songs in the lower panel: the album's by
// disc once read, in the state its download gives each, else the
// download's own tracks. Skeleton rows stand for them until either is
// there; a failed read of the album says so only where no download lists
// them. The stage bars, stages wide, show while the download runs.
func (a *App) pageTracks(c *ui.Context, k *Tokens, j *store.Job, accent ui.Color, stages float32) {
	s := a.Store
	p := &s.Search.Album
	col := ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Padding(14, 24, 0, 32)
	shown := float32(0)
	if j != nil && !j.Status.IsTerminal() {
		shown = stages
	}
	bar := col.AnimateTo("stages", shown, orderMotion)
	col.Children(func() {
		var items []domain.JobItem
		if j != nil {
			items = s.JobItems(j.ID)
		}
		switch {
		case p.ID != "" && p.Status == store.SearchReady:
			a.albumTracks(c, k, j, items, accent, bar)
		case len(items) > 0:
			a.trackList(c, k, items, accent, bar)
		case p.ID != "" && p.Status == store.SearchFailed && j == nil:
			searchFailed(c, k, p.Err, s.RetryAlbum)
		default:
			n := p.Hit.TrackCount
			if j != nil && j.TotalItems > 0 {
				n = j.TotalItems
			}
			for i := range min(max(n, 6), 14) {
				ui.Row(c.Key(i)).Height(trackHeight).PaddingX(4).Gap(16).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Width(trackNumber).Justify(ui.End).Children(func() { skeleton(c, k, 18, 10) })
					ui.Column(c).Grow(1).Children(func() { skeleton(c, k, float32(140+(i*53)%160), 10) })
					skeleton(c, k, 32, 10)
				})
			}
		}
	})
}

// listStill reports whether the page's list scrolls, which holds its stage
// bars still; its own scroll counts from this frame on.
func (a *App) listStill(c *ui.Context) bool {
	a.scrollState(c, "album")
	return a.scrolling(c)
}

// jobMark is the mark a download gives its album's songs.
func jobMark(j *store.Job) store.MarkState {
	switch j.Status {
	case domain.JobQueued, domain.JobRunning:
		return store.MarkActive
	case domain.JobCompleted:
		return store.MarkDone
	}
	return store.MarkNone
}

// albumTracks lists the album's songs by disc: in the state the page's
// download gives each, else the album's mark through a download the feed
// does not hold, and a song's own download.
func (a *App) albumTracks(c *ui.Context, k *Tokens, j *store.Job, items []domain.JobItem, accent ui.Color, bar float32) {
	s := a.Store
	p := &s.Search.Album
	al := &p.Album
	mark := store.MarkNone
	if j != nil {
		mark = jobMark(j)
	} else {
		var job string
		if mark, job = s.HitMark(p.Hit); mark != store.MarkNone {
			items = s.JobItems(job)
		}
	}
	lines := make([]trackMark, len(al.Tracks))
	for i, t := range al.Tracks {
		lines[i].mark, lines[i].item = s.TrackMark(t, mark, items)
	}
	discs := al.DiscCount > 1
	still := a.listStill(c)
	a.scrollArea(c, "album", detailGround(k), scrollGutter, func() {
		for i, t := range al.Tracks {
			if discs && (i == 0 || t.DiscNumber != al.Tracks[i-1].DiscNumber) {
				top := float32(4)
				if i > 0 {
					top = 18
				}
				ui.Row(c.Key(fmt.Sprint("disc", t.DiscNumber))).Height(28).PaddingStart(trackLead).Margin(top, 0, 2, 0).AlignItems(ui.Center).Children(func() {
					label(c, k, fmt.Sprintf("Disc %d", t.DiscNumber))
				})
			}
			nextLit := i+1 < len(lines) && lines[i+1].lit()
			a.trackLine(c, k, t, al.ArtistName, mark, lines[i], nextLit, accent, bar, still)
		}
	}).Grow(1)
}

// albumFacts are what the album screen shows of an album: the album once
// read, else the hit it opened from.
type albumFacts struct {
	title, artist, artwork, genre, year string
	traits                              []string
	explicit                            bool
	tracks                              int
	length                              time.Duration
	pal                                 Palette
}

func albumOf(p *store.AlbumPage) albumFacts {
	h := p.Hit
	f := albumFacts{title: h.Name, artist: h.ArtistName, artwork: h.ArtworkURL, genre: h.Genre, traits: h.AudioTraits,
		explicit: h.ContentRating == "explicit", tracks: h.TrackCount, pal: hitPalette(h)}
	date := h.ReleaseDate
	if p.Status == store.SearchReady {
		al := p.Album
		f.title = firstNonEmptyStr(al.Title, f.title)
		f.artist = firstNonEmptyStr(al.ArtistName, f.artist)
		f.artwork = firstNonEmptyStr(al.ArtworkURL, f.artwork)
		f.genre = firstNonEmptyStr(al.Genre, f.genre)
		date = firstNonEmptyStr(al.ReleaseDate, date)
		if len(al.AudioTraits) > 0 {
			f.traits = al.AudioTraits
		}
		f.explicit = f.explicit || al.ContentRating == "explicit"
		f.tracks = len(al.Tracks)
		for _, t := range al.Tracks {
			f.length += time.Duration(t.DurationMS) * time.Millisecond
		}
		if pal := ParsePalette(al.ArtworkBgColor, al.ArtworkTextColor1, al.ArtworkTextColor2, al.ArtworkTextColor3, al.ArtworkTextColor4); pal.OK {
			f.pal = pal
		}
	}
	if len(date) >= 4 {
		f.year = date[:4]
	}
	f.title = firstNonEmptyStr(f.title, "Album")
	return f
}

// heroStat is one figure of the head's band: its name over its value,
// smaller in a compact head, where four figures share less room.
func heroStat(c *ui.Context, k *Tokens, hc heroColors, l heroLayout, name, value, unit string) ui.Element {
	size := float32(22)
	if l.compact {
		size = 18
	}
	return ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
		label(c, k, name).TextColor(hc.meta.Alpha(0.8))
		ui.Row(c).AlignItems(ui.Baseline).Children(func() {
			mono(c, value, size).FontWeight(600).TextColor(hc.title).SingleLine().Ellipsis("…").Shrink(1)
			if unit != "" {
				mono(c, unit, 14).TextColor(hc.meta.Alpha(0.8))
			}
		})
	})
}

// trackMark is a song's mark and, while a job of its album holds it, the
// job's track.
type trackMark struct {
	mark store.MarkState
	item *domain.JobItem
}

// lit reports whether the song's row is highlighted, as a download's
// track: its job works on it now.
func (m trackMark) lit() bool { return m.item != nil && trackLit(m.item) }

// trackLine is one song of the album: its number, title, and under it what
// its job is doing with it, else the artists when they are not the album's,
// else what the job made of it; its stage bar while bar is wide, its
// length, and its state: the job's for a song its album's job holds, else
// a check for one on disk, the queue's mark for one on its way, else a
// download of the song alone while the row is pointed at.
func (a *App) trackLine(c *ui.Context, k *Tokens, t client.AlbumTrack, albumArtist string, albumMark store.MarkState, m trackMark, nextLit bool, accent ui.Color, bar float32, still bool) {
	s := a.Store
	// A job just started has not listed its tracks yet: they wait with it.
	queued := albumMark == store.MarkActive && m.item == nil && m.mark == store.MarkNone
	th := store.TrackHit(t)
	since, starting := s.Search.Starting[store.HitKey(th)]
	wait := waited(c, starting, since)
	lit := m.lit()
	line := k.Line
	if lit || nextLit {
		line = ui.Transparent
	}
	ui.Column(c.Key(t.ID)).PaddingY(trackInset).BorderWidth(0, 0, 1, 0).BorderColor(line).Children(func() {
		row := ui.Row(c).Gap(16).Padding(trackPadY, 4).AlignItems(ui.Center).Radius(radiusControl)
		// Only a row that downloads lights under the pointer.
		pointed := row.Hovered()
		switch {
		case lit:
			row.Background(k.AccentSoft.Alpha(0.45))
		case pointed && m.mark == store.MarkNone && m.item == nil && !wait && !queued:
			row.Background(k.Fg.Alpha(0.055))
		}
		row.Children(func() {
			num := k.Faint
			if m.mark == store.MarkDone {
				num = k.Muted
			}
			mono(c, fmt.Sprint(t.TrackNumber), 12).TextColor(num).Width(trackNumber).TextAlign(ui.End)
			sub := ""
			switch it := m.item; {
			case it != nil && (lit || it.Status == domain.ItemFailed || it.Attempt > 1) && trackSub(it) != "":
				sub = trackSub(it)
			case t.ArtistName != "" && t.ArtistName != albumArtist:
				sub = t.ArtistName
			case it != nil:
				sub = trackSub(it)
			}
			trackNames(c, k, t.Name, t.ContentRating == "explicit", sub)
			stageCell(c, k, m.item, accent, bar, still)
			trackDuration(c, k, t.DurationMS)
			ui.Row(c).Width(trackStateWidth).Height(trackControl).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				switch {
				case m.item != nil:
					trackState(c, k, m.item)
				case m.mark == store.MarkDone:
					stateWord(c, icCheck, "On disk", k.OK)
				case queued || m.mark == store.MarkActive && waiting(m.item):
					stateWord(c, icClock, "Waiting", k.Muted)
				case m.mark == store.MarkActive:
					stateWord(c, icDown, "In the queue", k.Accent)
				case wait:
					ui.Text(c, "…").FontSize(13).FontWeight(700).TextColor(k.Muted)
				default:
					a.trackDownload(c, k, th, pointed)
				}
			})
		})
	})
}

// trackNames is a row's title and, under it, sub; a row without one
// centers its title in the same height, which lines it up with its number
// and keeps the rows' rhythm.
func trackNames(c *ui.Context, k *Tokens, title string, explicit bool, sub string) {
	ui.Column(c).Grow(1).Shrink(1).MinWidth(0).MinHeight(trackNamesHeight).Gap(2).Justify(ui.Center).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontWeight(600).SingleLine().Ellipsis("…").Shrink(1)
			if explicit {
				explicitBadge(c, k)
			}
		})
		if sub != "" {
			mono(c, sub, 11).TextColor(k.Muted).Ellipsis("…")
		}
	})
}

// trackDuration is a song's length in its column; ms is 0 where it is not
// known.
func trackDuration(c *ui.Context, k *Tokens, ms int) {
	length := ""
	if ms > 0 {
		length = format.Duration(time.Duration(ms) * time.Millisecond)
	}
	mono(c, length, 12).TextColor(k.Muted).Width(trackLength).TextAlign(ui.End)
}

// stateWord is a state in the track lists' state column: its icon and word
// in col, as trackState draws a job's.
func stateWord(c *ui.Context, ic *ui.SVG, text string, col ui.Color) {
	ui.Icon(c, ic).FontSize(13).TextColor(col)
	ui.Text(c, text).FontSize(12).FontWeight(600).TextColor(col).SingleLine()
}

// waiting reports a track of a running job that has not started yet; nil
// is a song downloaded alone, whose job's tracks the screen does not read.
func waiting(it *domain.JobItem) bool {
	return it != nil && (it.Status == domain.ItemQueued || it.Status == domain.ItemResolving || it.Status == domain.ItemWaitingDownload)
}

// trackDownload downloads one song of the album alone; it shows while its
// row is pointed at.
func (a *App) trackDownload(c *ui.Context, k *Tokens, th client.SearchHit, shown bool) {
	b := ui.ButtonBase(c.Key("download")).Size(trackControl, trackControl).Radius(radiusFull).Center().Label("Download " + th.Name)
	if b.Hovered() {
		b.Background(k.Track)
	} else {
		b.Background(k.Raise2)
	}
	show := float32(0)
	if shown {
		show = 1
	}
	op := b.AnimateWith("shown", show, 120*time.Millisecond, ui.EaseOut)
	b.Opacity(op).Hidden(op < 0.01)
	if b.Clicked() {
		a.download(th)
	}
	b.Cursor(ui.CursorPointer).Children(func() { ui.Icon(c, icDown).FontSize(14).TextColor(k.Fg) })
}
