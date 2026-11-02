//go:build !race

package ui

import (
	"fmt"
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
)

// The way back from an album to its search tile, watched frame by frame:
// the checks that only a film of the flight shows, the hand-over from the
// flying copies to the tile, what the pointer can do meanwhile and where
// the page's edge goes, which a check of the cover's boxes and of the end
// state passes either way.

// filmFrame is the Tester's frame interval, as the real window's at 60 Hz;
// the flight's spring is solved, not stepped, so this is the motion a
// faster display shows too.
const filmFrame = time.Second / 60

// filmApp opens albums[0], in the library and Hi-Res, from the search tiles
// in the light theme, where shadows show, and lets the page land; tile and
// other are albums[0]'s cover and albums[2]'s.
func filmApp(t *testing.T) (a *App, tt *ui.Tester, tile, other ui.Rect) {
	t.Helper()
	a, albums := searchApp(t)
	a.PageMotion, a.Theme = true, "light"
	albums[0].ArtworkBgColor, albums[0].AudioTraits = "203040", []string{"lossless", "hi-res-lossless"}
	a.Store.Search.Result.Albums.Items[0] = albums[0]
	tt = ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	var ok bool
	if tile, ok = tt.Find(albums[0].Name); !ok {
		t.Fatal("no tile")
	}
	if other, ok = tt.Find(albums[2].Name); !ok {
		t.Fatal("no other tile")
	}
	if err := tt.Click(albums[0].Name); err != nil {
		t.Fatal(err)
	}
	flyFrames(a, tt, 120)
	if a.Screen != ScreenAlbum || a.fly.moving {
		t.Fatalf("the page did not land: screen %v, moving %v", a.Screen, a.fly.moving)
	}
	return a, tt, tile, other
}

func meanLuma(img *image.RGBA, r image.Rectangle) float64 {
	s, n := 0.0, 0.0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.RGBAAt(x, y)
			s += 0.2126*float64(p.R) + 0.7152*float64(p.G) + 0.0722*float64(p.B)
			n++
		}
	}
	return s / n
}

// Going back, the flight ends as the cover lands, not over the spring's
// slow tail: the tile's shadow is there as the cover settles, rather than
// popping in later.
func TestTheWayBackEndsAsTheCoverLands(t *testing.T) {
	a, tt, tile, _ := filmApp(t)
	a.Back()
	tt.Frame()
	under := image.Rect(int(tile.X+tile.W/2)-30, int(tile.Y+tile.H)+4, int(tile.X+tile.W/2)+30, int(tile.Y+tile.H)+8)
	// The shadow comes in with the screen under, frame by frame: the frame
	// the flight ends changes it no more than one before did.
	landed, prev, before, last := 0, -1.0, 0.0, 0.0
	for range 60 {
		tt.Advance(filmFrame)
		if a.fly.moving && a.fly.travel() == 0 {
			landed++
		}
		lum := meanLuma(tt.Image(), under)
		if prev >= 0 {
			if a.fly.moving {
				before = max(before, prev-lum)
			} else {
				last = prev - lum
			}
		}
		prev = lum
		if !a.fly.moving {
			break
		}
	}
	if landed > 1 {
		t.Errorf("the cover rested on its tile for %d frames before the flight ended", landed)
	}
	if last > 1 && last > 1.5*before {
		t.Errorf("the tile's shadow darkened by %.1f in the frame the flight ended, against at most %.1f a frame before: it popped in", last, before)
	}
}

// Going back, the other tiles take a click as soon as the cover has landed,
// not only once the screens are done crossing.
func TestAnotherTileTakesAClickOnceTheCoverLands(t *testing.T) {
	a, tt, _, other := filmApp(t)
	a.Back()
	tt.Frame()
	for range 60 {
		tt.Advance(filmFrame)
		if a.fly.travel() == 0 {
			break
		}
	}
	for range 3 {
		tt.Advance(filmFrame)
	}
	tt.ClickAt(other.X+other.W/2, other.Y+other.H/2)
	if a.Screen != ScreenAlbum || a.fly.from != "hit:"+store.HitKey(a.Store.Search.Result.Albums.Items[2]) {
		t.Errorf("a click on another tile three frames after the cover landed opened screen %v from %q", a.Screen, a.fly.from)
	}
}

// Going back, the page's lower panel lands on the top of the results, so
// its edge is above the tiles when they show, not across them.
func TestThePagesEdgeLeavesAboveTheTiles(t *testing.T) {
	a, tt, tile, _ := filmApp(t)
	a.Back()
	tt.Frame()
	for range 60 {
		tt.Advance(filmFrame)
		if !a.fly.moving {
			break
		}
		panel, ok := tt.Find("Tracks")
		if ok && a.fly.q <= crossover+0.25 && panel.Y > tile.Y {
			t.Fatalf("at q %.2f the page's edge is at %v, across the tiles from %v", a.fly.q, panel.Y, tile.Y)
		}
	}
}

// An album in the library shows its check, which gives way to the button
// that downloads it again under the pointer by crossing over, not at once.
func TestTheChecksChangeCrossesOver(t *testing.T) {
	a, albums := searchApp(t)
	a.PageMotion, a.Theme = true, "light"
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	tt.Advance(time.Second)
	mark, ok := tt.Find(albums[0].Name + " is in your library: download again")
	if !ok {
		t.Fatal("no mark")
	}
	at := func() float64 {
		r := image.Rect(int(mark.X+mark.W/2)-3, int(mark.Y+mark.H/2)+5, int(mark.X+mark.W/2)+3, int(mark.Y+mark.H/2)+8)
		img := tt.Image()
		g, n := 0.0, 0.0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				p := img.RGBAAt(x, y)
				g += float64(p.G) - (float64(p.R)+float64(p.B))/2
				n++
			}
		}
		return g / n
	}
	green := at()
	tt.Move(mark.X+mark.W/2, mark.Y+mark.H/2)
	tt.Advance(filmFrame)
	mid := at()
	tt.Advance(time.Second)
	gone := at()
	if green < 30 || gone > green/4 {
		t.Fatalf("the check shows green %.0f at rest and %.0f under the pointer", green, gone)
	}
	if mid < gone+(green-gone)/4 {
		t.Errorf("a frame after the pointer came, the check shows %.0f of %.0f: it left at once", mid, green)
	}
}

// Opening an artist from far down the results, the results fade out where
// they are, then the search goes to its top and the artist's albums rise
// in: its header shows, and nothing of the results is drawn over them.
func TestAnArtistOpensFromTheTopOfItsOwnView(t *testing.T) {
	a, run := demoApp(t)
	a.PageMotion = true
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenHome)
	s.SetOmniText("qqqq")
	s.WantSearch("qqqq")
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(0.5)
	run(func() bool {
		tt.Advance(filmFrame)
		return s.Search.Status == store.SearchReady && s.Search.Result != nil && s.Search.Result.Artists != nil
	})
	tt.Advance(time.Second)
	st := a.scrolls["home-body"]
	if st == nil {
		t.Fatal("the results do not scroll")
	}
	st.Y = 500
	tt.Frame()
	tt.Advance(time.Second)
	if st.Y != 500 {
		t.Fatalf("the results scrolled to %v, want 500", st.Y)
	}
	// As MyGO!!!!! in the recording: read already, with more albums than
	// a window holds.
	h := s.Search.Result.Artists.Items[0]
	var many []client.SearchHit
	for i := range 25 {
		al := s.Search.Result.Albums.Items[i%len(s.Search.Result.Albums.Items)]
		al.ID = fmt.Sprint("many", i)
		many = append(many, al)
	}
	a.openArtist(h)
	ar := &s.Search.Artist
	ar.ID, ar.Status, ar.Artist, ar.Albums = h.ID, store.SearchReady, h, client.HitGroup{Items: many, NextOffset: 25}
	tt.Frame()
	start := tt.Now()
	for tt.Now().Sub(start) < groupFade.Duration-filmFrame {
		tt.Advance(filmFrame)
		if st.Y != 500 {
			t.Fatalf("%v in, while the results fade, the search scrolled to %v", tt.Now().Sub(start), st.Y)
		}
	}
	tt.Advance(time.Second)
	back, ok := tt.Find("Back to the results")
	if st.Y != 0 || !ok || back.H <= 0 || back.Y < 0 || back.Y > 800 {
		t.Errorf("the artist's page shows from %v, its header at %v (%v)", st.Y, back, ok)
	}
}
