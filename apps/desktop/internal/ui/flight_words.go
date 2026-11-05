package ui

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// titleLines is the most lines a title morphing word by word may take on
// either side: the queue hero's and the page's titles end in an ellipsis
// past two.
const titleLines = 2

// wordAt is one word of a title as display lays it out in lines: what it
// says with the spaces after it, its x on its line and the line.
type wordAt struct {
	text string
	x    float32
	line int
}

// wordFlight is a title that morphs word by word: its words where its
// match lays them out and where the page does, in the same order.
type wordFlight struct{ src, dst []wordAt }

// titleWords lays text out as display sets it at size in lines at most
// width wide, breaking as the text breaks: after a space, or between two
// characters of a CJK run, which has none. Nil past titleLines lines.
func titleWords(c *ui.Context, text string, size, width float32) []wordAt {
	measure := func(s string) float32 {
		w, _ := c.MeasureText(0, displaySpan(s+"|", size))
		bar, _ := c.MeasureText(0, displaySpan("|", size))
		return w - bar
	}
	var out []wordAt
	line, at := 0, ""
	for _, p := range titlePieces(text) {
		if at != "" && measure(strings.TrimRight(at+p, " ")) > width+1 {
			line, at = line+1, ""
			if line >= titleLines {
				return nil
			}
		}
		out = append(out, wordAt{text: p, x: measure(at), line: line})
		at += p
	}
	return out
}

// titlePieces cuts text where a line may break: a word with the spaces
// after it, or one character of a CJK run.
func titlePieces(text string) []string {
	var out []string
	for len(text) > 0 {
		r, n := utf8.DecodeRuneInString(text)
		if r >= 0x2E80 {
			out = append(out, text[:n])
			text = text[n:]
			continue
		}
		i := 0
		for i < len(text) {
			q, m := utf8.DecodeRuneInString(text[i:])
			if q == ' ' || q >= 0x2E80 {
				break
			}
			i += m
		}
		for i < len(text) && text[i] == ' ' {
			i++
		}
		if i == 0 {
			i = n
		}
		out = append(out, text[:i])
		text = text[i:]
	}
	return out
}

// wordMorph is the word-by-word morph of a title the page sets as span,
// laid out at dst, from its match src, when the two break it into lines
// differently: a copy that keeps its match's lines would have to jump to
// the page's at some point of the way, so each word goes from its place
// to its place instead. False when they break it alike, say different
// things, or take more than titleLines lines.
func wordMorph(c *ui.Context, src flightSrc, span ui.Span, dst ui.Rect) (*wordFlight, bool) {
	if src.text != span.Text {
		return nil, false
	}
	from := titleWords(c, src.text, src.size, src.box.W)
	to := titleWords(c, span.Text, span.Size, dst.W)
	if from == nil || to == nil || len(from) != len(to) {
		return nil, false
	}
	for i := range from {
		if from[i].line != to[i].line {
			return &wordFlight{src: from, dst: to}, true
		}
	}
	return nil, false
}

// drawWords draws a title morphing word by word t of the way, each word
// from its place in its match's lines to its place in the page's, its size
// and color turning to the page's; o is the copies' layer. A word that
// changes lines goes along the lower line, which the other words leave
// clear, never across the upper one: going down it drops first and then
// slides, going up it slides first and then rises.
func drawWords(c *ui.Context, part flightPart, w *wordFlight, src flightSrc, dst flightText, o ui.Rect, t float32) {
	color := src.color.Mix(dst.color, t)
	// Each side's lines are as far apart as its text lays them out: its
	// height over its lines, not a line height worked out from its size,
	// which a font's leading makes miss and a word would jump as it lands.
	srcLine, dstLine := src.box.H/float32(lines(w.src)), dst.box.H/float32(lines(w.dst))
	for i := range w.src {
		s, d := w.src[i], w.dst[i]
		sx, sy := src.box.X+s.x, src.box.Y+float32(s.line)*srcLine
		dx, dy := dst.box.X+d.x, dst.box.Y+float32(d.line)*dstLine
		tx, ty := t, t
		switch {
		case d.line > s.line:
			ty, tx = smoothstep(0, lineLead, t), smoothstep(1-lineLead, 1, t)
		case d.line < s.line:
			tx, ty = smoothstep(0, lineLead, t), smoothstep(1-lineLead, 1, t)
		}
		// A word's size goes with its x, so that the words along a line,
		// all as far on their way, keep the room between them.
		size := lerp(src.size, dst.size, tx)
		display(c.Key(string(rune('a'+int(part)))+strconv.Itoa(i)), strings.TrimRight(s.text, " "), size).SingleLine().TextColor(color).
			Absolute().Left(lerp(sx, dx, tx) - o.X).Top(lerp(sy, dy, ty) - o.Y)
	}
}

// lineLead is the share of the way a word changing lines takes to make
// its first move, along the lower line or down to it; its second move
// takes the last lineLead, the two overlapping in the middle.
const lineLead = 0.6

// lines is how many lines words take.
func lines(words []wordAt) int {
	n := 0
	for _, w := range words {
		n = max(n, w.line+1)
	}
	return max(n, 1)
}
