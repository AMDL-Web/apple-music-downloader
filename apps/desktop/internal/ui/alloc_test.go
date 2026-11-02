//go:build !race

// The race detector allocates on its own account.

package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// What every frame builds whatever the screen allocates nothing: the
// window's shortcuts, and a hit's or a job's palette read from its colors.
func TestEveryFramesSharedPartsDoNotAllocate(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Screen = ScreenQueue
	tt := ui.NewTester(func(c *ui.Context) { a.shortcuts(c) }, 200, 100)
	tt.Frame()
	if n := testing.AllocsPerRun(20, func() { tt.Advance(refresh); tt.Frame() }); n != 0 {
		t.Errorf("a frame's shortcuts allocate %.0f times", n)
	}
	if n := testing.AllocsPerRun(100, func() { ParsePalette("1a2b3c", "ffffff", "#eeeeee", "dddddd", "") }); n != 0 {
		t.Errorf("ParsePalette allocates %.0f times", n)
	}
}
