package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/format"
)

// wallTile is a cover's size in the row of recent downloads, wallCompact
// its size in a narrow window, and wallGap the space between covers.
const wallTile, wallCompact, wallGap = 64, 44, 12

// latestHero shows the download that finished last while nothing
// downloads, where the "downloading now" hero goes and in its colors: its
// motion cover, and how much landed where the percent would be.
func (a *App) latestHero(c *ui.Context, k *Tokens, j *store.Job, compact bool) {
	a.Store.WantTracks(j)
	size := float32(heroCover)
	var video *ui.VideoPlayer
	if compact {
		size = heroCoverCompact
	} else {
		video = a.motionPlayer(c, j.MotionArtworkURL)
	}
	pal := jobPalette(j, video != nil)
	hc := a.heroInk(c, pal, k)
	ui.Row(c).Padding(18, 28, 20, 28).Gap(28).AlignItems(ui.End).Children(func() {
		a.heroCover(c, j, func() ui.Element {
			return widgets.Cover{Image: a.Art.Get(j.ArtworkURL, int(size*2)), Placeholder: pal.Bg, Size: size, Radius: radiusCover, Color: 1, Shadow: 1, Video: video}.Show(c)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(10).Children(func() {
			when := ""
			if j.FinishedAt != nil {
				when = " · finished " + dayClock(*j.FinishedAt, c.Now())
			}
			a.heroNames(c, k, j, "Latest download"+when, hc, compact)
		})
		if compact {
			return
		}
		ui.Column(c).AlignItems(ui.End).Gap(10).Children(func() {
			if b := j.Bytes(); b > 0 {
				num, unit, _ := strings.Cut(format.Bytes(b), " ")
				ui.Row(c).Gap(4).AlignItems(ui.Baseline).Children(func() {
					display(c, num, 52).LetterSpacing(-1.5).TextColor(hc.title).FontFeatures("tnum")
					ui.Text(c, unit).FontSize(20).FontWeight(700).TextColor(hc.meta)
				})
			}
			ui.Row(c).Gap(8).Children(func() {
				dir := a.revealDir(j)
				details := func(b ui.Element) {
					if b.Clicked() {
						a.openingFrom(heroFrom(j))
						a.OpenJob(j.ID)
					}
				}
				if dir == "" {
					details(artButton(c, "Details", hc.title, hc.ink))
					return
				}
				details(button(c, k, buttonOnArt, "Details", nil))
				if artButton(c, revealLabel, hc.title, hc.ink).Clicked() {
					a.Act.Reveal(dir)
				}
			})
		})
	})
}

// recentWall is the downloads that finished before the hero's, newest
// first, as one row of covers as wide as the screen. A cover opens its
// page, and its motion cover plays under the pointer.
func (a *App) recentWall(c *ui.Context, k *Tokens, finished []*store.Job, hero *store.Job, compact bool) {
	// A download run again shows its cover once.
	seen := map[string]bool{}
	if hero != nil {
		seen[coverKey(hero)] = true
	}
	var jobs []*store.Job
	for _, j := range finished {
		if key := coverKey(j); !seen[key] {
			seen[key] = true
			jobs = append(jobs, j)
		}
	}
	if len(jobs) == 0 {
		return
	}
	size, side := float32(wallTile), float32(232)
	if compact {
		size, side = wallCompact, 60
	}
	w, _ := c.Size()
	n := min(len(jobs), max(1, int((w-side-56+wallGap)/(size+wallGap))))
	shown := n
	if hero != nil {
		shown++
	}
	ui.Column(c).Padding(0, 28, 10, 28).Gap(8).Children(func() {
		// The covers need no title; the count says how many more there are.
		if done := a.Store.Count(store.TabDone); done > shown {
			ui.Row(c).Children(func() {
				ui.Spacer(c)
				mono(c, fmt.Sprintf("%d downloaded", done), 11).TextColor(k.Muted)
			})
		}
		ui.Row(c).Gap(wallGap).Padding(2, 0, 4, 0).Children(func() {
			for _, j := range jobs[:n] {
				a.wallCover(c, k, j, size)
			}
		})
	})
}

func coverKey(j *store.Job) string { return j.Type + "\x00" + j.Name() + "\x00" + j.By() }

// wallCover is one cover of the recent row, lifted and playing its motion
// cover while hovered, and while the album page flies from or back to it,
// so that the cover landing here goes on playing, then fades out.
func (a *App) wallCover(c *ui.Context, k *Tokens, j *store.Job, size float32) {
	t := ui.ButtonBase(c.Key(j.ID)).Size(size, size).Label(j.Name())
	hover := t.Hovered()
	lift := t.AnimateTo("lift", map[bool]float32{true: 1, false: 0}[hover], ui.Snappy)
	if t.Clicked() {
		a.openingFrom("wall:" + j.ID)
		a.OpenJob(j.ID)
	}
	var video *ui.VideoPlayer
	if hover || (a.fly.moving && a.fly.from == "wall:"+j.ID) {
		video = a.motionPlayer(c, j.MotionArtworkURL)
	}
	t.Cursor(ui.CursorPointer).Children(func() {
		frame := ui.Box(c).Size(size, size).Radius(radiusControl).Scale(1+0.06*lift, 1+0.06*lift)
		// A lifted cover's shadow is a hero cover's at its size, which the
		// page's cover keeps as it leaves.
		a.flySourceCover(frame, "wall:"+j.ID, radiusControl, 1, 0, lift)
		if lift > 0.01 {
			frame.Shadow(0, 10*lift, 24*lift, 0, ui.RGBA(0, 0, 0, 0.45*lift))
		}
		cover := func() {
			widgets.Cover{Image: a.Art.Get(j.ArtworkURL, int(size*2)), Placeholder: jobPalette(j, false).Bg, Size: size, Radius: radiusControl, Color: 1, Video: video}.Show(c)
		}
		frame.Children(cover)
		if f := &a.fly; f.moving && f.ready && !f.grow && strings.HasPrefix(f.from, "hero:") && lift <= 0.01 {
			// Its copy over the page stands for it (flightCopies), once
			// the page shows: before, the page is laid out hidden, and
			// its copy with it.
			f.wall = append(f.wall, flightWall{box: frame.Bounds(), build: cover})
			frame.Opacity(0)
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
