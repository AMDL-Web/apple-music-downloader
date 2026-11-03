// Renders on request only; it needs queue_records_test.go, a !race file.

//go:build !race

package ui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/domain"
)

// TestRenderQueueStills writes the queue in its states, and the Library,
// into $RENDER_DIR, to look at without running the app.
func TestRenderQueueStills(t *testing.T) {
	dir := os.Getenv("RENDER_DIR")
	if dir == "" {
		t.Skip("RENDER_DIR names where the stills go")
	}
	if err := RegisterFonts(); err != nil {
		t.Fatal(err)
	}
	save := func(tt *ui.Tester, name string) {
		t.Helper()
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	sizes := [][2]int{{1280, 800}, {820, 560}}
	tabs := []string{"all", "active", "done", "failed"}

	// The demo's queue, covers loaded: every tab, and a record downloaded
	// twice, whose second run is one row in Up next.
	for _, theme := range []string{"dark", "light"} {
		for _, size := range sizes {
			a, run := demoApp(t)
			a.Theme = theme
			a.Screen = ScreenQueue
			s := a.Store
			run(func() bool { return s.Loaded })
			tt := stillTester(a.View, size[0], size[1])
			tt.SetScale(2)
			frames := func(d time.Duration) {
				end := time.Now().Add(d)
				run(func() bool { tt.Frame(); return time.Now().After(end) })
				tt.Frame()
			}
			frames(1500 * time.Millisecond)
			for i, tab := range tabs {
				a.Tab = i
				frames(700 * time.Millisecond)
				save(tt, fmt.Sprintf("desktop-demo-%s-%d-%s", tab, size[0], theme))
			}
			a.Tab = int(store.TabAll)
			ref := domain.ResourceRef{Kind: domain.ResourceAlbum, ID: "920005"}
			s.Reset(ref, nil, nil, "Orchard Lights")
			frames(1200 * time.Millisecond)
			save(tt, fmt.Sprintf("desktop-demo-two-runs-%d-%s", size[0], theme))
			a.Go(ScreenLibrary)
			frames(1500 * time.Millisecond)
			save(tt, fmt.Sprintf("desktop-library-%d-%s", size[0], theme))
		}
	}

	// Seeded lists: the states one by one, one region and two, the failed
	// adds, many records, and none.
	for _, theme := range []string{"dark", "light"} {
		for _, size := range sizes {
			for _, regions := range [][]string{{"us"}, {"us", "jp"}} {
				a := seededApp(regions...)
				a.Theme = theme
				a.Store.Queue.Adds = []domain.PendingAdd{{ID: 7, Kind: domain.PendingAlbum, CatalogID: "1440857781", Storefront: "jp",
					State: domain.PendingAddFailed, LastError: "not in this storefront's catalog"}}
				tt := stillTester(a.View, size[0], size[1])
				tt.SetScale(2)
				for i, tab := range tabs {
					a.Tab = i
					tt.Frame()
					tt.Frame()
					save(tt, fmt.Sprintf("desktop-states-%s-regions%d-%d-%s", tab, len(regions), size[0], theme))
				}
			}
			a, _ := newTestApp(remoteSettings())
			a.Theme, a.Screen = theme, ScreenQueue
			a.Store.Loaded = true
			now := time.Now()
			var recs []store.Record
			for i := range 140 {
				r := store.Record{Ref: domain.ResourceRef{Kind: domain.ResourceAlbum, ID: fmt.Sprint(i)}, Name: fmt.Sprint("Album ", i+1),
					By: "Someone", ResolvedAt: true, ActivityAt: now.Add(-time.Duration(i) * time.Minute)}
				r.State, r.TracksDone, r.TracksWanted = domain.RecordComplete, 10, 10
				r.Artwork.Colors.Bg = []string{"264653", "5c2a3e", "1f3b5c", "3d2c1e"}[i%4]
				recs = append(recs, r)
			}
			a.Store.SeedQueue(recs)
			tt := stillTester(a.View, size[0], size[1])
			tt.SetScale(2)
			save(tt, fmt.Sprintf("desktop-many-%d-%s", size[0], theme))
			a.Store.SeedQueue(nil)
			tt.Frame()
			save(tt, fmt.Sprintf("desktop-empty-%d-%s", size[0], theme))
			a.Store.SeedQueue(recs[:1])
			tt.Frame()
			save(tt, fmt.Sprintf("desktop-one-%d-%s", size[0], theme))
		}
	}
}
