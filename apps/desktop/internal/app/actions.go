package app

import (
	"context"

	"github.com/egoist/mygo"

	"amdl/apps/desktop/internal/mode"
)

// The methods below implement ui.Actions.

// ApplySettings saves the settings and connects again when where downloads
// run changed.
func (a *App) ApplySettings(s mode.Settings) {
	prev := a.settings
	a.settings = s
	a.view.Settings = s
	if err := mode.SaveSettings(a.dirs.Data, s); err != nil {
		a.log.Error("settings not saved", "error", err)
	}
	if s.Mode != prev.Mode || s.RemoteURL != prev.RemoteURL || s.WrapperAddress != prev.WrapperAddress ||
		(s.Mode == mode.Standalone && (s.DownloadsDir != prev.DownloadsDir || s.FFmpeg != prev.FFmpeg)) {
		a.connect()
	}
	a.shell.Sync(a.store.Summary())
}

// TestServer tests a remote backend's address off the UI thread.
func (a *App) TestServer(address string, done func([]mode.Check)) {
	go func() {
		checks := mode.TestServer(context.Background(), address)
		a.post(func() { done(checks) })
	}()
}

// TestWrapper tests a wrapper address off the UI thread.
func (a *App) TestWrapper(address string, done func([]mode.Check)) {
	go func() {
		checks := mode.TestWrapper(context.Background(), address)
		a.post(func() { done(checks) })
	}()
}

// Reveal shows a path of this computer in the file manager.
func (a *App) Reveal(path string) { mygo.Shell.ShowItemInFolder(path) }

// CopyText puts text on the clipboard.
func (a *App) CopyText(s string) { mygo.Clipboard.WriteText(s) }

// Starting reports an embedded backend that is starting.
func (a *App) Starting() bool { return a.starting }

// StartErr is why the backend could not be reached or started.
func (a *App) StartErr() string { return a.startErr }
