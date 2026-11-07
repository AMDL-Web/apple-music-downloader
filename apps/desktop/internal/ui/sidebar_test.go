package ui

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/testkit"
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

// The Library shows the Apple Music library amdl copies, so its item is there
// only while an account has that copy on, and the screen gives way to Home
// when the last one is switched off.
func TestTheLibraryItemFollowsTheAppleLibrarySwitch(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	tt := ui.NewTester(a.View, 1280, 800)
	library := func() bool {
		for _, s := range tt.Texts() {
			if s == "Library" {
				return true
			}
		}
		return false
	}
	a.Store.Server.Accounts = []client.Account{{ID: "a1", Region: "cn", Default: true}, {ID: "a2", Region: "jp"}}
	tt.Frame()
	if library() {
		t.Error("a Library item with no account's Apple library on")
	}
	a.Store.Server.Accounts[1].AppleLibrary = true
	tt.Frame()
	if !library() {
		t.Fatal("no Library item with an account's Apple library on")
	}
	a.Go(ScreenLibrary)
	tt.Frame()
	a.Store.Server.Accounts[1].AppleLibrary = false
	tt.Frame()
	tt.Frame()
	if a.Screen != ScreenHome || library() {
		t.Errorf("screen %v, Library item %v after the last Apple library went off", a.Screen, library())
	}
}

// The window reads the accounts itself: the Library item is there from the
// start for an account with the Apple library on, without a visit to
// Settings, and a switch another client makes shows within ServerEvery,
// taking the Library screen away with the item.
func TestTheLibraryItemFollowsTheAccountsWithoutVisitingSettings(t *testing.T) {
	// Another client's change to the accounts shows in the next read.
	var mu sync.Mutex
	list := client.AccountList{Accounts: []client.Account{{ID: "a1", Region: "jp", Default: true, AppleLibrary: true, State: client.AccountActive}}}
	a, run := demoOver(t, map[string]http.HandlerFunc{"GET /api/v1/accounts": func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		testkit.Reply(w, http.StatusOK, list)
	}})
	now := time.Now()
	a.Store.SetClock(func() time.Time { return now })
	tt := ui.NewTester(a.View, 1280, 800)
	// Each frame asks for what is due; the answers are posted.
	until := func(cond func() bool) {
		t.Helper()
		run(func() bool { tt.Frame(); return cond() })
	}
	until(func() bool { return tt.HasText("Library") })
	if !a.Store.Server.Read.IsZero() {
		t.Fatal("the server's state was read: the item must not depend on it")
	}
	a.Go(ScreenLibrary)
	// Another client switches the Apple library off.
	mu.Lock()
	list.Accounts[0].AppleLibrary = false
	mu.Unlock()
	now = now.Add(store.ServerEvery)
	until(func() bool { return a.Screen != ScreenLibrary && !tt.HasText("Library") })
}
