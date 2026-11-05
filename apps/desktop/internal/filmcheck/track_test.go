package filmcheck

import (
	"image"
	"testing"
)

// at is a 40-pixel ink 10 tall at top y, left x.
func at(x, y int) Ink { return Ink{N: 40, Top: y, Bottom: y + 10, Left: x, Right: x + 30} }

func times(n int, period float64) []float64 {
	t := make([]float64, n)
	for i := range t {
		t[i] = float64(i) * period
	}
	return t
}

// The maintainer's recording: a line that eased to its place, rested 190
// ms and then dropped 11 pixels in one frame is a snap.
func TestALineThatRestsThenDropsSnaps(t *testing.T) {
	var ink []Ink
	tops := []int{60, 44, 34, 28, 25, 24}
	for _, y := range tops {
		ink = append(ink, at(10, y))
	}
	for range 22 {
		ink = append(ink, at(10, 24))
	}
	ink = append(ink, at(10, 35), at(10, 35), at(10, 35))
	steps := Steps(ink, times(len(ink), 1.0/120), 2.0/120, 2)
	if len(steps) != 1 || steps[0].Kind != "snap" || steps[0].Frame != 28 || steps[0].DY != 11 {
		t.Fatalf("steps %v, want one snap of +11 at frame 28", steps)
	}
	if steps[0].Rest < 0.18 {
		t.Errorf("the rest is %.3f s, want the 23 frames it held", steps[0].Rest)
	}
}

// A spring that eases in and out finds nothing; one frame that moves five
// times its neighbours is a jump.
func TestASmoothEaseFindsNothingAndAnOutlierJumps(t *testing.T) {
	var ink []Ink
	for _, y := range []int{0, 1, 4, 9, 16, 24, 31, 36, 39, 40, 40, 40} {
		ink = append(ink, at(10, y))
	}
	if s := Steps(ink, times(len(ink), 1.0/60), 2.0/60, 2); len(s) > 0 {
		t.Errorf("an ease finds %v", s)
	}
	ink = nil
	for _, y := range []int{0, 2, 4, 6, 20, 22, 24, 26, 26, 26} {
		ink = append(ink, at(10, y))
	}
	s := Steps(ink, times(len(ink), 1.0/60), 2.0/60, 2)
	if len(s) != 1 || s[0].Kind != "jump" || s[0].Frame != 4 {
		t.Errorf("steps %v, want one jump at frame 4", s)
	}
}

// A motion that pauses and goes on is not a snap, and ink that changes by
// over a tenth is another thing, not a move.
func TestAPauseOrAChangeOfInkIsNotASnap(t *testing.T) {
	ink := []Ink{at(10, 30), at(10, 20), at(10, 20), at(10, 10), at(10, 0), at(10, 0)}
	if s := Steps(ink, times(len(ink), 1.0/60), 2.0/60, 2); len(s) > 0 {
		t.Errorf("a pause finds %v", s)
	}
	ink = []Ink{at(10, 20), at(10, 20), at(10, 20), {N: 90, Top: 30, Bottom: 40, Left: 10, Right: 40}, at(10, 30)}
	if s := Steps(ink, times(len(ink), 1.0/60), 2.0/60, 2); len(s) > 0 {
		t.Errorf("a change of ink finds %v", s)
	}
}

// The playlist page's figures in dark, as a 64-pixel window saw them: a
// line easing up, with more ink until frame 3, that another element's ink
// clips at the window's edge in frames 3 and 6, so only the move into
// frame 5 is measured along Y. The frames around it did not hold still,
// so it is no jump, nor a snap. A line clipped along X all the way that
// steps along Y is still caught.
func TestAMoveBesideClippedFramesIsNoJump(t *testing.T) {
	tops := []int{80, 66, 54, 44, 35, 29, 25, 21, 19, 18, 18, 18}
	var ink []Ink
	for i, y := range tops {
		k := at(10, y)
		k.ClipY = i == 3 || i == 6
		if i < 3 {
			k.N = 60
		}
		ink = append(ink, k)
	}
	if s := Steps(ink, times(len(ink), 1.0/60), filmRest, 2); len(s) > 0 {
		t.Errorf("an ease clipped around a frame finds %v", s)
	}
	ink = nil
	for _, y := range []int{30, 29, 28, 27, 21, 20, 19, 18, 18, 18} {
		k := at(10, y)
		k.ClipX = true
		ink = append(ink, k)
	}
	s := Steps(ink, times(len(ink), 1.0/60), filmRest, 2)
	if len(s) != 1 || s[0].Kind != "jump" || s[0].Frame != 4 || s[0].DY != -6 {
		t.Errorf("steps %v, want one jump of -6 at frame 4", s)
	}
}

// InkOf bounds the dark pixels and says which edges they touch.
func TestInkOfBoundsWhatIsDrawn(t *testing.T) {
	const w, h = 8, 6
	pix := make([]uint8, w*h)
	for i := range pix {
		pix[i] = 240
	}
	for _, p := range []image.Point{{2, 1}, {5, 3}, {3, 5}} {
		pix[p.Y*w+p.X] = 20
	}
	k := InkOf(pix, w, w, h, func(v uint8) bool { return v < 60 })
	want := Ink{N: 3, Top: 1, Bottom: 5, Left: 2, Right: 5, ClipY: true}
	if k != want {
		t.Errorf("ink %+v, want %+v", k, want)
	}
}
