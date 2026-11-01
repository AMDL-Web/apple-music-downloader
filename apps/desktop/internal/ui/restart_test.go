package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// A remote backend's keys saved for a restart are offered a restart in a bar
// over the bottom of Settings, the same in every section; the backend the
// app runs restarts by itself, so it offers none.
func TestPendingRestartOffersARestart(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.GoSettings("jobs")
	a.Store.Server.Read = time.Now()
	a.Store.Config.Values = map[string]any{"download": map[string]any{"max_running_jobs": 5.0}}
	fieldY := func(tt *ui.Tester) float32 {
		r, ok := tt.Find("Jobs at once")
		if !ok {
			t.Fatalf("no Jobs at once field in %q", tt.Texts())
		}
		return r.Y
	}
	before := fieldY(ui.NewTester(a.View, 1280, 1600))
	a.Store.Config.PendingRestart = []string{"download.max_running_jobs", "wrapper.address"}
	tt := ui.NewTester(a.View, 1280, 1600)
	if !tt.HasText("Restart to apply Jobs at once, Wrapper address") {
		t.Fatalf("no restart offer: %q", tt.Texts())
	}
	// The bar floats over the sections: the fields stay where they were.
	if after := fieldY(tt); after != before {
		t.Errorf("Jobs at once moved from y %v to %v when the restart bar came", before, after)
	}
	a.Store.Config.Restarting = true
	tt = ui.NewTester(a.View, 1280, 1600)
	if !tt.HasText("Restarting…") {
		t.Fatalf("a restart under way should say so: %q", tt.Texts())
	}
	a.Store.Config.Restarting = false
	a.Store.AutoRestart = true
	tt = ui.NewTester(a.View, 1280, 1600)
	if tt.HasText("Restart to apply Jobs at once, Wrapper address") {
		t.Fatal("the app's own backend restarts by itself; no offer")
	}
	if got := pendingLabels([]string{"wrapper.address", "nope.key"}); len(got) != 2 || got[0] != "Wrapper address" || got[1] != "nope.key" {
		t.Fatalf("pendingLabels = %q", got)
	}
}
