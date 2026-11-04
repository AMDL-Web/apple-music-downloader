package ui

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/demo"
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

// accountsOver serves the accounts from a list a test changes, as another
// client's change to them does, and the demo's backend for the rest.
type accountsOver struct {
	rt   http.RoundTripper
	list client.AccountList
}

func (o *accountsOver) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet || r.URL.Path != "/api/v1/accounts" {
		return o.rt.RoundTrip(r)
	}
	body, err := json.Marshal(o.list)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
}

// The window reads the accounts itself: the Library item is there from the
// start for an account with the Apple library on, without a visit to
// Settings, and a switch another client makes shows within ServerEvery,
// taking the Library screen away with the item.
func TestTheLibraryItemFollowsTheAccountsWithoutVisitingSettings(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	o := &accountsOver{rt: demo.New(time.Now).Transport(),
		list: client.AccountList{Accounts: []client.Account{{ID: "a1", Region: "jp", Default: true, AppleLibrary: true, State: client.AccountActive}}}}
	queue := make(chan func(), 1024)
	post := func(fn func()) { queue <- fn }
	a.Store = store.New(post, nil)
	c, err := client.NewWith(demo.Address, o)
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Connect(c)
	t.Cleanup(a.Store.Close)
	now := time.Now()
	a.Store.SetClock(func() time.Time { return now })
	tt := ui.NewTester(a.View, 1280, 800)
	until := func(cond func() bool) {
		t.Helper()
		deadline := time.After(30 * time.Second)
		for tt.Frame(); !cond(); tt.Frame() {
			select {
			case fn := <-queue:
				fn()
			case <-time.After(20 * time.Millisecond):
			case <-deadline:
				t.Fatalf("timed out; texts %q", tt.Texts())
			}
		}
	}
	until(func() bool { return tt.HasText("Library") })
	if !a.Store.Server.Read.IsZero() {
		t.Fatal("the server's state was read: the item must not depend on it")
	}
	a.Go(ScreenLibrary)
	// Another client switches the Apple library off.
	o.list.Accounts[0].AppleLibrary = false
	now = now.Add(store.ServerEvery)
	until(func() bool { return a.Screen != ScreenLibrary && !tt.HasText("Library") })
}
