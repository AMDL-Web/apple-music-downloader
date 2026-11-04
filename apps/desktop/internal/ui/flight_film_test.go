//go:build !race

package ui

import (
	"fmt"
	"image"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
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
		if a.fly.moving && a.fly.lead() == 0 {
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
		if !a.fly.moving {
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

// A failed download's row shows its cover grey and dimmed; the page's cover
// leaves it so, and colors in as it flies, rather than in full color from
// the first frame.
func TestARowsGreyCoverColorsInAsItFlies(t *testing.T) {
	a, run := demoApp(t)
	a.PageMotion = true
	s := a.Store
	run(func() bool { return s.Loaded })
	var failed *store.Job
	for _, j := range s.Jobs {
		if j.Status == domain.JobFailed {
			failed = j
		}
	}
	if failed == nil {
		t.Fatal("the demo has no failed download")
	}
	a.Go(ScreenQueue)
	a.Tab = int(store.TabFailed)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	run(func() bool {
		tt.Advance(filmFrame)
		return s.Queue.List(store.TabFailed).Status == store.SearchReady &&
			a.Art.Get(failed.ArtworkURL, 88) != nil && a.Art.Get(failed.ArtworkURL, 512) != nil
	})
	tt.Advance(time.Second)
	tt.Frame()
	rest := image.NewRGBA(tt.Image().Bounds())
	copy(rest.Pix, tt.Image().Pix)
	if err := tt.Click(failed.Name()); err != nil {
		t.Fatalf("no row for %q: %v", failed.Name(), err)
	}
	// In the first frame the page's cover is where the row's was.
	tt.Advance(filmFrame)
	if !a.fly.moving || !a.fly.src[partCover].ok {
		t.Fatal("the page does not fly from the row")
	}
	box := a.fly.src[partCover].box
	first, still := saturation(tt.Image(), box), saturation(rest, box)
	flyFrames(a, tt, 120)
	page, ok := tt.Find("Artwork")
	if !ok {
		t.Fatal("no cover on the page")
	}
	landed := saturation(tt.Image(), page)
	if landed < 40 || landed < still+20 {
		t.Fatalf("the landed cover shows saturation %d, the row's %d: no colors to compare", landed, still)
	}
	if first > still+(landed-still)/4 {
		t.Errorf("the cover left the grey row (saturation %d) with %d of the landed %d: it was in color from the start", still, first, landed)
	}
}

// A recent cover leaves in its own colors, whatever the rows under it
// show: a queued row's grey is that row's alone.
func TestARecentCoverLeavesInItsColors(t *testing.T) {
	a, run := demoApp(t)
	a.PageMotion = true
	s := a.Store
	run(func() bool { return s.Loaded && len(s.Finished()) > 1 })
	fin := s.Finished()
	j := fin[len(fin)-1]
	a.Go(ScreenQueue)
	tt := ui.NewTester(a.View, 1280, 800)
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
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(time.Second)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(filmFrame)
	if !a.fly.moving || a.fly.from != "wall:"+j.ID {
		t.Fatalf("the page does not fly from the recent cover: %q", a.fly.from)
	}
	first := saturation(tt.Image(), a.fly.src[partCover].box)
	flyFrames(a, tt, 120)
	page, ok := tt.Find("Artwork")
	if !ok {
		t.Fatal("no cover on the page")
	}
	if landed := saturation(tt.Image(), page); landed < 40 || first < landed/2 {
		t.Errorf("the recent cover left with saturation %d of the landed %d: it left grey", first, landed)
	}
}

// saturation is the most saturated of a few points in the middle of r.
func saturation(img *image.RGBA, r ui.Rect) int {
	s := float32(img.Bounds().Dx()) / 1280
	hi := 0
	for dy := float32(-0.3); dy <= 0.3; dy += 0.1 {
		for dx := float32(-0.3); dx <= 0.3; dx += 0.1 {
			p := img.RGBAAt(int((r.X+r.W*(0.5+dx))*s), int((r.Y+r.H*(0.5+dy))*s))
			hi = max(hi, int(max(p.R, p.G, p.B))-int(min(p.R, p.G, p.B)))
		}
	}
	return hi
}

// contrast is the spread of luminance over r, by every other pixel: high
// where a screen shows its content, low where it is blank ground.
func contrast(tt *ui.Tester, r image.Rectangle) float64 {
	img := tt.Image()
	s := img.Bounds().Dx() / 1280
	var sum, sq, n float64
	for y := r.Min.Y; y < r.Max.Y; y += 2 {
		for x := r.Min.X; x < r.Max.X; x += 2 {
			p := img.RGBAAt(x*s, y*s)
			l := 0.2126*float64(p.R) + 0.7152*float64(p.G) + 0.0722*float64(p.B)
			sum, sq, n = sum+l, sq+l*l, n+1
		}
	}
	m := sum / n
	return math.Sqrt(max(sq/n-m*m, 0))
}

// Opening an album from its tile and going back, the search and the page
// dissolve into each other: no frame shows the window blank, its content
// gone and the other's not yet there, which flashes as white ground.
func TestTheScreensNeverGoBlankBetween(t *testing.T) {
	a, run := demoApp(t)
	a.Screen, a.PageMotion, a.Theme = ScreenHome, true, "light"
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	s := a.Store
	s.SetOmniText("a")
	tt.Frame()
	run(func() bool { tt.Frame(); return s.Search.Status == store.SearchReady })
	run(func() bool {
		tt.Advance(100 * time.Millisecond)
		for _, h := range s.Search.ShownHits() {
			if a.Art.Get(h.ArtworkURL, 320) == nil || a.Art.Get(h.ArtworkURL, 512) == nil {
				return false
			}
		}
		return true
	})
	tt.Advance(time.Second)
	h := s.Search.Result.Albums.Items[0]
	tile, ok := tt.Find(h.Name)
	if !ok {
		t.Fatal("no tile")
	}
	// The right of the window, which the cover does not cross.
	r := image.Rect(700, 300, 1250, 780)
	film := func(way string) {
		t.Helper()
		start, low := contrast(tt, r), math.Inf(1)
		for range 60 {
			tt.Advance(filmFrame)
			if !a.fly.moving {
				break
			}
			low = min(low, contrast(tt, r))
		}
		tt.Advance(time.Second)
		end := contrast(tt, r)
		if low < 0.5*min(start, end) {
			t.Errorf("%s, a frame shows the window's right with contrast %.1f, against %.1f before and %.1f after: it went blank", way, low, start, end)
		}
	}
	tt.ClickAt(tile.X+tile.W/2, tile.Y+tile.H/2)
	film("opening")
	run(func() bool { tt.Advance(100 * time.Millisecond); return s.Search.Album.Status == store.SearchReady })
	tt.Advance(time.Second)
	a.Back()
	tt.Frame()
	film("going back")
}

// panelFilm is the album page coming in from a search tile, or going back
// to it when out, one image of the window per frame at 60 Hz with the lower
// panel's box in it, the page's songs named track and the other tiles of
// the results named other.
func panelFilm(t *testing.T, theme, track, other string, out bool) (frames []*image.RGBA, panels []image.Rectangle) {
	t.Helper()
	a, albums := searchApp(t)
	a.PageMotion, a.Theme = true, theme
	for i := 1; i < len(albums); i++ {
		albums[i].Name = fmt.Sprintf("%s %d", other, i)
		a.Store.Search.Result.Albums.Items[i] = albums[i]
	}
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	if err := tt.Click(albums[0].Name); err != nil {
		t.Fatal(err)
	}
	// Read before: an album already cached shows its songs from the first
	// frame of the flight.
	p := &a.Store.Search.Album
	p.Status, p.Album = store.SearchReady, client.Album{ID: albums[0].ID, Title: albums[0].Name, ArtistName: "Yorushika", Tracks: []client.AlbumTrack{
		{ID: "t1", DiscNumber: 1, TrackNumber: 1, Name: track + " one", DurationMS: 200_000},
		{ID: "t2", DiscNumber: 1, TrackNumber: 2, Name: track + " two", DurationMS: 180_000},
		{ID: "t3", DiscNumber: 1, TrackNumber: 3, Name: track + " three", DurationMS: 190_000},
	}}
	if out {
		flyFrames(a, tt, 120)
		a.Back()
		tt.Frame()
	}
	for range 120 {
		tt.Advance(filmFrame)
		img := tt.Image()
		frames = append(frames, &image.RGBA{Pix: append([]uint8(nil), img.Pix...), Stride: img.Stride, Rect: img.Rect})
		r, _ := tt.Find("Tracks")
		panels = append(panels, image.Rect(0, int(r.Y), 1280, min(int(r.Y+r.H), 800)))
		if !a.fly.moving {
			break
		}
	}
	return frames, panels
}

// differs reports whether a and b differ at x, y.
func differs(a, b *image.RGBA, x, y int) bool {
	i := a.PixOffset(x, y)
	d := 0
	for k := range 3 {
		d += max(int(a.Pix[i+k])-int(b.Pix[i+k]), int(b.Pix[i+k])-int(a.Pix[i+k]))
	}
	return d > 12
}

// flightFrames counts the frames of a flight, from the next frame on.
func flightFrames(a *App, tt *ui.Tester) int {
	n := 0
	for range 150 {
		tt.Advance(filmFrame)
		n++
		if !a.fly.moving {
			break
		}
	}
	return n
}

// The way out takes about as long as the way in, within a third: a page
// that leaves at half the time it came in does not look like the same
// motion run back.
func TestTheWayOutTakesAboutAsLongAsTheWayIn(t *testing.T) {
	a, albums := searchApp(t)
	a.PageMotion, a.Theme = true, "light"
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	tt.Frame()
	if err := tt.Click(albums[0].Name); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	in := flightFrames(a, tt)
	a.Back()
	tt.Frame()
	out := flightFrames(a, tt)
	if r := float64(max(in, out)) / float64(min(in, out)); r > 1.3 {
		t.Errorf("the way in took %d frames and the way out %d: %.2f times", in, out, r)
	}
}

// A tile the screen under the page cuts off at the top of its list is cut
// the same way by the cover that lands on it: the frame the flight ends
// changes the tile's place no more than the frames before, rather than the
// cover flying over the bar above the list and vanishing.
func TestTheCoverLandsCutAsItsTileIs(t *testing.T) {
	a, albums := searchApp(t)
	for i := len(albums); i < 60; i++ {
		albums = append(albums, client.SearchHit{Type: "album", ID: fmt.Sprint(100 + i), Name: fmt.Sprintf("Album %d", i), ArtistName: "Yorushika", TrackCount: 3,
			URL: fmt.Sprintf("https://music.apple.com/cn/album/x/%d", 100+i)})
	}
	albums[1].ArtworkBgColor = "203040"
	a.Store.Search.Result.Albums.Items = albums
	a.Store.FocusGroup("albums")
	a.PageMotion, a.Theme = true, "light"
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	tt.Frame()
	tt.Advance(time.Second)
	st := a.scrolls["home-body"]
	if st == nil || st.MaxY < 300 {
		t.Fatalf("the results do not scroll: %+v", st)
	}
	// The first row's covers have their top 60 DIPs under the bar above the
	// list.
	cover, ok := tt.Find(albums[1].Name)
	if !ok {
		t.Fatalf("no tile: %q", tt.Texts())
	}
	st.Y = cover.Y - (124 - 60)
	tt.Frame()
	tt.Advance(time.Second)
	if r, ok := tt.Find(albums[1].Name); !ok || r.H > 150 {
		t.Fatalf("the first row's cover shows %v after scrolling to %v, not cut", r, st.Y)
	}
	if err := tt.Click(albums[1].Name); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for range 150 {
		tt.Advance(filmFrame)
		if !a.fly.moving {
			break
		}
	}
	a.Back()
	tt.Frame()
	var frames []*image.RGBA
	var box ui.Rect
	var clipped float32
	for range 150 {
		tt.Advance(filmFrame)
		frames = append(frames, cloneImage(tt.Image()))
		if src := a.fly.src[partCover]; src.ok {
			box = src.box
			clipped, _ = a.fly.flightClip(box)
		}
		if !a.fly.moving {
			break
		}
	}
	if clipped < 20 {
		t.Logf("cover %v, scrolled %v of %v, box %v, views %v", cover, st.Y, st.MaxY, box, a.fly.viewsPrev)
		t.Fatalf("the tile is cut %.0f DIPs at the top of its list, not enough to show", clipped)
	}
	region := image.Rect(int(box.X)-10, int(box.Y)-10, int(box.X+box.W)+10, int(box.Y+box.H)+10).Intersect(frames[0].Rect)
	step := func(i int) float64 { return meanAbsDiff(frames[i-1], frames[i], region) }
	end := len(frames) - 1
	var before float64
	for i := end - 5; i < end; i++ {
		before = max(before, step(i))
	}
	if at := step(end); at > max(1.5*before, 1) {
		t.Errorf("the frame the flight ends changes the tile's place by %.2f levels, against at most %.2f in the five before: the cover does not land cut as its tile is", at, before)
	}
}

// meanAbsDiff is the mean change of the color channels over r between two
// frames, in levels.
func meanAbsDiff(a, b *image.RGBA, r image.Rectangle) float64 {
	s, n := 0.0, 0.0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := a.PixOffset(x, y)
			for k := range 3 {
				s += math.Abs(float64(a.Pix[i+k]) - float64(b.Pix[i+k]))
			}
			n += 3
		}
	}
	if n == 0 {
		return 0
	}
	return s / n
}
