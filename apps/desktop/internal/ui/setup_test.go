package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
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
func setupApp(t *testing.T, s mode.Settings, m mode.Kind, opts ...func(*setup.Deps)) (*App, *fakeActions, *ui.Tester, func(), *[][]string) {
	t.Helper()
	a, act := newTestApp(s)
	q := make(chan func(), 256)
	su, ran := fakeSetup(func(fn func()) { q <- fn }, opts...)
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
	a.Setup.Guest = setup.Guest{State: setup.Ready, Installed: wrapperhost.Installed{Kind: wrapperhost.ManagerGuest, ID: "bde3335f47a3"}}
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
	sv.Wrapper = &client.WrapperStatus{Ready: true, LoggedIn: true, Regions: []string{"us"},
		LoginSupported: true, Managed: &client.ManagedWrapper{Mode: "serving", Accel: "tcg"}}
	sv.Accounts = []client.Account{{ID: "a1", Name: "someone@icloud.com", Region: "us", Default: true, State: client.AccountActive}}
	tt.Frame()
	if !tt.HasText("someone@icloud.com · United States") || tt.HasText("Log in") {
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

func TestRemoteRetestKeepsTheLastResultsUntilTheNewOnesLand(t *testing.T) {
	a, _, tt, _, _ := setupApp(t, mode.DefaultSettings(), mode.Remote)
	a.su.serverAddr = "http://nas:18570"
	a.su.serverFrom = tt.Now()
	a.Setup.Testing = "http://nas:18570"
	a.Setup.Server = setup.Server{Address: "http://nas:18570", State: setup.Ready,
		Reach:   setup.Line{State: setup.Ready, Text: "amdl 2.0.0 · 12 ms"},
		Account: setup.Line{State: setup.Ready, Text: "someone@icloud.com"}}
	tt.Frame()
	if !tt.HasText("amdl 2.0.0 · 12 ms") || !tt.HasText("someone@icloud.com") || tt.HasText("Testing") {
		t.Errorf("a retest that answers quickly: %q", tt.Texts())
	}
	tt.Advance(statusDelay + time.Millisecond)
	tt.Frame()
	if !tt.HasText("Testing") || !tt.HasText("someone@icloud.com") {
		t.Errorf("a slow retest says Testing over the last results: %q", tt.Texts())
	}

	a.su.serverFrom = tt.Now()
	a.Setup.Server = setup.Server{}
	tt.Frame()
	if tt.HasText("Testing") {
		t.Errorf("a first test shows Testing at once: %q", tt.Texts())
	}
	tt.Advance(statusDelay + time.Millisecond)
	tt.Frame()
	if !tt.HasText("Testing") {
		t.Errorf("a slow first test shows no Testing: %q", tt.Texts())
	}
}

func TestOpeningSetupForgetsTheLastServerTest(t *testing.T) {
	s := mode.DefaultSettings()
	s.Mode, s.RemoteURL = mode.Remote, "http://nas:18570"
	a, _, _, _, _ := setupApp(t, s, mode.Remote)
	a.Setup.Server = setup.Server{Address: "http://nas:18570", State: setup.Ready, Reach: setup.Line{State: setup.Ready, Text: "amdl 2.0.0"}}
	a.Setup.Testing = "http://nas:18570"
	a.OpenSetup(mode.Remote)
	if a.Setup.Server.Address != "" || a.Setup.Testing != "" {
		t.Errorf("the last test survived opening: %+v", a.Setup.Server)
	}
}

func TestAnotherMachineWithoutAnAddressIsNotApplied(t *testing.T) {
	s := mode.DefaultSettings()
	s.Mode = mode.Standalone
	a, _, _, _, _ := setupApp(t, s, mode.Standalone)
	a.su.elsewhere = true
	if a.localApplied() {
		t.Error("Another machine with no address counts as applied")
	}
	a.Settings.WrapperAddress, a.su.wrapperAddr = "http://wrapper:30020", "http://wrapper:30020"
	if !a.localApplied() {
		t.Error("the applied address does not count as applied")
	}
}

func TestMissingHomebrewIsASentenceNotACommand(t *testing.T) {
	a, _, tt, _, _ := setupApp(t, mode.DefaultSettings(), mode.Standalone)
	a.Setup.FFmpeg = setup.Tool{State: setup.Missing, Install: setup.Install{
		Command: "install Homebrew from https://brew.sh, then brew install ffmpeg", URL: "https://brew.sh"}}
	tt.Frame()
	if !tt.HasText("Install Homebrew from https://brew.sh, then brew install ffmpeg") || !tt.HasText("Open brew.sh") {
		t.Fatalf("no sentence or no Open: %q", tt.Texts())
	}
	if tt.HasText("Copy") {
		t.Errorf("a sentence offers Copy: %q", tt.Texts())
	}
}

func TestAFailedInstallOffersChooseForFFmpeg(t *testing.T) {
	a, _, tt, _, _ := setupApp(t, mode.DefaultSettings(), mode.Standalone)
	a.Setup.Run = &setup.Run{Tool: "ffmpeg", State: setup.Failed, Err: "exit 1", Command: "brew install ffmpeg"}
	tt.Frame()
	if !tt.HasText("Try again") || !tt.HasText("Choose…") {
		t.Errorf("failed ffmpeg install: %q", tt.Texts())
	}
}

func TestTheDemoOffersNoBackendOfThisComputer(t *testing.T) {
	s := mode.DefaultSettings()
	s.Mode, s.RemoteURL = mode.Remote, "http://demo"
	a, act := newTestApp(s)
	a.Demo = true
	a.GoSettings("connection")
	tt := ui.NewTester(a.View, 1280, 800)
	if !tt.HasText("Not in the demo") {
		t.Fatalf("settings: %q", tt.Texts())
	}
	if err := tt.Click("This computer"); err != nil {
		t.Fatal(err)
	}
	if a.Screen != ScreenSettings || act.applied != nil {
		t.Fatalf("the card opened %v, applied %+v", a.Screen, act.applied)
	}
	a.OpenSetup(mode.Remote)
	tt.Frame()
	if err := tt.Click("This computer"); err != nil {
		t.Fatal(err)
	}
	if a.su.mode != mode.Remote || !tt.HasText("Not in the demo") {
		t.Errorf("setup: mode %q, texts %q", a.su.mode, tt.Texts())
	}
}

// Another machine stays chosen over a wrapper installed here, and a backend
// that did not start says why on the wrapper row.
func TestAnotherMachineOverAnInstalledGuest(t *testing.T) {
	s := mode.DefaultSettings()
	s.Mode, s.WrapperAddress, s.WrapperElsewhere = mode.Standalone, "http://127.0.0.1:12340", true
	a, act, tt, _, _ := setupApp(t, s, mode.Standalone)
	a.Setup.Guest = setup.Guest{State: setup.Ready, Installed: wrapperhost.Installed{Kind: wrapperhost.ManagerGuest, ID: "bde3335f47a3"}}
	a.OpenSetup(mode.Standalone)
	tt.Frame()
	if !a.su.elsewhere || !a.localApplied() {
		t.Fatalf("elsewhere %v, applied %v; want Another machine, applied", a.su.elsewhere, a.localApplied())
	}
	act.startErr = "read config.yaml: unknown config key(s) download.memory_mode"
	tt.Frame()
	if !tt.HasText(act.startErr) || !tt.HasText("Restart") {
		t.Errorf("start error not on the wrapper row: %q", tt.Texts())
	}
	// Switching to This computer is a change to apply, not the applied state.
	a.su.elsewhere = false
	if a.localApplied() {
		t.Error("This computer counts as applied while the settings use another machine")
	}
}

func TestStartRecordsWhichWrapper(t *testing.T) {
	s := mode.DefaultSettings()
	s.Mode = mode.Standalone
	a, act, _, _, _ := setupApp(t, s, mode.Standalone)
	a.su.elsewhere, a.su.wrapperAddr = true, "http://127.0.0.1:12340"
	a.startLocal()
	if act.applied == nil || !act.applied.WrapperElsewhere || act.applied.WrapperAddress != "http://127.0.0.1:12340" {
		t.Fatalf("Another machine applied %+v", act.applied)
	}
	a.su.elsewhere = false
	a.startLocal()
	if act.applied.WrapperElsewhere {
		t.Errorf("This computer applied %+v", act.applied)
	}
}

// Settings show this computer's wrapper by how it runs, with no address to
// type or test; choosing Another machine opens the setup screen there.
func TestConnectionSettingsShowWhereTheWrapperRuns(t *testing.T) {
	s := mode.DefaultSettings()
	s.Mode, s.WrapperAddress = mode.Standalone, "http://127.0.0.1:12340"
	a, act := newTestApp(s)
	a.Store.Server.Wrapper = &client.WrapperStatus{Ready: true, Managed: &client.ManagedWrapper{Mode: "serving", Accel: "hvf"}}
	a.GoSettings("connection")
	tt := ui.NewTester(a.View, 1280, 800)
	if !tt.HasText("Serving · HVF") || tt.HasText("Test connection") || tt.HasText("http://127.0.0.1:12340") {
		t.Fatalf("this computer's wrapper: %q", tt.Texts())
	}
	if err := tt.Click("Another machine"); err != nil {
		t.Fatal(err)
	}
	if a.Screen != ScreenSetup || a.su.mode != mode.Standalone || !a.su.elsewhere || act.applied != nil {
		t.Fatalf("screen %v, mode %q, elsewhere %v, applied %+v", a.Screen, a.su.mode, a.su.elsewhere, act.applied)
	}
}

// connectionWithGuest is the connection page for this computer's wrapper in
// mode, or answering none when mode is empty, with a guest in state g.
func connectionWithGuest(t *testing.T, wrapperMode string, g setup.Guest) (*App, *ui.Tester) {
	t.Helper()
	s := mode.DefaultSettings()
	s.Mode = mode.Standalone
	a, _ := newTestApp(s)
	a.Setup, _ = fakeSetup(func(func()) {})
	a.Setup.Guest = g
	if wrapperMode == "" {
		a.Store.Server.WrapperErr = "wrapper /status: connection refused"
	} else {
		a.Store.Server.Wrapper = &client.WrapperStatus{Ready: wrapperMode == "serving", Managed: &client.ManagedWrapper{Mode: wrapperMode, Accel: "whpx"}}
	}
	a.GoSettings("connection")
	return a, ui.NewTester(a.View, 1280, 800)
}

// The uninstall dialog's label flips its switch, as a checkbox's label does,
// and a click on the switch itself flips it once.
func TestUninstallLabelFlipsTheSwitch(t *testing.T) {
	a, tt := connectionWithGuest(t, "serving", setup.Guest{State: setup.Ready, Installed: wrapperhost.Installed{Kind: wrapperhost.ManagerGuest, ID: "34e9b5796c0d"}})
	if err := tt.Click("Uninstall…"); err != nil {
		t.Fatal(err)
	}
	// Find gives the switch, labeled as its text; the text is 10 to its right.
	sw, ok := tt.Find("Also log out the account")
	if !ok {
		t.Fatalf("no switch: %q", tt.Texts())
	}
	tt.ClickAt(sw.X+sw.W+10+20, sw.Y+sw.H/2)
	tt.Frame()
	if !a.set.purge {
		t.Fatal("a click on the label left the switch off")
	}
	tt.ClickAt(sw.X+sw.W/2, sw.Y+sw.H/2)
	tt.Frame()
	if a.set.purge {
		t.Fatal("a click on the switch did not turn it off, or turned it twice")
	}
}

// A wrapper stopped because its guest is gone offers no Start: the guest
// row's Download is the way back.
func TestStoppedWrapperStartsOnlyWithAGuest(t *testing.T) {
	_, tt := connectionWithGuest(t, "stopped", setup.Guest{State: setup.Missing, Size: 64 << 20})
	if !tt.HasText("Stopped") || tt.HasText("Start") || !tt.HasText("Download") {
		t.Fatalf("no guest: %q", tt.Texts())
	}
	_, tt = connectionWithGuest(t, "stopped", setup.Guest{State: setup.Ready, Installed: wrapperhost.Installed{Kind: wrapperhost.ManagerGuest, ID: "34e9b5796c0d"}})
	if !tt.HasText("Stopped") || !tt.HasText("Start") {
		t.Fatalf("a guest: %q", tt.Texts())
	}
}

// A backend that started before its guest was installed answers no wrapper
// status: the row says the wrapper is not running, not that it is still
// being read, and offers Restart once a guest is installed.
func TestNoWrapperAnswerIsNotRunning(t *testing.T) {
	_, tt := connectionWithGuest(t, "", setup.Guest{State: setup.Missing, Size: 64 << 20})
	if !tt.HasText("Not running") || tt.HasText("Restart") {
		t.Fatalf("no guest: %q", tt.Texts())
	}
	_, tt = connectionWithGuest(t, "", setup.Guest{State: setup.Ready, Installed: wrapperhost.Installed{Kind: wrapperhost.ManagerGuest, ID: "34e9b5796c0d"}})
	if !tt.HasText("Not running") || !tt.HasText("Restart") {
		t.Fatalf("a guest: %q", tt.Texts())
	}
}

// A guest of the retired bare-lite kind, which the backend cannot run, is
// offered its update on both rows, never a Restart that runs nothing.
func TestAnOldGuestOffersTheUpdateNotARestart(t *testing.T) {
	dir := t.TempDir()
	raw, _ := json.Marshal(wrapperhost.Installed{ID: "bde3335f47a3", Port: 52376})
	if err := os.WriteFile(filepath.Join(dir, "installed.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, screen := range []string{"settings", "setup"} {
		s := mode.DefaultSettings()
		s.Mode = mode.Standalone
		a, act := newTestApp(s)
		a.Setup, _ = fakeSetup(func(func()) {}, func(d *setup.Deps) { d.Home = wrapperhost.Home{Dir: dir} })
		a.Setup.Check("")
		a.Store.Server.WrapperErr = "wrapper /status: connection refused"
		if screen == "settings" {
			a.GoSettings("connection")
		} else {
			a.OpenSetup(mode.Standalone)
		}
		tt := ui.NewTester(a.View, 1280, 800)
		tt.Frame()
		if !a.Setup.Guest.NeedsUpdate() || !a.Setup.Guest.UpdateAvailable() {
			t.Fatalf("%s: guest %+v", screen, a.Setup.Guest)
		}
		if !tt.HasText("needs an update") || !tt.HasText("Update") {
			t.Fatalf("%s: no update offered: %q", screen, tt.Texts())
		}
		if _, ok := tt.Find("Restart"); ok {
			t.Fatalf("%s: Restart offered for a guest no restart can run: %q", screen, tt.Texts())
		}
		if act.reconnects != 0 {
			t.Fatalf("%s: the backend restarted by itself", screen)
		}
	}
}

// A guest that replaced one the backend could not run starts the backend
// again, once.
func TestAReplacedGuestRestartsTheBackendOnce(t *testing.T) {
	a, tt := connectionWithGuest(t, "", setup.Guest{State: setup.Ready, Replaced: true, Installed: wrapperhost.Installed{Kind: wrapperhost.ManagerGuest, ID: "34e9b5796c0d"}})
	act := a.Act.(*fakeActions)
	tt.Frame()
	tt.Frame()
	if act.reconnects != 1 || a.Setup.Guest.Replaced {
		t.Fatalf("reconnects %d, replaced %v", act.reconnects, a.Setup.Guest.Replaced)
	}
}
