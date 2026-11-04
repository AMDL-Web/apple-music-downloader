package ui

import "testing"

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
