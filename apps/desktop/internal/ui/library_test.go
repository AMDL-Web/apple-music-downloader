package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/domain"
)

// The Library lists the records with their state, filters them by a flag,
// pauses and resumes one, and adds from the Apple library what amdl does
// not keep.
func TestTheLibraryListsPausesAndAddsRecords(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenLibrary)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	run(func() bool { return s.Library.Status == store.SearchReady && s.Library.Summary != nil })
	tt.Frame()
	for _, want := range []string{"Orchard Lights", "Downloaded", "Late Bloom", "2 of 5", "In the queue", "All · 7"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in the Library: %q", want, tt.Texts())
		}
	}
	late := domain.ResourceRef{Kind: domain.ResourceAlbum, ID: "920006"}
	s.Pause(late)
	run(func() bool { return s.Library.Summary.Albums.Flags[store.FilterPaused] == 1 })
	tt.Frame()
	if err := tt.Click("Paused filter"); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	run(func() bool { return s.Library.Status == store.SearchReady && len(s.Library.Rows) == 1 })
	tt.Frame()
	if r := s.Library.Rows[0]; r.Ref != late || !r.Paused || !tt.HasText("Paused") {
		t.Errorf("the paused filter lists %+v: %q", s.Library.Rows, tt.Texts())
	}
	s.Resume(late)
	run(func() bool { return s.Library.Summary.Albums.Flags[store.FilterPaused] == 0 })
	if m := s.RecordMark(domain.ResourceAlbum, "920006"); m == nil || m.Paused {
		t.Errorf("resumed, the record's mark is %+v", m)
	}
	// The Apple library marks the albums amdl keeps and adds the others,
	// from the corner of a cover pointed at.
	a.lib.tab = int(store.LibraryApple)
	s.SetLibraryTab(store.LibraryApple)
	run(func() bool { return s.Library.AppleStatus == store.SearchReady })
	tt.Frame()
	if !tt.HasText("In Library") {
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
