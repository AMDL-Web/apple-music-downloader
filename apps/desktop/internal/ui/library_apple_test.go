package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/artwork"
	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/demo"
	"amdl/internal/domain"
)

// bulkApple is the demo's backend with an Apple library of albums albums,
// "Album 0000" on, served a page at a time as the API does.
type bulkApple struct {
	rt     http.RoundTripper
	albums []domain.AppleLibraryItem
}

func (b *bulkApple) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet || r.URL.Path != "/api/v1/apple-library/albums" {
		return b.rt.RoundTrip(r)
	}
	q := r.URL.Query()
	off, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	// The API's most per page.
	limit = min(max(limit, 1), 200)
	off = min(off, len(b.albums))
	page := client.AppleLibraryPage{Kind: "albums", Items: b.albums[off:min(off+limit, len(b.albums))], Total: len(b.albums), Offset: off}
	body, err := json.Marshal(page)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
}

// bulkApp is demoApp with an Apple library of n albums, shown in a w×800
// window on the Apple tab.
func bulkApp(t *testing.T, n, w int) (*App, *ui.Tester, func(until func() bool)) {
	t.Helper()
	a, _ := newTestApp(remoteSettings())
	b := &bulkApple{rt: demo.New(time.Now).Transport()}
	for i := range n {
		id := strconv.Itoa(9100000 + i)
		b.albums = append(b.albums, domain.AppleLibraryItem{ID: "l." + id, CatalogID: id, URL: "https://music.apple.com/us/album/x/" + id,
			Name: fmt.Sprintf("Album %04d", i), ArtistName: fmt.Sprintf("Artist %d", i%37)})
		if i%7 == 1 {
			b.albums[i].Name += " (Deluxe Edition) [Remastered] and Other Songs From the Long Night"
		}
	}
	c, err := client.NewWith(demo.Address, b)
	if err != nil {
		t.Fatal(err)
	}
	queue := make(chan func(), 1024)
	post := func(fn func()) { queue <- fn }
	a.Store = store.New(post, nil)
	a.Art = artwork.NewWith(post, b)
	a.Store.Connect(c)
	t.Cleanup(a.Store.Close)
	run := func(until func() bool) {
		t.Helper()
		deadline := time.After(30 * time.Second)
		for !until() {
			select {
			case fn := <-queue:
				fn()
			case <-time.After(50 * time.Millisecond):
			case <-deadline:
				t.Fatal("timed out")
			}
		}
	}
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenLibrary)
	tt := ui.NewTester(a.View, w, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	run(func() bool { return s.Library.AppleStatus == store.SearchReady })
	tt.Frame()
	return a, tt, run
}

// pointAt rests the pointer on the element showing text and lets what
// shows under the pointer ease in.
func pointAt(t *testing.T, tt *ui.Tester, text string) {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("no element shows %q in %q", text, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
}

// builtAlbums counts the album tiles the frame built.
func builtAlbums(tt *ui.Tester) int {
	seen := map[string]bool{}
	for _, x := range tt.Texts() {
		if strings.HasPrefix(x, "Album ") {
			seen[x] = true
		}
	}
	return len(seen)
}

// A library of two thousand albums reads in whole, page by page, and the
// grid builds only the rows in view, down to the last album.
func TestTheAppleGridBuildsOnlyTheRowsInView(t *testing.T) {
	a, tt, _ := bulkApp(t, 2000, 1280)
	if n := len(a.Store.Library.Apple.Items); n != 2000 {
		t.Fatalf("read %d of 2000 albums", n)
	}
	t.Logf("first frame built %d tiles", builtAlbums(tt))
	tt.Frame()
	if n := builtAlbums(tt); n < 10 || n > 60 {
		t.Errorf("the grid built %d tiles at the top", n)
	}
	var spent time.Duration
	frames := 0
	for range 400 {
		if _, ok := tt.Find("Album 1999"); ok {
			break
		}
		t0 := time.Now()
		tt.Scroll(700, 500, 0, 600)
		spent += time.Since(t0)
		frames++
		if n := builtAlbums(tt); n > 60 {
			t.Fatalf("scrolled, the grid built %d tiles", n)
		}
	}
	if _, ok := tt.Find("Album 1999"); !ok {
		t.Fatalf("scrolled to the end, no last album: %q", tt.Texts())
	}
	t.Logf("%d scroll frames over 2000 albums, %v each with the software render", frames, spent/time.Duration(max(frames, 1)))
}

// The columns fill the page between its margins at any width, a row holds
// as many tiles as tileColumns says, and its covers stay in line whatever
// their titles' lines.
func TestTheAppleGridFillsTheWidth(t *testing.T) {
	for _, w := range []int{1280, 760} {
		t.Run(strconv.Itoa(w), func(t *testing.T) {
			a, tt, _ := bulkApp(t, 60, w)
			page := float32(w)
			if a.SidebarShown() {
				page -= sidebarWidth
			}
			cols := tileColumns(page - 2*libraryPadX)
			items := a.Store.Library.Apple.Items
			cover := func(i int) ui.Rect {
				r, ok := tt.Find(items[i].Name)
				if !ok {
					t.Fatalf("no %q in %q", items[i].Name, tt.Texts())
				}
				return r
			}
			first, last, next := cover(0), cover(cols-1), cover(cols)
			// Album 0001's title takes two lines; the covers stay in line.
			for i := 1; i < cols; i++ {
				if r := cover(i); r.Y != first.Y {
					t.Errorf("cover %d of the first row at %v, the first at %v", i, r.Y, first.Y)
				}
			}
			if next.Y <= first.Y {
				t.Errorf("row of %d: first %v, next %v", cols, first, next)
			}
			if right := last.X + last.W; abs32(right-(float32(w)-libraryPadX)) > 0.5 || abs32(first.X-(float32(w)-page+libraryPadX)) > 0.5 {
				t.Errorf("the row runs %v to %v in a page of %v", first.X, right, page)
			}
			if abs32(first.W-first.H) > 0.5 {
				t.Errorf("the cover is %v", first)
			}
		})
	}
}

// A tile shows its Add only while pointed at, which moves nothing, and
// adding shows the answer without a frame of waiting between.
func TestAnAppleTileAddsWithoutAFlash(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenLibrary)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	run(func() bool { return s.Library.AppleStatus == store.SearchReady })
	tt.Frame()
	if _, ok := tt.Find("Add Salt & Static"); ok {
		t.Fatal("a tile not pointed at shows its Add")
	}
	before := snapFrame(tt)
	pointAt(t, tt, "Salt & Static")
	after := snapFrame(tt)
	for text, r := range before.rects {
		if o, ok := after.rects[text]; ok && o != r {
			t.Errorf("pointing at a tile moves %q: %v to %v", text, r, o)
		}
	}
	if it := appleItem(s, "Salt & Static"); it.URL == "" {
		t.Fatalf("no Salt & Static in %+v", s.Library.Apple.Items)
	}
	noFlash(t, tt, "Add Salt & Static", func() {
		run(func() bool { return s.Library.Added[appleItem(s, "Salt & Static").URL] != "" })
	})
}

func appleItem(s *store.Store, name string) domain.AppleLibraryItem {
	for _, it := range s.Library.Apple.Items {
		if it.Name == name {
			return it
		}
	}
	return domain.AppleLibraryItem{}
}

// A library-only playlist's cover opens its tracks under its row, each
// with its own Add, and closes them again.
func TestALibraryOnlyPlaylistOpensItsTracksUnderItsRow(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenLibrary)
	a.AppleKind("playlists")
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	run(func() bool { return s.Library.AppleStatus == store.SearchReady })
	tt.Frame()
	cover, ok := tt.Find("Favourite Songs")
	if !ok {
		t.Fatalf("no Favourite Songs in %q", tt.Texts())
	}
	if err := tt.Click("Favourite Songs"); err != nil {
		t.Fatal(err)
	}
	run(func() bool { return len(s.Library.Tracks["p.favourites"]) > 0 })
	tt.Frame()
	first := s.Library.Tracks["p.favourites"][0]
	r, ok := tt.Find("Add " + first.Name)
	if !ok {
		t.Fatalf("no Add for %q in %q", first.Name, tt.Texts())
	}
	if r.Y < cover.Y+cover.H {
		t.Errorf("the tracks show at %v, above the cover's bottom %v", r.Y, cover.Y+cover.H)
	}
	if err := tt.Click("Favourite Songs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Add " + first.Name); ok {
		t.Errorf("closed, the tracks still show")
	}
}

// A tile whose add was taken shows "in the queue" and, as a search tile's
// mark of a download under way does, opens the album's page when clicked.
func TestAnAppleTileInTheQueueOpensItsAlbum(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenLibrary)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	run(func() bool { return s.Library.AppleStatus == store.SearchReady })
	it := appleItem(s, "Salt & Static")
	if s.Library.Kept[it.CatalogID] {
		t.Fatal("Salt & Static is already kept")
	}
	s.Library.Added[it.URL] = domain.SubmitAccepted
	tt.Frame()
	r, ok := tt.Find("Salt & Static is in the queue")
	if !ok {
		t.Fatalf("no queued mark on the tile: %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(500 * time.Millisecond)
	if a.Screen != ScreenAlbum {
		t.Errorf("a click on the queued mark left screen %v, want the album's page", a.Screen)
	}
}
