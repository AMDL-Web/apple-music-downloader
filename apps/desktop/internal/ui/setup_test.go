package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/setup"
	"amdl/internal/client"
	"amdl/internal/wrapperhost"
)

// setupApp is the setup screen for m, its Setup's posts queued for the test
// to run as the UI thread would.
func setupApp(t *testing.T, s mode.Settings, m mode.Kind) (*App, *fakeActions, *ui.Tester, func(), *[][]string) {
	t.Helper()
	a, act := newTestApp(s)
	q := make(chan func(), 256)
	su, ran := fakeSetup(func(fn func()) { q <- fn })
	a.Setup = su
	a.OpenSetup(m)
	tt := ui.NewTester(a.View, 1280, 800)
	drain := func() {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case fn := <-q:
				fn()
				tt.Frame()
				if su.Run == nil || su.Run.State != setup.Working {
					return
				}
			case <-deadline:
				t.Fatal("the setup's work did not finish")
			}
		}
	}
	return a, act, tt, drain, ran
}

func TestFirstLaunchShowsSetup(t *testing.T) {
	a, act, tt, _, _ := setupApp(t, mode.DefaultSettings(), "")
	if a.Screen != ScreenSetup || !tt.HasText("Set up AMDL") || !tt.HasText("WHERE DOWNLOADS RUN") || tt.HasText("Close") {
		t.Fatalf("first launch: screen %v, texts %q", a.Screen, tt.Texts())
	}
	if err := tt.Click("A server on my network"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("http://192.168.3.5:18570") || tt.HasText("Use this server") {
		t.Fatalf("remote before a test: %q", tt.Texts())
	}
	// What a test of the typed address found, as TestServer hands it back.
	a.su.serverAddr = "http://nas:18570"
	a.Setup.Server = setup.Server{Address: "http://nas:18570", State: setup.Ready,
		Reach:   setup.Line{State: setup.Ready, Text: "amdl 2.0.0 · 12 ms"},
		Account: setup.Line{State: setup.Missing, Text: "Not signed in"},
		Token:   setup.Line{State: setup.Missing, Text: "None"},
		FFmpeg:  setup.Line{State: setup.Failed, Text: "Missing"},
		Folder:  setup.Line{State: setup.Ready, Text: "/volume1/music · 2.1 TB free"}}
	tt.Frame()
	for _, want := range []string{"amdl 2.0.0 · 12 ms", "On the server", "1 of 3 ready", "Use this server"} {
		if !tt.HasText(want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Use this server"); err != nil {
		t.Fatal(err)
	}
	if act.applied == nil || act.applied.Mode != mode.Remote || act.applied.RemoteURL != "http://nas:18570" || a.Screen != ScreenHome {
		t.Errorf("applied %+v, screen %v", act.applied, a.Screen)
	}
}

func TestSetupRunsAnInstallOnlyAfterRun(t *testing.T) {
	a, _, tt, drain, ran := setupApp(t, mode.DefaultSettings(), mode.Standalone)
	if !tt.HasText("QEMU") || tt.HasText("brew install qemu") || !tt.HasText("Not installed") {
		t.Fatalf("missing QEMU before asking: %q", tt.Texts())
	}
	r, _ := tt.Find("QEMU")
	if _, ok := tt.Find("Install…"); !ok {
		t.Fatalf("no Install… for QEMU: %q", tt.Texts())
	}
	// The first Install… is QEMU's, the row above ffmpeg's.
	inst, _ := tt.Find("Install…")
	if inst.Y > r.Y+40 {
		t.Fatalf("the first Install… is not QEMU's")
	}
	tt.ClickAt(inst.X+inst.W/2, inst.Y+inst.H/2)
	tt.Frame()
	if !tt.HasText("brew install qemu") || !tt.HasText("Run") || len(*ran) != 0 {
		t.Fatalf("after Install…: ran %v, texts %q", *ran, tt.Texts())
	}
	if err := tt.Click("Run"); err != nil {
		t.Fatal(err)
	}
	if a.Setup.Run == nil || a.Setup.Run.Tool != "QEMU" {
		t.Fatalf("run %+v", a.Setup.Run)
	}
	drain()
	if got := strings.Join((*ran)[0], " "); got != "/opt/homebrew/bin/brew install qemu" {
		t.Errorf("ran %q", got)
	}
}

func TestSetupShowsTheGuestDownload(t *testing.T) {
	a, _, tt, _, _ := setupApp(t, mode.DefaultSettings(), mode.Standalone)
	a.Setup.Guest = setup.Guest{State: setup.Working, Size: 61_500_000, Rate: 4_200_000,
		Progress: wrapperhost.Progress{Phase: wrapperhost.Downloading, Done: 23_400_000, Total: 61_500_000}}
	tt.Frame()
	if !tt.HasText("23.4 / 61.5 MB") || !tt.HasText("4.2 MB/s") || !tt.HasText("Cancel") {
		t.Errorf("downloading: %q", tt.Texts())
	}
	a.Setup.Guest.Progress = wrapperhost.Progress{Phase: wrapperhost.Verifying, Done: 61_500_000, Total: 61_500_000}
	tt.Frame()
	if !tt.HasText("Checking sha256") || tt.HasText("Cancel") {
		t.Errorf("verifying: %q", tt.Texts())
	}
	a.Setup.Guest = setup.Guest{State: setup.Failed, Err: "artifact 1 is gone from GitHub", Size: 61_500_000}
	tt.Frame()
	if !tt.HasText("Try again") || !tt.HasText("Import…") {
		t.Errorf("failed: %q", tt.Texts())
	}
}

func TestSetupStartsOnceTheWrapperCanRun(t *testing.T) {
	a, act, tt, _, _ := setupApp(t, mode.DefaultSettings(), mode.Standalone)
	if err := tt.Click("Start"); err != nil {
		t.Fatal(err)
	}
	if act.applied != nil {
		t.Fatalf("Start applied with QEMU, the guest and ffmpeg missing: %+v", act.applied)
	}
	a.Setup.QEMU = setup.Tool{State: setup.Ready, Path: "/opt/homebrew/bin/qemu-system-x86_64"}
	a.Setup.FFmpeg = setup.Tool{State: setup.Ready, Path: "/opt/homebrew/bin/ffmpeg", Source: "system"}
	a.Setup.Guest = setup.Guest{State: setup.Ready, Installed: wrapperhost.Installed{ID: "bde3335f47a3"}}
	tt.Frame()
	if err := tt.Click("Start"); err != nil {
		t.Fatal(err)
	}
	if act.applied == nil || act.applied.Mode != mode.Standalone {
		t.Fatalf("applied %+v", act.applied)
	}
}

func TestSetupFollowsTheGuestThenSignsIn(t *testing.T) {
	s := mode.DefaultSettings()
	s.Mode = mode.Standalone
	a, _, tt, _, _ := setupApp(t, s, mode.Standalone)
	sv := &a.Store.Server
	sv.Read = time.Now()
	sv.Wrapper = &client.WrapperStatus{LoginSupported: true, Managed: &client.ManagedWrapper{Mode: "starting", Accel: "tcg"}}
	tt.Frame()
	tt.Advance(34 * time.Second)
	tt.Frame()
	if !tt.HasText("Starting · TCG") || !tt.HasText("0:34") {
		t.Errorf("booting: %q", tt.Texts())
	}
	sv.Wrapper = &client.WrapperStatus{Ready: true, LoginSupported: true, Managed: &client.ManagedWrapper{Mode: "serving", Accel: "tcg"}}
	tt.Frame()
	if !tt.HasText("Serving · TCG") || !tt.HasText("Log in") || !tt.HasText("Not signed in") {
		t.Errorf("serving, signed out: %q", tt.Texts())
	}
	a.Store.Login.LoginID = "x"
	tt.Frame()
	if !tt.HasText("Code sent to your devices") || !tt.HasText("Verify") || !tt.HasText("Start over") {
		t.Errorf("two-step: %q", tt.Texts())
	}
	a.Store.Login.LoginID = ""
	sv.Wrapper = &client.WrapperStatus{Ready: true, LoggedIn: true, Accounts: []string{"someone@icloud.com"}, Regions: []string{"us"},
		LoginSupported: true, Managed: &client.ManagedWrapper{Mode: "serving", Accel: "tcg"}}
	tt.Frame()
	if !tt.HasText("someone@icloud.com · us") || tt.HasText("Log in") {
		t.Errorf("signed in: %q", tt.Texts())
	}
}

func TestSettingsModeCardOpensSetup(t *testing.T) {
	s := mode.DefaultSettings()
	s.Mode, s.RemoteURL = mode.Remote, "http://nas:18570"
	a, act := newTestApp(s)
	a.GoSettings("connection")
	tt := ui.NewTester(a.View, 1280, 800)
	if err := tt.Click("This computer"); err != nil {
		t.Fatal(err)
	}
	if a.Screen != ScreenSetup || a.su.mode != mode.Standalone || act.applied != nil {
		t.Fatalf("screen %v, mode %q, applied %+v", a.Screen, a.su.mode, act.applied)
	}
	tt.Frame()
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	if a.Screen != ScreenSettings || a.Settings.Mode != mode.Remote {
		t.Errorf("after Close: screen %v, mode %q", a.Screen, a.Settings.Mode)
	}
}
