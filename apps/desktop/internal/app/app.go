// Package app runs the desktop app: it loads the settings, connects to a
// remote backend or starts the embedded one, owns the main window, the tray
// and the store, and does what the screens ask of it.
package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/artwork"
	"amdl/apps/desktop/internal/client"
	"amdl/apps/desktop/internal/ffmpeg"
	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/shell"
	"amdl/apps/desktop/internal/store"
	appui "amdl/apps/desktop/internal/ui"
)

// Options are the command line's choices. The flags exist for development
// and screenshots; a person launches the app without any.
type Options struct {
	// DataDir replaces the user data directory, to keep a test run apart.
	DataDir string
	// Remote, or Wrapper, starts in that mode with that address instead of
	// the saved settings, without saving them.
	Remote, Wrapper string
	// Theme forces "light" or "dark".
	Theme string
	// Shots captures every screen into this directory as PNGs, then quits.
	Shots string
	// Submit downloads this link once the list loads, before the shots.
	Submit string
}

// App is the running desktop app. Its fields belong to the UI thread.
type App struct {
	opts     Options
	log      *slog.Logger
	dirs     mode.Dirs
	settings mode.Settings

	win   *mygo.Window
	store *store.Store
	shell *shell.Shell
	view  *appui.App

	backend  *mode.Running
	starting bool
	startErr string
	// gen counts connections, so a backend that finishes starting after the
	// settings changed again is stopped instead of used.
	gen      int
	quitting bool
}

// Run runs the app until it quits.
func Run(opts Options) error {
	a := &App{opts: opts, log: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))}
	mygo.App.SetName("AMDL")
	if opts.DataDir != "" {
		// A test run downloads under its own directory too, not into the
		// user's music folder.
		mygo.App.SetPath(mygo.PathMusic, filepath.Join(opts.DataDir, "music"))
		mygo.App.SetPath(mygo.PathUserData, filepath.Join(opts.DataDir, "data"))
		mygo.App.SetPath(mygo.PathCache, filepath.Join(opts.DataDir, "cache"))
		mygo.App.SetPath(mygo.PathLogs, filepath.Join(opts.DataDir, "logs"))
	}
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.quit() })
	mygo.App.WhenReady(a.ready)
	return mygo.App.Run()
}

func (a *App) ready() {
	dirs, err := mode.AppDirs()
	if err != nil {
		mygo.Dialog.Error("AMDL cannot start", err.Error())
		mygo.App.Quit()
		return
	}
	a.dirs = dirs
	if a.settings, err = mode.LoadSettings(dirs.Data); err != nil {
		a.log.Warn("settings unreadable; starting from the defaults", "error", err)
	}
	switch {
	case a.opts.Remote != "":
		a.settings.Mode, a.settings.RemoteURL = mode.Remote, a.opts.Remote
	case a.opts.Wrapper != "":
		a.settings.Mode, a.settings.WrapperAddress = mode.Standalone, a.opts.Wrapper
	}

	a.store = store.New(a.post, nil)
	a.view = &appui.App{Store: a.store, Art: artwork.New(a.post), Settings: a.settings, Dirs: dirs, Act: a, Theme: a.opts.Theme}
	if a.settings.Mode == "" {
		a.view.Screen = appui.ScreenSettings
	}
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "AMDL",
		Width:           1280,
		Height:          800,
		MinWidth:        820,
		MinHeight:       560,
		StateKey:        "main",
		TitleBarStyle:   mygo.TitleBarHidden,
		Vibrancy:        vibrancy(),
		BackgroundColor: "#111219",
		Content:         ui.View(a.view.View),
	})
	a.shell = shell.New(shell.Options{
		Open:      a.showWindow,
		OpenJob:   func(id string) { a.showWindow(); a.view.OpenJob(id) },
		Notify:    func() bool { return a.settings.Notify },
		KeepAwake: func() bool { return a.settings.KeepAwake },
		Log:       a.log,
	})
	a.store.SetNotifier(a.shell)
	a.win.OnClose(func(e *mygo.CloseEvent) {
		// With a tray icon, closing the window keeps downloads and their
		// notifications going; the tray's Quit ends the app.
		if !a.quitting && a.shell.HasTray() {
			e.PreventDefault()
			a.win.Hide()
		}
	})
	a.connect()
	if a.opts.Shots != "" {
		go a.captureScreens(a.opts.Shots)
	}
}

// vibrancy is the sidebar's system material on macOS. Elsewhere the design's
// own solid sidebar reads better than Mica tinted by the desktop.
func vibrancy() mygo.Vibrancy {
	if runtime.GOOS == "darwin" {
		return mygo.VibrancySidebar
	}
	return mygo.VibrancyNone
}

// post runs fn on the UI thread, then refreshes what the shell shows, and
// redraws the window.
func (a *App) post(fn func()) {
	a.win.Update(func() {
		fn()
		if a.shell != nil {
			a.shell.Sync(a.store.Summary())
		}
	})
}

func (a *App) showWindow() {
	a.win.Show()
	a.win.Focus()
}

// connect follows the backend the settings name, starting the embedded one
// in standalone mode.
func (a *App) connect() {
	a.gen++
	gen := a.gen
	a.startErr = ""
	old := a.backend
	a.backend = nil
	a.store.Connect(nil)
	switch a.settings.Mode {
	case mode.Remote:
		if old != nil {
			go a.stopBackend(old)
		}
		c, err := client.New(a.settings.RemoteURL)
		if err != nil {
			a.startErr = err.Error()
			return
		}
		a.store.Connect(c)
	case mode.Standalone:
		a.starting = true
		b := mode.Backend{Dirs: a.dirs, Wrapper: a.settings.WrapperAddress, DownloadsDir: a.dirs.Downloads(a.settings)}
		if path, _, err := ffmpeg.Find(a.settings.FFmpeg, a.dirs.Resources); err == nil {
			b.FFmpeg = path
		} else {
			a.log.Warn("no ffmpeg; downloads will fail at remux", "error", err)
		}
		go func() {
			if old != nil {
				a.stopBackend(old)
			}
			r, err := b.Start()
			a.post(func() {
				if gen != a.gen || a.quitting {
					if r != nil {
						go a.stopBackend(r)
					}
					return
				}
				a.starting = false
				if err != nil {
					a.startErr = err.Error()
					a.log.Error("embedded backend did not start", "error", err)
					return
				}
				a.backend = r
				c, err := client.New(r.Base)
				if err != nil {
					a.startErr = err.Error()
					return
				}
				a.store.Connect(c)
			})
		}()
	default:
		if old != nil {
			go a.stopBackend(old)
		}
	}
}

func (a *App) stopBackend(r *mode.Running) {
	if err := r.Stop(); err != nil && err != context.Canceled {
		a.log.Warn("embedded backend stopped with an error", "error", err)
	}
}

func (a *App) quit() {
	if a.quitting {
		return
	}
	a.quitting = true
	if a.store != nil {
		a.store.Close()
	}
	if a.shell != nil {
		a.shell.Close()
	}
	if a.backend != nil {
		// The backend saves its state and stops its workers; unfinished
		// jobs resume on the next launch.
		a.stopBackend(a.backend)
	}
}
