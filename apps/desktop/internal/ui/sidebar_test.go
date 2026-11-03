package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/internal/client"
	"amdl/internal/domain"
)

func TestTheConnectionCardStaysAtTheFootAsADownloadStarts(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Screen = ScreenQueue
	tt := ui.NewTester(a.View, 1280, 800)
	find := func(label string) ui.Rect {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("no %q in %q", label, tt.Texts())
		}
		return r
	}
	card, settings := find("Connection settings"), find("Settings")
	now := time.Now()
	a.Store.ApplySnapshot(client.DownloadList{
		StatusCounts: domain.JobStatusCounts{Running: 1, Total: 1},
		Downloads:    []domain.Job{{ID: "r", Type: "album", Status: domain.JobRunning, Title: "Running Album", TotalItems: 5, CreatedAt: now, StartedAt: &now}},
	})
	tt.Frame()
	tt.Advance(sidebarSlide)
	tt.Frame()
	if got := find("Connection settings"); got != card {
		t.Errorf("the connection card moved from %v to %v", card, got)
	}
	if got := find("Settings"); got != settings {
		t.Errorf("the sections moved from %v to %v", settings, got)
	}
	if nowCard := find("Downloading now"); nowCard.Y+nowCard.H+nowGap != card.Y {
		t.Errorf("the now card at %v does not sit right over the connection card at %v", nowCard, card)
	}
}

// The Queue item counts what is under way, as the console's badge does: a
// failed run left behind by a deleted record, or finished runs, are not
// rows the queue shows, so they put no count beside it.
func TestTheQueueItemCountsOnlyWhatIsUnderWay(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Screen = ScreenSettings
	tt := ui.NewTester(a.View, 1280, 800)
	now := time.Now()
	a.Store.ApplySnapshot(client.DownloadList{
		StatusCounts: domain.JobStatusCounts{Completed: 40, Failed: 3, Total: 43},
		Downloads:    []domain.Job{{ID: "f", Type: "album", Status: domain.JobFailed, Title: "Gone Album", CreatedAt: now}},
	})
	tt.Frame()
	queue, ok := tt.Find("Queue")
	if !ok {
		t.Fatalf("no Queue item in %q", tt.Texts())
	}
	for _, n := range []string{"43", "3", "40"} {
		if r, ok := tt.Find(n); ok && r.Y < queue.Y+queue.H && r.Y+r.H > queue.Y {
			t.Errorf("the Queue item reads %q with nothing under way", n)
		}
	}
	a.Store.ApplySnapshot(client.DownloadList{
		StatusCounts: domain.JobStatusCounts{Queued: 2, Running: 1, Completed: 40, Failed: 3, Total: 46},
	})
	tt.Frame()
	if r, ok := tt.Find("3"); !ok || r.Y >= queue.Y+queue.H || r.Y+r.H <= queue.Y {
		t.Errorf("the Queue item does not read 3 with three runs under way: %q", tt.Texts())
	}
}
