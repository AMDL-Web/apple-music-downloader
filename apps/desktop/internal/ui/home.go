package ui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// Home's layout, in DIPs.
const (
	// fieldHeight is the field on one line; its text, wrapped, grows it up
	// to fieldMaxLines, past which it scrolls, unless its bottom edge was
	// dragged (App.fieldDrawn), down to fieldHeight or up to half the
	// window.
	fieldHeight   = 64
	fieldMaxLines = 8
	fieldMaxWidth = 720
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
// words in, and below it the links' cards or the catalog's answer, with the bar that starts the links floating over
// them, laid out in page, the width the sidebar leaves. Empty, the field
// waits at the page's optical center; the first character docks it at the
// top.
func (a *App) home(c *ui.Context, k *Tokens, page float32) {
	s := a.Store
	term, long := a.searchText()
	s.WantSearch(term)
	// A download's sheet reads the server's codec priority (download).
	s.WatchConfig()
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
				lift = max(0, 0.42*h-title-fieldHeight/2-homeTop)
			}
			sp := ui.Box(c.Key("lift")).Width(1)
			sp.Height(sp.AnimateWith("y", lift, heroGlide, ui.EaseOut))
			ui.Column(c).Width(fieldW).Padding(homeTop, 0, 0, 0).AlignItems(ui.Center).Children(func() {
				a.homeField(c, k, fieldW)
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
						a.linkCards(c, k, fieldW)
					case long:
						searchMessage(c, k, icSearch, "Too long to search", fmt.Sprintf("Search looks at the first %d characters.", store.SearchMaxTerm))
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
// keyboard. It grows with its text, and its bottom edge drags it taller or
// shorter.
func (a *App) homeField(c *ui.Context, k *Tokens, width float32) {
	s := a.Store
	o := &s.Omni
	if strings.TrimSpace(o.Text) == "" {
		a.fieldDrawn = 0
	}
	_, winH := c.Size()
	tallest := max(winH/2, fieldHeight)
	lift := ui.Box(c).Width(width).Radius(radiusCard)
	shade, deep := ui.RGBA(0, 0, 0, 0.32), ui.RGBA(0, 0, 0, 0.45)
	if !k.Dark {
		shade, deep = ui.RGBA(20, 22, 34, 0.12), ui.RGBA(20, 22, 34, 0.18)
	}
	// Links dragged over the window land here: the field rises to them.
	over := a.linkOver
	if over {
		lift.Shadow(0, 22, 48, -10, deep)
		lift.AnimatePose(ui.Pose{Y: -3, Scale: 1.01}, ui.Snappy)
	} else {
		lift.Shadow(0, 14, 36, -12, shade)
		lift.AnimatePose(ui.Pose{}, ui.Snappy)
	}
	lift.Children(func() {
		// The bottom edge, over the field as absolute children are, drags
		// it from where it shows; a double click fits it to its text again,
		// as a column's edge in a Finder list fits its column.
		var edge ui.Element
		dragging := false
		if o.Text != "" {
			edge = ui.Box(c.Key("edge")).Absolute().Left(0).Right(0).Bottom(-4).Height(10).Cursor(ui.CursorResizeNS).Label("Field height")
			// Dragged holds from the press on; only a move sets a height, so
			// a click, or a double click's first, leaves the field fitting.
			if _, dy, ok := edge.Dragged(); ok {
				dragging = true
				if dy != 0 {
					if a.fieldDrawn == 0 {
						a.fieldDrawn = lift.Bounds().H
					}
					a.fieldDrawn = min(max(a.fieldDrawn+dy, fieldHeight), tallest)
				}
			}
			if edge.DoubleClicked() {
				a.fieldDrawn = 0
			}
		}
		drawn := float32(0)
		if a.fieldDrawn > 0 {
			drawn = min(max(a.fieldDrawn, fieldHeight), tallest)
		}
		box := ui.Row(c.Key("box")).FillWidth().Padding(padField).Gap(12).Radius(radiusCard).Background(k.Raise).Clip()
		if over {
			box.Border(1.5, k.Accent).BorderStyle(ui.BorderDashed)
		} else {
			box.Border(1, k.Line2)
		}
		if o.Text == "" {
			box.AlignItems(ui.Center)
		} else {
			box.AlignItems(ui.Start)
		}
		ring := false
		// fits is how tall the text area was, as its text wrapped, in the
		// frame before.
		fits := float32(0)
		box.Children(func() {
			ic, col := icSearch, k.Muted
			if len(o.Links) > 0 || over {
				ic, col = icLink, k.Accent
			}
			well := float32(fieldHeight - 2*padField)
			ui.Box(c).Size(well, well).Shrink(0).Radius(radiusWell).Background(k.Raise2).Center().Children(func() {
				ui.Icon(c, ic).FontSize(20).TextColor(col)
			})
			a.fieldTxt = o.Text
			hint := "Paste links or search Apple Music"
			if over {
				hint = "Drop to download"
			}
			in := ui.TextAreaBase(c.Key("field"), &a.fieldTxt).Grow(1).Shrink(1).MinWidth(0).Bind(&a.field).
				Placeholder(hint).Label("Paste links or search Apple Music").
				PaddingY(10).FontSize(22).FontWeight(500).LineHeight(1.27)
			// Laid out at its whole height at once, the text shows as the
			// field opens to it, and neither scrolls nor moves meanwhile.
			if drawn > 0 {
				in.Height(drawn - 2*padField)
			} else {
				in.Lines(1, fieldMaxLines)
			}
			fits = in.LayoutBounds().H
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
			// Level with the icon well, where one line centers it.
			clear := ui.ButtonBase(c.Key("clear")).Size(32, 32).Shrink(0).Margin(8, 8, 0, 0).Radius(radiusFull).Center().Label("Clear the field")
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
		target := drawn
		if target == 0 {
			target = min(max(fits+2*padField, fieldHeight), tallest)
		}
		if dragging {
			box.Jump("height", target)
		}
		box.Height(box.AnimateTo("height", target, ui.Snappy))
		if ring {
			focusRing(box, radiusCard)
		}
		if o.Text != "" {
			// A grip shows where the edge drags while the pointer is near.
			shown := box.Hovered() || edge.Hovered() || dragging
			grip := ui.Row(c.Key("grip")).Absolute().Left(0).Right(0).Bottom(4).Justify(ui.Center).PassThrough()
			grip.Opacity(grip.AnimateTo("shown", map[bool]float32{true: 1, false: 0}[shown], ui.Snappy)).Children(func() {
				ui.Box(c).Size(28, 4).Radius(radiusFull).Background(k.Line2)
			})
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
