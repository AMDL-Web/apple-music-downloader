// Layout and frame checks, as the shift tests: the race detector adds
// nothing to them, so they run under make test only.

//go:build !race

package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// queueRecords are records in the states the queue shows: downloaded, in
// another region, partly downloaded and paused, failed, missing a file.
func queueRecords(now time.Time) []store.Record {
	rec := func(id, name, by, sf string, st domain.RecordState, done, wanted int, ago time.Duration) store.Record {
		r := store.Record{Ref: domain.ResourceRef{Kind: domain.ResourceAlbum, ID: id}, Name: name, By: by, Storefront: sf, ResolvedAt: true,
			Catalog: domain.CatalogOK, ActivityAt: now.Add(-ago)}
		r.State, r.TracksDone, r.TracksWanted = st, done, wanted
		r.Artwork.Colors.Bg = "264653"
		return r
	}
	paused := rec("4", "Late Bloom", "The Velvet Orchard", "", domain.RecordPartial, 2, 5, 4*time.Hour)
	paused.Paused = true
	failed := rec("5", "Tidal Rooms", "Mira Solenne", "jp", domain.RecordPartial, 8, 10, 5*time.Hour)
	failed.TracksFailed = 2
	missing := rec("6", "Salt & Static", "Nightjar Coast", "", domain.RecordPartial, 9, 10, 6*time.Hour)
	missing.TracksMissing = 1
	pl := store.Record{Ref: domain.ResourceRef{Kind: domain.ResourcePlaylist, ID: "pl.1"}, Name: "City Pop Essentials", By: "Apple Music",
		Storefront: "jp", ResolvedAt: true, ActivityAt: now.Add(-2 * time.Hour)}
	pl.State, pl.TracksDone, pl.TracksWanted = domain.RecordComplete, 25, 25
	return []store.Record{
		rec("1", "Orchard Lights", "The Velvet Orchard", "", domain.RecordComplete, 9, 9, time.Hour),
		pl,
		rec("3", "夏夜信号", "青柠电台", "jp", domain.RecordComplete, 10, 10, 3*time.Hour),
		paused, failed, missing,
	}
}

// seededQueue is the queue of a store without a backend, its lists seeded
// with queueRecords and a station's failed run, its accounts in regions.
func seededQueue(t *testing.T, regions ...string) (*App, *ui.Tester) {
	t.Helper()
	a := seededApp(regions...)
	return a, stillTester(a.View, 1280, 900)
}

// seededApp is seededQueue's App, not yet shown.
func seededApp(regions ...string) *App {
	a, _ := newTestApp(remoteSettings())
	a.Screen = ScreenQueue
	s := a.Store
	now := time.Now()
	failedAt := now.Add(-150 * time.Minute)
	s.ApplySnapshot(client.DownloadList{StatusCounts: domain.JobStatusCounts{Failed: 1, Total: 1}, Downloads: []domain.Job{
		{ID: "st", Type: "station", Status: domain.JobFailed, Title: "Chill Radio", Storefront: "jp", TotalItems: 10, CompletedItems: 3,
			FailedItems: 1, Error: "the station ended", CreatedAt: failedAt, UpdatedAt: failedAt, FinishedAt: &failedAt},
	}})
	for i, r := range regions {
		s.Server.Accounts = append(s.Server.Accounts, client.Account{ID: fmt.Sprint("acc", i), Region: r, Default: i == 0, Position: i, State: "active"})
	}
	s.SeedQueue(queueRecords(now))
	return a
}

// Each row is one record or a station's run, and says its region only
// while the accounts span two regions or more: with one, every row would
// say the same.
func TestQueueRowsTagTheirRegionOnlyAcrossRegions(t *testing.T) {
	_, tt := seededQueue(t, "us")
	for _, name := range []string{"Orchard Lights", "City Pop Essentials", "Late Bloom", "Tidal Rooms", "Chill Radio"} {
		if n := strings.Count(strings.Join(tt.Texts(), "\n"), name); n != 2 {
			t.Errorf("%q shows %d times in the queue, want one row (its label and its title): %q", name, n, tt.Texts())
		}
	}
	for _, tag := range []string{"US", "JP"} {
		if tt.HasText(tag) {
			t.Errorf("with one region, the rows say %q", tag)
		}
	}
	one, _ := tt.Find("Tidal Rooms")
	// A row without artwork still draws its cover slot, not a hole.
	for _, name := range []string{"City Pop Essentials", "Chill Radio"} {
		r, _ := tt.Find(name)
		img := tt.Image()
		scale := float32(img.Bounds().Dx()) / 1280
		at := func(x, y float32) [4]uint8 {
			o := img.PixOffset(int(x*scale), int(y*scale))
			return [4]uint8(img.Pix[o : o+4])
		}
		// The row: its padding, then the 44-point cover.
		y := r.Y + r.H/2
		if slot, beside := at(r.X+18+6, y), at(r.X+8, y); slot == beside {
			t.Errorf("%s: its cover slot is the ground's color %v", name, slot)
		}
	}

	_, tt = seededQueue(t, "us", "jp")
	for _, tag := range []string{"US", "JP"} {
		if !tt.HasText(tag) {
			t.Errorf("with two regions, no row says %q: %q", tag, tt.Texts())
		}
	}
	two, _ := tt.Find("Tidal Rooms")
	sameBox(t, "a row's title, with the region tag under it", one, two)
	for _, want := range []string{"2/5", "Paused", "Failed", "Missing", "2 failed", "1 missing"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in the queue: %q", want, tt.Texts())
		}
	}
}

// The tabs keep their place, and the rows theirs, as one is chosen.
func TestQueueTabsHoldStillOverRecords(t *testing.T) {
	a, tt := seededQueue(t, "us", "jp")
	done := fmt.Sprintf("Done · %d", a.Store.QueueCount(store.TabDone))
	holdsStill(t, tt, "All", done)
	// Done's disk check takes the place of "Newest first" without moving
	// the tabs or the list under them.
	all, _ := tt.Find("All")
	first, _ := tt.Find("Orchard Lights")
	if err := tt.Click(done); err != nil {
		t.Fatal(err)
	}
	tt.Move(2, 2)
	tt.Frame()
	if !tt.HasText("Check files") {
		t.Fatalf("Done has no disk check: %q", tt.Texts())
	}
	all2, _ := tt.Find("All")
	first2, _ := tt.Find("Orchard Lights")
	sameBox(t, "the All tab on Done", all, all2)
	sameBox(t, "the first row on Done", first, first2)
}

// The queue's first frame already holds its counts and its rows: the
// store is loaded once they are read, so nothing pops in after it.
func TestTheQueuesFirstFrameHoldsItsCountsAndRows(t *testing.T) {
	a, run := demoApp(t)
	a.Screen = ScreenQueue
	s := a.Store
	run(func() bool { return s.Loaded })
	tt := stillTester(a.View, 1280, 1400)
	first := snapFrame(tt)
	for _, want := range []string{"Done · 2", "Failed · 1", "Earlier", "Tidal Rooms"} {
		if !tt.HasText(want) {
			t.Errorf("the first frame has no %q: %q", want, tt.Texts())
		}
	}
	if s.Queue.Counting() || s.Queue.List(store.TabAll).Status != store.SearchReady {
		t.Errorf("loaded before the queue's reads landed")
	}
	tt.Frame()
	if d := snapFrame(tt).layoutDiffers(first); d != "" {
		t.Errorf("the frame after the first differs: %s", d)
	}
}

// Switching tabs shows the chosen tab's rows at once, read before, and a
// quick disk check keeps the bar as it was until its answer.
func TestQueueTabsAndTheCheckDoNotFlash(t *testing.T) {
	a, run := demoApp(t)
	a.Screen = ScreenQueue
	s := a.Store
	run(func() bool { return s.Loaded })
	tt := stillTester(a.View, 1280, 800)
	ready := func() bool {
		for _, tab := range []store.Tab{store.TabAll, store.TabDone, store.TabFailed} {
			if l := s.Queue.List(tab); l.Status != store.SearchReady || s.Queue.Counting() {
				return false
			}
		}
		return true
	}
	run(func() bool { tt.Frame(); return ready() })
	tt.Frame()
	done := fmt.Sprintf("Done · %d", s.QueueCount(store.TabDone))
	noFlash(t, tt, done, func() { run(ready) })
	if !tt.HasText("Check files") {
		t.Fatalf("Done has no disk check: %q", tt.Texts())
	}
	noFlash(t, tt, "Check files", func() { run(func() bool { return s.Records.Check != nil }) })
}
