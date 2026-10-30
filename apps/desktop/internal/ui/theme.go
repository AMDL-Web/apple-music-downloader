package ui

import (
	"math"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Tokens are the design's colors ("liner notes in the dark": a near-black
// ground lit by each album's palette, one periwinkle accent, status colors
// only for status). Light mode is designed separately, not inverted.
type Tokens struct {
	Dark bool
	// Ink is behind everything; Ground the content area; Raise and Raise2
	// cards and fields above it.
	Ink, Ground, Raise, Raise2 ui.Color
	// Line and Line2 are hairlines, the second stronger.
	Line, Line2                   ui.Color
	Fg, Muted, Faint              ui.Color
	Accent, AccentInk, AccentSoft ui.Color
	OK, Warn, Bad                 ui.Color
	OKSoft, WarnSoft, BadSoft     ui.Color
	// Skip is a track that was already on disk, as the web console's
	// --skipped.
	Skip ui.Color
	// Glass is the sidebar over the window material; Track an empty bar.
	Glass, Track ui.Color
	// Pill is the selected segment and the big action button: white on
	// dark, ink on light.
	Pill, PillInk ui.Color
	// Tile colors are the sidebar's icon squares.
	TileQueue, TileLibrary, TileSystem, TileSettings, TileDisk ui.Color
}

func rgba(hex string, a float32) ui.Color { return ui.Hex(hex).Alpha(a) }

// DarkTokens are the dark theme, the one the app is designed in first.
var DarkTokens = Tokens{
	Dark: true,
	Ink:  ui.Hex("#0c0d12"), Ground: ui.Hex("#111219"), Raise: ui.Hex("#181a23"), Raise2: ui.Hex("#1f2230"),
	Line: rgba("#eceef8", .09), Line2: rgba("#eceef8", .16),
	Fg: ui.Hex("#eef0f7"), Muted: ui.Hex("#a3a8bb"), Faint: ui.Hex("#878ca1"),
	Accent: ui.Hex("#93a8ff"), AccentInk: ui.Hex("#0c1030"), AccentSoft: rgba("#93a8ff", .16),
	OK: ui.Hex("#62d69a"), Warn: ui.Hex("#f3b75e"), Bad: ui.Hex("#ff8a7c"),
	OKSoft: rgba("#62d69a", .14), WarnSoft: rgba("#f3b75e", .14), BadSoft: rgba("#ff8a7c", .14),
	Skip:  ui.Hex("#bf5af2"),
	Glass: rgba("#181a24", .62), Track: rgba("#eceef8", .10),
	Pill: ui.Hex("#f4f5fa"), PillInk: ui.Hex("#12131a"),
	TileQueue: ui.Hex("#4f7cff"), TileLibrary: ui.Hex("#e0507a"), TileSystem: ui.Hex("#2fb36f"),
	TileSettings: ui.Hex("#8a8f9e"), TileDisk: ui.Hex("#c08a3e"),
}

// LightTokens are the light theme.
var LightTokens = Tokens{
	Ink: ui.Hex("#eef0f5"), Ground: ui.Hex("#f6f7fa"), Raise: ui.Hex("#ffffff"), Raise2: ui.Hex("#f0f2f7"),
	Line: rgba("#141622", .09), Line2: rgba("#141622", .16),
	Fg: ui.Hex("#161822"), Muted: ui.Hex("#555b6e"), Faint: ui.Hex("#5f6476"),
	Accent: ui.Hex("#3d58e0"), AccentInk: ui.Hex("#ffffff"), AccentSoft: rgba("#3d58e0", .12),
	OK: ui.Hex("#17834b"), Warn: ui.Hex("#9a5c00"), Bad: ui.Hex("#c23a2c"),
	OKSoft: rgba("#17834b", .10), WarnSoft: rgba("#9a5c00", .10), BadSoft: rgba("#c23a2c", .10),
	Skip:  ui.Hex("#8944ab"),
	Glass: rgba("#fafbfd", .70), Track: rgba("#141622", .10),
	Pill: ui.Hex("#161822"), PillInk: ui.Hex("#ffffff"),
	TileQueue: ui.Hex("#4f7cff"), TileLibrary: ui.Hex("#e0507a"), TileSystem: ui.Hex("#2fb36f"),
	TileSettings: ui.Hex("#8a8f9e"), TileDisk: ui.Hex("#c08a3e"),
}

// Fonts. The interface and the big titles use the system font; only audio
// facts (codec, resolution, counts) are set in the monospace face the app
// embeds.
const (
	MonoFont = "JetBrains Mono"
)

// Corner radii, from the outside in. A box inside another takes the outer
// radius less the room between them (r = R − p), so both arcs share a
// center; the elements of a level share its radius, and none goes under
// radiusInner. Pills and circles stay fully round.
const (
	// radiusCard rounds the window's own boxes: cards, dialogs, bars, the
	// tray panel.
	radiusCard = 24
	// padCard is the room inside a card around what sits in its corners.
	padCard = 16
	// radiusControl rounds what sits in a card or on a screen: buttons,
	// fields, list rows, small panels, the wall's covers.
	radiusControl = radiusCard - padCard
	// padControl is the room inside a control around a box in it.
	padControl = 4
	// radiusInner rounds what sits in a control: segments, chips, tags,
	// thumbnails.
	radiusInner = radiusControl - padControl
	// radiusMenu rounds a menu's panel, whose rows are controls.
	radiusMenu = radiusControl + padControl
	// radiusCover rounds a hero's cover: a picture, not a box.
	radiusCover = 16
	radiusFull  = 999
)

// applyTheme sets the widgets' theme from the tokens, so the toolkit's own
// controls (inputs, switches, menus) match the app.
func applyTheme(c *ui.Context, k *Tokens) {
	t := *c.Theme()
	t.Dark = k.Dark
	t.Background, t.Surface = k.Ground, k.Raise
	t.SurfaceHover, t.SurfacePressed = k.Raise2, k.Raise2.Mix(k.Fg, 0.06)
	t.Border = k.Line2
	t.Text, t.TextMuted = k.Fg, k.Muted
	t.Accent, t.AccentText = k.Accent, k.AccentInk
	t.AccentHover, t.AccentPressed = k.Accent.Mix(k.Fg, 0.12), k.Accent.Mix(k.Ink, 0.15)
	t.Danger, t.Warning, t.Success = k.Bad, k.Warn, k.OK
	t.Selection, t.Focus = k.AccentSoft, k.Accent
	t.Radius, t.FontSize = radiusControl, 13
	c.SetTheme(&t)
}

// Palette is an album's colors from the catalog: a background and four text
// colors, hex without "#". Zero when the catalog had none.
type Palette struct {
	Bg, Text1, Text2, Text3, Text4 ui.Color
	OK                             bool
}

// ParsePalette reads the catalog's hex colors.
func ParsePalette(bg, t1, t2, t3, t4 string) Palette {
	if bg == "" {
		return Palette{}
	}
	h := func(s string) ui.Color {
		if s == "" {
			return ui.Color{}
		}
		return ui.Hex("#" + strings.TrimPrefix(s, "#"))
	}
	return Palette{Bg: h(bg), Text1: h(t1), Text2: h(t2), Text3: h(t3), Text4: h(t4), OK: true}
}

// luminance is the relative luminance of c (WCAG).
func luminance(c ui.Color) float64 {
	ch := func(v uint8) float64 {
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

// Contrast is the WCAG contrast ratio between two colors.
func Contrast(a, b ui.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Readable returns fg when it reads at 4.5:1 on bg, else whichever of the
// theme's text colors reads better.
func Readable(fg, bg ui.Color, k *Tokens) ui.Color {
	if fg.A != 0 && Contrast(fg, bg) >= 4.5 {
		return fg
	}
	light, dark := DarkTokens.Fg, LightTokens.Fg
	if Contrast(light, bg) >= Contrast(dark, bg) {
		return light
	}
	return dark
}

// AccentFor is the color an album's progress bars take: the first of its
// background and text colors that stands out at 3:1 on the ground, so a bar
// reads as the album's rather than as plain black or white; else the app's
// accent.
func AccentFor(p Palette, k *Tokens) ui.Color {
	if !p.OK {
		return k.Accent
	}
	for _, c := range []ui.Color{p.Bg, p.Text2, p.Text1} {
		if c.A != 0 && Contrast(c, k.Ground) >= 3 {
			return c
		}
	}
	return k.Accent
}
