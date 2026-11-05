package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A title too wide for one line breaks before the qualifiers it ends in,
// or after its " - ", where both lines fit; without such a place, where
// its two lines are most even; one that fits, or that two lines cannot
// hold, is left alone.
func TestALongTitleBreaksBeforeItsQualifiers(t *testing.T) {
	type row struct{ title, want string }
	var rows []row
	var evenA, evenB, room float32
	var even string
	ui.NewTester(func(c *ui.Context) {
		w := func(s string) float32 { return titleWidth(c, s, 42) }
		check := func(title string, width float32, want string) {
			rows = append(rows, row{title, titleBreak(c, title, 42, width) + "|" + want})
		}
		const love = "First Love (Remastered 2014)"
		check(love, w(love)+4, love)
		check(love, w(love)*0.8, "First Love\n(Remastered 2014)")
		const story = "nameless story - 「とある科学の超電磁砲T」ED"
		check(story, max(w("nameless story -"), w("「とある科学の超電磁砲T」ED"))+2, "nameless story -\n「とある科学の超電磁砲T」ED")
		const moon = "The Dark Side of the Moon (50th Anniversary) [Remastered]"
		check(moon, w("The Dark Side of the Moon (50th Anniversary)")+2, "The Dark Side of the Moon\n(50th Anniversary) [Remastered]")
		check(moon, w("The Dark")+2, moon)
		const mid = "Love (Is) Blind Tonight"
		room = w(mid) * 0.7
		even = titleBreak(c, mid, 42, room)
		if a, b, ok := strings.Cut(even, "\n"); ok {
			evenA, evenB = w(a), w(b)
		}
	}, 400, 300).Frame()
	for _, r := range rows {
		got, want, _ := strings.Cut(r.want, "|")
		if got != want {
			t.Errorf("%s: %q, want %q", r.title, got, want)
		}
	}
	if !strings.Contains(even, "\n") || evenA > room+1 || evenB > room+1 || min(evenA, evenB) < max(evenA, evenB)/2 {
		t.Errorf("a title with nothing to break before is not in two even lines: %q (%.0f and %.0f in %.0f)", even, evenA, evenB, room)
	}
}
