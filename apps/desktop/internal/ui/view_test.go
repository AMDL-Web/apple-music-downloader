package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/artwork"
	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/setup"
	"amdl/apps/desktop/internal/store"
	"amdl/internal/wrapperhost"
)

type fakeActions struct {
	applied    *mode.Settings
	reconnects int
}

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
func (f *fakeActions) Reconnect()                             { f.reconnects++ }
func (f *fakeActions) ChooseFile(string, func(string))        {}
func (f *fakeActions) OpenURL(string)                         {}

func newTestApp(s mode.Settings) (*App, *fakeActions) {
	post := func(fn func()) { fn() }
	act := &fakeActions{}
	su, _ := fakeSetup(post)
	return &App{Store: store.New(post, nil), Art: artwork.New(post), Settings: s, Act: act, Theme: "dark", Setup: su}, act
}

func TestQueueWaitsForTheFirstList(t *testing.T) {
	a, _ := newTestApp(mode.Settings{Mode: mode.Remote, RemoteURL: "http://nas:18570"})
	a.Screen = ScreenQueue
	tt := ui.NewTester(a.View, 1280, 800)
	if !tt.HasText("Connecting…") || !tt.HasText("nas:18570") {
		t.Fatalf("texts %q", tt.Texts())
	}
	// A window too narrow for the sidebar closes it.
	tt.SetSize(820, 560)
	tt.Advance(sidebarSlide)
	tt.Frame()
	if tt.HasText("nas:18570") {
		t.Error("the narrow window still shows the sidebar's connection card")
	}
}

func TestSidebarFollowsTheUserThenTheWidth(t *testing.T) {
	a, act := newTestApp(remoteSettings())
	tt := ui.NewTester(a.View, 1280, 800)
	if !a.SidebarShown() {
		t.Fatal("a wide window opens with the sidebar closed")
	}
	if err := tt.Click("Hide the sidebar"); err != nil {
		t.Fatal(err)
	}
	if a.SidebarShown() || act.applied == nil || !act.applied.HideSidebar {
		t.Fatalf("after hiding: shown %v, saved %+v", a.SidebarShown(), act.applied)
	}
	tt.Advance(sidebarSlide)
	tt.Frame()
	if err := tt.Click("Show the sidebar"); err != nil {
		t.Fatal(err)
	}
	if !a.SidebarShown() || act.applied.HideSidebar {
		t.Fatalf("after showing: shown %v, saved %+v", a.SidebarShown(), act.applied)
	}
	// Narrowed, the window closes it; opened there, it stays until the
	// window is wide again, which keeps the user's choice.
	tt.SetSize(820, 560)
	tt.Frame()
	if a.SidebarShown() {
		t.Fatal("the narrow window kept the sidebar")
	}
	a.ToggleSidebar()
	tt.Frame()
	if !a.SidebarShown() || act.applied.HideSidebar {
		t.Fatalf("opened in the narrow window: shown %v, saved %+v", a.SidebarShown(), act.applied)
	}
	tt.SetSize(1280, 800)
	tt.Frame()
	tt.SetSize(820, 560)
	tt.Frame()
	if a.SidebarShown() {
		t.Error("narrowed again, the window kept the sidebar the user opened in the last narrow spell")
	}
}

var setupNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// fakeSetup is a Setup on a Mac with Homebrew and nothing else: ffmpeg and
// QEMU missing with runnable installs, no guest. Nothing it runs reaches
// this computer: Exec records the command and Pin fails.
func fakeSetup(post func(func())) (*setup.Setup, *[][]string) {
	var ran [][]string
	s := setup.New(post, setup.Deps{
		GOOS:     "darwin",
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Exists:   func(p string) bool { return p == "/opt/homebrew/bin/brew" },
		FFmpeg:   func(string) (string, string, error) { return "", "", errors.New("ffmpeg not found") },
		Home:     wrapperhost.Home{Dir: filepath.Join(os.TempDir(), "amdl-ui-test-no-wrapper")},
		Pin: func(context.Context) (wrapperhost.Pin, error) {
			return wrapperhost.Pin{}, errors.New("no download in tests")
		},
		Exec: func(_ context.Context, args []string, _ func(string)) error {
			ran = append(ran, args)
			return nil
		},
		// A fixed clock: a check looks again only after Recheck, so a test
		// sets the state it shows.
		Now: func() time.Time { return setupNow },
	})
	return s, &ran
}

// Where the window shows its material, the sidebar shows it: nothing in
// the view, the root included, paints over it, while the content stays
// opaque. Reduce Transparency (Opaque) gives the sidebar its own ground.
func TestSidebarShowsTheMaterial(t *testing.T) {
	a, _ := newTestApp(mode.Settings{Mode: mode.Remote, RemoteURL: "http://nas:18570"})
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetVibrancy(true)
	tt.Frame()
	img := tt.Image()
	b := img.Bounds()
	side := img.RGBAAt(b.Dx()*100/1280, b.Dy()*700/800)
	page := img.RGBAAt(b.Dx()*900/1280, b.Dy()*700/800)
	if side.A != 0 {
		t.Errorf("the sidebar covers the material: %v", side)
	}
	if page.A != 255 {
		t.Errorf("the content shows the material: %v", page)
	}
	a.Opaque = true
	tt.Frame()
	if side := tt.Image().RGBAAt(b.Dx()*100/1280, b.Dy()*700/800); side.A != 255 {
		t.Errorf("with Reduce Transparency the sidebar is not opaque: %v", side)
	}
}
