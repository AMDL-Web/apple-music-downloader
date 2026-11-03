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
	s.Guest = setup.Guest{State: setup.Ready, Installed: wrapperhost.Installed{Kind: wrapperhost.ManagerGuest, ID: "bde3335f47a3"}}
}

func standalone(s *mode.Settings) { s.Mode = mode.Standalone }

// typeInto types text into the field that shows placeholder.
func typeInto(tt *ui.Tester, placeholder, text string) {
	tt.Frame()
	if tt.Click(placeholder) == nil {
		tt.Type(text)
	}
}

// remoteAddr is a first launch that knows a server's address, as one typed
// before a test.
func remoteAddr(s *mode.Settings) { s.RemoteURL = "http://192.168.3.5:18570" }

func managed(st, accel string, ready, in bool) *client.WrapperStatus {
	w := &client.WrapperStatus{Ready: ready, LoggedIn: in, LoginSupported: true, Managed: &client.ManagedWrapper{Mode: st, Accel: accel}}
	if in {
		w.Regions = []string{"us"}
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
		s.Guest = setup.Guest{State: setup.Working, Size: 80_125_881, Rate: 6_500_000,
			Progress: wrapperhost.Progress{Phase: wrapperhost.Downloading, Done: 34_480_000, Total: 80_125_881}}
	}},
	{name: "07-guest-failed", show: mode.Standalone, stage: func(_ *appui.App, s *setup.Setup, _ *ui.Tester) {
		s.QEMU = setup.Tool{State: setup.Ready, Path: qemuPath}
		s.Guest = setup.Guest{State: setup.Failed, Size: 80_125_881, Err: "https://github.com/AMDL-Web/wrapper/releases/download/guest-bb4d4d86f6cd/manager-qemu-guest.zip is not published"}
	}},
	{name: "08-start", show: mode.Standalone, stage: func(_ *appui.App, s *setup.Setup, _ *ui.Tester) { tools(s) }},
	{name: "09-booting", settings: standalone, show: mode.Standalone, stage: func(v *appui.App, s *setup.Setup, tt *ui.Tester) {
		tools(s)
		v.Store.Server.Wrapper = managed("starting", "tcg", false, false)
		storage(&v.Store.Server)
		tt.Frame()
		tt.Advance(34 * time.Second)
	}},
	{name: "09a-wrapper-download", settings: standalone, show: mode.Standalone, stage: func(v *appui.App, s *setup.Setup, _ *ui.Tester) {
		tools(s)
		v.Store.Server.Wrapper = managed("starting", "tcg", false, false)
		v.Store.Server.Wrapper.Managed.Download = &client.WrapperDownload{DoneBytes: 21_400_000, TotalBytes: 50_118_510}
		storage(&v.Store.Server)
	}},
	{name: "10-sign-in", settings: standalone, show: mode.Standalone, stage: func(v *appui.App, s *setup.Setup, _ *ui.Tester) {
		tools(s)
		v.Store.Server.Wrapper = managed("serving", "tcg", true, false)
		storage(&v.Store.Server)
	}},
	{name: "10b-signing-in", settings: standalone, show: mode.Standalone, stage: func(v *appui.App, s *setup.Setup, tt *ui.Tester) {
		tools(s)
		v.Store.Server.Wrapper = managed("serving", "tcg", true, false)
		storage(&v.Store.Server)
		typeInto(tt, "Password", "not-a-real-password")
		tt.Key(ui.Shift, ui.KeyTab) // back to the Apple ID field; its row is titled the same
		tt.Type("someone@icloud.com")
		v.Store.Login.Busy, v.Store.Login.Since = true, tt.Now()
		tt.Frame()
		tt.Advance(3 * time.Second)
	}},
	{name: "11-code", settings: standalone, show: mode.Standalone, stage: func(v *appui.App, s *setup.Setup, _ *ui.Tester) {
		tools(s)
		v.Store.Server.Wrapper = managed("serving", "tcg", true, false)
		v.Store.Login.LoginID = "staged"
		storage(&v.Store.Server)
	}},
	{name: "11b-verifying", settings: standalone, show: mode.Standalone, stage: func(v *appui.App, s *setup.Setup, tt *ui.Tester) {
		tools(s)
		v.Store.Server.Wrapper = managed("serving", "tcg", true, false)
		v.Store.Login.LoginID = "staged"
		storage(&v.Store.Server)
		typeInto(tt, "6-digit code", "123456")
		v.Store.Login.Busy, v.Store.Login.Since = true, tt.Now()
		tt.Frame()
		tt.Advance(3 * time.Second)
	}},
	{name: "12-ready", settings: standalone, show: mode.Standalone, stage: func(v *appui.App, s *setup.Setup, _ *ui.Tester) {
		tools(s)
		v.Store.Server.Wrapper = managed("serving", "tcg", true, true)
		v.Store.Server.Accounts = []client.Account{{ID: "a1", Name: "someone@icloud.com", Region: "us", Default: true, State: client.AccountActive}}
		storage(&v.Store.Server)
	}},
	{name: "13-start-failed", settings: standalone, show: mode.Standalone,
		act:   shotActions{startErr: "open database: /Users/me/Library/Application Support/AMDL/server/db/amdl.db was written by a 2.0 release candidate (schema 5), which this version does not read: move the file aside (it is not changed) and start again"},
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
			FFmpeg:  setup.Line{State: setup.Ready, Text: "/usr/bin/ffmpeg"},
			Folder:  setup.Line{State: setup.Ready, Text: "/volume1/music · 2.1 TB free"}}
	}},
	{name: "16-remote-fixes", settings: remoteAddr, show: mode.Remote, stage: func(_ *appui.App, s *setup.Setup, _ *ui.Tester) {
		s.Server = setup.Server{Address: "http://192.168.3.5:18570", State: setup.Ready,
			Reach:   setup.Line{State: setup.Ready, Text: "amdl 2.0.0 · 38 ms"},
			Account: setup.Line{State: setup.Missing, Text: "Not signed in"},
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
		for _, name := range []string{"remote", "standalone", "update", "uninstall", "elsewhere"} {
			settings := mode.DefaultSettings()
			settings.Mode, settings.RemoteURL = mode.Standalone, "http://192.168.3.5:18570"
			if name == "remote" {
				settings.Mode = mode.Remote
			}
			v := &appui.App{Store: store.New(post, nil), Art: artwork.New(post), Settings: settings, Dirs: dirs,
				Act: shotActions{}, Theme: theme, Setup: stagedSetup(post), NoMotion: true}
			switch name {
			case "standalone", "update", "uninstall":
				v.Store.Server.Wrapper = managed("serving", "hvf", true, true)
				tools(v.Setup)
				v.Setup.Guest.Installed.Commit = "bde3335f47a30518e1f53f79cbae94bf76b27fb3"
				v.Setup.Guest.Latest.Commit = v.Setup.Guest.Installed.Commit
				if name == "update" {
					v.Setup.Guest.Latest.Commit = "7c41d09e2b8a5f3361c0d4e9a8b7f2e1d0c9b8a7"
				}
			case "elsewhere":
				v.Settings.WrapperElsewhere, v.Settings.WrapperAddress = true, "http://192.168.3.44:12340"
				v.Store.Server.Wrapper = &client.WrapperStatus{Ready: true}
			}
			v.GoSettings("connection")
			tt := ui.NewTester(v.View, 1280, 800)
			tt.SetScale(2)
			tt.Frame()
			if name == "uninstall" {
				if err := tt.Click("Uninstall…"); err != nil {
					return err
				}
				tt.Frame()
			}
			if err := writePNG(filepath.Join(dir, fmt.Sprintf("connection-%s-%s.png", name, theme)), tt.Image()); err != nil {
				return err
			}
		}
	}
	// Settings' Account page: two accounts signed in, and none yet, where
	// the storefront shows under the login.
	seen := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	for _, theme := range []string{"dark", "light"} {
		for _, name := range []string{"accounts", "none"} {
			settings := mode.DefaultSettings()
			settings.Mode = mode.Standalone
			v := &appui.App{Store: store.New(post, nil), Art: artwork.New(post), Settings: settings, Dirs: dirs,
				Act: shotActions{}, Theme: theme, Setup: stagedSetup(post), NoMotion: true}
			sv := &v.Store.Server
			sv.Read = seen
			sv.Wrapper = managed("serving", "hvf", true, name == "accounts")
			sv.Accounts = []client.Account{}
			if name == "accounts" {
				sv.Wrapper.Regions = []string{"cn", "us"}
				sv.Accounts = []client.Account{
					{ID: "a1", Name: "someone@icloud.com", Region: "cn", Default: true, LibrarySync: true, AppleLibrary: true, State: client.AccountActive, AddedAt: seen, SeenAt: &seen},
					{ID: "a2", Name: "someone.us@icloud.com", Region: "us", Position: 1, State: client.AccountActive, AddedAt: seen, SeenAt: &seen},
				}
			}
			v.Store.Config.Values = map[string]any{"catalog": map[string]any{"default_storefront": "cn", "language": "zh-Hans_CN"}}
			v.GoSettings("account")
			tt := ui.NewTester(v.View, 1280, 800)
			tt.SetScale(2)
			tt.Frame()
			tt.Frame()
			if err := writePNG(filepath.Join(dir, fmt.Sprintf("account-%s-%s.png", name, theme)), tt.Image()); err != nil {
				return err
			}
		}
	}
	return nil
}
