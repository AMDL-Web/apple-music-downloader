package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

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
