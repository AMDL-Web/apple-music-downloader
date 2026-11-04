//go:build !race

package ui

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// switchFilm clicks the segment showing click and films the switch at 60
// Hz, frames frames: for each, how much of area shows, as the pixels of
// text and covers on the dark theme's ground (ink), after the frame before
// the click.
func switchFilm(t *testing.T, tt *ui.Tester, click string, area image.Rectangle, frames int) (before int, film []int) {
	t.Helper()
	tt.Frame()
	before = ink(tt.Image(), area)
	if err := tt.Click(click); err != nil {
		t.Fatal(err)
	}
	for range frames {
		tt.Advance(filmFrame)
		tt.Frame()
		film = append(film, ink(tt.Image(), area))
	}
	return before, film
}

// ink counts the bright pixels of r, text and covers over a dark ground.
func ink(img *image.RGBA, r image.Rectangle) int {
	n := 0
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			o := img.PixOffset(x, y)
			if 0.2126*float32(img.Pix[o])+0.7152*float32(img.Pix[o+1])+0.0722*float32(img.Pix[o+2]) > 110 {
				n++
			}
		}
	}
	return n
}

// blankRun is the longest run of frames of film that show less than a
// tenth of the less of before and the last frame: the time a switch shows
// nothing.
func blankRun(before int, film []int) int {
	floor := min(before, film[len(film)-1]) / 10
	longest, run := 0, 0
	for _, n := range film {
		if n < floor {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// settledBy is the first frame of film from which it no longer changes by
// more than a hundredth of the last frame.
func settledBy(film []int) int {
	last := film[len(film)-1]
	at := len(film)
	for i := len(film) - 1; i >= 0; i-- {
		d := film[i] - last
		if d < 0 {
			d = -d
		}
		if d > last/100+1 {
			break
		}
		at = i
	}
	return at
}

// A change of the search's tab goes in one move, in the time the capsule's
// fill slides: the tab left fades out and the one chosen fades in at once
// after it, never a stretch of frames with neither past the two fades'
// crossing, three frames, whether the tab left had hits or only its "No
// playlists".
func TestASearchTabSwitchShowsNoGap(t *testing.T) {
	a, run := demoApp(t)
	a.Screen, a.PageMotion, a.Theme = ScreenHome, true, "dark"
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	s := a.Store
	s.SetOmniText("glass weather")
	tt.Frame()
	run(func() bool { tt.Frame(); return s.Search.Status == store.SearchReady })
	run(func() bool {
		tt.Advance(50 * time.Millisecond)
		return a.Art.Get(s.Search.ShownHits()[0].ArtworkURL, 512) != nil
	})
	tt.Advance(time.Second)
	tabs, ok := tt.Find("Overview")
	if !ok {
		t.Fatalf("no tabs: %q", tt.Texts())
	}
	area := image.Rect(int(tabs.X), int(tabs.Y+tabs.H+8), 1280-24, 800-24)
	for _, tab := range []string{"Albums", "Songs", "Playlists", "Overview"} {
		before, film := switchFilm(t, tt, tab, area, 40)
		if n := blankRun(before, film); n > 3 {
			t.Errorf("to %s: %d frames show neither tab (%v, before %d)", tab, n, film, before)
		}
		// The fill's spring, 300 ms, and a few frames for the fade's tail.
		if at := settledBy(film); at > 24 {
			t.Errorf("to %s: the switch settles at frame %d, past 400 ms (%v)", tab, at, film)
		}
		tt.Advance(time.Second)
	}
}
