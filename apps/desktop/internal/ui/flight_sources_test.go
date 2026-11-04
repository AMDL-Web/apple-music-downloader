//go:build !race

package ui

import (
	"fmt"
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Two tiles can stand for one album, as two copies of it in the Apple
// library do: the page flies from the one clicked and back to it, and the
// other stays as it is. Going back, in each of the flight's last frames,
// both tiles' covers show as they do once it has landed. The twin is built
// before the tile clicked and after it, so building order alone picks it
// either way.
func TestOnlyTheTileClickedIsTheFlightsSource(t *testing.T) {
	for _, pair := range [][2]int{{0, 2}, {2, 0}} {
		t.Run(fmt.Sprintf("click %d, twin %d", pair[0], pair[1]), func(t *testing.T) { onlyTheTileClicked(t, pair[0], pair[1]) })
	}
}

func onlyTheTileClicked(t *testing.T, click, other int) {
	a, albums := searchApp(t)
	a.PageMotion, a.Theme = true, "light"
	twin := albums[other]
	twin.ID, twin.URL, twin.Resource = albums[click].ID, albums[click].URL, albums[click].Resource
	a.Store.Search.Result.Albums.Items[other] = twin
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	for range 10 {
		tt.Advance(filmFrame)
	}
	clicked, ok := tt.Find(albums[click].Name)
	at, ok2 := tt.Find(twin.Name)
	if !ok || !ok2 {
		t.Fatal("no tiles")
	}
	if err := tt.Click(albums[click].Name); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
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
	if a.Screen != ScreenAlbum || cover.W == 0 {
		t.Fatalf("the page did not open from a tile: screen %v", a.Screen)
	}
	// The covers sit above their titles, as far over as the tiles are apart.
	twinCover := pixels(ui.Rect{X: cover.X + at.X - clicked.X, Y: cover.Y + at.Y - clicked.Y, W: cover.W, H: cover.H})
	if cx := cover.X + cover.W/2; cx < clicked.X || cx > clicked.X+clicked.W {
		t.Errorf("the page flew from %v, not the tile clicked at %v", cover, clicked)
	}
	a.Back()
	tt.Frame()
	var out []*image.RGBA
	for range 90 {
		tt.Advance(filmFrame)
		if !a.fly.moving {
			break
		}
		out = append(out, cloneImage(tt.Image()))
	}
	for range 10 {
		tt.Advance(filmFrame)
	}
	after := cloneImage(tt.Image())
	for i := max(0, len(out)-4); i < len(out); i++ {
		if d := meanAbsDiff(after, out[i], pixels(cover)); d > 6 {
			t.Errorf("frame %d of the way back: the clicked tile's cover is %.1f levels from its landed look", i, d)
		}
		if d := meanAbsDiff(after, out[i], twinCover); d > 6 {
			t.Errorf("frame %d of the way back: the other tile's cover is %.1f levels from its landed look", i, d)
		}
	}
}
