package ui

import (
	"image"
	"math"
	"slices"
	"strconv"
	"testing"
	"time"

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

// A long title the queue hero breaks in two lines and the page sets in one
// flies there and back, and lands in the hero where the flight left it,
// at lyjw's window and at 1300, where the hero puts "(Remastered 2014)" on
// its second line (titleBreak).
func TestALongTitleLandsInTheHeroWithoutAJump(t *testing.T) {
	for _, w := range []int{1189, 1300} {
		t.Run(strconv.Itoa(w), func(t *testing.T) { titleFlight(t, w) })
	}
}

func titleFlight(t *testing.T, width int) {
	const title = "First Love (Remastered 2014)"
	words := []string{"First", "Love", "(Remastered", "2014)"}
	a, run := demoApp(t)
	a.PageMotion = true
	s := a.Store
	run(func() bool { return s.Loaded && s.Now() != nil })
	a.Go(ScreenQueue)
	j := a.heroJob()
	j.Title = title
	tt := ui.NewTester(a.View, width, 838)
	for range 30 {
		tt.Advance(50 * time.Millisecond)
	}
	// tail is where the title's last word was in the last frame of a
	// flight, as the copy drew it.
	var tail ui.Rect
	fly := func() (frames int, last *image.RGBA) {
		for range 120 {
			tt.Advance(filmFrame)
			if !a.fly.moving {
				break
			}
			frames++
			last = cloneImage(tt.Image())
			tail, _ = tt.Find(words[len(words)-1])
		}
		return frames, last
	}
	if err := tt.Click("Open " + j.Name()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if n, _ := fly(); n == 0 {
		t.Fatal("the page did not fly in")
	}
	tt.Advance(time.Second)
	a.Back()
	tt.Frame()
	_, last := fly()
	tt.Advance(time.Second)
	rest := tt.Image()
	// The last word, in its band, is where it was in the frame before the
	// landing: a word that stops a few points off its place and then jumps
	// to it as the page's copy gives way to the hero's text moves the ink's
	// middle row.
	band := image.Rect(int(tail.X), int(tail.Y)-24, int(tail.X+tail.W), int(tail.Y+tail.H)+24)
	if was, is := inkRow(last, band), inkRow(rest, band); math.Abs(was-is) > 1.5 {
		t.Errorf("%q jumps %.1f DIPs as it lands in the hero", words[len(words)-1], is-was)
	}
}

// inkRow is the middle row of what is drawn in r over its top row, the
// ground: each row weighed by how far its pixels are from the top row's.
func inkRow(img *image.RGBA, r image.Rectangle) float64 {
	var sum, at float64
	for y := r.Min.Y; y < r.Max.Y; y++ {
		var w float64
		for x := r.Min.X; x < r.Max.X; x++ {
			w += math.Abs(luma(img, x, y) - luma(img, x, r.Min.Y))
		}
		sum, at = sum+w, at+w*float64(y)
	}
	if sum == 0 {
		return 0
	}
	return at / sum
}
