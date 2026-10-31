package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/client"
	"amdl/internal/format"
)

// sidebar is the window's left column (Pulse, MyGo 云音乐): the traffic
// lights and the sidebar button over its top, the connection, the sections
// with colored tiles and live readings, and the job downloading now at its
// foot. It is w wide as it slides open or closed; its content keeps its
// width meanwhile, clipped, so nothing in it reflows.
//
// Where the window shows its material (vib), the sidebar paints no ground
// of its own, as MyGo 云音乐's: the system's sidebar material, which
// follows the appearance and blurs the desktop behind the window, is the
// ground, and what sits on it fills with neutral washes (Wash, Track)
// that darken or lighten it rather than cover it. Secondary text is Muted,
// not Faint, to read over the material's darkest and lightest.
func (a *App) sidebar(c *ui.Context, k *Tokens, vib bool, w float32) {
	if w < 0.5 {
		return
	}
	ui.Column(c.Key("sidebar")).Width(w).Clip().Children(func() {
		col := ui.Column(c).Width(sidebarWidth).Grow(1).Padding(0, 10, 12, 10).Gap(14).BorderWidth(0, 1, 0, 0).BorderColor(k.Line)
		if !vib || a.Opaque {
			col.Background(k.Ground.Mix(k.Raise, 0.55))
		}
		col.Children(func() {
			// The band of the traffic lights and the sidebar button
			// (sidebarToggle), which drags the window.
			ui.Box(c).Height(controlsBand(c.TitleBar())).Margin(0, -10, -6, -10).DragWindow()
			a.modeCard(c, k)
			ui.Column(c).Gap(2).Children(func() {
				a.navItem(c, k, ScreenHome, icHome, k.TileHome, "Home", "", ui.Color{})
				a.navItem(c, k, ScreenQueue, icQueue, k.TileQueue, "Queue", countText(a.Store.Count(0)), ui.Color{})
				ui.Text(c, a.serverHeading()).FontSize(11).FontWeight(600).TextColor(k.Muted).Padding(8, 10, 4, 10)
				reading, col := a.systemReading(k)
				a.navItem(c, k, ScreenSettings, icSettings, k.TileSettings, "Settings", reading, col)
			})
			ui.Spacer(c)
			a.nowCard(c, k)
		})
	})
}

// sidebarMods with S open and close the sidebar where no View menu holds
// the key: Ctrl+Shift+S. On macOS the menu's Control-Command-S does.
var sidebarMods = ui.Ctrl | ui.Shift

// controlsBand is the height of the band the window controls sit in, and
// the sidebar button beside them: the title bar's band, or the content
// title bar's height where the controls hide (full screen) or draw none.
func controlsBand(bar ui.TitleBar) float32 {
	if bar.Height > 0 {
		return bar.Height
	}
	return 52
}

// toggleRight is where the sidebar button ends, from the window's left:
// beside the traffic lights, or in from the edge where there are none.
func toggleRight(bar ui.TitleBar) float32 {
	return toggleLeft(bar) + toggleSize
}

func toggleLeft(bar ui.TitleBar) float32 {
	if bar.Left > 0 {
		return bar.Left + 4
	}
	return 12
}

// toggleSize is the sidebar button's side.
const toggleSize = 28

// sidebarToggle is the sidebar button. It floats over the window's top-left
// corner beside the traffic lights, so it stays put while the sidebar
// opens and closes under it, as the system apps' does.
func (a *App) sidebarToggle(c *ui.Context, k *Tokens) {
	bar := c.TitleBar()
	label := "Hide the sidebar"
	if !a.SidebarShown() {
		label = "Show the sidebar"
	}
	top := (controlsBand(bar) - toggleSize) / 2
	b := ui.ButtonBase(c.Key("sidebar-toggle")).Size(toggleSize, toggleSize).Radius(radiusControl).Center().
		Absolute().Left(toggleLeft(bar)).Top(top).Label(label).Tooltip(label)
	switch {
	case b.Pressed():
		b.Background(k.Track.Mix(k.Fg, 0.08))
	case b.Hovered():
		b.Background(k.Track)
	}
	if b.Clicked() {
		a.ToggleSidebar()
	}
	b.Cursor(ui.CursorPointer).Children(func() { ui.Icon(c, icSidebar).FontSize(18).TextColor(k.Muted) })
}

// followWidth keeps the window's width, and lets the sidebar close by
// itself as the window narrows past sidebarFits: an opening the user asked
// for in a narrow window lasts until the window is wide again.
func (a *App) followWidth(w float32) {
	a.winW = w
	if w >= sidebarFits {
		a.openNarrow = false
	}
}

// SidebarShown reports whether the sidebar is open: as the user last left
// it, unless the window is too narrow for it and the user has not opened it
// there.
func (a *App) SidebarShown() bool {
	if a.Settings.HideSidebar {
		return false
	}
	return a.winW >= sidebarFits || a.openNarrow
}

// ToggleSidebar opens or closes the sidebar, and keeps the choice for the
// next launch.
func (a *App) ToggleSidebar() {
	if a.SidebarShown() {
		a.Settings.HideSidebar, a.openNarrow = true, false
	} else {
		a.Settings.HideSidebar = false
		a.openNarrow = a.winW < sidebarFits
	}
	a.Act.ApplySettings(a.Settings)
}

func countText(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprint(n)
}

func (a *App) serverHeading() string {
	if a.Settings.Mode == mode.Standalone {
		return "This computer"
	}
	return "Server"
}

// systemReading is the Settings item's live value: OK, or what is wrong.
func (a *App) systemReading(k *Tokens) (string, ui.Color) {
	switch a.Store.Conn.Phase {
	case client.Offline:
		return "Offline", k.Bad
	case client.Connecting:
		return "…", k.Muted
	}
	if h := a.Store.Server.Health; h != nil && h.Status != "ok" {
		return "Check", k.Warn
	}
	return "OK", ui.Color{}
}

func (a *App) navItem(c *ui.Context, k *Tokens, s Screen, ic *ui.SVG, tile ui.Color, name, reading string, readingColor ui.Color) {
	on := a.navScreen() == s
	item := ui.ButtonBase(c.Key(name)).Height(30).PaddingX(10).Gap(10).Radius(radiusControl).AlignItems(ui.Center).Justify(ui.Start).Label(name)
	// Under the pointer a row takes the faint wash, selected the stronger
	// one, so the two never look alike.
	rest, hover := ui.Transparent, k.Wash
	if on {
		rest, hover = k.Track, k.Track
	}
	item.Background(sideFill(item, k, rest, hover))
	if item.Clicked() {
		a.Go(s)
	}
	item.Cursor(ui.CursorPointer).Children(func() {
		ui.Box(c).Size(22, 22).Radius(radiusInner).Background(tile).Center().Children(func() {
			ui.Icon(c, ic).FontSize(13).TextColor(ui.RGB(255, 255, 255))
		})
		ui.Text(c, name).FontSize(13).TextColor(k.Fg).Grow(1).SingleLine()
		if reading != "" {
			col := k.Muted
			w := 500
			if readingColor.A != 0 {
				col, w = readingColor, 600
			}
			mono(c, reading, 11).FontWeight(w).TextColor(col)
		}
	})
}

// modeCard says where downloads run and whether that place answers.
func (a *App) modeCard(c *ui.Context, k *Tokens) {
	title, sub := a.modeText()
	dot, ring := k.OK, k.OKSoft
	switch {
	case a.Act.StartErr() != "" || a.Store.Conn.Phase == client.Offline:
		dot, ring = k.Bad, k.BadSoft
	case a.Act.Starting() || a.Store.Conn.Phase == client.Connecting:
		dot, ring = k.Warn, k.WarnSoft
	}
	card := ui.ButtonBase(c.Key("mode")).Padding(8, 10).Gap(10).Radius(radiusControl).AlignItems(ui.Center).Justify(ui.Start).
		Label("Connection settings")
	card.Background(sideFill(card, k, k.Wash, k.Track))
	if card.Clicked() {
		a.Go(ScreenSettings)
	}
	card.Cursor(ui.CursorPointer).Children(func() {
		ui.Box(c).Size(8, 8).Radius(radiusFull).Background(dot).Shadow(0, 0, 0, 3, ring)
		ui.Column(c).Gap(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, title).FontSize(12.5).FontWeight(600).SingleLine()
			mono(c, sub, 11).TextColor(k.Muted).Ellipsis("…")
		})
	})
}

func (a *App) modeText() (title, sub string) {
	switch a.Settings.Mode {
	case mode.Remote:
		return "Server · remote", hostOf(a.Settings.RemoteURL)
	case mode.Standalone:
		if shared := a.Act.Shared(); shared != "" {
			return "This computer · shared", shared
		}
		return "This computer", "wrapper " + hostOf(a.Settings.WrapperAddress)
	}
	return "Not connected", "choose where downloads run"
}

func hostOf(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	return strings.TrimSuffix(u, "/")
}

// nowCard is the small card of the job downloading now, at the sidebar's
// foot (MyGo 云音乐's now-playing card).
func (a *App) nowCard(c *ui.Context, k *Tokens) {
	j := a.Store.Now()
	if j == nil {
		return
	}
	pal := jobPalette(j, false)
	accent := AccentFor(pal, k)
	card := ui.ButtonBase(c.Key("now")).Padding(8).Gap(10).Radius(radiusControl).AlignItems(ui.Center).Justify(ui.Start).Label("Downloading now")
	card.Background(sideFill(card, k, k.Wash, k.Track))
	if card.Clicked() {
		a.OpenJob(j.ID)
	}
	card.Cursor(ui.CursorPointer).Children(func() {
		widgets.Cover{Image: a.Art.Get(j.ArtworkURL, 80), Placeholder: pal.Bg, Size: 40, Radius: radiusInner, Color: 0.25 + 0.75*float32(j.Progress)}.Show(c)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, j.Name()).FontSize(12).FontWeight(600).Ellipsis("…").SingleLine()
			line := fmt.Sprintf("%d of %d", j.Done(), j.TotalItems)
			if sp := j.AvgSpeed(c.Now()); sp > 0 {
				line += " · " + format.Rate(sp)
			}
			mono(c, line, 10.5).TextColor(k.Muted).Margin(0, 0, 4, 0)
			widgets.StageBar{Stages: j.Stages, Color: accent, Track: k.Track, Height: 3, Running: true, Still: a.scrolling(c)}.Show(c, "now-bar")
		})
	})
}

// sideFill is the fill of a row or a card in the sidebar: rest, hover
// under the pointer and a stronger wash while pressed, each a translucent
// neutral that reads over the window's material.
func sideFill(b ui.Element, k *Tokens, rest, hover ui.Color) ui.Color {
	switch {
	case b.Pressed():
		return k.Track.Mix(k.Fg, 0.08)
	case b.Hovered():
		return hover
	}
	return rest
}
