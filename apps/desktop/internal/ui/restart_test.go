package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// A remote backend's keys saved for a restart are offered a restart beside
// them; the backend the app runs restarts by itself, so it offers none.
func TestPendingRestartOffersARestart(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.GoSettings("jobs")
	a.Store.Server.Read = time.Now()
	a.Store.Config.Values = map[string]any{"download": map[string]any{"max_running_jobs": 5.0}}
	a.Store.Config.PendingRestart = []string{"download.max_running_jobs", "wrapper.address"}
	tt := ui.NewTester(a.View, 1280, 1600)
	if !tt.HasText("Restart to apply Jobs at once") {
		t.Fatalf("no restart offer in Jobs: %q", tt.Texts())
	}
	a.Store.AutoRestart = true
	tt = ui.NewTester(a.View, 1280, 1600)
	if tt.HasText("Restart to apply Jobs at once") {
		t.Fatal("the app's own backend restarts by itself; no offer")
	}
	if got := pendingIn([]string{"wrapper.address", "logging.console"}, "account"); len(got) != 1 || got[0] != "Wrapper address" {
		t.Fatalf("pendingIn = %q", got)
	}
}
