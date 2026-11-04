package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/artwork"
	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/demo"
)

// demoApp is an App on the demo's in-memory backend, whose answers wait
// until run applies them on the test's goroutine, as the UI thread would.
func demoApp(t *testing.T) (*App, func(until func() bool)) {
	t.Helper()
	a, _ := newTestApp(remoteSettings())
	queue := make(chan func(), 1024)
	post := func(fn func()) { queue <- fn }
	b := demo.New(time.Now)
	a.Store = store.New(post, nil)
	// Covers too come back on the test's goroutine, from the demo.
	a.Art = artwork.NewWith(post, b.Transport())
	c, err := client.NewWith(demo.Address, b.Transport())
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Connect(c)
	t.Cleanup(a.Store.Close)
	return a, func(until func() bool) {
		t.Helper()
		// The race detector on a busy runner slows everything several
		// times over.
		deadline := time.After(30 * time.Second)
		// An until that builds a frame may be what sends the request it
		// waits on, so it runs again even when nothing arrives.
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for !until() {
			select {
			case fn := <-queue:
				fn()
				// Everything that arrived meanwhile goes first: an until
				// that builds a frame takes most of a second under the race
				// detector, and the answer waited on queues behind every
				// cover that loads.
				for more := true; more; {
					select {
					case fn := <-queue:
						fn()
					default:
						more = false
					}
				}
			case <-tick.C:
			case <-deadline:
				t.Fatal("the demo backend's answer never arrived")
			}
		}
	}
}

func TestTheRowMenuOpensOnAClickAndDeletesOnlyAFinishedJob(t *testing.T) {
	a, run := demoApp(t)
	a.Screen = ScreenQueue
	run(func() bool { return a.Store.Loaded })

	// A job under way is cancelled from its menu, never deleted.
	a.Tab = int(store.TabActive)
	tt := stillTester(a.View, 1280, 800)
	// A record's run is paused, not cancelled.
	cancels := strings.Count(strings.Join(tt.Texts(), "\n"), "Pause")
	if err := tt.Click("More actions"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Show details") || strings.Count(strings.Join(tt.Texts(), "\n"), "Pause") <= cancels || tt.HasText("Delete…") {
		t.Errorf("a running job's menu shows %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)

	// A downloaded record is removed from amdl, after its dialog.
	a.Tab = int(store.TabDone)
	tt.Frame()
	run(func() bool { return a.Store.Queue.List(store.TabDone).Status == store.SearchReady })
	tt.Frame()
	rows, _ := a.Store.QueueRows(store.TabDone)
	if len(rows) == 0 || rows[0].Record == nil {
		t.Fatalf("Done lists %+v", rows)
	}
	done := rows[0].Record
	name := recordName(*done)
	if err := tt.Click("More actions"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Show details", "Download again", "Copy link"} {
		if !tt.HasText(want) {
			t.Errorf("a downloaded record's menu has no %q: %q", want, tt.Texts())
		}
	}
	if tt.HasText("Check files") {
		t.Errorf("a downloaded record's menu offers a disk check: %q", tt.Texts())
	}
	if err := tt.Click("Remove from amdl…"); err != nil {
		t.Fatalf("a downloaded record's menu: %v in %q", err, tt.Texts())
	}
	if !tt.HasText("Remove “" + name + "” from amdl?") {
		t.Fatalf("no remove dialog in %q", tt.Texts())
	}
	if err := tt.Click("Remove"); err != nil {
		t.Fatal(err)
	}
	run(func() bool { return a.Store.Queue.Find(done.Ref) == nil })
	tt.Frame()
	if tt.HasText("Remove “" + name + "” from amdl?") {
		t.Errorf("the dialog stays after the removal: %q", tt.Texts())
	}
	if rows, _ := a.Store.QueueRows(store.TabDone); len(rows) > 0 && rows[0].Record != nil && rows[0].Record.Ref == done.Ref {
		t.Errorf("%s still lists after its removal", name)
	}
}

// Each finished row shows its quality, though the feed lists the jobs
// without the tracks it is read from: the row reads them, not only the
// latest download or one opened this run.
func TestEveryFinishedRowShowsItsQuality(t *testing.T) {
	a, run := demoApp(t)
	a.Screen = ScreenQueue
	run(func() bool { return a.Store.Loaded })
	a.Tab = int(store.TabDone)
	tt := stillTester(a.View, 1280, 1600)
	done := a.Store.Filter(store.TabDone, "")
	if len(done) < 2 {
		t.Fatalf("the demo has %d finished jobs, want the latest and one before it", len(done))
	}
	run(func() bool {
		tt.Frame()
		for _, j := range done {
			if jobQuality(j) == "" {
				return false
			}
		}
		return true
	})
}
