//go:build !race

// The album page comes and goes frame by frame on the Tester's clock, which
// the race detector slows many times over with no concurrency to check.

package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// resultsApp is home over the demo backend showing its results for "glass
// weather", with the album page's motion on.
func resultsApp(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	a, run := demoApp(t)
	a.PageMotion = true
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenHome)
	tt := ui.NewTester(a.View, 1280, 800)
	// Boxes are in DIPs whatever the scale; fewer pixels draw faster.
	tt.SetScale(0.5)
	tt.Type("glass weather")
	run(func() bool {
		tt.Advance(refresh)
		return s.Search.Status == store.SearchReady && s.Search.Result != nil
	})
	run(func() bool { tt.Advance(refresh); return a.Art.Get(s.Search.ShownHits()[0].ArtworkURL, 512) != nil })
	tt.Advance(time.Second)
	return a, tt
}

// An album opened, left and opened again comes in at one click with
// nothing else drawing meanwhile, as when what it shows is cached: the
// store's answers wait here.
func TestAnAlbumLeftOpensAgainAtOneClick(t *testing.T) {
	a, tt := resultsApp(t)
	for i := 1; i <= 2; i++ {
		if err := tt.Click("Glass Weather"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(time.Second)
		if a.Screen != ScreenAlbum || a.fly.q != 1 || a.fly.moving {
			t.Fatalf("open %d: a second after one click the page is %v in, moving %v", i, a.fly.q, a.fly.moving)
		}
		tt.Key(0, ui.KeyEscape)
		tt.Advance(time.Second)
		if a.Screen != ScreenHome || a.fly.moving {
			t.Fatalf("back %d: a second after Escape the screen is %v, moving %v", i, a.Screen, a.fly.moving)
		}
	}
}
