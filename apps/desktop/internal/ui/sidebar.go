package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
	"amdl/internal/client"
	"amdl/internal/format"
)

// sidebar is the window's left column (Pulse, MyGo 云音乐): the traffic
// lights and the sidebar button over its top, the sections with bare
// icons and live readings, and at its foot the job downloading now over
// the connection, which opens Settings. It is w wide as it slides open or closed; its content keeps its
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
			ui.Column(c).Gap(2).Children(func() {
				a.navItem(c, k, ScreenHome, icHome, "Home", "")
				a.navItem(c, k, ScreenQueue, icQueue, "Queue", countText(a.Store.Count(store.TabActive)))
				if a.Store.AppleLibraryOn() {
					a.navItem(c, k, ScreenLibrary, icShelf, "Library", "")
				}
			})
			ui.Spacer(c)
			ui.Column(c).Children(func() {
				a.nowSlot(c, k)
				a.serverCard(c, k)
			})
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
	// With the sidebar closed its button carries the connection's dot,
	// while the connection is not live.
	st := a.serverState(k)
	trouble := !a.SidebarShown() && st.tone != toneOK
	if trouble {
		b.Tooltip(st.host + ": " + st.word)
	}
	b.Cursor(ui.CursorPointer).Children(func() {
		ui.Icon(c, icSidebar).FontSize(18).TextColor(k.Muted)
		if trouble {
			ui.Box(c).Size(7, 7).Radius(radiusFull).Background(st.fg).Border(1.5, k.Ground).Absolute().Top(3).Right(2)
		}
	})
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

// navItem is a section of the sidebar: its icon bare, as Settings' own
// navigation draws them, and its reading.
func (a *App) navItem(c *ui.Context, k *Tokens, s Screen, ic *ui.SVG, name, reading string) {
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
		col := k.Muted
		if on {
			col = k.Fg
		}
		ui.Box(c).Size(22, 22).Center().Children(func() {
			ui.Icon(c, ic).FontSize(14).TextColor(col)
		})
		ui.Text(c, name).FontSize(13).TextColor(k.Fg).Grow(1).SingleLine()
		if reading != "" {
			mono(c, reading, 11).FontWeight(500).TextColor(k.Muted)
		}
	})
}

// connTone is how the connection stands.
type connTone int

const (
	toneOK connTone = iota
	toneWait
	toneBad
)

// serverStatus is what the sidebar's connection card shows: where
// downloads run, whether that place answers, and whether an account must
// log in again; the rest of the accounts' state is left to Settings.
type serverStatus struct {
	icon       *ui.SVG
	host, word string
	tone       connTone
	fg, soft   ui.Color
}

func (a *App) serverState(k *Tokens) serverStatus {
	st := serverStatus{icon: icServer, word: "Live", tone: toneOK}
	switch a.Settings.Mode {
	case mode.Remote:
		st.host = hostOf(a.Settings.RemoteURL)
	case mode.Standalone:
		st.icon, st.host = icLaptop, "This computer"
		if a.Act.Shared() != "" {
			st.word = "Shared"
		}
	default:
		st.host, st.word, st.tone = "Not connected", "Set up", toneBad
	}
	switch {
	case st.tone == toneBad:
	case a.Act.StartErr() != "":
		st.word, st.tone = "Did not start", toneBad
	case a.Act.Starting():
		st.word, st.tone = "Starting…", toneWait
	case a.Store.Conn.Phase == client.Offline:
		st.word, st.tone = "Offline", toneBad
	case a.Store.Conn.Phase == client.Connecting:
		st.word, st.tone = "Connecting…", toneWait
	case slices.ContainsFunc(a.Store.Server.Accounts, func(acc client.Account) bool { return acc.NeedsLogin }):
		st.word, st.tone = "Account needs login", toneWait
	case a.Store.Server.Health != nil && a.Store.Server.Health.Status != "ok":
		st.word, st.tone = "Check", toneWait
	}
	st.fg, st.soft = k.OK, k.OKSoft
	switch st.tone {
	case toneWait:
		st.fg, st.soft = k.Warn, k.WarnSoft
	case toneBad:
		st.fg, st.soft = k.Bad, k.BadSoft
	}
	return st
}

// serverCard says where downloads run and whether that place answers; it
// opens Settings at the connection, and shows selected while Settings is
// up, as the sidebar's way there.
func (a *App) serverCard(c *ui.Context, k *Tokens) {
	st := a.serverState(k)
	card := ui.ButtonBase(c.Key("server")).Padding(9, 10).Gap(10).Radius(radiusControl).AlignItems(ui.Center).Justify(ui.Start).
		Label("Connection settings")
	rest := k.Wash
	if a.navScreen() == ScreenSettings {
		rest = k.Track
	}
	card.Background(sideFill(card, k, rest, k.Track))
	if card.Clicked() {
		a.GoSettings("connection")
	}
	card.Cursor(ui.CursorPointer).Children(func() {
		ui.Box(c).Size(30, 30).Radius(radiusInner).Background(st.soft).Center().Children(func() {
			ui.Icon(c, st.icon).FontSize(15).TextColor(st.fg)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, st.host).FontSize(12.5).FontWeight(600).SingleLine().Ellipsis("…")
			word := k.Muted
			if st.tone != toneOK {
				word = st.fg
			}
			mono(c, st.word, 10.5).TextColor(word).SingleLine()
		})
	})
}

func hostOf(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	return strings.TrimSuffix(u, "/")
}

// nowHeight is the height of the card of the job downloading now, and
// nowGap the room between it and the connection card under it: its slot
// opens and closes over both, upward into the empty room under the
// sections, so neither the sections nor the connection card move.
const nowHeight, nowGap = 56, 8

// nowSlot holds the small card of the job downloading now, at the
// sidebar's foot (MyGo 云音乐's now-playing card). It opens as a download
// starts and closes after the last one, showing the job it last showed
// while it closes.
func (a *App) nowSlot(c *ui.Context, k *Tokens) {
	j := a.Store.Now()
	if j != nil {
		a.nowShown = j
	}
	open := float32(0)
	if j != nil {
		open = 1
	}
	slot := ui.Column(c.Key("now-slot")).Clip()
	shown := slot.AnimateWith("open", open, sidebarSlide, ui.EaseOut)
	slot.Height((nowHeight + nowGap) * shown)
	if shown < 0.01 {
		a.nowShown = nil
		return
	}
	slot.Opacity(shown).Children(func() { a.nowCard(c, k, a.nowShown) })
}

func (a *App) nowCard(c *ui.Context, k *Tokens, j *store.Job) {
	pal := jobPalette(j, false)
	accent := AccentFor(pal, k)
	card := ui.ButtonBase(c.Key("now")).Height(nowHeight).Padding(8).Gap(10).Radius(radiusControl).AlignItems(ui.Center).Justify(ui.Start).Label("Downloading now")
	card.Background(sideFill(card, k, k.Wash, k.Track))
	a.flySourceCard(card, "now:"+j.ID, radiusControl)
	if card.Clicked() {
		a.openingFrom("now:" + j.ID)
		a.OpenJob(j.ID)
	}
	card.Cursor(ui.CursorPointer).Children(func() {
		cover := widgets.Cover{Image: a.Art.Get(j.ArtworkURL, 80), Placeholder: pal.Bg, Size: 40, Radius: radiusInner, Color: 1}.Show(c)
		a.flySourceCoverShown(cover, "now:"+j.ID, radiusInner)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, j.Name()).FontSize(12).FontWeight(600).Ellipsis("…").SingleLine()
			line := fmt.Sprintf("%d of %d", j.Done(), j.TotalItems)
			if sp := j.ShownSpeed(c.Now()); sp > 0 {
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
