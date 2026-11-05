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
// flies both ways with no two of its words over each other in any frame,
// and lands in the hero where it rests: the frame before the landing and
// the one after differ by no more than a word's last sliver of way. At
// lyjw's window the hero puts "(Remastered 2014)" on its second line; at
// 1300 it puts "2014)" there alone, as on lyjw's Mac.
func TestALongTitleFliesBothWaysWithoutWordsCrossingOrJumping(t *testing.T) {
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
	fly := func(way string) (frames int, last *image.RGBA) {
		for range 120 {
			tt.Advance(filmFrame)
			if !a.fly.moving {
				break
			}
			frames++
			last = cloneImage(tt.Image())
			var rs []ui.Rect
			for _, w := range words {
				if r, ok := tt.Find(w); ok {
					// The glyphs, not the line box, whose leading
					// overlaps the next line's at rest.
					r.Y, r.H = r.Y+0.2*r.H, 0.6*r.H
					rs = append(rs, r)
				}
			}
			for i := range rs {
				for k := i + 1; k < len(rs); k++ {
					p, q := rs[i], rs[k]
					if p.X+p.W-1 > q.X && q.X+q.W-1 > p.X && p.Y+p.H-1 > q.Y && q.Y+q.H-1 > p.Y {
						t.Errorf("%s, frame %d: words over each other: %v and %v", way, frames, p, q)
					}
				}
			}
		}
		return frames, last
	}
	if err := tt.Click("Open " + j.Name()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if n, _ := fly("in"); n == 0 {
		t.Fatal("the page did not fly in")
	}
	tt.Advance(time.Second)
	a.Back()
	tt.Frame()
	_, last := fly("out")
	tt.Advance(time.Second)
	rest := tt.Image()
	// The hero's title: the words' rows over the cover's right, where the
	// sidebar's and the rows' copies of it are not.
	cover, _ := tt.Find("Open " + j.Name())
	box := image.Rect(int(cover.X), int(cover.Y), int(cover.X+cover.W), int(cover.Y)+130)
	off := 0
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if math.Abs(luma(last, x, y)-luma(rest, x, y)) > 40 {
				off++
			}
		}
	}
	if share := float64(off) / float64(box.Dx()*box.Dy()); share > 0.02 {
		t.Errorf("the title jumps as it lands in the hero: %.1f%% of its box changes in the landing frame", 100*share)
	}
}
