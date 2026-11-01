//go:build !race

// Home's field grows frame by frame on the Tester's clock, which the race
// detector slows many times over with no concurrency to check.

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// pastedLinks are three of the demo's albums, one to a line.
var pastedLinks = strings.Join([]string{
	"https://music.apple.com/us/album/glass-weather/920001",
	"https://music.apple.com/us/album/low-tide-signals/920003?i=92000302",
	"https://music.apple.com/us/album/orchard-lights/920005",
}, "\n")

// fieldApp is home with the demo's links pasted into its field, settled.
func fieldApp(t *testing.T, text string) (*App, *ui.Tester) {
	t.Helper()
	a, run := demoApp(t)
	run(func() bool { return a.Store.Loaded })
	a.Go(ScreenHome)
	tt := ui.NewTester(a.View, 1280, 800)
	a.Store.SetOmniText(text)
	run(func() bool {
		for _, l := range a.Store.Omni.Links {
			if l.Lookup.Title == "" {
				return false
			}
		}
		return len(a.Store.Omni.Links) > 0
	})
	tt.Advance(2 * time.Second)
	return a, tt
}

// fieldBottom is the field's bottom edge as it shows, which the edge's
// handle straddles.
func fieldBottom(t *testing.T, tt *ui.Tester) float32 {
	t.Helper()
	e, ok := tt.Find("Field height")
	if !ok {
		t.Fatalf("no field edge in %q", tt.Texts())
	}
	return e.Y + e.H - 4
}

// Pasted links grow the field to their lines, and each link's row takes the
// field's whole width, one under another.
func TestTheFieldGrowsToItsLinksAndTheirRowsStack(t *testing.T) {
	a, tt := fieldApp(t, pastedLinks)
	if h := fieldBottom(t, tt) - 77 + 16; h <= fieldHeight+40 {
		t.Errorf("three pasted links left the field %v DIPs tall", h)
	}
	field, _ := tt.Find("Paste links or search Apple Music")
	var last ui.Rect
	for i, l := range a.Store.Omni.Links {
		r, ok := tt.Find("Remove " + title(l))
		if !ok {
			t.Fatalf("no row for %s", l.URL)
		}
		if i > 0 && (r.X != last.X || r.Y <= last.Y) {
			t.Errorf("row %d at %v, not under row %d at %v", i, r, i-1, last)
		}
		last = r
	}
	// The rows' remove buttons end where the field's clear button does.
	clear, _ := tt.Find("Clear the field")
	if d := (last.X + last.W) - (clear.X + clear.W); d < -12 || d > 12 {
		t.Errorf("the rows end %v DIPs from the field's end (%v, %v)", d, last, field)
	}
}

// The field's bottom edge drags it taller and shorter, following the
// pointer; it keeps that height while links go, and fits one line again once
// it is emptied.
func TestTheFieldsEdgeDragsItsHeight(t *testing.T) {
	a, tt := fieldApp(t, pastedLinks)
	e, _ := tt.Find("Field height")
	x, y := e.X+e.W/2, e.Y+e.H/2
	start := fieldBottom(t, tt)
	// A click that does not move leaves it fitting its text.
	tt.Press(x, y)
	tt.Release(x, y)
	tt.Advance(time.Second)
	if a.fieldDrawn != 0 {
		t.Fatalf("a click on the edge set the field to %v DIPs", a.fieldDrawn)
	}
	tt.Press(x, y)
	for i := 1; i <= 20; i++ {
		tt.Move(x, y+float32(5*i))
		tt.Advance(refresh)
		if got := fieldBottom(t, tt); got < start+float32(5*i)-1 || got > start+float32(5*i)+1 {
			t.Fatalf("dragged %d DIPs down, the field's bottom moved %v", 5*i, got-start)
		}
	}
	tt.Release(x, y+100)
	tt.Advance(time.Second)
	drawn := fieldBottom(t, tt)
	// Changes the Tester does not hear of: the frames that show them are
	// built here.
	a.removeLink(a.Store.Omni.Links[0].URL)
	tt.Frame()
	tt.Advance(time.Second)
	if got := fieldBottom(t, tt); got != drawn {
		t.Errorf("a link going moved the dragged field's bottom from %v to %v", drawn, got)
	}
	a.clearField()
	tt.Frame()
	tt.Advance(time.Second)
	if a.fieldDrawn != 0 {
		t.Errorf("an emptied field kept its dragged height %v", a.fieldDrawn)
	}
}

// A double click on the dragged field's edge fits it to its text again.
func TestADoubleClickOnTheEdgeFitsTheField(t *testing.T) {
	a, tt := fieldApp(t, pastedLinks)
	fits := fieldBottom(t, tt)
	e, _ := tt.Find("Field height")
	x, y := e.X+e.W/2, e.Y+e.H/2
	tt.Press(x, y)
	for i := 1; i <= 10; i++ {
		tt.Move(x, y+float32(10*i))
		tt.Advance(refresh)
	}
	tt.Release(x, y+100)
	tt.Advance(time.Second)
	if fieldBottom(t, tt) <= fits+50 {
		t.Fatalf("the drag left the field's bottom at %v from %v", fieldBottom(t, tt), fits)
	}
	e, _ = tt.Find("Field height")
	x, y = e.X+e.W/2, e.Y+e.H/2
	for range 2 {
		tt.Press(x, y)
		tt.Release(x, y)
		tt.Advance(50 * time.Millisecond)
	}
	tt.Advance(time.Second)
	if a.fieldDrawn != 0 || fieldBottom(t, tt) != fits {
		t.Errorf("after a double click the field is %v DIPs drawn, its bottom at %v, not %v", a.fieldDrawn, fieldBottom(t, tt), fits)
	}
}
