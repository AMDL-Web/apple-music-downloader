//go:build !race

// The race detector allocates on its own account.

package widgets

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/internal/progress"
)

// frameAllocs is how many allocations a frame of view makes once it has
// settled.
func frameAllocs(t *testing.T, view func(c *ui.Context)) float64 {
	t.Helper()
	tt := ui.NewTester(view, 400, 40)
	// Stripes held still: moving, they change what the painter records,
	// and MyGo allocates to record it.
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	for range 5 {
		tt.Advance(time.Second / 30)
	}
	return testing.AllocsPerRun(20, func() { tt.Advance(time.Second / 30); tt.Frame() })
}

// The bars draw on every frame a download moves: building and drawing
// them allocates nothing, which a collection would make a dropped frame.
func TestTheBarsDrawWithoutAllocating(t *testing.T) {
	red, grey := ui.RGBA(255, 0, 0, 1), ui.RGBA(60, 60, 60, 1)
	stage := StageBar{Stages: progress.Stages{1, 0.4}, Color: red, Track: grey, Running: true}
	album := progress.Album{Total: 10, Ticks: 10, Runs: []progress.Run{{Share: 0.3}}, Done: 0.3, Live: 0.4}
	for name, view := range map[string]func(c *ui.Context){
		"stage bar":         func(c *ui.Context) { stage.Show(c, "b") },
		"album bar":         func(c *ui.Context) { AlbumBar{Album: album, Track: grey}.Show(c, "a") },
		"resolving album":   func(c *ui.Context) { AlbumBar{Album: album, Track: grey, Resolving: true}.Show(c, "a") },
		"indeterminate bar": func(c *ui.Context) { s := stage; s.Indeterminate = true; s.Show(c, "b") },
	} {
		if n := frameAllocs(t, view); n != 0 {
			t.Errorf("a frame of the %s allocates %.0f times", name, n)
		}
	}
}
