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
	"amdl/internal/client"
)

// growFrame is a frame of a page growing from its tile: the window as it
// showed, q, the surface's box in the window and how much of the page's
// parts show (lateShown).
type growFrame struct {
	img     *image.RGBA
	q       float32
	surface ui.Rect
	parts   float32
}

// growFilmed is a page growing from the first tile of a search or the
// library, or the queue's last recent cover, filmed both ways: rest is the
// screen before the click, landed the page once in, after the screen once
// back, from the cover clicked in the window, to the page's cover once
// landed, main the main column, and marks the boxes of the tiles' words on
// the screen under.
type growFilmed struct {
	in, out             []growFrame
	rest, landed, after *image.RGBA
	from, to            ui.Rect
	main                ui.Rect
	marks               []ui.Rect
}

// growFilms keeps each film for every test that watches it.
var growFilms = map[string]*growFilmed{}

// growFilm opens the first album of scene, "search" or the demo's
// "library", from its tile in theme, in its own colors when palette (the
// search's), or, in the demo's "queue", the last of its recent covers, and
// films the way in until the page lands, then the way back until it is in
// its cover again.
func growFilm(t *testing.T, scene, theme string, palette bool) *growFilmed {
	t.Helper()
	key := fmt.Sprint(scene, theme, palette)
	if g := growFilms[key]; g != nil {
		return g
	}
	var a *App
	var tt *ui.Tester
	var words [][2]string
	var click func()
	switch scene {
	case "queue":
		var run func(func() bool)
		a, run = demoApp(t)
		a.PageMotion, a.Theme = true, theme
		s := a.Store
		run(func() bool { return s.Loaded && len(s.Finished()) > 1 })
		fin := s.Finished()
		j := fin[len(fin)-1]
		a.Go(ScreenQueue)
		tt = ui.NewTester(a.View, 1280, 800)
		tt.SetScale(1)
		run(func() bool {
			tt.Advance(filmFrame)
			return a.Art.Get(j.ArtworkURL, 160) != nil && a.Art.Get(j.ArtworkURL, 512) != nil
		})
		tt.Advance(time.Second)
		r, ok := tt.Find(j.Name())
		if !ok {
			t.Fatalf("no recent cover %q", j.Name())
		}
		click = func() {
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			tt.Advance(time.Second)
			tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
		}
	case "library":
		var run func(func() bool)
		a, run = demoApp(t)
		a.PageMotion, a.Theme = true, theme
		s := a.Store
		run(func() bool { return s.Loaded })
		a.Go(ScreenLibrary)
		tt = ui.NewTester(a.View, 1280, 800)
		tt.SetScale(1)
		run(func() bool {
			tt.Advance(filmFrame)
			return s.Library.AppleStatus == store.SearchReady
		})
		for _, it := range s.Library.Apple.Items[:min(6, len(s.Library.Apple.Items))] {
			words = append(words, [2]string{it.Name, it.ArtistName})
		}
		if len(words) == 0 {
			t.Fatal("the demo's library is empty")
		}
		// Its tiles show once its answers after the list land.
		run(func() bool {
			tt.Advance(250 * time.Millisecond)
			return tt.HasText(words[0][0])
		})
	default:
		var albums []client.SearchHit
		a, albums = searchApp(t)
		a.PageMotion, a.Theme = true, theme
		if palette {
			// Its colors light the page (paletteBackdrop).
			albums[0].ArtworkBgColor = "203040"
			a.Store.Search.Result.Albums.Items[0] = albums[0]
		}
		tt = ui.NewTester(a.View, 1280, 800)
		tt.SetScale(1)
		for _, h := range albums[:4] {
			words = append(words, [2]string{h.Name, h.ArtistName})
		}
	}
	for range 10 {
		tt.Advance(filmFrame)
	}
	g := &growFilmed{rest: cloneImage(tt.Image())}
	for _, w := range words {
		for _, x := range w {
			// A word, not a cover named by it.
			if r, ok := tt.Find(x); ok && r.H < 40 && !slices.Contains(g.marks, r) {
				g.marks = append(g.marks, r)
			}
		}
	}
	if click != nil {
		click()
	} else if err := tt.Click(words[0][0]); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
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
			frames = append(frames, growFrame{img: cloneImage(tt.Image()), q: a.fly.q, surface: a.fly.surface, parts: a.lateShown()})
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
	g.to, g.landed = to, cloneImage(tt.Image())
	a.Back()
	g.out = film()
	for range 10 {
		tt.Advance(filmFrame)
	}
	g.after = cloneImage(tt.Image())
	if g.from.W == 0 || len(g.in) == 0 || len(g.out) == 0 || len(g.marks) == 0 && scene != "queue" {
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

// strokesAlike is how alike the strokes of img and rest are over r, from
// -1 to 1, by the steps the luma takes from pixel to pixel along each row:
// near 1 where img shows the words rest does, even blurred and dimmed, near
// 0 where it shows other words, even on the same line.
func strokesAlike(img, rest *image.RGBA, r image.Rectangle) float64 {
	var sa, sb, saa, sbb, sab, n float64
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X + 1; x < r.Max.X; x++ {
			a, b := luma(img, x, y)-luma(img, x-1, y), luma(rest, x, y)-luma(rest, x-1, y)
			sa, sb, saa, sbb, sab, n = sa+a, sb+b, saa+a*a, sbb+b*b, sab+a*b, n+1
		}
	}
	va, vb := saa/n-(sa/n)*(sa/n), sbb/n-(sb/n)*(sb/n)
	if va <= 0 || vb <= 0 {
		return 0
	}
	return (sab/n - sa/n*sb/n) / math.Sqrt(va*vb)
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

// What grows from a cover is the page, neither an empty box of its ground
// ahead of it nor a veil the screen under shows through: in every frame,
// both ways, of a search (light and dark, with and without the album's
// colors) and the library, for each tile's word on the screen under that
// the flying cover is not over: until the page is half the column wide,
// while none of its parts show yet, or outside its surface, the word shows
// through as at rest, blurred and dimmed, with at least a third of its
// detail; in any frame, a word the surface covers does not show through
// it at all.
func TestAGrowingPageIsNeitherAnEmptyBoxNorAVeil(t *testing.T) {
	for _, film := range []struct {
		scene, theme string
		palette      bool
	}{{"search", "light", false}, {"search", "light", true}, {"search", "dark", false}, {"search", "dark", true}, {"library", "light", false}, {"library", "dark", false}} {
		g := growFilm(t, film.scene, film.theme, film.palette)
		name := fmt.Sprintf("%s, %s, colors %v", film.scene, film.theme, film.palette)
		checked := [2]int{}
		for _, way := range []struct {
			name   string
			frames []growFrame
		}{{"in", g.in}, {"back", g.out}} {
			for i, f := range way.frames {
				if f.surface.W <= 0 {
					continue
				}
				half := f.surface.W <= g.main.W/2
				cover := g.coverAt(f.q)
				for _, m := range g.marks {
					if overlaps(m, cover, 4) {
						continue
					}
					r := pixels(m)
					rest := lumaStd(g.rest, r)
					if rest < 5 {
						// Too faint to tell.
						continue
					}
					switch {
					case half && (f.parts == 0 || !overlaps(m, f.surface, 0)):
						checked[0]++
						if at := lumaStd(f.img, r); at < rest/3 {
							t.Errorf("%s, way %s, frame %d (q %.3f): the screen under at %v is hidden (detail %.1f of %.1f at rest) with none of the page shown there: an empty box of its ground (surface %v)", name, way.name, i, f.q, m, at, rest, f.surface)
						}
					case inside(m, f.surface):
						checked[1]++
						if c := strokesAlike(f.img, g.rest, r); c > 0.3 {
							t.Errorf("%s, way %s, frame %d (q %.3f): the screen under at %v shows through the page's surface %v (alike %.2f): a veil", name, way.name, i, f.q, m, f.surface, c)
						}
					}
				}
			}
		}
		if checked[0] == 0 || checked[1] == 0 {
			t.Errorf("%s: words watched %v, with none of the page shown and under it: too few", name, checked)
		}
	}
}

// flat reports whether img is plain around (x, y): its luma within 4
// levels over the pixels as far as r from there, as on a ground or a
// smooth light, not on a word's stroke or a cover's edge.
func flat(img *image.RGBA, x, y, r int) bool {
	b := img.Bounds()
	if x < b.Min.X+r || y < b.Min.Y+r || x >= b.Max.X-r || y >= b.Max.Y-r {
		return true
	}
	lo, hi := 255.0, 0.0
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			l := luma(img, x+dx, y+dy)
			lo, hi = min(lo, l), max(hi, l)
		}
	}
	return hi-lo < 4
}

// A growing page shows no more of its ground than its content fills: in
// every frame, both ways, of a page growing from a search tile, a library
// tile and a recent cover in the queue, light and dark, beside the flying
// cover and in from the edges its parts fade at, the surface shows plain
// ground where the landed page, around its cover and at its cover's size,
// shows something (a stroke, an edge, or, once its parts are all in, a
// shade far from it) over no more than a fifth of the flying cover's area.
func TestAGrowingPageShowsNoGroundItsContentDoesNotFill(t *testing.T) {
	for _, film := range []struct{ scene, theme string }{
		{"search", "light"}, {"search", "dark"}, {"library", "light"}, {"library", "dark"}, {"queue", "light"}, {"queue", "dark"},
	} {
		g := growFilm(t, film.scene, film.theme, false)
		for _, way := range []struct {
			name   string
			frames []growFrame
		}{{"in", g.in}, {"back", g.out}} {
			for i, f := range way.frames {
				sf := f.surface
				cover := g.coverAt(f.q)
				if sf.W <= 0 || cover.W <= 0 {
					continue
				}
				k := g.to.W / cover.W
				empty := float32(0)
				for y := int(sf.Y) + 1; y < int(sf.Y+sf.H-growBand); y += 2 {
					for x := int(sf.X) + 1; x < int(sf.X+sf.W-growBand); x += 2 {
						p := ui.Rect{X: float32(x), Y: float32(y)}
						// Plain a little further round than the page's
						// pixel, which its scale draws smaller.
						if overlaps(p, cover, 4) || !flat(f.img, x, y, 2) {
							continue
						}
						// Where the landed page shows what lies there
						// from its cover, at its cover's size.
						lx := int(g.to.X + (float32(x)-cover.X)*k)
						ly := int(g.to.Y + (float32(y)-cover.Y)*k)
						if lx < int(g.main.X) || ly < int(g.main.Y) || lx >= int(g.main.X+g.main.W) || ly >= int(g.main.Y+g.main.H) {
							continue
						}
						if !flat(g.landed, lx, ly, 1) || f.parts >= 0.99 && math.Abs(luma(f.img, x, y)-luma(g.landed, lx, ly)) > 80 {
							empty += 4
						}
					}
				}
				if area := cover.W * cover.H; empty > area/5 {
					t.Errorf("%s, %s, way %s, frame %d (q %.3f): %.0f px of the surface %v is plain ground where the page shows something, more than a fifth of the cover's %.0f", film.scene, film.theme, way.name, i, f.q, empty, sf, area)
				}
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
// the surface is still smaller than the page, the page right at those
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
			main, s, p := f.main, f.surface, f.page
			box := ui.Rect{X: s.X - main.X, Y: s.Y - main.Y, W: s.W, H: s.H}
			if !f.growing() || a.lateShown() < 0.5 || box.W > p.W-60 || box.H > p.H-60 {
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
			t.Fatalf("%s: no frame showed the page's parts on a surface smaller than the page", theme)
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
	g := growFilm(t, "search", "light", true)
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
