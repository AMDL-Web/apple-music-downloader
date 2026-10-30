package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/client"
	"amdl/apps/desktop/internal/format"
	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/widgets"
)

// sidebar is the window's left column (Pulse, MyGo 云音乐): the connection,
// the sections with colored tiles and live readings, and the job downloading
// now at its foot.
func (a *App) sidebar(c *ui.Context, k *Tokens, vib bool) {
	side := ui.Column(c).Width(232).Padding(12, 10).Gap(14).BorderWidth(0, 1, 0, 0).BorderColor(k.Line)
	if vib {
		side.Background(k.Glass)
	} else {
		side.Background(k.Ground.Mix(k.Raise, 0.55))
	}
	side.Children(func() {
		// Room for the traffic lights, which the window draws over the
		// sidebar on macOS.
		if bar := c.TitleBar(); bar.Left > 0 {
			ui.Box(c).Height(max(bar.Height-12, 18)).DragWindow()
		} else {
			ui.Box(c).Height(6)
		}
		a.modeCard(c, k)
		ui.Column(c).Gap(2).Children(func() {
			a.navItem(c, k, ScreenQueue, icQueue, k.TileQueue, "Queue", countText(a.Store.Count(0)), ui.Color{})
			a.navItem(c, k, ScreenLibrary, icLibrary, k.TileLibrary, "Library", countText(a.Store.Counts.Completed), ui.Color{})
			ui.Text(c, a.serverHeading()).FontSize(11).FontWeight(600).TextColor(k.Faint).Padding(8, 10, 4, 10)
			reading, col := a.systemReading(k)
			a.navItem(c, k, ScreenSystem, icSystem, k.TileSystem, "System", reading, col)
			a.navItem(c, k, ScreenSettings, icSettings, k.TileSettings, "Settings", "", ui.Color{})
		})
		ui.Spacer(c)
		a.nowCard(c, k)
	})
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

// systemReading is the System item's live value: OK, or what is wrong.
func (a *App) systemReading(k *Tokens) (string, ui.Color) {
	switch a.Store.Conn.Phase {
	case client.Offline:
		return "Offline", k.Bad
	case client.Connecting:
		return "…", k.Muted
	}
	return "OK", ui.Color{}
}

func (a *App) navItem(c *ui.Context, k *Tokens, s Screen, ic *ui.SVG, tile ui.Color, name, reading string, readingColor ui.Color) {
	on := a.Screen == s || (s == ScreenQueue && (a.Screen == ScreenDetail || a.Screen == ScreenNew))
	item := ui.ButtonBase(c.Key(name)).Height(30).PaddingX(10).Gap(10).Radius(7).AlignItems(ui.Center).Justify(ui.Start).Label(name)
	if on {
		item.Background(k.AccentSoft)
	} else if item.Hovered() {
		item.Background(k.Track)
	}
	if item.Clicked() {
		a.Go(s)
	}
	item.Cursor(ui.CursorPointer).Children(func() {
		ui.Box(c).Size(22, 22).Radius(6).Background(tile).Center().Children(func() {
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
	card := ui.ButtonBase(c.Key("mode")).Padding(8, 10).Gap(10).Radius(10).AlignItems(ui.Center).Justify(ui.Start).
		Background(k.Raise2).Border(1, k.Line).Label("Connection settings")
	if card.Clicked() {
		a.Go(ScreenSettings)
	}
	card.Cursor(ui.CursorPointer).Children(func() {
		ui.Box(c).Size(8, 8).Radius(999).Background(dot).Shadow(0, 0, 0, 3, ring)
		ui.Column(c).Gap(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, title).FontSize(12.5).FontWeight(600).SingleLine()
			mono(c, sub, 11).TextColor(k.Faint).Ellipsis("…")
		})
	})
}

func (a *App) modeText() (title, sub string) {
	switch a.Settings.Mode {
	case mode.Remote:
		return "Server · remote", hostOf(a.Settings.RemoteURL)
	case mode.Standalone:
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
	card := ui.ButtonBase(c.Key("now")).Padding(10).Gap(10).Radius(12).AlignItems(ui.Center).Justify(ui.Start).Background(k.Raise2).Label("Downloading now")
	if card.Clicked() {
		a.OpenJob(j.ID)
	}
	card.Cursor(ui.CursorPointer).Children(func() {
		widgets.Cover{Image: a.Art.Get(j.ArtworkURL, 80), Placeholder: pal.Bg, Size: 40, Radius: 7, Color: 0.25 + 0.75*float32(j.Progress)}.Show(c)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, j.Name()).FontSize(12).FontWeight(600).Ellipsis("…").SingleLine()
			line := fmt.Sprintf("%d of %d", j.Done(), j.TotalItems)
			if sp := j.AvgSpeed(c.Now()); sp > 0 {
				line += " · " + format.Rate(sp)
			}
			mono(c, line, 10.5).TextColor(k.Muted).Margin(0, 0, 4, 0)
			widgets.StageBar{Stages: j.Stages, Color: accent, Track: k.Track, Height: 3, Running: true}.Show(c, "now-bar")
		})
	})
}

// rail is the sidebar folded into icons, below compactWidth.
func (a *App) rail(c *ui.Context, k *Tokens, vib bool) {
	r := ui.Column(c).Width(60).PaddingY(12).Gap(6).AlignItems(ui.Center).BorderWidth(0, 1, 0, 0).BorderColor(k.Line)
	if vib {
		r.Background(k.Glass)
	} else {
		r.Background(k.Ground.Mix(k.Raise, 0.55))
	}
	r.Children(func() {
		if bar := c.TitleBar(); bar.Left > 0 {
			ui.Box(c).Height(bar.Height).DragWindow()
		}
		for _, it := range []struct {
			s    Screen
			ic   *ui.SVG
			name string
		}{{ScreenQueue, icQueue, "Queue"}, {ScreenLibrary, icLibrary, "Library"}, {ScreenNew, icPlus, "New download"}} {
			a.railItem(c, k, it.s, it.ic, it.name)
		}
		ui.Spacer(c)
		a.railItem(c, k, ScreenSystem, icSystem, "System")
		a.railItem(c, k, ScreenSettings, icSettings, "Settings")
	})
}

func (a *App) railItem(c *ui.Context, k *Tokens, s Screen, ic *ui.SVG, name string) {
	b := ui.ButtonBase(c.Key("rail-"+name)).Size(38, 34).Radius(8).Center().Label(name)
	col := k.Muted
	if a.Screen == s {
		b.Background(k.AccentSoft)
		col = k.Accent
	}
	if b.Clicked() {
		a.Go(s)
	}
	b.Cursor(ui.CursorPointer).Tooltip(name).Children(func() { ui.Icon(c, ic).FontSize(17).TextColor(col) })
}
