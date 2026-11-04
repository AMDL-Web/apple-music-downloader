//go:build !race

package ui

import (
	"fmt"
	"image"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// growFrame is a frame of a page growing from its tile: the window as it
// showed, q and the surface's box in the window.
type growFrame struct {
	img     *image.RGBA
	q       float32
	surface ui.Rect
}

// growFilmed is a page growing from albums[0]'s search tile filmed both
// ways: rest is the screen before the click, after the one once back, from
// the tile's cover in the window, to the page's cover once landed, main the
// main column, and marks the boxes of the tiles' words on the screen under.
type growFilmed struct {
	in, out     []growFrame
	rest, after *image.RGBA
	from, to    ui.Rect
	main        ui.Rect
	marks       []ui.Rect
}

// growFilms keeps each film for every test that watches it.
var growFilms = map[string]*growFilmed{}

// growFilm opens albums[0] from its search tile in theme, in its own colors
// when palette, and films the way in until the page lands, then the way
// back until it is in its tile again.
func growFilm(t *testing.T, theme string, palette bool) *growFilmed {
	t.Helper()
	key := fmt.Sprint(theme, palette)
	if g := growFilms[key]; g != nil {
		return g
	}
	a, albums := searchApp(t)
	a.PageMotion, a.Theme = true, theme
	if palette {
		// Its colors light the page (paletteBackdrop).
		albums[0].ArtworkBgColor = "203040"
		a.Store.Search.Result.Albums.Items[0] = albums[0]
	}
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	for range 10 {
		tt.Advance(filmFrame)
	}
	g := &growFilmed{rest: cloneImage(tt.Image())}
	for _, h := range albums[:4] {
		for _, x := range []string{h.Name, h.ArtistName} {
			if r, ok := tt.Find(x); ok && !slices.Contains(g.marks, r) {
				g.marks = append(g.marks, r)
			}
		}
	}
	if err := tt.Click(albums[0].Name); err != nil {
		t.Fatal(err)
	}
	film := func() (frames []growFrame) {
		tt.Frame()
		for range 90 {
			tt.Advance(filmFrame)
			if src := a.fly.src[partCover]; src.ok && g.from.W == 0 {
				g.from = src.box
			}
			if !a.fly.moving {
				break
			}
			g.main = a.fly.main
			frames = append(frames, growFrame{img: cloneImage(tt.Image()), q: a.fly.q, surface: a.fly.surface})
		}
		return frames
	}
	g.in = film()
	if a.Screen != ScreenAlbum || a.fly.moving || !a.fly.grow {
		t.Fatalf("the page did not grow in: screen %v, moving %v, grow %v", a.Screen, a.fly.moving, a.fly.grow)
	}
	to, ok := tt.Find("Artwork")
	if !ok {
		t.Fatal("no cover on the page")
	}
	g.to = to
	a.Back()
	g.out = film()
	for range 10 {
		tt.Advance(filmFrame)
	}
	g.after = cloneImage(tt.Image())
	if g.from.W == 0 || len(g.in) == 0 || len(g.out) == 0 || len(g.marks) == 0 {
		t.Fatal("no flight to watch")
	}
	growFilms[key] = g
	return g
}

// coverAt is where the flying cover is drawn at q: on the line from its
// tile to its place on the page.
func (g *growFilmed) coverAt(q float32) ui.Rect {
	return ui.Rect{X: lerp(g.from.X, g.to.X, q), Y: lerp(g.from.Y, g.to.Y, q), W: lerp(g.from.W, g.to.W, q), H: lerp(g.from.H, g.to.H, q)}
}

func inside(r, box ui.Rect) bool {
	return r.X >= box.X && r.Y >= box.Y && r.X+r.W <= box.X+box.W && r.Y+r.H <= box.Y+box.H
}

func overlaps(a, b ui.Rect, pad float32) bool {
	return a.X < b.X+b.W+pad && b.X < a.X+a.W+pad && a.Y < b.Y+b.H+pad && b.Y < a.Y+a.H+pad
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

// What grows from a cover is the page, never an empty box of its ground
// ahead of it: in every frame, both ways, in which the page is not yet half
// the column wide, each tile's word on the screen under that its surface
// covers and the cover does not still shows through, blurred and dimmed,
// with at least a third of the detail it has at rest, light and dark, with
// and without the album's colors. An opaque ground with nothing on it
// leaves none.
func TestAGrowingPageShowsNoBoxBesideItsCover(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		for _, palette := range []bool{false, true} {
			g := growFilm(t, theme, palette)
			checked := 0
			for _, way := range []struct {
				name   string
				frames []growFrame
			}{{"in", g.in}, {"back", g.out}} {
				for i, f := range way.frames {
					if f.surface.W <= 0 || f.surface.W > g.main.W/2 {
						continue
					}
					cover := g.coverAt(f.q)
					for _, m := range g.marks {
						if !inside(m, f.surface) || overlaps(m, cover, 4) {
							continue
						}
						checked++
						r := pixels(m)
						if at, rest := lumaStd(f.img, r), lumaStd(g.rest, r); at < rest/3 {
							t.Errorf("%s, colors %v, way %s, frame %d (q %.3f): the surface %v hides the screen under at %v (detail %.1f of %.1f at rest): an empty box of the page's ground", theme, palette, way.name, i, f.q, f.surface, m, at, rest)
						}
					}
				}
			}
			if checked == 0 {
				t.Errorf("%s, colors %v: no word under the growing page to watch", theme, palette)
			}
		}
	}
}

// demoSearch is the demo's search for "glass weather" in theme, its
// results and covers in.
func demoSearch(t *testing.T, theme string) (*App, *ui.Tester) {
	t.Helper()
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
	return a, tt
}

// Going back, the screen under the page is clear by the time the page has
// gone into its tile, not sharpening on its own over the frames after: in
// the demo's search for "glass weather", every frame of the way back in
// which the page is all but gone beside its cover shows the song rows
// within a level of rest.
func TestTheScreenUnderIsClearOnceThePageIsInItsTile(t *testing.T) {
	for _, theme := range []string{"dark", "light"} {
		a, tt := demoSearch(t, theme)
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
			out = append(out, growFrame{img: cloneImage(tt.Image()), q: a.fly.q, surface: a.fly.surface})
		}
		for range 10 {
			tt.Advance(filmFrame)
		}
		after := cloneImage(tt.Image())
		for i, f := range out {
			page, under := meanAbsDiff(after, f.img, beside(cover)[0]), meanAbsDiff(after, f.img, rows)
			if page <= 2 && under > 1 {
				t.Errorf("%s: frame %d of the way back (q %.3f): the page is all but gone beside its cover (%.2f levels) while the song rows are still %.2f levels from rest", theme, i, f.q, page, under)
			}
		}
	}
}

// edgeDetail is how many sharp steps, as a word's strokes or a row's line
// make, the window takes from pixel to pixel along a line: down column x
// from y0 to y1, or, with across, along row x from y0 to y1. A light's
// smooth ramp takes none.
func edgeDetail(img *image.RGBA, x, y0, y1 int, across bool) float64 {
	n := 0.0
	for y := y0 + 1; y < y1; y++ {
		d := luma(img, x, y) - luma(img, x, y-1)
		if across {
			d = luma(img, y, x) - luma(img, y-1, x)
		}
		if math.Abs(d) > 8 {
			n++
		}
	}
	return n
}

// A page growing from a cover is not cut by its surface's right and bottom
// edges as hard lines through its words and rows: while its parts show and
// the surface is still smaller than the column, the page right at those
// edges has a fraction of the sharp detail it has a little further in.
func TestAGrowingPageIsNotCutByAHardEdge(t *testing.T) {
	for _, theme := range []string{"dark", "light"} {
		a, tt := demoSearch(t, theme)
		if err := tt.Click("Glass Weather"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		var right, rightIn, bottom, bottomIn float64
		frames := 0
		for range 90 {
			tt.Advance(filmFrame)
			f := &a.fly
			if !f.moving {
				break
			}
			main := f.main
			box, _ := f.growBox(main)
			if !f.growing() || a.lateShown() < 0.5 || box.W > main.W-60 || box.H > main.H-60 {
				continue
			}
			frames++
			img := tt.Image()
			x0, y0 := int(main.X+box.X), int(main.Y+box.Y)
			x1, y1 := x0+int(box.W), y0+int(box.H)
			right += edgeDetail(img, x1-2, y0+40, y1-40, false)
			rightIn += edgeDetail(img, x1-40, y0+40, y1-40, false)
			bottom += edgeDetail(img, y1-2, x0+40, x1-40, true)
			bottomIn += edgeDetail(img, y1-40, x0+40, x1-40, true)
		}
		if frames == 0 {
			t.Fatalf("%s: no frame showed the page's parts on a surface smaller than the column", theme)
		}
		if right > 0.3*rightIn {
			t.Errorf("%s: over %d frames the page at its surface's right edge has %.0f sharp steps against %.0f further in: cut by a hard edge", theme, frames, right, rightIn)
		}
		if bottom > 0.3*bottomIn {
			t.Errorf("%s: over %d frames the page at its surface's bottom edge has %.0f sharp steps against %.0f further in: cut by a hard edge", theme, frames, bottom, bottomIn)
		}
	}
}

// While the page's parts fade in at the surface's edges, the cover flies
// on its own way: in each frame of the way in, the cover, dark on the light
// page, is drawn on the line from its tile to its place on the page, as far
// along as the page has come (q), not off it.
func TestAGrowingPagesCoverKeepsItsWay(t *testing.T) {
	g := growFilm(t, "light", true)
	for i, f := range g.in {
		r := g.coverAt(f.q)
		for _, p := range [][2]float32{{r.X + 8, r.Y + r.H/2}, {r.X + r.W/2, r.Y + 8}, {r.X + r.W - 8, r.Y + r.H/2}} {
			if l := luma(f.img, int(p[0]), int(p[1])); l > 90 {
				t.Errorf("frame %d (q %.3f): the cover should be at %v, but %v is %.0f bright: it is off its way", i, f.q, r, p, l)
				break
			}
		}
	}
}
