package ui

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/testkit"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// appleAlbums answers GET /apple-library/albums from albums a page at a
// time, as the API does.
func appleAlbums(albums []domain.AppleLibraryItem) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		off, _ := strconv.Atoi(q.Get("offset"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		// The API's most per page.
		limit = min(max(limit, 1), 200)
		off = min(off, len(albums))
		testkit.Reply(w, http.StatusOK, client.AppleLibraryPage{Kind: "albums", Items: albums[off:min(off+limit, len(albums))], Total: len(albums), Offset: off})
	}
}

// bulkApp is demoApp with an Apple library of n albums, "Album 0000" on,
// shown in a w×800 window on the Apple tab.
func bulkApp(t *testing.T, n, w int) (*App, *ui.Tester, func(until func() bool)) {
	t.Helper()
	var albums []domain.AppleLibraryItem
	for i := range n {
		id := strconv.Itoa(9100000 + i)
		albums = append(albums, domain.AppleLibraryItem{ID: "l." + id, CatalogID: id, URL: "https://music.apple.com/us/album/x/" + id,
			Name: fmt.Sprintf("Album %04d", i), ArtistName: fmt.Sprintf("Artist %d", i%37)})
		if i%7 == 1 {
			albums[i].Name += " (Deluxe Edition) [Remastered] and Other Songs From the Long Night"
		}
	}
	a, run := demoOver(t, map[string]http.HandlerFunc{"GET /api/v1/apple-library/albums": appleAlbums(albums)})
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

// A library-only playlist's cover opens its page, as any playlist of the
// Library does, with its songs as the library copy has them: each with a
// download of its own, and no download of the whole playlist, which the
// catalog does not have.
func TestALibraryOnlyPlaylistOpensItsPageWithItsSongs(t *testing.T) {
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
	if err := tt.Click("Favourite Songs"); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	p := &s.Search.Album
	run(func() bool { tt.Frame(); return p.Status == store.SearchReady })
	tt.Frame()
	if a.Screen != ScreenAlbum || p.From.Playlist != "p.favourites" {
		t.Fatalf("screen %v, page from %+v", a.Screen, p.From)
	}
	// The demo's library playlist holds the first four songs of Glass
	// Weather.
	for _, name := range []string{"Glass Weather", "Paper Satellites", "Slow Lightning", "Northbound"} {
		if !tt.HasText(name) {
			t.Errorf("no %q on the page: %q", name, tt.Texts())
		}
	}
	if _, ok := tt.Find("Download Favourite Songs"); ok {
		t.Error("the page offers to download the whole of a playlist only the library has")
	}
	// A song's download shows while its row is pointed at.
	row, _ := tt.Find("Paper Satellites")
	tt.Move(row.X+row.W/2, row.Y+row.H/2)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if _, ok := tt.Find("Download Paper Satellites"); !ok {
		t.Errorf("no download of a song of it: %q", tt.Texts())
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

// An album whose record wants only the one track added by hand (1 of 6
// done, the record complete) reads on the Library tile as it does on the
// album's page and in the search: not fully downloaded, and one click adds
// the rest. Only the record decides; nothing reads the disk.
func TestAnAppleTileOfAPartRecordOffersTheRest(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenLibrary)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	run(func() bool { return s.Library.AppleStatus == store.SearchReady })
	it := appleItem(s, "Salt & Static")
	for i := range s.Library.Apple.Items {
		if s.Library.Apple.Items[i].ID == it.ID {
			s.Library.Apple.Items[i].TrackCount = 6
		}
	}
	s.Library.Kept[it.CatalogID] = true
	s.Library.Marks[domain.ResourceRef{Kind: domain.ResourceAlbum, ID: it.CatalogID}] = &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: it.CatalogID,
		State: domain.RecordComplete, TracksDone: 1, TracksWanted: 1}
	tt.Frame()
	if _, ok := tt.Find("In Library: open it"); ok {
		t.Fatal("a part record shows the plain check")
	}
	r, ok := tt.Find("Salt & Static: 1 of 6 tracks downloaded, download the rest")
	if !ok {
		t.Fatalf("no partial mark on the tile: %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if _, on := s.Search.Starting[store.HitKey(client.SearchHit{Type: "album", ID: it.CatalogID})]; !on {
		t.Error("the click started no download of the album")
	}
	if a.asking != nil {
		t.Errorf("a record that wants only some tracks asked about the library: %+v", a.asking)
	}
}

// A downloaded album's tile marks it as the search does: a click offers to
// download it again.
func TestAnAppleTileOfADownloadedRecordOffersItAgain(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenLibrary)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	run(func() bool { return s.Library.AppleStatus == store.SearchReady })
	it := appleItem(s, "Salt & Static")
	s.Library.Kept[it.CatalogID] = true
	s.Library.Marks[domain.ResourceRef{Kind: domain.ResourceAlbum, ID: it.CatalogID}] = &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: it.CatalogID,
		State: domain.RecordComplete, TracksDone: 6, TracksWanted: 6, WantedAll: true}
	tt.Frame()
	r, ok := tt.Find("Salt & Static is in your library: download again")
	if !ok {
		t.Fatalf("no downloaded mark on the tile: %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if a.asking == nil || !a.asking.open {
		t.Errorf("a click on a downloaded album's mark asked nothing")
	}
}

// The Library is the Apple library: it marks the albums amdl keeps and adds
// the others, from the corner of a cover pointed at.
func TestTheLibraryAddsFromTheAppleLibrary(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenLibrary)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	run(func() bool { return s.Library.AppleStatus == store.SearchReady })
	tt.Frame()
	// A kept album is marked as the search marks it: by its record.
	if !tt.HasText("Orchard Lights is in your library: download again") || !tt.HasText("Glass Weather is in the queue") {
		t.Errorf("no album marked: %q", tt.Texts())
	}
	if _, ok := tt.Find("Add Salt & Static"); ok {
		t.Errorf("a tile not pointed at shows its Add")
	}
	pointAt(t, tt, "Salt & Static")
	if err := tt.Click("Add Salt & Static"); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	run(func() bool { return s.Library.Kept["920004"] })
	tt.Frame()
	if _, ok := tt.Find("Add Salt & Static"); ok {
		t.Errorf("the added album still offers Add: %q", tt.Texts())
	}
}

// An album opened from the Library has its catalog link, so its page offers
// the download as a search result's does: Download while the record has
// nothing, the rest while it holds part, Download again once it is whole.
func TestAnAppleAlbumOpenedFromTheLibraryOffersItsDownload(t *testing.T) {
	for _, c := range []struct {
		name string
		done int
		want string
	}{
		{"none", 0, "Download Salt & Static"},
		{"partial", 1, "Download the rest of Salt & Static"},
		{"done", 9, "Download Salt & Static again"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, run := demoApp(t)
			s := a.Store
			run(func() bool { return s.Loaded })
			a.Go(ScreenLibrary)
			tt := ui.NewTester(a.View, 1280, 800)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			tt.Frame()
			run(func() bool { return s.Library.AppleStatus == store.SearchReady })
			it := appleItem(s, "Salt & Static")
			if it.URL == "" || s.AlbumJob(domain.ResourceAlbum, it.CatalogID) != "" {
				t.Fatalf("Salt & Static needs a catalog link and no job: %+v", it)
			}
			for i := range s.Library.Apple.Items {
				if s.Library.Apple.Items[i].ID == it.ID {
					s.Library.Apple.Items[i].TrackCount = 9
				}
			}
			it = appleItem(s, "Salt & Static")
			if c.done > 0 {
				s.Library.Kept[it.CatalogID] = true
				s.Library.Marks[domain.ResourceRef{Kind: domain.ResourceAlbum, ID: it.CatalogID}] = &domain.ResourceMark{Kind: domain.ResourceAlbum,
					ID: it.CatalogID, State: domain.RecordComplete, TracksDone: c.done, TracksWanted: c.done}
			}
			a.openAppleAlbum(it)
			tt.Advance(time.Second)
			tt.Frame()
			if _, ok := tt.Find(c.want); !ok {
				t.Errorf("no %q on the page of an album opened from the Library: %q", c.want, tt.Texts())
			}
		})
	}
}
