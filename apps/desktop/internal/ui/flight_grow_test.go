//go:build !race

package ui

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// growFrame is a frame of a page growing from its search tile: the window
// as it showed and q.
type growFrame struct {
	img *image.RGBA
	q   float32
}

// growFilm opens albums[0] from its search tile in theme and films the way
// in until the page lands, then the way back until it is in its tile
// again: rest is the screen before the click, after the one once back,
// cover the tile's cover in the window, and other how far albums[2]'s tile
// is from it.
func growFilm(t *testing.T, theme string) (in, out []growFrame, rest, after *image.RGBA, cover ui.Rect, other image.Point) {
	t.Helper()
	a, albums := searchApp(t)
	a.PageMotion, a.Theme = true, theme
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	for range 10 {
		tt.Advance(filmFrame)
	}
	rest = cloneImage(tt.Image())
	tile, ok := tt.Find(albums[0].Name)
	two, ok2 := tt.Find(albums[2].Name)
	if !ok || !ok2 {
		t.Fatal("no tiles")
	}
	other = image.Pt(int(two.X-tile.X), int(two.Y-tile.Y))
	if err := tt.Click(albums[0].Name); err != nil {
		t.Fatal(err)
	}
	film := func() (frames []growFrame) {
		tt.Frame()
		for range 90 {
			tt.Advance(filmFrame)
			if src := a.fly.src[partCover]; src.ok && cover.W == 0 {
				cover = src.box
			}
			if !a.fly.moving {
				break
			}
			frames = append(frames, growFrame{img: cloneImage(tt.Image()), q: a.fly.q})
		}
		return frames
	}
	in = film()
	if a.Screen != ScreenAlbum || a.fly.moving || !a.fly.grow {
		t.Fatalf("the page did not grow in: screen %v, moving %v, grow %v", a.Screen, a.fly.moving, a.fly.grow)
	}
	a.Back()
	out = film()
	for range 10 {
		tt.Advance(filmFrame)
	}
	after = cloneImage(tt.Image())
	if cover.W == 0 {
		t.Fatal("no cover to grow from")
	}
	return in, out, rest, after, cover, other
}

// beside is the screen beside a tile's cover that the page growing from it
// first spreads over: the tile's title under the cover and the room to its
// right.
func beside(cover ui.Rect) []image.Rectangle {
	x0, y0 := int(cover.X), int(cover.Y)
	x1, y1 := int(cover.X+cover.W), int(cover.Y+cover.H)
	return []image.Rectangle{
		image.Rect(x0, y1+3, x1+40, y1+30),
		image.Rect(x1+3, y0, x1+40, y1),
	}
}

// A page growing from a cover does not open as a box of its ground larger
// than the cover over the tile's title, nor go back as one: in the first
// frame of the way in and the last of the way back, the screen beside the
// cover changes no more than the same place beside another tile, which
// only the screen under the page going back shows.
func TestAGrowingPageShowsNoBoxBesideItsCover(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		in, out, rest, after, cover, other := growFilm(t, theme)
		if len(in) == 0 || len(out) == 0 {
			t.Fatalf("%s: no flight filmed", theme)
		}
		first, last := in[0], out[len(out)-1]
		for _, r := range beside(cover) {
			box := func(still, frame *image.RGBA) float64 {
				return meanAbsDiff(still, frame, r) - meanAbsDiff(still, frame, r.Add(other))
			}
			if d := box(rest, first.img); d > 3 {
				t.Errorf("%s: the first frame of the way in (q %.3f) changed the screen beside the cover at %v by %.1f levels more than beside another tile: a box of the page's ground", theme, first.q, r, d)
			}
			if d := box(after, last.img); d > 3 {
				t.Errorf("%s: the last frame of the way back (q %.3f) changed the screen beside the cover at %v by %.1f levels more than beside another tile: a box of the page's ground", theme, last.q, r, d)
			}
		}
	}
}

// Going back, the screen under the page is clear by the time the page has
// gone into its tile, not sharpening on its own over the frames after: in
// the demo's search for "glass weather", every frame of the way back in
// which the page is all but gone beside its cover shows the song rows as
// they are at rest.
func TestTheScreenUnderIsClearOnceThePageIsInItsTile(t *testing.T) {
	for _, theme := range []string{"dark", "light"} {
		a, run := demoApp(t)
		a.PageMotion, a.Theme = true, theme
		s := a.Store
		run(func() bool { return s.Loaded })
		a.Go(ScreenHome)
		tt := ui.NewTester(a.View, 1280, 800)
		tt.SetScale(1)
		tt.Type("glass weather")
		run(func() bool {
			tt.Advance(filmFrame)
			return s.Search.Status == store.SearchReady && s.Search.Result != nil
		})
		for range 20 {
			run(func() bool { return true })
			tt.Advance(50 * time.Millisecond)
		}
		row, ok := tt.Find("Paper Satellites")
		if !ok {
			t.Fatal("no song row")
		}
		rows := image.Rect(int(row.X)-60, int(row.Y)-10, int(row.X+row.W)+40, int(row.Y+row.H)+20)
		if err := tt.Click("Glass Weather"); err != nil {
			t.Fatal(err)
		}
		var cover ui.Rect
		for range 90 {
			tt.Advance(filmFrame)
			if src := a.fly.src[partCover]; src.ok && cover.W == 0 {
				cover = src.box
			}
			if !a.fly.moving {
				break
			}
		}
		if a.Screen != ScreenAlbum || !a.fly.grow || cover.W == 0 {
			t.Fatalf("%s: the page did not grow from its tile", theme)
		}
		a.Back()
		tt.Frame()
		var out []growFrame
		for range 90 {
			tt.Advance(filmFrame)
			if !a.fly.moving {
				break
			}
			out = append(out, growFrame{img: cloneImage(tt.Image()), q: a.fly.q})
		}
		for range 10 {
			tt.Advance(filmFrame)
		}
		after := cloneImage(tt.Image())
		for i, f := range out {
			page, under := meanAbsDiff(after, f.img, beside(cover)[0]), meanAbsDiff(after, f.img, rows)
			if page <= 2 && under > 0.25 {
				t.Errorf("%s: frame %d of the way back (q %.3f): the page is all but gone beside its cover (%.2f levels) while the song rows are still %.2f levels from rest", theme, i, f.q, page, under)
			}
		}
	}
}
