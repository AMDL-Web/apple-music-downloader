package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// The Library is the Apple library: it marks the albums amdl keeps and adds
// the others, from the corner of a cover pointed at.
func TestTheLibraryAddsFromTheAppleLibrary(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenLibrary)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	run(func() bool { return s.Library.AppleStatus == store.SearchReady })
	tt.Frame()
	// A kept album is marked as the search marks it: by its record.
	if !tt.HasText("Orchard Lights is in your library: download again") || !tt.HasText("Glass Weather is in the queue") {
		t.Errorf("no album marked: %q", tt.Texts())
	}
	if _, ok := tt.Find("Add Salt & Static"); ok {
		t.Errorf("a tile not pointed at shows its Add")
	}
	pointAt(t, tt, "Salt & Static")
	if err := tt.Click("Add Salt & Static"); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	run(func() bool { return s.Library.Kept["920004"] })
	tt.Frame()
	if _, ok := tt.Find("Add Salt & Static"); ok {
		t.Errorf("the added album still offers Add: %q", tt.Texts())
	}
}
