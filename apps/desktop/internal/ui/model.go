// Package ui builds the desktop app's screens with MyGo's native UI, to the
// design canvas: home with its one field for links and catalog search, the
// queue with the album-lit "downloading now" hero, the album page, settings,
// system, the menu bar panel, and their empty, loading and error states, dark
// and light.
package ui

import (
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/artwork"
	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/motion"
	"amdl/apps/desktop/internal/setup"
	"amdl/apps/desktop/internal/store"
)

// Screen is a page of the main window.
type Screen int

// Screens. Home, the zero Screen, is the one the window opens on.
const (
	ScreenHome Screen = iota
	ScreenQueue
	ScreenSettings
	// ScreenAlbum is the one page of an album and of a download (OpenAlbum,
	// OpenJob).
	ScreenAlbum
	// ScreenSetup checks and installs what a mode needs (OpenSetup).
	ScreenSetup
	// ScreenLibrary is amdl's records and the Apple Music library.
	ScreenLibrary
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
	// ChooseFolder asks for a folder of this computer, starting at start,
	// and hands it to done on the UI thread unless the user cancels.
	ChooseFolder(start string, done func(dir string))
	// ShowWindow brings the main window forward, closing the tray panel.
	ShowWindow()
	// FitPanel sizes the tray panel to its content's height in DIPs.
	FitPanel(height float32)
	// Starting reports a standalone backend that is starting, and StartErr
	// why the last start failed.
	Starting() bool
	StartErr() string
	// Shared names the backend another program on this computer runs, such
	// as amdl service, which standalone mode uses instead of its own; "" when
	// the app runs its own.
	Shared() string
	// Reconnect connects to the backend the settings name again, restarting
	// the one standalone mode runs, so that it picks up a guest or a tool
	// installed since it started.
	Reconnect()
	// ChooseFile asks for a file of this computer and hands it to done on
	// the UI thread unless the user cancels.
	ChooseFile(title string, done func(path string))
	// OpenURL opens a web page in the browser.
	OpenURL(url string)
}

// App is the main window's view and the state of its screens. Use it on the
// UI thread.
type App struct {
	Store *store.Store
	Art   *artwork.Cache
	// Motion plays motion covers; nil shows the stills only.
	Motion   *motion.Cache
	Settings mode.Settings
	Dirs     mode.Dirs
	Act      Actions
	// Setup checks and installs what standalone mode needs here.
	Setup *setup.Setup
	// Theme overrides the system appearance for screenshots: "light",
	// "dark", or "" to follow the settings.
	Theme string
	// NoMotion shows stills in place of motion covers, for a render of the
	// view beside its window.
	NoMotion bool
	// Opaque is set while the desktop asks for opaque surfaces (Reduce
	// Transparency, Windows's transparency effects off): the sidebar draws
	// its own ground over the window's material.
	Opaque bool
	// Demo is set while the app runs the demo (package demo): there is no
	// backend of this computer to offer.
	Demo bool
	// PageMotion moves the album page in and out over the screen it opens
	// from (flight.go), and a search's groups between its tabs
	// (groupMotion); without it the screens change at once, as tests need,
	// whose clock stands still between Advances.
	PageMotion bool
	// Capture renders the window as it shows, as PNG, for the tuning
	// panel's film (tuneFilm); nil where there is no window.
	Capture func() ([]byte, error)
	// EdgeFades fades the scroll containers' edges (scrollArea) through a
	// shader, which a GPU draws in no time and a headless Tester runs on
	// the CPU many times slower than the rest of a frame: tests that look
	// at the edges set it.
	EdgeFades bool

	Screen Screen
	// trail holds the screens Back returns to, the last one on top: the
	// album screen pushes the one it was opened from.
	trail []Screen
	// pageKey is the album and download the album screen last opened on,
	// and albumSeen the album's download as it was then (followAlbumJob).
	pageKey   string
	albumSeen string
	// fly is the album page's way in and out.
	fly flight
	// tune is how the album page grows, which the tuning panel changes.
	tune motionTune
	// Tab is the queue's filter, tabShown the one its list last read for,
	// and tabAt when the list last changed filter (switchMotion).
	Tab      int
	tabShown int
	tabAt    time.Time

	// winW is the window's width as last built; compact is set while the
	// page is narrower than compactContent. openNarrow is set while the
	// user keeps the sidebar open in a window too narrow for it.
	winW       float32
	compact    bool
	openNarrow bool

	// ScrollbarsAlways shows the scroll thumbs at all times, as the desktop
	// does where its setting says so; otherwise they show while a list
	// moves (scrollbars).
	ScrollbarsAlways bool
	// scrolls are the places of the scroll containers by key, scrollSeen
	// where each was last built, and scrolledAt when one last moved.
	scrolls    map[string]*ui.ScrollState
	scrollSeen map[string]float32
	scrolledAt time.Time

	queue ui.ListState
	lib   libraryForm
	// field is home's field, and fieldTxt its text, which follows the
	// store's Omni.Text.
	field    ui.Handle
	fieldTxt string
	// fieldDrawn is the height the field was dragged to, in DIPs, which it
	// keeps whatever its text until it empties; 0 while it fits its text.
	fieldDrawn float32
	// docked is whether home's field was at the top as home last built it,
	// and dockedAt when it last went there.
	docked   bool
	dockedAt time.Time
	// focusField asks home to put the keyboard in its field as it builds
	// it, in the view that does: a render beside the window binds the
	// field too, which an unaddressed Handle.Focus refuses.
	focusField bool
	set        settingsForm
	su         setupForm
	// searchSel is the HitKey of the hit the search acts on; searchReveal
	// scrolls it into view once, after the keyboard moved it. searchKeyed
	// is set while the keys chose it, which rings it: one chosen otherwise,
	// as the album Back returns to, is only where the keys go on from.
	searchSel    string
	searchReveal bool
	searchKeyed  bool
	// keys are the window shortcuts' actions (shortcuts).
	keys struct{ toggle, field, home, queue, library, settings, tune func() }
	// groupsAt is when the search last changed tab, which groupsMoved asks
	// the next frame to note (tabChange).
	groupsAt time.Time
	// groupsShown is the tab the search's last build asked for, and
	// groupsLast the one the frame before showed, which the frame at
	// groupsFrame shows (searchBody).
	groupsShown, groupsLast string
	groupsFrame             time.Time
	groupsMoved             bool
	// groupsMoreAt is when each group shown alone began to read its next
	// page (groupMore).
	groupsMoreAt map[string]time.Time
	// viewArtist is the artist whose albums the search shows, "" for the
	// results, since viewAt; viewScroll waits for the view going out to
	// fade before the search scrolls (followView).
	viewArtist string
	viewAt     time.Time
	viewScroll bool
	// texts keeps the words lists made, for the frames after (words).
	texts texts
	// confirmAll is the artist whose "Download all" waits for its second
	// click since confirmAt.
	confirmAll string
	confirmAt  time.Time
	// deleting is the job whose delete waits for its dialog's answer;
	// deleteOpen keeps the dialog up.
	deleting   *store.Job
	deleteOpen bool
	// removing is the record whose removal waits for its dialog's answer;
	// removeOpen keeps the dialog up.
	removing   *store.Record
	removeOpen bool
	// asking is the download whose sheet is up, or nil.
	asking *downloadAsk
	// qd is a queued job dragged through Up next.
	qd queueDrag
	// linkOver is set while links dragged from another app are over the
	// window; dropping is the dropped link being looked up since
	// dropSince.
	linkOver  bool
	dropping  string
	dropSince time.Time
	// hookAsk is the hook sheet that is up, or nil.
	hookAsk *hookSheet
	// nowShown is the job the sidebar's now card shows, kept while the
	// card closes after it.
	nowShown *store.Job
	// panelH is the height the tray panel was last fitted to.
	panelH float32
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

// Go switches the main window to a screen, forgetting where Back would
// return. Home puts the keyboard in its field.
func (a *App) Go(s Screen) {
	if s != ScreenAlbum {
		a.Store.CloseDetail()
	}
	a.fly.instant = true
	a.trail = nil
	a.Screen = s
	if s != ScreenLibrary {
		a.Store.CloseLibrary()
	}
	if s == ScreenHome {
		a.focusField = true
	}
	if s == ScreenSettings && a.Settings.Mode == "" && a.set.pin == "" {
		// With nothing set up, Connection is where Settings opens; once
		// there, the page scrolls freely.
		a.showSection("connection")
	}
}

// push shows s, which Back leaves for the screen shown now. Setup is left
// behind: Back never returns to it.
func (a *App) push(s Screen) {
	if a.Screen != s && a.Screen != ScreenSetup {
		a.trail = append(a.trail, a.Screen)
	}
	a.Screen = s
}

// backTo is the screen Back returns to: the one the album screen was opened
// from, else the queue, as for a download opened from a notification over
// setup.
func (a *App) backTo() Screen {
	if n := len(a.trail); n > 0 {
		return a.trail[n-1]
	}
	return ScreenQueue
}

// navScreen is the sidebar's screen for the one shown: the album screen
// lights the screen it was opened from.
func (a *App) navScreen() Screen {
	if a.Screen == ScreenAlbum {
		return a.backTo()
	}
	return a.Screen
}

// Back returns to the screen the album screen was opened from: home keeps
// its words, results, artist and place, so the search comes back as it was
// left, with the keyboard in its field; the queue keeps its tab and place.
// The page goes out over it showing its download, which closes once the
// page is gone.
func (a *App) Back() {
	to := a.backTo()
	if n := len(a.trail); n > 0 {
		a.trail = a.trail[:n-1]
	}
	if a.PageMotion && a.Screen == ScreenAlbum {
		a.fly.under, a.fly.closeDetail = to, true
	} else {
		a.Store.CloseDetail()
	}
	if to == ScreenHome {
		if a.Screen == ScreenAlbum {
			a.revealAlbum()
		}
		a.focusField = true
	}
	a.Screen = to
}

// FocusField shows home and puts the keyboard in its field.
func (a *App) FocusField() { a.Go(ScreenHome) }

// PanelClosed forgets the tray panel's size, so the next one fits again.
func (a *App) PanelClosed() { a.panelH = 0 }
