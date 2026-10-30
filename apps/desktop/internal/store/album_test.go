package store

import (
	"math"
	"testing"
	"time"

	"amdl/internal/domain"
)

// These cases are the web console's apps/console/jstest/album-progress.test.mjs,
// so the two clients agree on every number.

func track(index int, status domain.ItemStatus, p domain.ItemProgress, finished time.Time) domain.JobItem {
	return domain.JobItem{Index: index, Status: status, Progress: p, UpdatedAt: finished}
}

func near(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestItemFractionWeighsTheStages(t *testing.T) {
	none := domain.ItemProgress{}
	near(t, ItemFraction(track(1, domain.ItemCompleted, none, at(0))), 1)
	near(t, ItemFraction(track(1, domain.ItemSkipped, none, at(0))), 1)
	near(t, ItemFraction(track(1, domain.ItemQueued, none, at(0))), 0)
	near(t, ItemFraction(track(1, domain.ItemDownloading, domain.ItemProgress{Resolved: true, Download: 0.5}, at(0))), 0.04+0.28)
	near(t, ItemFraction(track(1, domain.ItemTagging, domain.ItemProgress{Resolved: true, Download: 1, Decrypt: 1, Remuxed: true}, at(0))), 0.92)
	// Saved ends the pipeline even when integrity checks are off.
	near(t, ItemFraction(track(1, domain.ItemSaving, domain.ItemProgress{Resolved: true, Download: 1, Decrypt: 1, Remuxed: true, Tagged: true, Saved: true}, at(0))), 1)
	// A failed track stays where it stopped.
	near(t, ItemFraction(track(1, domain.ItemFailed, domain.ItemProgress{Resolved: true, Download: 0.25}, at(0))), 0.04+0.14)
}

func TestAlbumPercentStaysBelow100WhileATrackWorks(t *testing.T) {
	half := domain.ItemProgress{Resolved: true, Download: 1}
	p := AlbumProgress(domain.JobRunning, []domain.JobItem{track(1, domain.ItemCompleted, domain.ItemProgress{}, at(0)), track(2, domain.ItemDownloading, half, at(0))})
	if want := int(math.Round((1 + 0.6) / 2 * 100)); p.Percent != want {
		t.Fatalf("percent = %d, want %d", p.Percent, want)
	}
	all := domain.ItemProgress{Resolved: true, Download: 1, Decrypt: 1, Remuxed: true, Verified: true, Tagged: true}
	many := make([]domain.JobItem, 200)
	for i := range many {
		many[i] = track(i+1, domain.ItemCompleted, all, at(0))
	}
	many[0].Status = domain.ItemTagging
	if got := AlbumProgress(domain.JobRunning, many).Percent; got != 99 {
		t.Fatalf("200 tracks, one tagging: percent = %d, want 99", got)
	}
	done := []domain.JobItem{track(1, domain.ItemCompleted, domain.ItemProgress{}, at(0)), track(2, domain.ItemSkipped, domain.ItemProgress{}, at(0))}
	if got := AlbumProgress(domain.JobCompleted, done).Percent; got != 100 {
		t.Fatalf("completed job: percent = %d, want 100", got)
	}
	if got := AlbumProgress(domain.JobRunning, nil).Percent; got != 0 {
		t.Fatalf("no tracks: percent = %d, want 0", got)
	}
}

func TestAlbumLightsInFinishOrder(t *testing.T) {
	none := domain.ItemProgress{}
	p := AlbumProgress(domain.JobRunning, []domain.JobItem{
		track(1, domain.ItemDownloading, none, at(0)),
		track(2, domain.ItemCompleted, none, at(30)),
		track(3, domain.ItemFailed, none, at(10)),
		track(4, domain.ItemCompleted, none, at(20)),
		track(5, domain.ItemSkipped, none, at(40)),
	})
	if p.Lit != 4 {
		t.Fatalf("lit = %d, want 4", p.Lit)
	}
	want := []struct {
		status domain.ItemStatus
		cells  int
	}{{domain.ItemFailed, 1}, {domain.ItemCompleted, 2}, {domain.ItemSkipped, 1}}
	if len(p.Runs) != len(want) {
		t.Fatalf("runs = %+v", p.Runs)
	}
	for i, w := range want {
		if p.Runs[i].Status != w.status || int(math.Round(p.Runs[i].Share*5)) != w.cells {
			t.Fatalf("run %d = %+v, want %v × %d", i, p.Runs[i], w.status, w.cells)
		}
	}
	near(t, p.Done, 4.0/5)
	if p.Live < p.Done {
		t.Fatalf("live %v behind done %v", p.Live, p.Done)
	}
}

func TestAlbumOrderWithoutFinishTimes(t *testing.T) {
	none := domain.ItemProgress{}
	p := AlbumProgress(domain.JobCancelled, []domain.JobItem{
		track(1, domain.ItemCancelled, none, time.Time{}), track(2, domain.ItemFailed, none, time.Time{}),
		track(3, domain.ItemSkipped, none, time.Time{}), track(4, domain.ItemCompleted, none, time.Time{}),
		track(5, domain.ItemCompleted, none, time.Time{}),
	})
	want := []domain.ItemStatus{domain.ItemCompleted, domain.ItemSkipped, domain.ItemFailed, domain.ItemCancelled}
	if len(p.Runs) != len(want) {
		t.Fatalf("runs = %+v", p.Runs)
	}
	for i, w := range want {
		if p.Runs[i].Status != w {
			t.Fatalf("run %d = %v, want %v", i, p.Runs[i].Status, w)
		}
	}
}

func TestAlbumTranslucentFillNeverBehindTheSolid(t *testing.T) {
	p := AlbumProgress(domain.JobRunning, []domain.JobItem{track(1, domain.ItemFailed, domain.ItemProgress{}, at(0)), track(2, domain.ItemQueued, domain.ItemProgress{}, at(0))})
	near(t, p.Done, 0.5)
	near(t, p.Live, 0.5)
}

func TestAlbumTicks(t *testing.T) {
	n := func(k int) []domain.JobItem {
		out := make([]domain.JobItem, k)
		for i := range out {
			out[i] = track(i+1, domain.ItemQueued, domain.ItemProgress{}, at(0))
		}
		return out
	}
	for k, want := range map[int]int{1: 0, 2: 2, AlbumTicksMax: AlbumTicksMax, AlbumTicksMax + 1: 0} {
		if got := AlbumProgress(domain.JobRunning, n(k)).Ticks; got != want {
			t.Errorf("%d tracks: ticks = %d, want %d", k, got, want)
		}
	}
}

func TestAlbumSingleTrackIsItsOwnProgress(t *testing.T) {
	p := AlbumProgress(domain.JobRunning, []domain.JobItem{track(1, domain.ItemDownloading, domain.ItemProgress{Resolved: true, Download: 0.5}, at(0))})
	if len(p.Runs) != 1 || p.Runs[0].Status != "" {
		t.Fatalf("runs = %+v", p.Runs)
	}
	near(t, p.Done, 0.32)
	near(t, p.Live, p.Done)
	f := AlbumProgress(domain.JobFailed, []domain.JobItem{track(1, domain.ItemFailed, domain.ItemProgress{}, at(0))})
	if len(f.Runs) != 1 || f.Runs[0].Status != domain.ItemFailed || f.Runs[0].Share != 1 {
		t.Fatalf("failed single track: runs = %+v", f.Runs)
	}
}
