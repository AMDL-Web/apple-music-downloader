package ui

import (
	"fmt"
	"strings"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/domain"
	"amdl/internal/format"
	"github.com/egoist/mygo/ui"
)

// hero is the band of the download under way: the cover, the album in big
// type, its stage bar and the percent, all in the album's colors.
func (a *App) hero(c *ui.Context, k *Tokens, j *store.Job, compact bool) {
	size := float32(heroCover)
	var video *ui.VideoPlayer
	if compact {
		size = heroCoverBarred
	} else {
		video = a.motionPlayer(c, j.MotionArtworkURL)
	}
	pal := jobPalette(j, video != nil)
	hc := a.heroInk(c, pal, k)
	ui.Row(c).Padding(18, 28, 24, 28).Gap(28).AlignItems(ui.End).Children(func() {
		a.heroCover(c, j, func() ui.Element {
			return widgets.Cover{Image: a.Art.Get(j.ArtworkURL, int(size*2)), Placeholder: pal.Bg, Size: size, Radius: radiusCover, Color: 1, Shadow: 1, Video: video}.Show(c)
		})
		// A compact cover lines up with the words beside it at the top and
		// the bottom: they spread over its height.
		names := ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(10)
		if compact {
			names.Height(size).Justify(ui.SpaceBetween)
		}
		names.Children(func() {
			a.heroNames(c, k, j, "", hc, compact)
			ui.Column(c).MaxWidth(600).Gap(6).Margin(6, 0, 0, 0).Children(func() {
				widgets.StageBar{Stages: j.Stages, Color: hc.accent, Track: k.Track, Height: 10, Gap: 3, Running: true, Still: a.scrolling(c)}.Show(c, "hero-bar")
				ui.Row(c).Children(func() {
					mono(c, a.heroProgressText(j), 11.5).TextColor(hc.meta)
					ui.Spacer(c)
					mono(c, a.heroRateText(c, j), 11.5).TextColor(hc.meta)
				})
			})
		})
		// A narrow page has no room for the percent and the buttons: the
		// cover opens the page, and the row's menu pauses.
		if a.compact {
			return
		}
		ui.Column(c).AlignItems(ui.End).Gap(10).Children(func() {
			// The number keeps the room of three digits, so reaching 100
			// takes nothing from the title beside it.
			ui.Box(c).Children(func() {
				percent := func(digits string, shown bool) {
					row := ui.Row(c).AlignItems(ui.Baseline)
					if !shown {
						row.Invisible()
					} else {
						row.Absolute().Top(0).Right(0)
					}
					row.Children(func() {
						display(c, digits, 64).LetterSpacing(-1.9).TextColor(hc.title).FontFeatures("tnum")
						ui.Text(c, "%").FontSize(26).FontWeight(700).TextColor(hc.meta)
					})
				}
				percent("100", false)
				percent(fmt.Sprint(j.Percent()), true)
			})
			ui.Row(c).Gap(8).Children(func() {
				if button(c, k, buttonOnArt, cancelWord(j), nil).Clicked() {
					a.Store.Cancel(j.ID)
				}
				if artButton(c, "Details", hc.title, hc.ink).Clicked() {
					a.openingFrom(heroFrom(j))
					a.OpenJob(j.ID)
				}
			})
		})
	})
}

// The queue hero's cover and title, each at its size in a narrow window.
// heroCoverBarred is the compact cover of the hero with a stage bar under
// its names, as tall as they are on the systems' fonts with room to spare:
// they spread over its height.
const (
	heroCover, heroCoverCompact = 196, 88
	heroCoverBarred             = 132
	heroTitle, heroTitleCompact = 42, 24
	heroBy                      = 15
)

// heroFrom names the queue hero showing j, which the album page opens from
// (openingFrom).
func heroFrom(j *store.Job) string { return "hero:" + j.ID }

// heroInk is the queue hero's text colors over pal, easing from the last
// palette while the queue is the screen shown, not the one the album page
// moves over.
func (a *App) heroInk(c *ui.Context, pal Palette, k *Tokens) heroColors {
	hc := heroColorsFor(pal, k)
	if a.Screen == ScreenQueue {
		hc = hc.eased(c)
	}
	return hc
}

// heroNames are the queue hero's label, if it has one, title, artists and
// tags, which fly to the album page's as it opens from the hero
// (flySource).
func (a *App) heroNames(c *ui.Context, k *Tokens, j *store.Job, kicker string, hc heroColors, compact bool) {
	from := heroFrom(j)
	if kicker != "" {
		a.flySourceLabel(label(c, k, kicker).TextColor(hc.label), from, partKicker, kicker, hc.label)
	}
	size, lines := float32(heroTitle), 2
	if compact {
		size, lines = heroTitleCompact, 1
	}
	a.flySourceText(c, func() ui.Element {
		return display(c, j.Name(), size).TextColor(hc.title).MaxLines(lines).Ellipsis("…")
	}, displaySpan(j.Name(), size), from, partTitle, hc.title)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		a.flySourceText(c, func() ui.Element {
			return ui.Text(c, j.By()).FontSize(heroBy).TextColor(hc.meta).SingleLine().Shrink(1)
		}, ui.Span{Text: j.By(), Size: heroBy}, from, partArtist, hc.meta)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			if q := jobQuality(j); q != "" {
				a.flySourceTag(tag(c, k, q, isHiRes(q), hc.meta), from, q, isHiRes(q), hc.meta)
			}
			if j.TotalItems > 0 {
				n := plural(j.TotalItems, "track")
				// The page has no place for it: it fades with the queue.
				tag(c, k, n, false, hc.meta)
			}
		})
	})
}

func (a *App) heroProgressText(j *store.Job) string {
	parts := []string{fmt.Sprintf("%d of %s", j.Done(), plural(j.TotalItems, "track"))}
	var down, dec int
	for _, it := range j.Items {
		switch it.Status {
		case domain.ItemDownloading:
			down++
		case domain.ItemDecrypting:
			dec++
		}
	}
	if down > 0 {
		parts = append(parts, fmt.Sprintf("downloading %d", down))
	}
	if dec > 0 {
		parts = append(parts, fmt.Sprintf("decrypting %d", dec))
	}
	if j.TotalItems == 0 {
		return "resolving the link"
	}
	return strings.Join(parts, " · ")
}

func (a *App) heroRateText(c *ui.Context, j *store.Job) string {
	var parts []string
	if sp := j.ShownSpeed(c.Now()); sp > 0 {
		parts = append(parts, "avg "+format.Rate(sp))
	}
	if r := format.Remaining(j.Remaining); r != "" {
		parts = append(parts, r+" left")
	}
	return strings.Join(parts, " · ")
}

// heroCover makes a hero's cover open the job's detail, as its Details
// button does, lifting a little under the pointer; the cover flies to the
// album page's. Another job's cover is another element, which takes
// nothing over from this one, as its motion cover fading out.
func (a *App) heroCover(c *ui.Context, j *store.Job, show func() ui.Element) {
	b := ui.ButtonBase(c.Key("hero-cover:" + j.ID)).Label("Open " + j.Name())
	lift := b.AnimateTo("lift", map[bool]float32{true: 1, false: 0}[b.Hovered()], ui.Snappy)
	if b.Clicked() {
		a.openingFrom(heroFrom(j))
		a.OpenJob(j.ID)
	}
	b.Cursor(ui.CursorPointer).Scale(1+0.02*lift, 1+0.02*lift).Children(func() {
		a.flySourceCover(show(), heroFrom(j), radiusCover, 1, 0, 1)
	})
}
