package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The hero's tag and the page's share the codec and both numbers, which
// keep their runes as the tag morphs; the separator and the units are
// each tag's own.
func TestATagsWordsArePairedInOrder(t *testing.T) {
	a, b := tagPieces("ALAC 24/96"), tagPieces("ALAC 24-bit · 96 kHz")
	matchTagWords(a, b)
	var shared []string
	for _, p := range a {
		if p.match >= 0 {
			if b[p.match].text != p.text {
				t.Errorf("%q is paired with %q", p.text, b[p.match].text)
			}
			shared = append(shared, p.text)
		}
	}
	if len(shared) != 3 || shared[0] != "ALAC" || shared[1] != "24" || shared[2] != "96" {
		t.Fatalf("shared words %q, want ALAC, 24 and 96", shared)
	}
	for _, p := range b {
		if p.text == "kHz" && p.match >= 0 {
			t.Error("kHz, which only the page's tag says, is paired")
		}
	}
	if b[len(b)-1].at != len([]rune("ALAC 24-bit · 96 ")) {
		t.Errorf("kHz is at rune %d", b[len(b)-1].at)
	}
}

// Halfway through the morph, both ways, the separator is on its way from
// the one tag's place to the other's with the gap between the numbers: it
// starts more than a third of the way across that gap, not at the end of
// 24, with both its glyphs, "/" and "·", drawn there.
func TestATagsSeparatorTravelsWithTheGap(t *testing.T) {
	for _, way := range [][2]string{{"ALAC 24/96", "ALAC 24-bit · 96 kHz"}, {"ALAC 24-bit · 96 kHz", "ALAC 24/96"}} {
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Size(300, 20).Children(func() { tagMorph(c, way[0], way[1], 0.5, ui.RGBA(0, 0, 0, 1)) })
		}, 300, 20)
		tt.Frame()
		tt.Frame()
		n24, ok1 := tt.Find("24")
		n96, ok2 := tt.Find("96")
		if !ok1 || !ok2 {
			t.Fatalf("%q → %q: no numbers in %q", way[0], way[1], tt.Texts())
		}
		gap := n96.X - (n24.X + n24.W)
		for _, g := range []string{"/", "·"} {
			r, ok := tt.Find(g)
			if !ok {
				t.Errorf("%q → %q: no %q halfway in %q", way[0], way[1], g, tt.Texts())
				continue
			}
			if at := r.X - (n24.X + n24.W); at < gap/3 || r.X+r.W/2 > n96.X {
				t.Errorf("%q → %q: halfway %q is %.1f px past 24 in a gap of %.1f to 96", way[0], way[1], g, at, gap)
			}
		}
	}
}
