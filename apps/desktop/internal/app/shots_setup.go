package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/artwork"
	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/setup"
	"amdl/apps/desktop/internal/store"
	appui "amdl/apps/desktop/internal/ui"
	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/wrapperhost"
)

// The setup screen's shots: every state it can show, staged, rendered
// without a window in both themes, at the boards' size and at the window's
// smallest. Nothing staged runs a command or downloads.

// shotActions stands in for the app around a staged setup screen.
type shotActions struct {
	starting bool
	startErr string
}

func (shotActions) ApplySettings(mode.Settings)            {}
func (shotActions) TestServer(string, func([]mode.Check))  {}
func (shotActions) TestWrapper(string, func([]mode.Check)) {}
func (shotActions) Reveal(string)                          {}
func (shotActions) CopyText(string)                        {}
func (shotActions) ChooseFolder(string, func(string))      {}
func (shotActions) ShowWindow()                            {}
func (shotActions) FitPanel(float32)                       {}
func (s shotActions) Starting() bool                       { return s.starting }
func (s shotActions) StartErr() string                     { return s.startErr }
func (shotActions) Shared() string                         { return "" }
func (shotActions) Reconnect()                             {}
func (shotActions) ChooseFile(string, func(string))        {}
func (shotActions) OpenURL(string)                         {}

// setupStage is one state of the setup screen: the settings it opens on,
// the mode it shows, and what to set or click once it is built.
type setupStage struct {
	name     string
	settings func(*mode.Settings)
	show     mode.Kind
	act      shotActions
	stage    func(v *appui.App, s *setup.Setup, tt *ui.Tester)
}

var (
	qemuPath   = "/opt/homebrew/bin/qemu-system-x86_64"
	ffmpegPath = "/opt/homebrew/bin/ffmpeg"
)

func tools(s *setup.Setup) {
	s.QEMU = setup.Tool{State: setup.Ready, Path: qemuPath}
	s.FFmpeg = setup.Tool{State: setup.Ready, Path: ffmpegPath, Source: "system"}
	s.Guest = setup.Guest{State: setup.Ready, Installed: wrapperhost.Installed{ID: "bde3335f47a3"}}
}

func standalone(s *mode.Settings) { s.Mode = mode.Standalone }

// remoteAddr is a first launch that knows a server's address, as one typed
// before a test.
func remoteAddr(s *mode.Settings) { s.RemoteURL = "http://192.168.3.5:18570" }

func managed(st, accel string, ready, in bool) *client.WrapperStatus {
	w := &client.WrapperStatus{Ready: ready, LoggedIn: in, LoginSupported: true, Managed: &client.ManagedWrapper{Mode: st, Accel: accel}}
	if in {
		w.Accounts, w.Regions = []string{"someone@icloud.com"}, []string{"us"}
	}
	return w
}

func storage(sv *store.Server) {
	sv.System = &client.SystemInfo{Version: "2.0.0", Storage: []client.Storage{{Name: "downloads_dir", Path: "/Users/someone/Music/AMDL", FreeBytes: 412 << 30, TotalBytes: 994 << 30}}}
	sv.System.Tools = append(sv.System.Tools, domain.Capability{Name: "ffmpeg", Available: true, Path: ffmpegPath})
}

var setupStages = []setupStage{
	{name: "01-choose", show: ""},
	{name: "02-local", show: mode.Standalone, stage: func(_ *appui.App, s *setup.Setup, _ *ui.Tester) {
		s.FFmpeg = setup.Tool{State: setup.Ready, Path: ffmpegPath, Source: "system"}
	}},
	{name: "03-consent", show: mode.Standalone, stage: func(_ *appui.App, s *setup.Setup, tt *ui.Tester) {
		s.FFmpeg = setup.Tool{State: setup.Ready, Path: ffmpegPath, Source: "system"}
		tt.Frame()
		_ = tt.Click("Install…") // QEMU's, the first
	}},
	{name: "03b-consent-winget", show: mode.Standalone, stage: func(_ *appui.App, s *setup.Setup, tt *ui.Tester) {
		s.FFmpeg = setup.Tool{State: setup.Ready, Path: `C:\Users\someone\AppData\Local\Microsoft\WinGet\Links\ffmpeg.exe`, Source: "system"}
		cmd := "winget install --id SoftwareFreedomConservancy.QEMU -e --accept-package-agreements --accept-source-agreements --disable-interactivity"
		s.QEMU = setup.Tool{State: setup.Missing, Install: setup.Install{Command: cmd, Args: []string{"winget.exe"}}}
		tt.Frame()
		_ = tt.Click("Install…")
	}},
	{name: "04-installing", show: mode.Standalone, stage: func(_ *appui.App, s *setup.Setup, _ *ui.Tester) {
		s.FFmpeg = setup.Tool{State: setup.Ready, Path: ffmpegPath, Source: "system"}
		s.QEMU.State = setup.Working
		s.Run = &setup.Run{Tool: "QEMU", Command: "brew install qemu", State: setup.Working, Started: time.Now().Add(-72 * time.Second),
			Lines: []string{"==> Fetching dependencies for qemu: capstone, dtc, glib, gnutls, libslirp", "==> Fetching capstone",
				"==> Downloading https://ghcr.io/v2/homebrew/core/capstone/manifests/5.0.3", "########################################### 100.0%",
				"==> Fetching glib", "==> Downloading https://ghcr.io/v2/homebrew/core/glib/blobs/sha256:4f2a91"}}
	}},
	{name: "05-install-failed", show: mode.Standalone, stage: func(_ *appui.App, s *setup.Setup, _ *ui.Tester) {
		s.Run = &setup.Run{Tool: "QEMU", Command: "brew install qemu", State: setup.Failed, Err: "exit status 1",
			Lines: []string{"==> Fetching qemu", "Error: qemu: no bottle available!", "You can try to install from source with:", "  brew install --build-from-source qemu"}}
	}},
	{name: "06-downloading", show: mode.Standalone, stage: func(_ *appui.App, s *setup.Setup, _ *ui.Tester) {
		s.QEMU = setup.Tool{State: setup.Ready, Path: qemuPath}
		s.FFmpeg = setup.Tool{State: setup.Ready, Path: ffmpegPath, Source: "system"}
		s.Guest = setup.Guest{State: setup.Working, Size: 64_451_213, Rate: 4_200_000,
			Progress: wrapperhost.Progress{Phase: wrapperhost.Downloading, Done: 24_480_000, Total: 64_451_213}}
	}},
	{name: "07-guest-failed", show: mode.Standalone, stage: func(_ *appui.App, s *setup.Setup, _ *ui.Tester) {
		s.QEMU = setup.Tool{State: setup.Ready, Path: qemuPath}
		s.Guest = setup.Guest{State: setup.Failed, Size: 64_451_213, Err: "artifact 12598758939 is gone from GitHub"}
	}},
	{name: "08-start", show: mode.Standalone, stage: func(_ *appui.App, s *setup.Setup, _ *ui.Tester) { tools(s) }},
	{name: "09-booting", settings: standalone, show: mode.Standalone, stage: func(v *appui.App, s *setup.Setup, tt *ui.Tester) {
		tools(s)
		v.Store.Server.Wrapper = managed("starting", "tcg", false, false)
		storage(&v.Store.Server)
		tt.Frame()
		tt.Advance(34 * time.Second)
	}},
	{name: "10-sign-in", settings: standalone, show: mode.Standalone, stage: func(v *appui.App, s *setup.Setup, _ *ui.Tester) {
		tools(s)
		v.Store.Server.Wrapper = managed("serving", "tcg", true, false)
		storage(&v.Store.Server)
	}},
	{name: "11-code", settings: standalone, show: mode.Standalone, stage: func(v *appui.App, s *setup.Setup, _ *ui.Tester) {
		tools(s)
		v.Store.Server.Wrapper = managed("serving", "tcg", true, false)
		v.Store.Login.LoginID = "staged"
		storage(&v.Store.Server)
	}},
	{name: "12-ready", settings: standalone, show: mode.Standalone, stage: func(v *appui.App, s *setup.Setup, _ *ui.Tester) {
		tools(s)
		v.Store.Server.Wrapper = managed("serving", "tcg", true, true)
		storage(&v.Store.Server)
	}},
	{name: "13-start-failed", settings: standalone, show: mode.Standalone,
		act:   shotActions{startErr: "no wrapper address: the embedded backend needs one on its first start"},
		stage: func(_ *appui.App, s *setup.Setup, _ *ui.Tester) { tools(s) }},
	{name: "14-elsewhere", show: mode.Standalone, stage: func(_ *appui.App, s *setup.Setup, tt *ui.Tester) {
		s.FFmpeg = setup.Tool{State: setup.Ready, Path: ffmpegPath, Source: "system"}
		tt.Frame()
		_ = tt.Click("Another machine")
	}},
	{name: "15-remote", settings: remoteAddr, show: mode.Remote, stage: func(_ *appui.App, s *setup.Setup, _ *ui.Tester) {
		s.Server = setup.Server{Address: "http://192.168.3.5:18570", State: setup.Ready,
			Reach:   setup.Line{State: setup.Ready, Text: "amdl 2.0.0 · 38 ms"},
			Account: setup.Line{State: setup.Ready, Text: "us, cn"},
			Token:   setup.Line{State: setup.Ready, Text: "From the wrapper · ends …kg=="},
			FFmpeg:  setup.Line{State: setup.Ready, Text: "/usr/bin/ffmpeg"},
			Folder:  setup.Line{State: setup.Ready, Text: "/volume1/music · 2.1 TB free"}}
	}},
	{name: "16-remote-fixes", settings: remoteAddr, show: mode.Remote, stage: func(_ *appui.App, s *setup.Setup, _ *ui.Tester) {
		s.Server = setup.Server{Address: "http://192.168.3.5:18570", State: setup.Ready,
			Reach:   setup.Line{State: setup.Ready, Text: "amdl 2.0.0 · 38 ms"},
			Account: setup.Line{State: setup.Missing, Text: "Not signed in"},
			Token:   setup.Line{State: setup.Missing, Text: "None"},
			FFmpeg:  setup.Line{State: setup.Failed, Text: "Missing"},
			Folder:  setup.Line{State: setup.Missing, Text: "/volume1/music · 1.2 GB free"}}
	}},
}

// stagedSetup is a Setup on a Mac with Homebrew and nothing else, whose
// commands and downloads never run.
func stagedSetup(post func(func())) *setup.Setup {
	none := errors.New("staged")
	return setup.New(post, setup.Deps{
		GOOS:     "darwin",
		LookPath: func(string) (string, error) { return "", none },
		Exists:   func(p string) bool { return p == "/opt/homebrew/bin/brew" },
		FFmpeg:   func(string) (string, string, error) { return "", "", none },
		Home:     wrapperhost.Home{Dir: filepath.Join(string(filepath.Separator), "nonexistent", "amdl-wrapper")},
		Pin:      func(context.Context) (wrapperhost.Pin, error) { return wrapperhost.Pin{}, none },
		Exec:     func(context.Context, []string, func(string)) error { return none },
		Now:      func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) },
	})
}

// renderSetupShots writes setup-<state>-<theme>-<size>.png into dir for
// every staged state, with dirs for the paths the screen names.
func renderSetupShots(dir string, dirs mode.Dirs) error {
	post := func(fn func()) { fn() }
	for _, st := range setupStages {
		for _, theme := range []string{"dark", "light"} {
			for _, size := range [][2]int{{1280, 800}, {820, 560}} {
				settings := mode.DefaultSettings()
				if st.settings != nil {
					st.settings(&settings)
				}
				s := stagedSetup(post)
				v := &appui.App{Store: store.New(post, nil), Art: artwork.New(post), Settings: settings, Dirs: dirs,
					Act: st.act, Theme: theme, Setup: s, NoMotion: true}
				v.OpenSetup(st.show)
				tt := ui.NewTester(v.View, size[0], size[1])
				tt.SetScale(2)
				tt.Frame()
				if st.stage != nil {
					st.stage(v, s, tt)
				}
				tt.Frame()
				path := filepath.Join(dir, fmt.Sprintf("setup-%s-%s-%d.png", st.name, theme, size[0]))
				if err := writePNG(path, tt.Image()); err != nil {
					return err
				}
			}
		}
	}
	// Settings' connection page, which opens the setup screen.
	for _, theme := range []string{"dark", "light"} {
		for _, m := range []mode.Kind{mode.Remote, mode.Standalone} {
			settings := mode.DefaultSettings()
			settings.Mode, settings.RemoteURL = m, "http://192.168.3.5:18570"
			v := &appui.App{Store: store.New(post, nil), Art: artwork.New(post), Settings: settings, Dirs: dirs,
				Act: shotActions{}, Theme: theme, Setup: stagedSetup(post), NoMotion: true}
			v.GoSettings("connection")
			tt := ui.NewTester(v.View, 1280, 800)
			tt.SetScale(2)
			tt.Frame()
			if err := writePNG(filepath.Join(dir, fmt.Sprintf("connection-%s-%s.png", m, theme)), tt.Image()); err != nil {
				return err
			}
		}
	}
	return nil
}
