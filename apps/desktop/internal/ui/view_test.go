package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/artwork"
	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/store"
)

type fakeActions struct{ applied *mode.Settings }

func (f *fakeActions) ApplySettings(s mode.Settings)          { f.applied = &s }
func (f *fakeActions) TestServer(string, func([]mode.Check))  {}
func (f *fakeActions) TestWrapper(string, func([]mode.Check)) {}
func (f *fakeActions) Reveal(string)                          {}
func (f *fakeActions) CopyText(string)                        {}
func (f *fakeActions) ChooseFolder(string, func(string))      {}
func (f *fakeActions) ShowWindow()                            {}
func (f *fakeActions) FitPanel(float32)                       {}
func (f *fakeActions) Starting() bool                         { return false }
func (f *fakeActions) StartErr() string                       { return "" }
func (f *fakeActions) Shared() string                         { return "" }

func newTestApp(s mode.Settings) (*App, *fakeActions) {
	post := func(fn func()) { fn() }
	act := &fakeActions{}
	return &App{Store: store.New(post, nil), Art: artwork.New(post), Settings: s, Act: act, Theme: "dark"}, act
}

func TestFirstLaunchChoosesAMode(t *testing.T) {
	a, act := newTestApp(mode.DefaultSettings())
	a.Screen = ScreenSettings
	tt := ui.NewTester(a.View, 1280, 800)
	if !tt.HasText("WHERE DOWNLOADS RUN") || !tt.HasText("Not connected") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click("A server on my network"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Server address") {
		t.Fatalf("no address field after choosing remote: %q", tt.Texts())
	}
	if err := tt.Click("http://192.168.3.5:18570"); err != nil {
		t.Fatal(err)
	}
	tt.Type("http://nas:18570")
	if err := tt.Click("Apply"); err != nil {
		t.Fatal(err)
	}
	if act.applied == nil || act.applied.Mode != mode.Remote || act.applied.RemoteURL != "http://nas:18570" {
		t.Fatalf("applied %+v", act.applied)
	}
}

func TestQueueWaitsForTheFirstList(t *testing.T) {
	a, _ := newTestApp(mode.Settings{Mode: mode.Remote, RemoteURL: "http://nas:18570"})
	tt := ui.NewTester(a.View, 1280, 800)
	if !tt.HasText("Connecting…") || !tt.HasText("nas:18570") {
		t.Fatalf("texts %q", tt.Texts())
	}
	// The compact window folds the sidebar into a rail.
	tt.SetSize(820, 560)
	tt.Frame()
	if tt.HasText("nas:18570") {
		t.Error("the compact window still shows the sidebar's connection card")
	}
}
