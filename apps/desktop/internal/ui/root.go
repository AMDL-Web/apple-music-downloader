package ui

import (
	"runtime"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// shortcutLabel names the field's shortcut as the platform writes it.
var shortcutLabel = map[bool]string{true: "⌘L", false: "Ctrl L"}[runtime.GOOS == "darwin"]

// compactWidth is the window width below which the sidebar folds into an
// icon rail and the title bar's field into a button (the 820×560 board).
const compactWidth = 1000

// View builds the main window.
func (a *App) View(c *ui.Context) {
	k := a.tokens(c)
	applyTheme(c, k)
	a.shortcuts(c)
	w, _ := c.Size()
	compact := w < compactWidth
	vib := c.Vibrancy()
	if vib {
		c.Root().Background(ui.Transparent)
	} else {
		c.Root().Background(k.Ground)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		if compact {
			a.rail(c, k, vib)
		} else {
			a.sidebar(c, k, vib)
		}
		main := ui.Box(c).Grow(1).Shrink(1).MinWidth(0).Background(k.Ground).Clip()
		main.Children(func() {
			// Absolute children paint above the flow, in the order they are
			// built, so the content is a layer of its own over the backdrop.
			a.backdrop(c, k)
			ui.Column(c.Key("content")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() {
				a.titleBar(c, k, compact)
				ui.Column(c).Grow(1).MinHeight(0).Children(func() {
					switch a.Screen {
					case ScreenSettings:
						a.settings(c, k)
					case ScreenDetail:
						a.detail(c, k)
					case ScreenNew:
						a.newDownload(c, k, compact)
					case ScreenLibrary:
						a.library(c, k, compact)
					case ScreenSystem:
						a.system(c, k, compact)
					default:
						a.queueScreen(c, k, compact)
					}
				})
			})
		})
	})
	a.toast(c, k)
}

func (a *App) shortcuts(c *ui.Context) {
	c.OnShortcut(ui.Cmd, ui.KeyL, a.FocusOmni)
	c.OnShortcut(ui.Cmd, ui.Key1, func() { a.Go(ScreenQueue) })
	c.OnShortcut(ui.Cmd, ui.Key2, func() { a.Go(ScreenLibrary) })
	c.OnShortcut(ui.Cmd, ui.KeyComma, func() { a.Go(ScreenSettings) })
	if a.Screen == ScreenDetail && c.Shortcut(0, ui.KeyEscape) {
		a.Go(ScreenQueue)
	}
	if a.Screen == ScreenNew {
		if term, long := a.searchText(); term != "" && !long {
			hits := a.Store.Search.ShownHits()
			a.searchKeys(c, hits, a.chosenHit(hits))
		}
	}
}

// titleBar is the window's top strip: the one field for links and search,
// clear of the window controls, dragging the window.
func (a *App) titleBar(c *ui.Context, k *Tokens, compact bool) {
	bar := c.TitleBar()
	right := float32(18)
	if bar.Right > 0 {
		right = bar.Right + 8
	}
	ui.Row(c).Height(max(52, bar.Height)).PaddingStart(18).PaddingEnd(right).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
		if a.Screen == ScreenDetail {
			if button(c, k, buttonGhost, "Queue", icBack).Clicked() {
				a.Go(ScreenQueue)
			}
		}
		if compact {
			ui.Spacer(c)
			if button(c, k, buttonPlain, shortcutLabel, icLink).Label("Paste a link or search").Clicked() {
				a.Go(ScreenNew)
			}
			return
		}
		ui.Row(c).Grow(1).Justify(ui.Center).Children(func() { a.omniField(c, k) })
	})
}

// omniField is the one field: paste links to download, or type to search
// Apple Music (the library, on its screen).
func (a *App) omniField(c *ui.Context, k *Tokens) {
	s := a.Store
	focused := c.Focused(a.omni)
	box := ui.Row(c).Grow(1).MaxWidth(500).Height(30).PaddingX(10).Gap(8).Radius(9).AlignItems(ui.Center).
		Background(k.Raise2).Border(1, k.Line)
	if focused {
		box.Border(1, k.Accent).Shadow(0, 0, 0, 3, k.AccentSoft)
	}
	box.Children(func() {
		ic, hint := icLink, "Paste a link, or search Apple Music"
		if a.Screen == ScreenLibrary {
			ic, hint = icSearch, "Search the library"
		} else if term, _ := a.searchText(); term != "" {
			ic = icSearch
		}
		ui.Icon(c, ic).FontSize(14).TextColor(k.Faint)
		a.omniTxt = s.Omni.Text
		in := ui.TextInputBase(c.Key("omni"), &a.omniTxt).Grow(1).Bind(&a.omni).FontSize(12.5).
			Placeholder(hint).Label(hint)
		if in.Changed() {
			s.SetOmniText(a.omniTxt)
			// Links preview and words search on the new download screen;
			// the library keeps its own search.
			term, _ := a.searchText()
			if (len(s.Omni.Links) > 0 || term != "" && a.Screen != ScreenLibrary) && a.Screen != ScreenNew {
				a.Go(ScreenNew)
			}
		}
		if in.Submitted() {
			switch term, _ := a.searchText(); {
			case a.Screen == ScreenNew && term != "":
				a.actOnHit()
			case len(s.Startable()) > 0:
				s.Submit()
			}
		}
		ui.Row(c).PaddingX(5).PaddingY(1).Radius(4).Border(1, k.Line2).Children(func() {
			mono(c, shortcutLabel, 10.5).TextColor(k.Muted)
		})
	})
}

// toast shows the store's latest notice for a few seconds.
func (a *App) toast(c *ui.Context, k *Tokens) {
	n := a.Store.Notice
	if n.Text == "" {
		return
	}
	age := c.Now().Sub(n.At)
	if age > 4*time.Second {
		return
	}
	c.After(4*time.Second - age)
	ui.Overlay(c, func() {
		ui.Row(c).Attach(ui.AnchorBottom, ui.AnchorBottom).Bottom(24).
			PaddingX(14).PaddingY(9).Gap(8).Radius(10).AlignItems(ui.Center).
			Background(k.Pill).Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.35)).Children(func() {
			ic, col := icCheck, k.OK
			if n.Error {
				ic, col = icCross, k.Bad
			}
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
