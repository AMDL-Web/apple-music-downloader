package ui

import (
	"testing"
	"time"

	"amdl/internal/client"

	"github.com/egoist/mygo/ui"
)

// The Jobs card keeps its height while its history grows from idle to busy,
// so nothing below it moves.
func TestJobsCardKeepsItsHeightAsItsHistoryGrows(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.GoSettings("")
	sys := &client.SystemInfo{}
	sys.Jobs.Workers = 2
	a.Store.Server.Read = time.Now()
	a.Store.Server.System = sys
	a.Store.Server.Active = []int{0, 0, 0, 2}
	tt := ui.NewTester(a.View, 1280, 1000)
	tt.Frame()
	idle := snapFrame(tt)
	a.Store.Server.Active = []int{0, 0, 2, 2}
	tt.Frame()
	if d := snapFrame(tt).layoutDiffers(idle); d != "" {
		t.Errorf("a second busy sample moves things: %s", d)
	}
}
