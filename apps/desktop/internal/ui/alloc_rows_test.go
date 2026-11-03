//go:build !race

package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// perRow is how many more times a frame allocates for each more of n
// things lay out, as build lays out few and many of them in a window tall
// enough to show them all.
func perRow(t *testing.T, few, many int, build func(n int) *ui.Tester) float64 {
	t.Helper()
	count := func(n int) float64 {
		tt := build(n)
		for range 10 {
			tt.Advance(refresh)
			tt.Frame()
		}
		return testing.AllocsPerRun(20, func() { tt.Advance(refresh); tt.Frame() })
	}
	return (count(many) - count(few)) / float64(many-few)
}

// A row of the queue makes its words once, not on every frame: a frame
// with more rows allocates hardly more, for MyGo's own share of a row.
func TestAQueueRowAllocatesLittleEachFrame(t *testing.T) {
	n := perRow(t, 5, 25, func(n int) *ui.Tester {
		a, _ := newTestApp(remoteSettings())
		a.Screen, a.Tab = ScreenQueue, int(store.TabDone)
		now := time.Now()
		var jobs []domain.Job
		for i := range n {
			done := now.Add(-time.Duration(i) * time.Minute)
			jobs = append(jobs, domain.Job{ID: fmt.Sprint("j", i), Type: "playlist", Status: domain.JobCompleted, Title: fmt.Sprint("Playlist ", i),
				CuratorName: "Apple Music", TotalItems: 12, CompletedItems: 12, CreatedAt: done, FinishedAt: &done})
		}
		a.Store.ApplySnapshot(client.DownloadList{StatusCounts: domain.JobStatusCounts{Completed: n, Total: n}, Downloads: jobs})
		a.Store.SeedQueue(nil)
		return ui.NewTester(a.View, 1280, 3000)
	})
	t.Logf("%.1f allocations a frame for each row", n)
	if n > rowAllocs {
		t.Errorf("each row of the queue allocates %.1f times a frame, want at most %d", n, rowAllocs)
	}
}

// A record's row, the queue's usual one, makes its words once too, its
// region's tag included.
func TestARecordRowAllocatesLittleEachFrame(t *testing.T) {
	n := perRow(t, 5, 25, func(n int) *ui.Tester {
		a, _ := newTestApp(remoteSettings())
		a.Screen = ScreenQueue
		a.Store.Loaded = true
		a.Store.Server.Accounts = []client.Account{{ID: "a", Region: "us", Default: true}, {ID: "b", Region: "jp", Position: 1}}
		now := time.Now()
		var recs []store.Record
		for i := range n {
			r := store.Record{Ref: domain.ResourceRef{Kind: domain.ResourceAlbum, ID: fmt.Sprint(i)}, Name: fmt.Sprint("Album ", i), By: "Someone",
				ResolvedAt: true, ActivityAt: now.Add(-time.Duration(i) * time.Minute)}
			r.State, r.TracksDone, r.TracksWanted = domain.RecordPartial, i%9, 12
			switch i % 3 {
			case 1:
				r.TracksFailed, r.Storefront = 2, "jp"
			case 2:
				r.Paused = true
			}
			recs = append(recs, r)
		}
		a.Store.SeedQueue(recs)
		return ui.NewTester(a.View, 1280, 3000)
	})
	t.Logf("%.1f allocations a frame for each record row", n)
	if n > rowAllocs {
		t.Errorf("each record row allocates %.1f times a frame, want at most %d", n, rowAllocs)
	}
}

// A tile of the search makes its words and keys once, not on every frame.
func TestASearchTileAllocatesLittleEachFrame(t *testing.T) {
	n := perRow(t, 6, 30, func(n int) *ui.Tester {
		a, _ := newTestApp(remoteSettings())
		s := a.Store
		s.SetOmniText("elma")
		s.WantSearch("elma")
		var albums []client.SearchHit
		for i := range n {
			albums = append(albums, client.SearchHit{Type: "album", ID: fmt.Sprint(100 + i), Name: fmt.Sprintf("Album %d - EP", i), ArtistName: "Yorushika",
				ReleaseDate: "2023-04-05", AudioTraits: []string{"lossless", "hi-res-lossless"}, URL: fmt.Sprintf("https://music.apple.com/cn/album/x/%d", 100+i)})
		}
		s.Search.Result = &client.SearchResult{Albums: &client.HitGroup{Items: albums}}
		s.Search.Status = store.SearchReady
		s.FocusGroup("albums")
		a.Screen = ScreenHome
		return ui.NewTester(a.View, 1280, 4000)
	})
	t.Logf("%.1f allocations a frame for each tile", n)
	if n > tileAllocs {
		t.Errorf("each search tile allocates %.1f times a frame, want at most %d", n, tileAllocs)
	}
}

// What a row and a tile may allocate a frame: MyGo's share, and the
// closures their elements take. Each made its words every frame, 11 and 12
// times.
const rowAllocs, tileAllocs = 4, 7
