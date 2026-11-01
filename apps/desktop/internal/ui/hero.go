package ui

import (
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// heroLayout is the head of the album screen: the cover's side, the title's
// size, the room above and between.
type heroLayout struct {
	cover, title, top, gap float32
	compact                bool
}

// pageHero is the head's layout on a page width wide: a narrow or short
// window keeps room for the tracks under a smaller head.
func pageHero(c *ui.Context, page float32) heroLayout {
	_, height := c.Size()
	if page < 960 || height < 700 {
		return heroLayout{cover: 148, title: 34, top: 12, gap: 24, compact: true}
	}
	return heroLayout{cover: 248, title: 52, top: 26, gap: 32}
}

// heroActions is the least width of the head's buttons: its widest,
// "Download again" or "Reveal in Finder".
const heroActions = 136

// heroArtist is the size of the head's artists.
const heroArtist = 17

// titleFor is the title's size: smaller as a title runs longer, so most fit
// two lines.
func (l heroLayout) titleFor(title string) float32 {
	switch n := utf8.RuneCountInString(title); {
	case n > 48:
		return l.title * 0.7
	case n > 26:
		return l.title * 0.84
	}
	return l.title
}

// heroSpec is what one page puts in the head: the cover drawn at the
// layout's side, the label over the title, the title, the artists, the
// tags after them, what the page says under them (band) and its buttons
// at the end (actions).
type heroSpec struct {
	cover         func(size float32) ui.Element
	kicker, title string
	artist        string
	tags, band    func()
	actions       func()
	colors        heroColors
}

// collectionHero is the head of the album screen: the cover, then the
// label, title and artists at the cover's top and the page's band at its
// bottom, then its buttons at the bottom. A title on more lines than the
// room beside the cover takes makes the head taller below the cover,
// never above it. The cover, label, title, artists and tags fly in from
// what the page opened from (flyPage), the rest rises in.
func (a *App) collectionHero(c *ui.Context, k *Tokens, l heroLayout, h heroSpec) {
	hc := h.colors
	ui.Row(c).Padding(l.top, 32, 22, 32).Gap(l.gap).AlignItems(ui.Start).Children(func() {
		a.flyPage(h.cover(l.cover), partCover, "", radiusCover)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).MinHeight(l.cover).Gap(12).Justify(ui.SpaceBetween).Children(func() {
			ui.Column(c).Gap(12).Children(func() {
				a.flyPage(label(c, k, h.kicker).TextColor(hc.label), partKicker, h.kicker, 11)
				size := l.titleFor(h.title)
				a.flyPage(display(c, h.title, size).TextColor(hc.title).MaxLines(2).Ellipsis("…"), partTitle, h.title, size)
				ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
					if h.artist != "" {
						// In the queue hero's colors as it flies from there.
						mix := float32(0.4)
						if a.fly.moving {
							mix = 1 - 0.6*a.fly.q
						}
						artist := ui.Text(c, h.artist).FontSize(heroArtist).TextColor(hc.title.Mix(hc.meta, mix)).SingleLine().Ellipsis("…").Shrink(1)
						a.flyPage(artist, partArtist, h.artist, heroArtist)
					}
					if h.tags != nil {
						a.flyPage(ui.Row(c).Gap(10).AlignItems(ui.Center).Children(h.tags), partTags, "", 0)
					}
				})
			})
			if h.band != nil {
				a.flyRise(ui.Column(c).Gap(12).Children(h.band))
			}
		})
		if h.actions != nil {
			// One width whatever the buttons, so the band beside them ends
			// at the same place whatever the page's state.
			a.flyRise(ui.Column(c).MinWidth(heroActions).Gap(8).AlignItems(ui.End).AlignSelf(ui.End).Children(h.actions))
		}
	})
}
