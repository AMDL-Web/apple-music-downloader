// The shift tests are layout checks with no goroutines of their own: the
// race detector adds nothing to them and makes them about twenty times
// slower, so they run under make test only.

//go:build !race

package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The shift tests. A control's box must not depend on its state: choosing
// it, hovering it or pressing it may change colors, never the size of the
// control or the place of anything around it. holdsStill asserts that for
// one control; a new control whose look changes with its state gets a test
// that calls it.

// holdsStill walks the pointer over control, presses it, releases it
// (which toggles it in place) and leaves, and clicks it once more to put it
// back. After each step no text on screen may have changed box. Texts must
// be on screen all along, the control's neighbours.
func holdsStill(t *testing.T, tt *ui.Tester, control string, texts ...string) {
	t.Helper()
	r, ok := tt.Find(control)
	if !ok {
		t.Fatalf("no element shows %q in %q", control, tt.Texts())
	}
	for _, w := range texts {
		if _, ok := tt.Find(w); !ok {
			t.Fatalf("no element shows %q in %q", w, tt.Texts())
		}
	}
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	tt.Move(2, 2)
	tt.Frame()
	last := snapFrame(tt)
	step := func(what string) {
		t.Helper()
		tt.Frame()
		now := snapFrame(tt)
		if d := now.layoutDiffers(last); d != "" {
			t.Errorf("%s %q moves things: %s", what, control, d)
		}
		last = now
	}
	tt.Move(cx, cy)
	step("hovering")
	tt.Press(cx, cy)
	step("pressing")
	tt.Release(cx, cy)
	step("choosing")
	tt.Move(2, 2)
	step("leaving")
	tt.Press(cx, cy)
	tt.Release(cx, cy)
	tt.Move(2, 2)
	step("choosing again")
}

// bench is a window of one control group alone, in the light or the dark
// tokens, so that every label on screen is the control's.
func bench(dark bool, build func(c *ui.Context, k *Tokens)) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) {
		k := &LightTokens
		if dark {
			k = &DarkTokens
		}
		applyTheme(c, k)
		ui.Column(c).Fill().Padding(40).Background(k.Ground).Children(func() { build(c, k) })
	}, 800, 400)
	tt.SetDark(dark)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	return tt
}

// themes runs f in the light and the dark tokens.
func themes(t *testing.T, f func(t *testing.T, dark bool)) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) { f(t, dark) })
	}
}

// openHookSheet opens the sheet that adds a webhook of the ntfy preset.
func openHookSheet(t *testing.T) (*App, *ui.Tester, func(until func() bool)) {
	t.Helper()
	a, tt, run := hooksApp(t)
	for _, step := range []string{"Add hook", "ntfy"} {
		if err := tt.Click(step); err != nil {
			t.Fatalf("%s: %v in %q", step, err, tt.Texts())
		}
	}
	tt.Frame()
	return a, tt, run
}

// The chips of the sheet itself, as lyjw's recording showed them: choosing
// Cancelled moved Queued.
func TestHookSheetChipsStayPutWhenChosen(t *testing.T) {
	themes(t, func(t *testing.T, dark bool) {
		a, tt, _ := openHookSheet(t)
		a.Theme = map[bool]string{false: "light", true: "dark"}[dark]
		tt.Frame()
		for _, chip := range []string{"Failed", "Cancelled", "Queued", "All"} {
			holdsStill(t, tt, chip, "Failed", "Cancelled", "Queued", "All")
		}
	})
}

func TestChoiceChipsKeepTheirSizeWhenChosen(t *testing.T) {
	themes(t, func(t *testing.T, dark bool) {
		on := map[string]bool{"Finished": true}
		labels := []string{"Finished", "Failed", "Cancelled", "Queued"}
		tt := bench(dark, func(c *ui.Context, k *Tokens) {
			ui.Row(c).Gap(6).Children(func() {
				for _, l := range labels {
					if choiceChip(c, k, "chip-"+l, l, on[l]) {
						on[l] = !on[l]
					}
				}
			})
		})
		for _, l := range labels {
			holdsStill(t, tt, l, labels...)
		}
	})
}
