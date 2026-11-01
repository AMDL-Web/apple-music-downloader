package ui

import (
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// heroLayout is the head of the album screen: the cover's side, the
// title's size on one line and on two (title2), the room above and
// between, and the width of the column beside the cover.
type heroLayout struct {
	cover, title, title2, top, gap float32
	width                          float32
	compact                        bool
}

// pageHero is the head's layout on a page width wide: a narrow or short
// window keeps room for the tracks under a smaller head. The cover is as
// tall as the column beside it with a title of two lines, so that its top
// and bottom are the column's whatever the title.
func pageHero(c *ui.Context, page float32) heroLayout {
	_, height := c.Size()
	l := heroLayout{cover: 248, title: 52, title2: 36, top: 26, gap: 32}
	if page < 960 || height < 700 {
		l = heroLayout{cover: 216, title: 34, title2: 26, top: 12, gap: 24, compact: true}
	}
	l.width = page - 2*heroPad - l.cover - l.gap
	return l
}

// heroPad is the room left and right of the head, and heroBottom under
// it.
const heroPad, heroBottom = 32, 22

// height is the head's: its column never runs taller than the cover
// (titleSize).
func (l heroLayout) height() float32 { return l.top + l.cover + heroBottom }

// heroArtist is the size of the head's artists.
const heroArtist = 17

// titleFor is the title's size on one line: smaller as a title runs
// longer.
func (l heroLayout) titleFor(title string) float32 {
	switch n := utf8.RuneCountInString(title); {
	case n > 48:
		return l.title * 0.7
	case n > 26:
		return l.title * 0.84
	}
	return l.title
}

// titleSize is the title's size: titleFor's while the title fits one line
// beside the cover, else title2's, at which two lines fit the column.
// Only the title changes as a window's width takes it from one line to
// two; the cover and the figures stay.
func (l heroLayout) titleSize(c *ui.Context, title string) float32 {
	s := l.titleFor(title)
	if w, _ := c.MeasureText(0, displaySpan(title, s)); w <= l.width {
		return s
	}
	return min(s, l.title2)
}

// heroSpec is what one page puts in the head: the cover drawn at the
// layout's side, the label over the title, the title, the artists, the
// tags after them, the quality's first, gilt when hi, and the rest from
// tags, and what the page says under them (band). Its buttons are in the
// title bar (pageActions).
type heroSpec struct {
	cover         func(size float32) ui.Element
	kicker, title string
	artist        string
	quality       string
	hi            bool
	tags, band    func()
	colors        heroColors
}

// collectionHero is the head of the album screen: the cover, then the
// label, title and artists at the cover's top and the page's band at its
// bottom, the column as tall as the cover whatever the title
// (heroLayout.titleSize). The cover and label fly in from what the page
// opened from (flyPage), the title and artists morph (flyText), and the
// quality tag as one box (flyTags); the band rises in.
func (a *App) collectionHero(c *ui.Context, k *Tokens, l heroLayout, h heroSpec) {
	hc := h.colors
	ui.Row(c).Padding(l.top, heroPad, heroBottom, heroPad).Gap(l.gap).AlignItems(ui.Start).Children(func() {
		a.flyPage(h.cover(l.cover), partCover, "", radiusCover)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).MinHeight(l.cover).Gap(12).Justify(ui.SpaceBetween).Children(func() {
			ui.Column(c).Gap(12).Children(func() {
				a.flyPage(label(c, k, h.kicker).TextColor(hc.label), partKicker, h.kicker, 11)
				size := l.titleSize(c, h.title)
				a.flyText(c, display(c, h.title, size).TextColor(hc.title).MaxLines(2).Ellipsis("…"), partTitle, displaySpan(h.title, size), hc.title, l.width)
				ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
					if h.artist != "" {
						ink := hc.title.Mix(hc.meta, 0.4)
						a.flyText(c, ui.Text(c, h.artist).FontSize(heroArtist).TextColor(ink).SingleLine().Ellipsis("…").Shrink(1), partArtist, ui.Span{Text: h.artist, Size: heroArtist}, ink, l.width)
					}
					if h.quality != "" || h.tags != nil {
						var quality ui.Element
						row := ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
							if h.quality != "" {
								quality = tag(c, k, h.quality, h.hi, hc.meta)
							}
							if h.tags != nil {
								rest := ui.Row(c).Gap(10).AlignItems(ui.Center).Children(h.tags)
								if a.tagFlies() {
									a.flyFade(rest)
								}
							}
						})
						a.flyTags(row, quality, h.quality, h.hi, hc.meta)
					}
				})
			})
			if h.band != nil {
				a.flyRise(ui.Column(c).Gap(12).Children(h.band))
			}
		})
	})
}
