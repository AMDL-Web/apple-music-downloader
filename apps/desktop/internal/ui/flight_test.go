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

// The cover sets off in the first frames, ahead of the rest of the page,
// which follows it; going out, the queue comes back over the last stretch
// of the cover's way and is whole only as it lands, rather than standing
// still while the cover is still crossing it.
func TestTheCoverLeadsAndLandsWithTheQueue(t *testing.T) {
	a, tt, j := flightApp(t)
	from, ok := tt.Find("Open " + j.Name())
	if !ok {
		t.Fatalf("no hero cover in %q", tt.Texts())
	}
	type frame struct {
		under float32
		box   ui.Rect
	}
	run := func() []frame {
		var fs []frame
		for range 60 {
			tt.Advance(refresh)
			if r, ok := tt.Find("Artwork"); ok && a.fly.moving {
				fs = append(fs, frame{a.fly.underShown(), r})
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
	in := run()
	if len(in) < 10 {
		t.Fatalf("the page came in over %d frames", len(in))
	}
	to, _ := tt.Find("Artwork")
	whole := dist(from, to)
	if d := dist(in[1].box, from); d < whole/30 {
		t.Errorf("coming in, the cover went %.1f of %.1f in its first two frames: it waited", d, whole)
	}
	tt.Key(0, ui.KeyEscape)
	out := run()
	if len(out) == 0 {
		t.Fatal("no frame of the page going out")
	}
	for i, f := range out {
		if d := dist(f.box, from); f.under >= 1 && d > whole/4 {
			t.Errorf("going out, frame %d: the queue is whole while the cover is still %.1f of %.1f from the hero", i, d, whole)
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

// The hero's track count, which has no place on the page, fades with the
// queue where it is, both ways, rather than flying off to nowhere.
func TestTheTrackCountFadesInPlace(t *testing.T) {
	a, tt, j := flightApp(t)
	n := plural(j.TotalItems, "track")
	from, ok := tt.Find(n)
	if !ok {
		t.Fatalf("no track count %q on the hero: %q", n, tt.Texts())
	}
	way := func(name string) {
		t.Helper()
		for i := range 60 {
			tt.Advance(refresh)
			if r, ok := tt.Find(n); ok && a.fly.moving && dist(from, r) > 0.5 {
				t.Errorf("%s, frame %d: the track count is at %v, not its place %v", name, i, r, from)
			}
			if !a.fly.moving {
				break
			}
		}
	}
	if err := tt.Click("Open " + j.Name()); err != nil {
		t.Fatal(err)
	}
	way("coming in")
	if _, ok := tt.Find(n); ok {
		t.Errorf("landed, the page still shows the hero's track count")
	}
	tt.Key(0, ui.KeyEscape)
	way("going out")
	if r, ok := tt.Find(n); !ok || dist(from, r) > 0.5 {
		t.Errorf("back on the queue, the track count is at %v (%v), not %v", r, ok, from)
	}
}

// A click on the tile the album page goes back to, while it does, opens
// the page again at once, from where it is, not after the page is gone.
func TestAClickOnTheTileGoingBackOpensItAgain(t *testing.T) {
	a, albums := searchApp(t)
	a.PageMotion = true
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(0.5)
	name := albums[1].Name
	// The tile's cover, which its name labels.
	tile, ok := tt.Find(name)
	if !ok {
		t.Fatalf("no tile %q: %q", name, tt.Texts())
	}
	if err := tt.Click(name); err != nil {
		t.Fatal(err)
	}
	flyFrames(a, tt, 120)
	if a.Screen != ScreenAlbum {
		t.Fatalf("the tile opened %v", a.Screen)
	}
	a.Back()
	tt.Frame()
	for range 60 {
		tt.Advance(refresh)
		if a.fly.travel() < 0.6 {
			break
		}
	}
	if !a.fly.moving || a.fly.q <= 0 {
		t.Fatalf("the page is not on its way out: q %.2f", a.fly.q)
	}
	tt.ClickAt(tile.X+tile.W/2, tile.Y+tile.H/2)
	flyFrames(a, tt, 120)
	if a.Screen != ScreenAlbum || a.fly.q != 1 {
		t.Errorf("a click on the tile going back left the screen %v, q %.2f", a.Screen, a.fly.q)
	}
}

// A tile's mark rides the cover that flies from it: in the first frames out
// and the last frames back, while the cover is still at the tile, the mark
// shows over it at its corner, as on the tile, rather than the cover
// hiding it and the tile showing it again at once on landing.
func TestATilesMarkRidesItsFlyingCover(t *testing.T) {
	a, albums := searchApp(t)
	a.PageMotion = true
	albums[0].ArtworkBgColor, albums[0].AudioTraits = "203040", []string{"lossless", "hi-res-lossless"}
	a.Store.Search.Result.Albums.Items[0] = albums[0]
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	mark, ok := tt.Find(albums[0].Name + " is in your library: download again")
	if !ok {
		t.Fatalf("no library mark on the tile: %q", tt.Texts())
	}
	green := func(when string) {
		t.Helper()
		px := tt.Image().RGBAAt(int(mark.X+mark.W/2), int(mark.Y+mark.H/2))
		if !(int(px.G) > int(px.R)+40 && int(px.G) > int(px.B)+20) {
			t.Errorf("%s, the tile's mark is not over the cover: %v at its center", when, px)
		}
	}
	if err := tt.Click(albums[0].Name); err != nil {
		t.Fatal(err)
	}
	tt.Advance(refresh)
	tt.Frame()
	if !a.fly.moving {
		t.Fatal("the page does not fly")
	}
	green("leaving")
	flyFrames(a, tt, 120)
	a.Back()
	tt.Frame()
	// The last frame of the flight back and the first without it.
	for range 120 {
		tt.Advance(refresh)
		if a.fly.moving && a.fly.lead() < 0.05 {
			green("landing")
			flyFrames(a, tt, 10)
			break
		}
	}
	if a.fly.moving {
		t.Fatalf("the page did not land: q %.2f", a.fly.q)
	}
	green("landed")
}

// A tile's quality hint is its own badge, not the page's tag: on its way
// out it keeps to its corner of the flying cover, as the mark does, while
// the page's tag comes in apart.
func TestATilesHintKeepsToItsCoverCorner(t *testing.T) {
	a, albums := searchApp(t)
	a.PageMotion = true
	albums[0].AudioTraits = []string{"hi-res-lossless"}
	a.Store.Search.Result.Albums.Items[0] = albums[0]
	tt := ui.NewTester(a.View, 1280, 800)
	cover, ok := tt.Find(albums[0].Name)
	if !ok {
		t.Fatalf("no tile %q: %q", albums[0].Name, tt.Texts())
	}
	hint, ok := tt.Find("Hi-Res")
	if !ok {
		t.Fatalf("no quality hint on the tile: %q", tt.Texts())
	}
	left, below := hint.X-cover.X, cover.Y+cover.H-hint.Y
	if err := tt.Click(albums[0].Name); err != nil {
		t.Fatal(err)
	}
	for range 60 {
		tt.Advance(refresh)
		if a.fly.travel() >= 0.25 {
			break
		}
	}
	art, ok := tt.Find("Artwork")
	if !ok {
		t.Fatalf("no flying cover: %q", tt.Texts())
	}
	now, ok := tt.Find("Hi-Res")
	if !ok {
		t.Fatalf("a quarter of the way, the hint is gone: %q", tt.Texts())
	}
	if dx, dy := now.X-art.X-left, art.Y+art.H-now.Y-below; dx*dx+dy*dy > 4 {
		t.Errorf("a quarter of the way the hint is %.1f, %.1f off its corner of the cover %v: %v", dx, dy, art, now)
	}
}

// Once the page is gone, q rests at 0 and asks for no more frames: the
// flight aims past 0 only while it goes out.
func TestTheFlightAtRestAsksForNoFrames(t *testing.T) {
	a, albums := searchApp(t)
	a.PageMotion = true
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(0.5)
	if err := tt.Click(albums[1].Name); err != nil {
		t.Fatal(err)
	}
	flyFrames(a, tt, 120)
	a.Back()
	tt.Frame()
	flyFrames(a, tt, 120)
	if a.fly.moving || a.fly.q != 0 {
		t.Fatalf("the page did not go out: q %.2f", a.fly.q)
	}
	tt.Advance(time.Second)
	if tt.FramePending() {
		t.Errorf("a second after the page went out, the window still asks for frames")
	}
}
