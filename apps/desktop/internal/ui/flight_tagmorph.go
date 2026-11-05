package ui

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// tagPiece is a run of a tag's text: a word (letters, digits and dots, as
// "ALAC", "24" or "96") or what is between two, at rune offset at.
type tagPiece struct {
	text string
	at   int
	word bool
	// match is the index of the same word, or of the separator in the same
	// place (matchTagSeps), in the other tag, -1 for none.
	match int
}

func tagPieces(s string) []tagPiece {
	var out []tagPiece
	at := 0
	for len(s) > 0 {
		r, _ := utf8.DecodeRuneInString(s)
		word := isTagWord(r)
		n := 0
		for i, q := range s {
			if isTagWord(q) != word {
				break
			}
			n = i + utf8.RuneLen(q)
		}
		out = append(out, tagPiece{text: s[:n], at: at, word: word, match: -1})
		at += utf8.RuneCountInString(s[:n])
		s = s[n:]
	}
	return out
}

func isTagWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' }

// matchTagWords pairs the words two tags share, in their order: "ALAC
// 24/96" and "ALAC 24-bit · 96 kHz" share ALAC, 24 and 96.
func matchTagWords(a, b []tagPiece) {
	from := 0
	for i := range a {
		if !a[i].word {
			continue
		}
		for j := from; j < len(b); j++ {
			if b[j].word && b[j].text == a[i].text {
				a[i].match, b[j].match, from = j, i, j+1
				break
			}
		}
	}
}

// matchTagSeps pairs the separators that stand in the same place between
// two words paired in order: the last one before the second word in each
// tag, when both draw something. "/" in "24/96" pairs with " · " in
// "24-bit · 96", which both lead into 96; "-" binds "bit" to 24 and stays
// the page's own.
func matchTagSeps(a, b []tagPiece) {
	last := func(ps []tagPiece, from, to int) int {
		for j := to - 1; j > from; j-- {
			if !ps[j].word {
				if strings.TrimSpace(ps[j].text) == "" {
					return -1
				}
				return j
			}
		}
		return -1
	}
	prev := -1
	for i := range a {
		if !a[i].word || a[i].match < 0 {
			continue
		}
		if prev >= 0 {
			if si, sj := last(a, prev, i), last(b, a[prev].match, a[i].match); si >= 0 && sj >= 0 {
				a[si].match, b[sj].match = sj, si
			}
		}
		prev = i
	}
}

// tagMorph builds the text of a tag morphing from src's text to dst's, t
// of the way, in ink: the words both say move from their place in the one
// to their place in the other, and a separator paired with the other's
// (matchTagSeps) moves likewise, its glyph fading into the other's on t;
// what only one says sits between the words around it, spread over the
// room they leave as they move, fading out or in, and units after the last
// word come with it, so those the page adds grow out of the box as it
// widens. Places are measured in the tag's own type.
func tagMorph(c *ui.Context, from, to string, t float32, ink ui.Color) {
	a, b := tagPieces(from), tagPieces(to)
	matchTagWords(a, b)
	matchTagSeps(a, b)
	measure := func(s string) float32 {
		w, _ := c.MeasureText(0, ui.Span{Text: s, Font: MonoFont, Size: 10, Weight: 600, Features: "tnum"})
		return w
	}
	// A mark after the runes keeps a space they end in measured.
	bar := measure("|")
	width := func(s string) float32 { return measure(s+"|") - bar }
	// x is where piece i of ps begins in its own tag, text.
	x := func(ps []tagPiece, text string, i int) float32 { return width(string([]rune(text)[:ps[i].at])) }
	// place is where piece i of ps, which says own, is t of the way from
	// src's text to dst's (src tells which ps is).
	place := func(ps, other []tagPiece, own, theirs string, i int, src bool) float32 {
		at := func(j int) float32 {
			here, there := x(ps, own, j), x(other, theirs, ps[j].match)
			if src {
				return lerp(here, there, t)
			}
			return lerp(there, here, t)
		}
		before, after := -1, -1
		for j := i - 1; j >= 0 && before < 0; j-- {
			if ps[j].word && ps[j].match >= 0 {
				before = j
			}
		}
		for j := i + 1; j < len(ps) && after < 0; j++ {
			if ps[j].word && ps[j].match >= 0 {
				after = j
			}
		}
		p := x(ps, own, i)
		switch {
		case before >= 0 && after >= 0:
			w := width(ps[before].text)
			from, to := x(ps, own, before)+w, x(ps, own, after)
			now, next := at(before)+w, at(after)
			if to <= from {
				return now
			}
			return now + (p-from)*(next-now)/(to-from)
		case before >= 0:
			return at(before) + p - x(ps, own, before)
		case after >= 0:
			return at(after) - (x(ps, own, after) - p)
		}
		return p
	}
	piece := func(key string, left, op float32, text string) {
		// Spaces only place what they lead; a text of its own that ends in
		// one would end in an ellipsis.
		trimmed := strings.TrimLeft(text, " ")
		left += width(text[:len(text)-len(trimmed)])
		if trimmed = strings.TrimRight(trimmed, " "); trimmed == "" {
			return
		}
		ui.Row(c.Key(key)).Absolute().Left(left).Top(0).Bottom(0).Width(width(trimmed) + 2).AlignItems(ui.Center).Opacity(op).
			Children(func() { tagText(c, trimmed, ink) })
	}
	// gapAt is where the glyph of separator i of ps, which says own, is
	// centered, as a share of the gap between the words around it.
	gapAt := func(ps []tagPiece, own string, i, before, after int) float32 {
		s := ps[i].text
		trimmed := strings.TrimLeft(s, " ")
		c := x(ps, own, i) + width(s[:len(s)-len(trimmed)]) + width(strings.TrimRight(trimmed, " "))/2
		l, r := x(ps, own, before)+width(ps[before].text), x(ps, own, after)
		if r <= l {
			return 0.5
		}
		return (c - l) / (r - l)
	}
	for i, p := range a {
		if p.match >= 0 && !p.word {
			// The gap between the words around it opens or closes as they
			// move, and the separator keeps its share of it, going from
			// its share in the one tag to its share in the other between
			// 0.3 and 0.6: the middle of the gap while it opens, then its
			// place by the time what only the other says shows (from 0.4).
			// Paired words stand on both sides (matchTagSeps).
			before, after := i-1, i+1
			for !a[before].word || a[before].match < 0 {
				before--
			}
			for !a[after].word || a[after].match < 0 {
				after++
			}
			q := b[p.match]
			l := lerp(x(a, from, before)+width(a[before].text), x(b, to, a[before].match)+width(a[before].text), t)
			r := lerp(x(a, from, after), x(b, to, a[after].match), t)
			at := l + (r-l)*lerp(gapAt(a, from, i, before, after), gapAt(b, to, p.match, a[before].match, a[after].match), smoothstep(0.3, 0.6, t))
			for _, g := range []struct {
				key, text string
				op        float32
			}{{"a", p.text, 1 - t}, {"s", q.text, t}} {
				glyph := strings.TrimSpace(g.text)
				piece(g.key+strconv.Itoa(i), at-width(glyph)/2, g.op, glyph)
			}
		} else if p.match >= 0 {
			piece("a"+strconv.Itoa(i), lerp(x(a, from, i), x(b, to, p.match), t), 1, p.text)
		} else {
			piece("a"+strconv.Itoa(i), place(a, b, from, to, i, true), 1-unit(t/0.5), p.text)
		}
	}
	for i, p := range b {
		if p.match < 0 {
			piece("b"+strconv.Itoa(i), place(b, a, to, from, i, false), unit((t-0.4)/0.5), p.text)
		}
	}
}
