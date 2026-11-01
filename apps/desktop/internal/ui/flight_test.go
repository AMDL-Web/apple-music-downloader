//go:build !race

// The album page's flight runs frame by frame on the Tester's clock, which
// the race detector slows many times over with no concurrency to check.

package ui

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// refresh is one frame of a 60 Hz display.
const refresh = time.Second / 60

// flightApp is the queue over the demo backend, with the album page's
// motion on, and the job its hero shows.
func flightApp(t *testing.T) (*App, *ui.Tester, *store.Job) {
	t.Helper()
	a, run := demoApp(t)
	a.PageMotion = true
	s := a.Store
	run(func() bool { return s.Loaded && (s.Now() != nil || len(s.Finished()) > 0) })
	a.Go(ScreenQueue)
	tt := ui.NewTester(a.View, 1280, 800)
	// Boxes are in DIPs whatever the scale; fewer pixels draw faster.
	tt.SetScale(0.5)
	j := a.heroJob()
	if j == nil {
		t.Fatal("the queue has no hero")
	}
	return a, tt, j
}

// flyFrames runs frames until the page stops moving, or n of them, and
// returns the boxes its cover showed in.
func flyFrames(a *App, tt *ui.Tester, n int) []ui.Rect {
	var boxes []ui.Rect
	for range n {
		tt.Advance(refresh)
		if r, ok := tt.Find("Artwork"); ok {
			boxes = append(boxes, r)
		}
		if !a.fly.moving {
			break
		}
	}
	return boxes
}

func dist(a, b ui.Rect) float64 {
	return math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y)) + math.Abs(float64(a.W-b.W))
}

// smooth fails when one frame moves the cover more than a fifth of the
// way, which a jump or a restart would; going out, the cover goes all the
// way in about ten frames, while the screen under the page is still gone.
func smooth(t *testing.T, boxes []ui.Rect, from, to ui.Rect) {
	t.Helper()
	whole := dist(from, to)
	for i := 1; i < len(boxes); i++ {
		if d := dist(boxes[i-1], boxes[i]); d > whole/5+0.5 {
			t.Errorf("frame %d moved the cover %.1f of %.1f: %v -> %v", i, d, whole, boxes[i-1], boxes[i])
		}
	}
}

func TestTheHeroCoverFliesToThePageAndBack(t *testing.T) {
	a, tt, j := flightApp(t)
	from, ok := tt.Find("Open " + j.Name())
	if !ok {
		t.Fatalf("no hero cover in %q", tt.Texts())
	}
	if err := tt.Click("Open " + j.Name()); err != nil {
		t.Fatal(err)
	}
	in := flyFrames(a, tt, 60)
	if a.fly.moving || len(in) < 10 {
		t.Fatalf("the page came in over %d frames, still moving: %v", len(in), a.fly.moving)
	}
	to := in[len(in)-1]
	if to.W != 248 {
		t.Errorf("the page's cover came to rest at %v, not its 248 DIPs", to)
	}
	if d := dist(in[0], from); d > dist(from, to)/10+1 {
		t.Errorf("the cover first showed at %v, %.1f from the hero's %v", in[0], d, from)
	}
	smooth(t, in, from, to)
	for i := 1; i < len(in); i++ {
		if in[i].W < in[i-1].W {
			t.Errorf("the cover shrank on its way in at frame %d: %v -> %v", i, in[i-1], in[i])
		}
	}
	if tt.HasText("Recently finished") {
		t.Errorf("the queue is still built under the page that came in")
	}

	tt.Key(0, ui.KeyEscape)
	out := flyFrames(a, tt, 60)
	if a.fly.moving || a.Screen != ScreenQueue {
		t.Fatalf("the page is still going out (%v) or Back went to %v", a.fly.moving, a.Screen)
	}
	if len(out) < 10 {
		t.Fatalf("the page went out over %d frames", len(out))
	}
	if d := dist(out[len(out)-1], from); d > dist(from, to)/10+1 {
		t.Errorf("the cover last showed at %v, %.1f from the hero's %v", out[len(out)-1], d, from)
	}
	smooth(t, out, to, from)
	if _, ok := tt.Find("Artwork"); ok || a.Store.Detail() != nil {
		t.Errorf("the page is still built, or its download still open, once it went out")
	}
}

// The cover sets off only once the queue is gone, and lands before the
// queue comes back: it never slides beside what stands still.
func TestNothingMovesBesideTheQueue(t *testing.T) {
	a, tt, j := flightApp(t)
	from, ok := tt.Find("Open " + j.Name())
	if !ok {
		t.Fatalf("no hero cover in %q", tt.Texts())
	}
	type frame struct {
		q   float32
		box ui.Rect
	}
	run := func() []frame {
		var fs []frame
		for range 60 {
			tt.Advance(refresh)
			if r, ok := tt.Find("Artwork"); ok {
				fs = append(fs, frame{a.fly.q, r})
			}
			if !a.fly.moving {
				break
			}
		}
		return fs
	}
	if err := tt.Click("Open " + j.Name()); err != nil {
		t.Fatal(err)
	}
	for i, f := range run() {
		if f.q < crossover && dist(f.box, from) > 1 {
			t.Errorf("coming in, frame %d (q %.3f): the cover is at %v, not the hero's %v, while the queue shows", i, f.q, f.box, from)
		}
	}
	tt.Key(0, ui.KeyEscape)
	out := run()
	if len(out) == 0 {
		t.Fatal("no frame of the page going out")
	}
	last := out[len(out)-1].box
	for i, f := range out {
		if f.q < crossover && dist(f.box, last) > 0.5 {
			t.Errorf("going out, frame %d (q %.3f): the cover is at %v, still on its way to %v, while the queue shows", i, f.q, f.box, last)
		}
	}
}

// The light turns from the queue's to the page's: no frame on the way in or
// out lies outside the two ends, as the bare ground would, darker than
// both in the dark theme and lighter in the light one.
func TestTheLightTurnsWithoutADip(t *testing.T) {
	for _, theme := range []string{"dark", "light"} {
		a, tt, j := flightApp(t)
		a.Theme = theme
		// The light eases to the theme's first.
		tt.Advance(time.Second)
		// A band of the column's top that both screens leave empty, in
		// pixels at the Tester's half scale.
		lum := func() float64 {
			img := tt.Image()
			sum, n := 0.0, 0.0
			for y := 12; y < 30; y++ {
				for x := 300; x < 500; x++ {
					o := img.PixOffset(x, y)
					sum += 0.2126*float64(img.Pix[o]) + 0.7152*float64(img.Pix[o+1]) + 0.0722*float64(img.Pix[o+2])
					n++
				}
			}
			return sum / n
		}
		// way measures from before go, which builds the way's first frame.
		way := func(name string, goes func()) {
			from := lum()
			goes()
			seen := []float64{lum()}
			for range 60 {
				tt.Advance(refresh)
				seen = append(seen, lum())
				if !a.fly.moving {
					break
				}
			}
			to := seen[len(seen)-1]
			for i, l := range seen {
				if l < min(from, to)-1 || l > max(from, to)+1 {
					t.Errorf("%s, %s: frame %d is %.1f bright, outside the ends, %.1f and %.1f", theme, name, i, l, from, to)
				}
			}
		}
		way("in", func() {
			if err := tt.Click("Open " + j.Name()); err != nil {
				t.Fatal(err)
			}
		})
		way("out", func() { tt.Key(0, ui.KeyEscape) })
	}
}

func TestBackOnTheWayInTurnsTheCoverRound(t *testing.T) {
	a, tt, j := flightApp(t)
	from, _ := tt.Find("Open " + j.Name())
	// Once all the way, for where the cover comes to rest.
	if err := tt.Click("Open " + j.Name()); err != nil {
		t.Fatal(err)
	}
	in := flyFrames(a, tt, 60)
	to := in[len(in)-1]
	tt.Key(0, ui.KeyEscape)
	flyFrames(a, tt, 60)

	if err := tt.Click("Open " + j.Name()); err != nil {
		t.Fatal(err)
	}
	boxes := flyFrames(a, tt, 8)
	tt.Key(0, ui.KeyEscape)
	boxes = append(boxes, flyFrames(a, tt, 60)...)
	if a.fly.moving {
		t.Fatal("the page never left")
	}
	// The cover turned round where it was: no frame moved it far.
	smooth(t, boxes, from, to)
	if d := dist(boxes[len(boxes)-1], from); d > dist(from, to)/10+1 {
		t.Errorf("the cover went back to %v, not the hero's %v", boxes[len(boxes)-1], from)
	}
	if a.Store.Detail() != nil {
		t.Errorf("the page's download stayed open after it left")
	}
}

func TestBackBeforeThePageShowsLeavesTheQueue(t *testing.T) {
	a, tt, j := flightApp(t)
	if err := tt.Click("Open " + j.Name()); err != nil {
		t.Fatal(err)
	}
	// Still the frame that lays the page out, before it shows.
	tt.Key(0, ui.KeyEscape)
	tt.Advance(refresh)
	if a.fly.moving || a.Screen != ScreenQueue || a.Store.Detail() != nil {
		t.Errorf("moving %v, screen %v, download open %v", a.fly.moving, a.Screen, a.Store.Detail() != nil)
	}
	if _, ok := tt.Find("Artwork"); ok {
		t.Errorf("the page showed after Back")
	}
}

func TestWithLessMotionThePageFadesInPlace(t *testing.T) {
	a, tt, j := flightApp(t)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	if err := tt.Click("Open " + j.Name()); err != nil {
		t.Fatal(err)
	}
	boxes := flyFrames(a, tt, 30)
	if a.fly.moving {
		t.Fatal("the fade never ended")
	}
	if n := len(boxes); n < 5 || n > 12 {
		t.Errorf("the fade took %d frames, not about %v", n, flightFade)
	}
	for i, r := range boxes {
		if r != boxes[len(boxes)-1] {
			t.Errorf("the cover moved, at frame %d: %v", i, r)
		}
	}
}

func TestAnotherScreenLeavesThePageAtOnce(t *testing.T) {
	a, tt, j := flightApp(t)
	if err := tt.Click("Open " + j.Name()); err != nil {
		t.Fatal(err)
	}
	flyFrames(a, tt, 5)
	a.Go(ScreenSettings)
	tt.Advance(refresh)
	if a.fly.moving {
		t.Errorf("the page still moves after Settings opened")
	}
	if _, ok := tt.Find("Artwork"); ok || tt.HasText("Recently finished") {
		t.Errorf("the page or the queue still shows over Settings")
	}
}
