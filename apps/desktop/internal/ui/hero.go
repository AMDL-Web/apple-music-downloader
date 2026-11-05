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

// pageHero is the head's layout on a page width wide: compact, keeping
// room for the tracks under a smaller head, exactly when the queue's hero
// is (App.heroCompact), so a title flying from the one to the other keeps
// its scale. The cover is as tall as the column beside it with a title of
// two lines, so that its top and bottom are the column's whatever the
// title.
func pageHero(page float32, compact bool) heroLayout {
	l := heroLayout{cover: pageCover, title: 52, title2: 36, top: 26, gap: 32}
	if compact {
		l = heroLayout{cover: 216, title: 34, title2: 26, top: 12, gap: 24, compact: true}
	}
	l.width = page - 2*heroPad - l.cover - l.gap
	return l
}

// heroPad is the room left and right of the head, and heroBottom under
// it; pageCover is the head's cover at its largest.
const heroPad, heroBottom, pageCover = 32, 22, 248

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
// (heroLayout.titleSize). The cover flies in from what the page opened
// from (flyPage), the label swaps with its match (flyLabel), the title and
// artists morph (flyText), and the quality tag as one box (flyTags); the
// band rises in.
func (a *App) collectionHero(c *ui.Context, k *Tokens, l heroLayout, h heroSpec) {
	hc := h.colors
	ui.Row(c).Padding(l.top, heroPad, heroBottom, heroPad).Gap(l.gap).AlignItems(ui.Start).Children(func() {
		a.flyPage(h.cover(l.cover), partCover, "", radiusCover)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).MinHeight(l.cover).Gap(12).Justify(ui.SpaceBetween).Children(func() {
			ui.Column(c).Gap(12).Children(func() {
				a.flyLabel(label(c, k, h.kicker).TextColor(hc.label), h.kicker, hc.label)
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
								quality = a.qualityTag(c, k, h.quality, h.hi, hc.meta)
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
				a.flyBand(ui.Column(c).Gap(12).Children(h.band))
			}
		})
	})
}

// qualityTag is the head's quality tag, keyed by what it says: when that
// changes on a page at rest, as the album's own hint gives way to its
// stream's spec once the run knows it (specKnown), the old tag goes and the
// new one comes as a switch's content does (switchOut, switchIn), not at
// once. In a flight the tag is the flight's (flyTags).
func (a *App) qualityTag(c *ui.Context, k *Tokens, q string, hi bool, fg ui.Color) ui.Element {
	el := tag(c.Key("quality:"+q), k, q, hi, fg)
	if a.PageMotion && !a.fly.moving {
		el.Transition(swapMotion(el, fadeOut))
	}
	return el
}

// barMotion is the head's progress bar's Transition: it opens under the
// figures as a download of the page's record starts, fading in and rising
// as the room it takes opens, and closes the same way, in a switch's time.
func (a *App) barMotion(el ui.Element) ui.ElementTransition {
	return swapMotion(el, barGone)
}

// barGone is the head's bar where it is not: no room, risen a little, clear.
var barGone = ui.Motion{Y: 6, Collapse: true}

// swapMotion is the Transition of el, a part of a page at rest that comes
// and goes as what the page holds changes, with gone where it is not: one
// new in this frame comes in with switchIn, once what it replaces has gone
// with switchOut.
func swapMotion(el ui.Element, gone ui.Motion) ui.ElementTransition {
	if fresh(el) {
		return ui.ElementTransition{Animation: &switchIn, Enter: &gone, Exit: &gone}
	}
	return ui.ElementTransition{Animation: &switchOut, Exit: &gone}
}
