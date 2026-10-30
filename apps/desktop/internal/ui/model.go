// Package ui builds the desktop app's screens with MyGo's native UI, to the
// design canvas: the queue with the album-lit "downloading now" hero, the job
// detail, the new-download preview, the library, settings, system, the menu
// bar panel, and their empty, loading and error states, dark and light.
package ui

import (
	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/artwork"
	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/store"
)

// Screen is a page of the main window.
type Screen int

// Screens.
const (
	ScreenQueue Screen = iota
	ScreenLibrary
	ScreenSystem
	ScreenSettings
	ScreenDetail
	ScreenNew
)

// Actions are what the screens ask of the app around them.
type Actions interface {
	// ApplySettings saves the settings and, when the mode or its address
	// changed, connects again.
	ApplySettings(s mode.Settings)
	// TestServer and TestWrapper run a connection test and hand its checks
	// back on the UI thread.
	TestServer(address string, done func([]mode.Check))
	TestWrapper(address string, done func([]mode.Check))
	// Reveal shows a file or folder of the backend's machine in the file
	// manager, when it is this computer.
	Reveal(path string)
	// CopyText puts text on the clipboard.
	CopyText(s string)
	// Starting reports a standalone backend that is starting, and StartErr
	// why the last start failed.
	Starting() bool
	StartErr() string
}

// App is the main window's view and the state of its screens. Use it on the
// UI thread.
type App struct {
	Store    *store.Store
	Art      *artwork.Cache
	Settings mode.Settings
	Dirs     mode.Dirs
	Act      Actions
	// Theme overrides the system appearance for screenshots: "light",
	// "dark", or "" to follow the settings.
	Theme string

	Screen Screen
	// Tab is the queue's filter.
	Tab int

	queue   ui.ListState
	omni    ui.Handle
	omniTxt string
	set     settingsForm
}

// tokens returns the colors of the current appearance.
func (a *App) tokens(c *ui.Context) *Tokens {
	dark := c.Theme().Dark
	switch {
	case a.Theme == "light":
		dark = false
	case a.Theme == "dark":
		dark = true
	case a.Settings.Theme == "light":
		dark = false
	case a.Settings.Theme == "dark":
		dark = true
	}
	if dark {
		return &DarkTokens
	}
	return &LightTokens
}

// Go switches the main window to a screen.
func (a *App) Go(s Screen) {
	if s != ScreenDetail {
		a.Store.CloseDetail()
	}
	a.Screen = s
}

// OpenJob shows a job's detail.
func (a *App) OpenJob(id string) {
	a.Store.OpenDetail(id)
	a.Screen = ScreenDetail
}

// FocusOmni puts the keyboard in the title bar's field.
func (a *App) FocusOmni() { a.omni.Focus() }
