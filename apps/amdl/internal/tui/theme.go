package tui

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// theme is the palette, chosen for a dark or a light terminal. Job cards take
// their accent from the album's artwork color instead of clay.
type theme struct {
	dark                         bool
	bg, fg, dim, faint, gap      color.Color
	clay, ok, bad, skipped, info color.Color
}

func newTheme(dark bool) theme {
	if dark {
		return theme{dark: true,
			bg: hex("1c1b19"), fg: hex("e8e6dc"), dim: hex("8a8880"), faint: hex("3a3936"), gap: hex("2e2d2a"),
			clay: hex("d97757"), ok: hex("9cc27a"), bad: hex("e3906f"), skipped: hex("6b6a64"), info: hex("7fb2e5")}
	}
	return theme{
		bg: hex("faf9f5"), fg: hex("141413"), dim: hex("7a7870"), faint: hex("dedcd3"), gap: hex("ebe9e1"),
		clay: hex("c4623f"), ok: hex("4f7a34"), bad: hex("b0532f"), skipped: hex("a3a19a"), info: hex("2f6aa8")}
}

func hex(s string) color.Color { return lipgloss.Color("#" + s) }

func (t theme) style(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// accent returns the card color for an album's background color: the color
// itself when it stands out from the terminal, else a lighter or darker
// version of it, else clay.
func (t theme) accent(bgHex string) color.Color {
	c, ok := parseHex(bgHex)
	if !ok {
		return t.clay
	}
	for i := 0; i < 6 && contrast(c, t.bg) < 3; i++ {
		if t.dark {
			c = mix(c, hex("ffffff"), 0.25)
		} else {
			c = mix(c, hex("000000"), 0.25)
		}
	}
	if contrast(c, t.bg) < 3 {
		return t.clay
	}
	return c
}

// soft is c faded halfway into the background: the album bar's translucent
// fill.
func (t theme) soft(c color.Color) color.Color { return mix(c, t.bg, 0.55) }

func parseHex(s string) (color.Color, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return nil, false
	}
	if _, err := strconv.ParseUint(s, 16, 32); err != nil {
		return nil, false
	}
	return hex(s), true
}

func rgb(c color.Color) (r, g, b float64) {
	cr, cg, cb, _ := c.RGBA()
	return float64(cr) / 0xffff, float64(cg) / 0xffff, float64(cb) / 0xffff
}

// mix moves c toward d by f in [0,1].
func mix(c, d color.Color, f float64) color.Color {
	r1, g1, b1 := rgb(c)
	r2, g2, b2 := rgb(d)
	at := func(a, b float64) int { return int(math.Round((a + (b-a)*f) * 255)) }
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", at(r1, r2), at(g1, g2), at(b1, b2)))
}

// contrast is the WCAG contrast ratio of two colors.
func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(c color.Color) float64 {
	r, g, b := rgb(c)
	lin := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}
