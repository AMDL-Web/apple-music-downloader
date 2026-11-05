package ui

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A title its match breaks into two lines and the page sets in one flies
// word by word, each word keeping its order; one both set alike flies as
// one copy.
func TestATitleBrokenDifferentlyFliesWordByWord(t *testing.T) {
	const title = "First Love (Remastered 2014)"
	if got := titlePieces(title); !slices.Equal(got, []string{"First ", "Love ", "(Remastered ", "2014)"}) {
		t.Errorf("pieces of %q: %q", title, got)
	}
	if got := titlePieces("輪符雨 EP"); !slices.Equal(got, []string{"輪", "符", "雨", " ", "EP"}) && !slices.Equal(got, []string{"輪", "符", "雨 ", "EP"}) {
		t.Errorf("pieces of a CJK title: %q", got)
	}
	var narrow, wide []wordAt
	var morph, same bool
	ui.NewTester(func(c *ui.Context) {
		full, _ := c.MeasureText(0, displaySpan(title, 42))
		narrow = titleWords(c, title, 42, full*0.66)
		wide = titleWords(c, title, 42, full+10)
		src := flightSrc{text: title, size: 42, box: ui.Rect{W: full * 0.66}}
		_, morph = wordMorph(c, src, displaySpan(title, 44), ui.Rect{W: full * 1.2})
		src.box.W = full + 10
		_, same = wordMorph(c, src, displaySpan(title, 42), ui.Rect{W: full + 10})
	}, 400, 300).Frame()
	if len(narrow) != 4 || narrow[len(narrow)-1].line != 1 || narrow[0].line != 0 {
		t.Errorf("a title too wide for its room is not broken in two lines: %+v", narrow)
	}
	for _, w := range wide {
		if w.line != 0 {
			t.Errorf("a title that fits breaks: %+v", wide)
		}
	}
	if !morph {
		t.Error("a title broken in two lines flying to one does not morph word by word")
	}
	if same {
		t.Error("a title set alike on both sides morphs word by word")
	}
}
