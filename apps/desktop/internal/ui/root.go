package ui

import (
	"runtime"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// shortcutLabel names the field's shortcut as the platform writes it.
var shortcutLabel = map[bool]string{true: "⌘L", false: "Ctrl L"}[runtime.GOOS == "darwin"]

// The main window's columns, in DIPs.
const (
	// sidebarWidth is the sidebar's width.
	sidebarWidth = 232
	// compactContent is the page width below which the pages use their
	// compact layout (the 820×560 board with the sidebar open).
	compactContent = 768
	// sidebarFits is the narrowest window the sidebar stays open in by
	// itself; a narrower one closes it until the user opens it there.
	sidebarFits = sidebarWidth + compactContent
	// sidebarSlide is how long the sidebar takes to open or close.
	sidebarSlide = 200 * time.Millisecond
)

// View builds the main window.
func (a *App) View(c *ui.Context) {
	k := a.tokens(c)
	applyTheme(c, k)
	a.shortcuts(c)
	a.set.login.settle(a.Store.Login.Busy)
	a.su.login.settle(a.Store.Login.Busy)
	w, _ := c.Size()
	a.followWidth(w)
	// The setup screen has the window to itself; the sidebar keeps the
	// user's choice for the screens after it.
	inSetup := a.Screen == ScreenSetup
	shown := a.SidebarShown() && !inSetup
	target := float32(0)
	if shown {
		target = sidebarWidth
	}
	a.compact = w-target < compactContent
	vib := c.Vibrancy()
	root := ui.Box(c).Fill()
	if !inSetup {
		a.linkTarget(c, root)
	}
	// The sidebar's width as it slides; the page takes the rest.
	side := root.AnimateWith("sidebar", target, sidebarSlide, ui.EaseOut)
	page := w - side
	a.flightQ(c, root)
	root.Children(func() {
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
			a.sidebar(c, k, vib, side)
			main := ui.Box(c).Grow(1).Shrink(1).MinWidth(0).Background(k.Ground).Clip()
			main.Children(func() {
				// Absolute children paint above the flow, in the order they
				// are built, so the content is a layer of its own over the
				// backdrop.
				a.backdrop(c, k)
				ui.Column(c.Key("content")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() {
					if inSetup {
						a.setupScreen(c, k)
						return
					}
					a.titleBar(c, k, shown)
					ui.Box(c).Grow(1).MinHeight(0).Children(func() { a.screens(c, k, page) })
				})
				// Home's field is where links land there; elsewhere a card
				// over the page shows it.
				if a.linkOver && a.Screen != ScreenHome {
					a.dropZone(c, k)
				}
			})
		})
		if !inSetup {
			a.sidebarToggle(c, k)
		}
	})
	a.deleteDialog(c, k)
	a.downloadSheet(c, k)
	a.hookDialog(c, k)
	a.toast(c, k)
	a.scrollbars(c, k)
	// Last: SetTheme, which scrollbars calls, gives the root the theme's
	// ground, which would cover the window's material under the sidebar.
	if vib {
		c.Root().Background(ui.Transparent)
	} else {
		c.Root().Background(k.Ground)
	}
}

// shortcuts are the window's keys: Cmd+1 home, Cmd+2 the queue, Cmd+,
// settings, Cmd+L home's field from any screen, and the sidebar's, which
// the View menu holds on macOS (sidebarKey).
func (a *App) shortcuts(c *ui.Context) {
	k := &a.keys
	if k.home == nil {
		// Made once: bound here every frame, they would be allocated every
		// frame.
		k.toggle, k.field = a.ToggleSidebar, a.FocusField
		k.home = func() { a.Go(ScreenHome) }
		k.queue = func() { a.Go(ScreenQueue) }
		k.settings = func() { a.Go(ScreenSettings) }
	}
	if runtime.GOOS != "darwin" {
		c.OnShortcut(sidebarMods, ui.KeyS, k.toggle)
	}
	c.OnShortcut(ui.Cmd, ui.KeyL, k.field)
	c.OnShortcut(ui.Cmd, ui.Key1, k.home)
	c.OnShortcut(ui.Cmd, ui.Key2, k.queue)
	c.OnShortcut(ui.Cmd, ui.KeyComma, k.settings)
	switch a.Screen {
	case ScreenAlbum:
		if c.Shortcut(0, ui.KeyEscape) {
			a.Back()
		}
	case ScreenHome:
		a.homeKeys(c)
	}
}

// titleBar is the content's top strip, which drags the window, clear of
// the window controls; with the sidebar closed it keeps the traffic lights
// and the sidebar button (sidebarToggle) on the content's own ground.
func (a *App) titleBar(c *ui.Context, k *Tokens, sidebar bool) {
	bar := c.TitleBar()
	right := float32(18)
	if bar.Right > 0 {
		right = bar.Right + 8
	}
	left := float32(18)
	if !sidebar {
		left = toggleRight(bar) + 10
	}
	ui.Row(c).Height(max(52, bar.Height)).PaddingStart(left).PaddingEnd(right).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
		// Back and the page's buttons come and go with the page, Back
		// naming where it went.
		in := a.Screen == ScreenAlbum
		if in {
			a.fly.back = a.backLabel()
		} else if !a.fly.moving {
			return
		}
		b := button(c, k, buttonGhost, a.fly.back, icBack)
		a.flyFade(b)
		if b.Clicked() && in {
			a.Back()
		}
		ui.Spacer(c)
		acts := ui.Row(c).Gap(8).AlignItems(ui.Center)
		a.flyFade(acts)
		acts.Children(func() { a.titleActions(c, k, in) })
	})
}

// toast shows the store's latest notice for a few seconds.
func (a *App) toast(c *ui.Context, k *Tokens) {
	n := a.Store.Notice
	ic, col := icCheck, k.OK
	if n.Error {
		ic, col = icCross, k.Bad
	}
	switch age := c.Now().Sub(n.At); {
	case waited(c, a.dropping != "", a.dropSince):
		n.Text, ic, col = "Looking up the link…", icLink, k.Accent
	case n.Text == "" || age > 4*time.Second:
		return
	default:
		c.After(4*time.Second - age)
	}
	// Over home's dock, the toast rises above it.
	bottom := float32(24)
	if a.dockShown() {
		bottom = dockBottom + dockHeight + 12
	}
	ui.Overlay(c, func() {
		ui.Row(c).Attach(ui.AnchorBottom, ui.AnchorBottom).Bottom(bottom).
			PaddingX(14).PaddingY(9).Gap(8).Radius(radiusFull).AlignItems(ui.Center).
			Background(k.Pill).Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.35)).Children(func() {
			ui.Icon(c, ic).FontSize(13).TextColor(col.Mix(k.PillInk, 0.2))
			ui.Text(c, n.Text).FontSize(12.5).FontWeight(600).TextColor(k.PillInk)
		})
	})
}

// jobPalette is the palette of the asset a job shows: its motion cover's
// while that plays, else the still cover's. Pairing a motion asset with the
// still's palette puts dark text on a dark video (domain.Job).
func jobPalette(j *store.Job, motion bool) Palette {
	if motion && j.MotionArtworkURL != "" && j.MotionArtworkBgColor != "" {
		return ParsePalette(j.MotionArtworkBgColor, j.MotionArtworkTextColor1, j.MotionArtworkTextColor2, j.MotionArtworkTextColor3, j.MotionArtworkTextColor4)
	}
	return ParsePalette(j.ArtworkBgColor, j.ArtworkTextColor1, j.ArtworkTextColor2, j.ArtworkTextColor3, j.ArtworkTextColor4)
}
