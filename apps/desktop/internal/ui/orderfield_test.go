package ui

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

const priorityKey = "download.quality_priority"

// qualityApp is Settings at Quality over the demo backend, whose codec
// priority is ALAC, then Dolby Atmos.
func qualityApp(t *testing.T) (*App, *ui.Tester, func(until func() bool)) {
	t.Helper()
	a, run := demoApp(t)
	a.GoSettings("quality")
	tt := ui.NewTester(a.View, 1280, 900)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	run(func() bool { return a.Store.Config.Values != nil })
	for range 4 {
		tt.Frame()
	}
	return a, tt, run
}

func priority(a *App) []string { return strs(a.Store.Config.Value(priorityKey)) }

func center(t *testing.T, tt *ui.Tester, s string) (float32, float32) {
	t.Helper()
	r, ok := tt.Find(s)
	if !ok {
		t.Fatalf("no %q in %q", s, tt.Texts())
	}
	return r.X + r.W/2, r.Y + r.H/2
}

// drag presses from's row and moves the pointer to to's in steps, as a
// hand does, without letting go.
func drag(t *testing.T, tt *ui.Tester, from, to string) (float32, float32) {
	t.Helper()
	x0, y0 := center(t, tt, from)
	x1, y1 := center(t, tt, to)
	tt.Press(x0, y0)
	tt.Frame()
	for i := 1; i <= 8; i++ {
		f := float32(i) / 8
		tt.Move(x0+(x1-x0)*f, y0+(y1-y0)*f)
		tt.Frame()
	}
	return x1, y1
}

func TestACodecDragsIntoUseAndAppliesOnlyOnceLetGo(t *testing.T) {
	a, tt, run := qualityApp(t)
	x, y := drag(t, tt, "AAC binaural", "ALAC (lossless)")
	if got := a.set.order.on; !slices.Equal(got, []string{"aac-binaural", "alac", "ec3"}) {
		t.Errorf("held over the first row, the list shows %v", got)
	}
	if a.Store.Config.Saving[priorityKey] || !slices.Equal(priority(a), []string{"alac", "ec3"}) {
		t.Errorf("the priority changed before the codec was let go: %v", priority(a))
	}
	tt.Release(x, y)
	tt.Frame()
	// The list shows the new order while the backend answers.
	if a.set.order.item != "" || !slices.Equal(priority(a), []string{"aac-binaural", "alac", "ec3"}) {
		t.Errorf("let go, the list shows %v, dragging %q", priority(a), a.set.order.item)
	}
	run(func() bool { return !a.Store.Config.Saving[priorityKey] })
	tt.Frame()
	if got := priority(a); !slices.Equal(got, []string{"aac-binaural", "alac", "ec3"}) {
		t.Errorf("the backend has %v", got)
	}
}

func TestADragOutOfUseAndEscapeLeaveThePriority(t *testing.T) {
	a, tt, _ := qualityApp(t)
	// Dolby Atmos dragged into the other block, then given up.
	drag(t, tt, "Dolby Atmos (E-AC-3)", "AAC binaural")
	if !slices.Equal(a.set.order.on, []string{"alac"}) || !slices.Contains(a.set.order.off, "ec3") {
		t.Errorf("held over the other block, the list shows %v / %v", a.set.order.on, a.set.order.off)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	x, y := center(t, tt, "AAC binaural")
	tt.Release(x, y)
	tt.Frame()
	if a.set.order.item != "" || a.Store.Config.Saving[priorityKey] || !slices.Equal(priority(a), []string{"alac", "ec3"}) {
		t.Errorf("after Escape: dragging %q, priority %v", a.set.order.item, priority(a))
	}
}

func TestTheKeyboardMovesACodecAcrossTheBlocks(t *testing.T) {
	a, tt, run := qualityApp(t)
	if err := tt.Click("Dolby Digital (AC-3)"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Alt, ui.KeyUp)
	run(func() bool { return !a.Store.Config.Saving[priorityKey] })
	tt.Frame()
	if got := priority(a); !slices.Equal(got, []string{"alac", "ec3", "ac3"}) {
		t.Fatalf("Alt+Up on the first unused codec gave %v", got)
	}
	// It kept the focus as it moved, so the next press moves it on.
	tt.Key(ui.Alt, ui.KeyUp)
	run(func() bool { return !a.Store.Config.Saving[priorityKey] })
	tt.Frame()
	if got := priority(a); !slices.Equal(got, []string{"alac", "ac3", "ec3"}) {
		t.Errorf("a second Alt+Up gave %v", got)
	}
}

func TestOneCodecStaysInUse(t *testing.T) {
	on, off := placeCodec([]string{"alac"}, []string{"aac"}, "alac", 1e6, 1)
	if !slices.Equal(on, []string{"alac"}) || !slices.Equal(off, []string{"aac"}) {
		t.Errorf("dragged far below, the last codec in use went to %v / %v", on, off)
	}
	if _, _, ok := moveCodec([]string{"alac"}, []string{"aac"}, "alac", 1, 1); ok {
		t.Errorf("Alt+Down took the last codec out of use")
	}
	on, off, _ = moveCodec([]string{"alac", "ec3"}, []string{"aac"}, "ec3", 1, 1)
	if !slices.Equal(on, []string{"alac"}) || !slices.Equal(off, []string{"ec3", "aac"}) {
		t.Errorf("Alt+Down on the last codec in use gave %v / %v", on, off)
	}
}
