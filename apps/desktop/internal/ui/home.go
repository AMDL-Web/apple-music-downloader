package ui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
)

// Home's layout, in DIPs.
const (
	// fieldHeight is the field on one line; pasted links grow it a
	// fieldLine at a time up to fieldMaxHeight, then it scrolls.
	fieldHeight    = 64
	fieldLine      = 21
	fieldMaxHeight = fieldHeight + 5*fieldLine
	fieldMaxWidth  = 720
	// chipHeight is the quality and overwrite chips under the field.
	chipHeight = 32
	// homeMaxWidth bounds the results; the field is narrower.
	homeMaxWidth = 960
	// homeTop is the room between the title bar and the docked field.
	homeTop = 8
	// heroGlide is how long the field takes to glide between the page's
	// optical center and the top.
	heroGlide = 220 * time.Millisecond
)

// submitLabel names the field's submit keys as the platform writes them.
var submitLabel = map[bool]string{true: "⌘↵", false: "Ctrl ↵"}[runtime.GOOS == "darwin"]

// home is the window's first page: one field to paste links into or type
// words in, the preset chips under it, and below them the links' cards or
// the catalog's answer, with the bar that starts the links floating over
// them, laid out in page, the width the sidebar leaves. Empty, the field
// waits at the page's optical center; the first character docks it at the
// top.
func (a *App) home(c *ui.Context, k *Tokens, page float32) {
	s := a.Store
	term, long := a.searchText()
	s.WantSearch(term)
	docked := strings.TrimSpace(s.Omni.Text) != ""
	_, h := c.Size()
	padX := float32(32)
	if a.compact {
		padX = 20
	}
	width := max(min(page-2*padX, homeMaxWidth), 280)
	fieldW := min(width, fieldMaxWidth)
	ui.Box(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c.Key("home")).Absolute().Top(0).Left(0).Right(0).Bottom(0).AlignItems(ui.Center).Children(func() {
			lift := float32(0)
			if !docked {
				title := max(52, c.TitleBar().Height)
				lift = max(0, 0.42*h-title-(fieldHeight+12+chipHeight)/2-homeTop)
			}
			sp := ui.Box(c.Key("lift")).Width(1)
			sp.Height(sp.AnimateWith("y", lift, heroGlide, ui.EaseOut))
			ui.Column(c).Width(fieldW).Padding(homeTop, 0, 0, 0).Gap(12).AlignItems(ui.Center).Children(func() {
				a.homeField(c, k, fieldW)
				a.homeChips(c, k)
			})
			if !docked {
				return
			}
			bottom := float32(32)
			if a.dockShown() {
				bottom = dockBottom + dockHeight + 24
			}
			a.scrollArea(c, "home-body", k.Ground, 0, func() {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(32, 0, bottom, 0).Children(func() {
					switch {
					case len(s.Omni.Links) > 0:
						a.linkCards(c, k, width)
					case long:
						searchMessage(c, k, icSearch, "Too long to search", fmt.Sprintf("Search looks at the first %d characters; shorten the words.", store.SearchMaxTerm))
					default:
						a.searchBody(c, k, width)
					}
				})
			}).Grow(1).FillWidth()
		})
		if a.dockShown() {
			a.dock(c, k, min(width, 600))
		}
	})
}

// homeField is the one field: paste links to download, or type words to
// search Apple Music. Return acts on the chosen result, Cmd+Return starts
// the links, and the arrows move through the results while it keeps the
// keyboard.
func (a *App) homeField(c *ui.Context, k *Tokens, width float32) {
	s := a.Store
	o := &s.Omni
	dense := len(o.Links) > 1 || strings.Contains(strings.TrimRight(o.Text, "\n"), "\n")
	height := float32(fieldHeight)
	if dense {
		lines := strings.Count(strings.TrimRight(o.Text, "\n"), "\n") + 1
		height = min(max(float32(2*padField+20+lines*fieldLine), fieldHeight), fieldMaxHeight)
	}
	lift := ui.Box(c).Width(width).Radius(radiusCard)
	shade := ui.RGBA(0, 0, 0, 0.32)
	if !k.Dark {
		shade = ui.RGBA(20, 22, 34, 0.12)
	}
	lift.Shadow(0, 14, 36, -12, shade)
	lift.Children(func() {
		box := ui.Row(c).FillWidth().Height(height).Padding(padField).Gap(12).Radius(radiusCard).
			Background(k.Raise).Border(1, k.Line2)
		if dense {
			box.AlignItems(ui.Start)
		} else {
			box.AlignItems(ui.Center)
		}
		ring := false
		box.Children(func() {
			ic, col := icSearch, k.Muted
			if len(o.Links) > 0 {
				ic, col = icLink, k.Accent
			}
			well := float32(fieldHeight - 2*padField)
			ui.Box(c).Size(well, well).Radius(radiusWell).Background(k.Raise2).Center().Children(func() {
				ui.Icon(c, ic).FontSize(20).TextColor(col)
			})
			a.fieldTxt = o.Text
			in := ui.TextAreaBase(c.Key("field"), &a.fieldTxt).Grow(1).Shrink(1).MinWidth(0).Bind(&a.field).
				Placeholder("Paste links or search Apple Music").Label("Paste links or search Apple Music")
			if dense {
				in.FillHeight().PaddingY(10).Font(MonoFont).FontSize(13).LineHeight(1.6)
			} else {
				in.Height(fieldHeight - 2*padField).PaddingY(10).FontSize(22).FontWeight(500).LineHeight(1.27)
			}
			in.OnTextKey(a.fieldKey).FocusRing(false)
			ring = in.Focused()
			if a.focusField {
				a.focusField = false
				a.field.Focus(c)
			}
			if in.Changed() {
				s.SetOmniText(a.fieldTxt)
				a.searchSel = ""
			}
			if o.Text == "" {
				ui.Row(c).PaddingX(7).PaddingY(3).Margin(0, 6, 0, 0).Radius(radiusInner).Border(1, k.Line2).Children(func() {
					mono(c, shortcutLabel, 11).TextColor(k.Muted)
				})
				return
			}
			// Centered on one line; level with the icon well as the field grows.
			top := float32(0)
			if dense {
				top = 8
			}
			clear := ui.ButtonBase(c.Key("clear")).Size(32, 32).Margin(top, 8, 0, 0).Radius(radiusFull).Center().Label("Clear the field")
			if clear.Hovered() {
				clear.Background(k.Track)
			} else {
				clear.Background(k.Raise2)
			}
			if clear.Clicked() {
				a.clearField()
			}
			clear.Cursor(ui.CursorPointer).Children(func() { ui.Icon(c, icCross).FontSize(14).TextColor(k.Muted) })
		})
		if ring {
			focusRing(box, radiusCard)
		}
	})
}

// fieldKey takes the keys the field would otherwise edit with: Return and
// the arrows over search results, Cmd+Return to start the links.
func (a *App) fieldKey(key ui.TextKey) bool {
	s := a.Store
	term, long := a.searchText()
	search := term != "" && !long
	switch {
	case key.Key == ui.KeyEnter && key.Mods == ui.Cmd:
		if len(s.Startable()) > 0 {
			s.Submit()
		} else if search {
			a.actOnHit()
		}
		return true
	case !search || key.Mods != 0:
		return false
	case key.Key == ui.KeyEnter:
		a.actOnHit()
		return true
	case key.Key == ui.KeyDown:
		a.moveHit(1)
		return true
	case key.Key == ui.KeyUp:
		a.moveHit(-1)
		return true
	}
	return false
}

// clearField empties the field and keeps the keyboard in it.
func (a *App) clearField() {
	a.Store.SetOmniText("")
	a.searchSel = ""
	a.focusField = true
}

// homeKeys are home's window keys, for when the keyboard is out of the
// field: the arrows, Return and Escape over results, Cmd+Return to start
// the links, Escape to clear a search.
func (a *App) homeKeys(c *ui.Context) {
	s := a.Store
	c.OnShortcut(ui.Cmd, ui.KeyEnter, s.Submit)
	if term, long := a.searchText(); term != "" && !long {
		a.searchKeys(c)
	}
}

// homeChips are the preset the next download applies, links and search
// results alike, and whether it replaces files already on disk.
func (a *App) homeChips(c *ui.Context, k *Tokens) {
	o := &a.Store.Omni
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		a.qualityChip(c, k)
		on := o.Overwrite
		chip := ui.ButtonBase(c.Key("overwrite")).Height(chipHeight).PaddingStart(12).PaddingEnd((chipHeight - 18) / 2).Gap(8).
			Radius(radiusFull).AlignItems(ui.Center).Label("Overwrite existing files")
		// The chip's colors move with the knob, on the same curve.
		bg, edge, fg, track := k.Raise, k.Line2, k.Fg, k.Track
		if on {
			bg, edge, fg, track = k.WarnSoft, k.Warn.Alpha(0.45), k.Warn, k.Warn
		}
		chip.Background(chip.AnimateColor("bg", bg, ui.Snappy)).Border(1, chip.AnimateColor("edge", edge, ui.Snappy))
		fg = chip.AnimateColor("fg", fg, ui.Snappy)
		track = chip.AnimateColor("track", track, ui.Snappy)
		if chip.Hovered() {
			chip.Opacity(0.9)
		}
		if chip.Clicked() {
			o.Overwrite = !on
		}
		chip.Cursor(ui.CursorPointer).Children(func() {
			ui.Text(c, "Overwrite").FontSize(12.5).FontWeight(600).TextColor(fg).SingleLine()
			sw := ui.Box(c).Size(30, 18).Radius(radiusFull).Background(track)
			x := sw.AnimateTo("knob", map[bool]float32{true: 14, false: 2}[on], ui.Snappy)
			sw.Children(func() {
				ui.Box(c).Size(14, 14).Radius(radiusFull).Background(ui.RGB(255, 255, 255)).Absolute().Top(2).Left(x)
			})
		})
	})
}

// qualityLabels name the presets of the quality chip, the backend's own
// settings first.
var qualityLabels = []string{"Server settings", "Hi-Res", "Lossless", "Atmos", "AAC"}

// qualityChip picks the preset among qualityTabs.
func (a *App) qualityChip(c *ui.Context, k *Tokens) {
	o := &a.Store.Omni
	cur := qualityLabels[0]
	for i, q := range qualityTabs {
		if q == o.Quality {
			cur = qualityLabels[i]
		}
	}
	sel := ui.SelectBase(c.Key("quality"), &cur)
	trigger := sel.Trigger.Height(chipHeight).PaddingStart(12).PaddingEnd(10).Gap(6).Radius(radiusFull).AlignItems(ui.Center).
		Background(k.Raise).Border(1, k.Line2).Label("Quality")
	if sel.Open() {
		trigger.Border(1, k.Accent)
	}
	// The chip keeps the width of its longest preset, so choosing one does
	// not move the overwrite chip beside it.
	trigger.Cursor(ui.CursorPointer).Children(func() {
		ui.Icon(c, icNote).FontSize(13).TextColor(k.Muted)
		ui.Box(c).Children(func() {
			ui.Text(c, qualityLabels[0]).FontSize(12.5).FontWeight(600).SingleLine().Invisible()
			ui.Text(c, cur).FontSize(12.5).FontWeight(600).SingleLine().Absolute().Top(0).Left(0)
		})
		ui.Icon(c, icChevron).FontSize(11).TextColor(k.Muted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Width(180).Padding(padControl).Gap(1).Radius(radiusMenu).Background(k.Raise2).Border(1, k.Line2).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.3))
		for _, l := range qualityLabels {
			item := sel.Item(l).Height(28).PaddingX(10).Radius(radiusControl).AlignItems(ui.Center)
			col := k.Fg
			if item.Highlighted() {
				item.Background(k.Accent)
				col = k.AccentInk
			}
			item.Cursor(ui.CursorPointer).Children(func() { ui.Text(c, l).FontSize(12.5).TextColor(col) })
		}
	})
	for i, l := range qualityLabels {
		if l == cur {
			o.Quality = qualityTabs[i]
		}
	}
}

// homePalette is the album color home is lit by: the first pasted link's,
// else the first result's.
func (a *App) homePalette() Palette {
	s := a.Store
	for _, l := range s.Omni.Links {
		if l.State == store.LinkReady {
			lk := l.Lookup
			if p := ParsePalette(lk.ArtworkBgColor, lk.ArtworkTextColor1, lk.ArtworkTextColor2, lk.ArtworkTextColor3, lk.ArtworkTextColor4); p.OK {
				return p
			}
		}
	}
	if len(s.Omni.Links) == 0 {
		for _, h := range s.Search.ShownHits() {
			if p := hitPalette(h); p.OK {
				return p
			}
		}
	}
	return Palette{}
}

// homeBackdrop lights home's top with pal, as the queue's hero lights it
// with the album downloading. Its band reaches past the discs' blur, so the
// light fades out above the results instead of ending in a line.
func homeBackdrop(c *ui.Context, k *Tokens, pal Palette) {
	strength := float32(0.45)
	if !k.Dark {
		strength = 0.22
	}
	widgets.Backdrop{Light: pal.Bg, Deep: pal.Bg.Mix(ui.Hex("#05081f"), 0.65), Strength: strength, Height: 560}.Show(c)
}
