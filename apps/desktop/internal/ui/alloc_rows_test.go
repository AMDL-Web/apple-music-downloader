//go:build !race

package ui

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// perRow is how many more times a frame allocates in this module's code
// for each more of n things laid out, as build lays out few and many of
// them in a window tall enough to show them all. A frame is a build of the
// view: how many a step runs depends on the fonts too.
//
// MyGo's own share is left out: how often its text system allocates for a
// row depends on the machine's fonts (on Linux, a run of glyphs whose
// antialiased edges touch is joined, and each join allocates), from 3 to
// over 30 times a row for the same words. A total count measured the fonts
// installed rather than the app; what the app allocates is the same
// everywhere.
func perRow(t *testing.T, few, many int, build func(n int) (view func(*ui.Context), height int)) float64 {
	t.Helper()
	count := func(n int) float64 {
		view, height := build(n)
		frames := 0
		tt := ui.NewTester(func(c *ui.Context) { frames++; view(c) }, 1280, height)
		for range 10 {
			tt.Advance(refresh)
			tt.Frame()
		}
		frames = 0
		allocs := appAllocs(func() {
			for range 20 {
				tt.Advance(refresh)
				tt.Frame()
			}
		})
		return float64(allocs) / float64(frames)
	}
	return (count(many) - count(few)) / float64(many-few)
}

// appAllocs counts the allocations f makes on behalf of this module: those
// whose innermost caller of this module or of MyGo is of this module (the
// app's words, and the closures its elements take), not those MyGo makes
// on its own. It profiles every allocation while f runs.
func appAllocs(f func()) int64 {
	defer func(rate int) { runtime.MemProfileRate = rate }(runtime.MemProfileRate)
	runtime.MemProfileRate = 1
	before := allocsByStack()
	f()
	after := allocsByStack()
	var n int64
	for stack, count := range after {
		if count -= before[stack]; count > 0 && allocatedByApp(stack) {
			n += count
		}
	}
	return n
}

// allocsByStack returns the allocations made so far by stack. The heap
// profile shows an allocation once the garbage collection after next is
// over.
func allocsByStack() map[[32]uintptr]int64 {
	runtime.GC()
	runtime.GC()
	n, _ := runtime.MemProfile(nil, true)
	var recs []runtime.MemProfileRecord
	for {
		recs = make([]runtime.MemProfileRecord, n+64)
		var ok bool
		if n, ok = runtime.MemProfile(recs, true); ok {
			break
		}
	}
	m := make(map[[32]uintptr]int64, n)
	for _, r := range recs[:n] {
		m[r.Stack0] += r.AllocObjects
	}
	return m
}

// allocatedByApp tells whether the innermost frame of stack that is of this
// module or of MyGo is of this module.
func allocatedByApp(stack [32]uintptr) bool {
	frames := runtime.CallersFrames(stack[:])
	for {
		fr, more := frames.Next()
		switch {
		case strings.HasPrefix(fr.Function, "amdl/"):
			return true
		case strings.HasPrefix(fr.Function, "github.com/egoist/mygo/"):
			return false
		}
		if !more {
			return false
		}
	}
}

// A row of the queue makes its words once, not on every frame: a frame
// with more rows allocates hardly more, for MyGo's own share of a row.
func TestAQueueRowAllocatesLittleEachFrame(t *testing.T) {
	n := perRow(t, 5, 25, func(n int) (func(*ui.Context), int) {
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
		return a.View, 3000
	})
	t.Logf("%.1f allocations a frame for each row", n)
	if n > rowAllocs {
		t.Errorf("each row of the queue allocates %.1f times a frame, want at most %d", n, rowAllocs)
	}
}

// A record's row, the queue's usual one, makes its words once too, its
// region's tag included.
func TestARecordRowAllocatesLittleEachFrame(t *testing.T) {
	n := perRow(t, 5, 25, func(n int) (func(*ui.Context), int) {
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
		return a.View, 3000
	})
	t.Logf("%.1f allocations a frame for each record row", n)
	if n > rowAllocs {
		t.Errorf("each record row allocates %.1f times a frame, want at most %d", n, rowAllocs)
	}
}

// A tile of the search makes its words and keys once, not on every frame.
func TestASearchTileAllocatesLittleEachFrame(t *testing.T) {
	n := perRow(t, 6, 30, func(n int) (func(*ui.Context), int) {
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
		return a.View, 4000
	})
	t.Logf("%.1f allocations a frame for each tile", n)
	if n > tileAllocs {
		t.Errorf("each search tile allocates %.1f times a frame, want at most %d", n, tileAllocs)
	}
}

// What a row and a tile may allocate a frame in the app's code: the
// closures and values their elements take, about 3 and 4. Making their
// words every frame as well comes to 7 to 10 for a row and 13 for a tile.
const rowAllocs, tileAllocs = 4, 5
