package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/client"
	"amdl/apps/desktop/internal/format"
	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/domain"
)

// libraryTabs name the library's tabs, and libraryNouns one item of each,
// in store.LibraryKinds order.
var (
	libraryTabs  = []string{"Albums", "Playlists", "Songs"}
	libraryNouns = []string{"album", "playlist", "song"}
)

// libraryMinTile is the narrowest a library tile gets before the grid
// drops a column; libraryGap spaces the tiles across.
const libraryMinTile, libraryGap = 140, 18

// library is the finished downloads as a grid of covers, the newest one
// large above it on its album's color.
func (a *App) library(c *ui.Context, k *Tokens, compact bool) {
	s := a.Store
	l := &s.Library
	s.ShowLibrary(a.libKind, s.Omni.Query())
	// Read once on entering for the folder's path; the system screen is the
	// one that keeps it fresh.
	s.WatchServer()
	ui.Row(c).Padding(4, 24, 0, 24).Gap(12).AlignItems(ui.Center).Children(func() {
		display(c, "Library", 22).SingleLine()
		segmented(c, k, "lib-kind", &a.libKind, libraryTabs, nil)
		ui.Spacer(c)
		if st := s.Server.DownloadsStorage(); st != nil && !compact {
			if a.Settings.Mode == mode.Standalone {
				if button(c, k, buttonGhost, st.Path+" ↗", nil).Label(revealLabel).Clicked() {
					a.Act.Reveal(st.Path)
				}
			} else {
				ui.Text(c, st.Path).FontSize(12).TextColor(k.Muted).SingleLine().Ellipsis("…").MaxWidth(320)
			}
		}
	})
	if !l.Loaded && a.unreachable(c, k) {
		return
	}
	switch {
	case !l.Loaded && l.Err != "":
		a.libraryMessage(c, k, icCross, "The library could not be read", l.Err, true)
		return
	case !l.Loaded:
		a.libraryLoading(c, k)
		return
	case len(l.Jobs) == 0 && l.Search != "":
		a.libraryMessage(c, k, icSearch, "Nothing matches “"+l.Search+"”", "Search looks at titles, artists and links.", false)
		return
	case len(l.Jobs) == 0:
		a.libraryMessage(c, k, icLibrary, "Nothing here yet", "Finished "+strings.ToLower(libraryTabs[l.Kind])+" show up here.", false)
		return
	}

	w, _ := c.Size()
	side := float32(232)
	if compact {
		side = 60
	}
	width := w - side - 48
	cols := max(2, int((width+libraryGap)/(libraryMinTile+libraryGap)))
	tile := (width - float32(cols-1)*libraryGap) / float32(cols)
	rows := (len(l.Jobs) + cols - 1) / cols
	hero := l.Search == ""
	n := rows + 1
	if hero {
		n++
	}
	ui.List(c.Key(fmt.Sprintf("library-%d", l.Kind)), &a.libList, n, func(i int) {
		if hero {
			if i == 0 {
				a.libraryHero(c, k, l.Jobs[0], l.Latest)
				return
			}
			i--
		}
		if i == rows {
			a.libraryFooter(c, k, l)
			return
		}
		ui.Row(c).Padding(10, 24).Gap(libraryGap).Children(func() {
			for _, j := range l.Jobs[i*cols : min((i+1)*cols, len(l.Jobs))] {
				a.libraryTile(c, k, j, tile)
			}
		})
	}).Grow(1).ItemKey(func(i int) any {
		if hero {
			if i == 0 {
				return "hero"
			}
			i--
		}
		if i == rows {
			return "footer"
		}
		return fmt.Sprintf("%d/%s", cols, l.Jobs[i*cols].ID)
	})
	if a.libList.AtEnd() {
		s.MoreLibrary()
	}
}

// libraryHero shows the newest finished download large, on its album's
// color, as the queue's hero shows the one downloading.
func (a *App) libraryHero(c *ui.Context, k *Tokens, dj domain.Job, d *client.DownloadDetail) {
	j := &store.Job{Job: dj}
	if d != nil && d.Job.ID == dj.ID {
		j.Items = d.Items
	}
	pal := jobPalette(j, false)
	bg := k.Raise
	if pal.OK {
		bg = pal.Bg
	}
	t1, t2, t3 := Readable(pal.Text1, bg, k), Readable(pal.Text2, bg, k), Readable(pal.Text3, bg, k)
	ui.Column(c).Padding(14, 24, 8, 24).Children(func() {
		card := ui.Row(c).Padding(22, 24).Gap(28).Radius(18).Clip().AlignItems(ui.End)
		shown := card.AnimateColor("bg", bg, ui.Smooth)
		// The light falls across the card in its own background: a layer of
		// discs, being absolute, would paint over the cover and the text.
		// Shades this close keep the album's text colors readable.
		card.LinearGradient(ui.LinearGradient{Angle: 115, Stops: []ui.GradientStop{
			{Color: shown.Mix(ui.RGB(255, 255, 255), 0.14), At: 0},
			{Color: shown, At: 0.45},
			{Color: shown.Mix(ui.RGB(0, 0, 0), 0.16), At: 1},
		}}).Children(func() {
			cover := widgets.Cover{Image: a.Art.Get(j.ArtworkURL, 400), Placeholder: bg.Mix(k.Ink, 0.2), Size: 184, Radius: 12, Color: 1, Shadow: true}.Show(c)
			if j.MotionArtworkURL != "" {
				cover.Children(func() { motionBadge(c, "MOTION") })
			}
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(10).Children(func() {
				when := ""
				if j.FinishedAt != nil {
					when = " · finished " + dayClock(*j.FinishedAt, c.Now())
				}
				label(c, k, "Latest"+when).TextColor(t3)
				display(c, j.Name(), 46).TextColor(t1).MaxLines(2).Ellipsis("…")
				facts := []string{j.By()}
				if j.TotalItems > 0 {
					facts = append(facts, plural(j.TotalItems, "track"))
				}
				if q := jobQuality(j); q != "" {
					facts = append(facts, q)
				}
				if b := j.Bytes(); b > 0 {
					facts = append(facts, format.Bytes(b))
				}
				ui.Text(c, strings.Join(facts, " · ")).FontSize(15).TextColor(t2).SingleLine().Ellipsis("…")
				ui.Row(c).Gap(8).Margin(6, 0, 0, 0).Children(func() {
					if a.Settings.Mode == mode.Standalone {
						if dir := savedDir(j); dir != "" && artButton(c, revealLabel, t1, Readable(bg, t1, k)).Clicked() {
							a.Act.Reveal(dir)
						}
					}
					if button(c, k, buttonOnArt, "Details", nil).Clicked() {
						a.OpenJob(j.ID)
					}
				})
			})
		})
	})
}

// libraryTile is one finished download in the grid: its cover, title and
// artist; a click opens its detail.
func (a *App) libraryTile(c *ui.Context, k *Tokens, j domain.Job, size float32) {
	sj := &store.Job{Job: j}
	pal := jobPalette(sj, false)
	t := ui.ButtonBase(c.Key(j.ID)).Width(size).Gap(7).Justify(ui.Start).AlignItems(ui.Stretch).Label(sj.Name())
	hover := t.Hovered()
	lift := t.AnimateTo("lift", map[bool]float32{true: 1, false: 0}[hover], ui.Snappy)
	if t.Clicked() {
		a.OpenJob(j.ID)
	}
	t.Cursor(ui.CursorPointer).Children(func() {
		ui.Column(c).Gap(7).MinWidth(0).Children(func() { a.tileFace(c, k, sj, pal, size, lift) })
	})
}

// tileFace is a library tile's cover, title and artist, the cover lifted
// by lift (0..1) while hovered.
func (a *App) tileFace(c *ui.Context, k *Tokens, j *store.Job, pal Palette, size, lift float32) {
	frame := ui.Box(c).Size(size, size).Radius(10).Scale(1+0.03*lift, 1+0.03*lift)
	if lift > 0.01 {
		frame.Shadow(0, 18*lift, 40*lift, 0, ui.RGBA(0, 0, 0, 0.5*lift))
	}
	frame.Children(func() {
		widgets.Cover{Image: a.Art.Get(j.ArtworkURL, int(size*2)), Placeholder: pal.Bg, Size: size, Radius: 10, Color: 1}.Show(c)
		if lift > 0.01 {
			ui.Box(c).Fill().Radius(10).Absolute().Top(0).Left(0).Border(2, k.Accent.Alpha(lift)).PassThrough()
		}
		if j.MotionArtworkURL != "" {
			ui.Box(c).Size(20, 20).Radius(999).Background(ui.RGBA(10, 12, 20, 0.55)).Absolute().Right(7).Top(7).Center().Children(func() {
				ui.Icon(c, icPlay).FontSize(9).TextColor(ui.Hex("#f2f3f8"))
			})
		}
	})
	ui.Text(c, j.Name()).FontSize(12.5).FontWeight(600).SingleLine().Ellipsis("…")
	ui.Text(c, j.By()).FontSize(11.5).TextColor(k.Muted).SingleLine().Ellipsis("…").Margin(-4, 0, 0, 0)
}

// libraryFooter ends the grid: more loading, a read that failed, or the
// count when everything is shown.
func (a *App) libraryFooter(c *ui.Context, k *Tokens, l *store.Library) {
	ui.Row(c).Padding(12, 24, 28, 24).Justify(ui.Center).Gap(10).AlignItems(ui.Center).Children(func() {
		switch {
		case l.Loading:
			pill(c, k, pillWait, nil, "Loading more…")
		case l.Err != "":
			ui.Text(c, l.Err).FontSize(12).TextColor(k.Bad).SingleLine().Ellipsis("…")
			if button(c, k, buttonPlain, "Try again", nil).Clicked() {
				a.Store.MoreLibrary()
			}
		case len(l.Jobs) < l.Total:
			if button(c, k, buttonPlain, fmt.Sprintf("Show more · %d left", l.Total-len(l.Jobs)), nil).Clicked() {
				a.Store.MoreLibrary()
			}
		default:
			mono(c, plural(l.Total, libraryNouns[l.Kind]), 11).TextColor(k.Faint)
		}
	})
}

// libraryLoading holds the grid's place while the first page loads.
func (a *App) libraryLoading(c *ui.Context, k *Tokens) {
	ui.Column(c).Grow(1).Padding(14, 24).Gap(22).Children(func() {
		skeleton(c, k, 0, 228).Radius(18)
		ui.Row(c).Gap(libraryGap).Children(func() {
			for i := 0; i < 6; i++ {
				ui.Column(c.Key(i)).Grow(1).Gap(7).Children(func() {
					skeleton(c, k, 0, 140).Radius(10)
					skeleton(c, k, 0, 10)
				})
			}
		})
	})
}

// libraryMessage stands in for the grid when there is nothing to show.
func (a *App) libraryMessage(c *ui.Context, k *Tokens, ic *ui.SVG, title, help string, retry bool) {
	ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
		ui.Icon(c, ic).FontSize(26).TextColor(k.Faint)
		ui.Text(c, title).FontSize(15).FontWeight(650)
		ui.Text(c, help).FontSize(12.5).TextColor(k.Muted).MaxWidth(420)
		if retry && button(c, k, buttonPlain, "Try again", nil).Clicked() {
			a.Store.ReloadLibrary()
		}
	})
}

// dayClock says when something happened: "today 10:43", "yesterday
// 22:05", else the date.
func dayClock(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	same := func(a, b time.Time) bool {
		ay, am, ad := a.Date()
		by, bm, bd := b.Date()
		return ay == by && am == bm && ad == bd
	}
	switch {
	case same(t, now):
		return "today " + format.Clock(t)
	case same(t, now.AddDate(0, 0, -1)):
		return "yesterday " + format.Clock(t)
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}
