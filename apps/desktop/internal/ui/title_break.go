package ui

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// titleBreak is text as a head sets it in at most two lines width wide at
// size, with a newline where its first line ends: one line when it fits;
// else before the qualifiers it ends in ("(Remastered 2014)", "[Deluxe]",
// "「…」") or after a " - ", the earliest such place where both lines fit;
// else where its two lines come out most even. Text that two lines do not
// hold is left to wrap as it will and end in an ellipsis.
func titleBreak(c *ui.Context, text string, size, width float32) string {
	fits := func(s string) bool { return titleWidth(c, s, size) <= width+1 }
	if width <= 0 || strings.Contains(text, "\n") || fits(text) {
		return text
	}
	for _, at := range qualifierBreaks(text) {
		a, b := strings.TrimRight(text[:at], " "), strings.TrimLeft(text[at:], " ")
		if fits(a) && fits(b) {
			return a + "\n" + b
		}
	}
	pieces := titlePieces(text)
	best, out := float32(-1), text
	for i := 1; i < len(pieces); i++ {
		a := strings.TrimRight(strings.Join(pieces[:i], ""), " ")
		b := strings.TrimRight(strings.Join(pieces[i:], ""), " ")
		wa, wb := titleWidth(c, a, size), titleWidth(c, b, size)
		if wa > width+1 || wb > width+1 {
			continue
		}
		if m := max(wa, wb); best < 0 || m < best {
			best, out = m, a+"\n"+b
		}
	}
	return out
}

// titleWidth is how wide s is set as display sets it at size: measured
// before a bar, so trailing tracking counts as it does inside a line.
func titleWidth(c *ui.Context, s string, size float32) float32 {
	w, _ := c.MeasureText(0, displaySpan(s+"|", size))
	bar, _ := c.MeasureText(0, displaySpan("|", size))
	return w - bar
}

// qualifierOpen pairs each bracket a title's qualifiers open with the one
// that closes them.
var qualifierOpen = map[rune]rune{'(': ')', '[': ']', '（': '）', '【': '】', '「': '」', '『': '』'}

// qualifierBreaks are the places text may break before the qualifiers it
// ends in, as byte offsets of its second line, earliest first: before a
// bracket from which only bracketed groups reach the end, and after the
// dash of a " - ", which stays on the first line.
func qualifierBreaks(text string) []int {
	var out []int
	for i, r := range text {
		if _, ok := qualifierOpen[r]; ok && strings.TrimSpace(text[:i]) != "" && onlyQualifiers(text[i:]) {
			out = append(out, i)
		}
	}
	for _, dash := range []string{" - ", " – ", " — "} {
		for from := 0; ; {
			j := strings.Index(text[from:], dash)
			if j < 0 {
				break
			}
			at := from + j + len(dash) - 1
			if strings.TrimSpace(text[:at]) != "" && strings.TrimSpace(text[at:]) != "" {
				out = append(out, at)
			}
			from = at
		}
	}
	sort.Ints(out)
	return out
}

// onlyQualifiers reports whether s is bracketed groups and the spaces
// between them, to its end.
func onlyQualifiers(s string) bool {
	for s = strings.TrimLeft(s, " "); s != ""; s = strings.TrimLeft(s, " ") {
		r, n := utf8.DecodeRuneInString(s)
		end, ok := qualifierOpen[r]
		if !ok {
			return false
		}
		j := strings.IndexRune(s[n:], end)
		if j < 0 {
			return false
		}
		s = s[n+j+len(string(end)):]
	}
	return true
}
