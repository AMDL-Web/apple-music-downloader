// Layout checks with no goroutines of their own to race: the race detector
// adds nothing to them and makes them about twenty times slower, so they
// run without it.

//go:build !race

package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// Layout stability. A control's box must not depend on its state: choosing
// it, hovering it or pressing it may change colors, never the size of the
// control or the place of anything around it; nor may an icon that comes
// and goes, a count that gains a digit or words that change to a longer
// waiting word. A new control whose look changes with its state gets a
// case here.

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

// sameBox fails the test when the element showing after is not where, and as
// large as, the one that showed before.
func sameBox(t *testing.T, what string, before, after ui.Rect) {
	t.Helper()
	if abs32(before.X-after.X) > 0.01 || abs32(before.Y-after.Y) > 0.01 || abs32(before.W-after.W) > 0.01 || abs32(before.H-after.H) > 0.01 {
		t.Errorf("%s moved: %v then %v", what, before, after)
	}
}

// findAny is the box of the first of labels that is on screen.
func findAny(t *testing.T, tt *ui.Tester, labels ...string) ui.Rect {
	t.Helper()
	for _, l := range labels {
		if r, ok := tt.Find(l); ok {
			return r
		}
	}
	t.Fatalf("none of %q on screen in %q", labels, tt.Texts())
	return ui.Rect{}
}

// middleOf is where the middle of r is.
func middleOf(r ui.Rect) (float32, float32) { return r.X + r.W/2, r.Y + r.H/2 }

// keepsCenter fails when the words of a button that changed from before to
// after are not centered where they were: its edges, or what sits beside
// it, moved.
func keepsCenter(t *testing.T, what string, before, after ui.Rect) {
	t.Helper()
	bx, by := middleOf(before)
	ax, ay := middleOf(after)
	if abs32(bx-ax) > 0.5 || abs32(by-ay) > 0.5 {
		t.Errorf("%s: the button's words were centered at (%v, %v), then at (%v, %v)", what, bx, by, ax, ay)
	}
}

// moved says which element showing text in both frames changed its box, or
// returns "". A lift, an element scaled up about its center a few percent
// by a transform, moves nothing around it and is allowed; the texts of a
// menu or a tooltip the pointer opens are not compared.
func moved(now, rest frameSnap) string {
	for text, r := range rest.rects {
		o, ok := now.rects[text]
		if !ok || (abs32(r.X-o.X) <= 0.5 && abs32(r.Y-o.Y) <= 0.5 && abs32(r.W-o.W) <= 0.5 && abs32(r.H-o.H) <= 0.5) {
			continue
		}
		cx, cy, ox, oy := r.X+r.W/2, r.Y+r.H/2, o.X+o.W/2, o.Y+o.H/2
		sw, sh := o.W/r.W, o.H/r.H
		if r.W > 0 && r.H > 0 && abs32(cx-ox) <= 0.5 && abs32(cy-oy) <= 0.5 && abs32(sw-sh) < 0.002 && sw < 1.05 {
			continue
		}
		return fmt.Sprintf("the box of %q: %v vs %v", text, r, o)
	}
	return ""
}

// hoverSweep rests the pointer on every element of the screen in turn and
// reports each one whose box, or any other element's, moved against the
// pointer at rest. Hover may recolor, never move. It only moves the
// pointer: pressing starts drags and clicks change the screen.
func hoverSweep(t *testing.T, tt *ui.Tester) {
	t.Helper()
	const restX, restY = 3, 3
	tt.Move(restX, restY)
	tt.Frame()
	rest := snapFrame(tt)
	seen := map[ui.Rect]bool{}
	for _, text := range rest.texts {
		r, ok := rest.rects[text]
		if !ok || seen[r] || r.W <= 0 || r.H <= 0 {
			continue
		}
		seen[r] = true
		x, y := r.X+r.W/2, r.Y+r.H/2
		if x < 0 || y < 0 || x >= 1279 || y >= 799 {
			continue
		}
		tt.Move(x, y)
		tt.Frame()
		if d := moved(snapFrame(tt), rest); d != "" {
			t.Errorf("the pointer on %q moves things: %s", text, d)
		}
	}
	tt.Move(restX, restY)
	tt.Frame()
}

// Each control, chosen, hovered and pressed, keeps its box and its
// neighbours' (holdsStill), in both themes.
func TestControlsHoldStillWhenChosen(t *testing.T) {
	cases := []struct {
		name string
		// open shows the controls labels name.
		open   func(t *testing.T, dark bool) *ui.Tester
		labels []string
	}{
		// As lyjw's recording showed them: choosing Cancelled moved Queued.
		{"hook sheet chips", func(t *testing.T, dark bool) *ui.Tester {
			a, tt, _ := openHookSheet(t)
			a.Theme = map[bool]string{false: "light", true: "dark"}[dark]
			tt.Frame()
			return tt
		}, []string{"Failed", "Cancelled", "Queued", "All"}},
		{"choice chips", func(t *testing.T, dark bool) *ui.Tester {
			on := map[string]bool{"Finished": true}
			labels := []string{"Finished", "Failed", "Cancelled", "Queued"}
			return bench(dark, func(c *ui.Context, k *Tokens) {
				ui.Row(c).Gap(6).Children(func() {
					for _, l := range labels {
						if choiceChip(c, k, "chip-"+l, l, on[l]) {
							on[l] = !on[l]
						}
					}
				})
			})
		}, []string{"Finished", "Failed", "Cancelled", "Queued"}},
		{"segments", func(t *testing.T, dark bool) *ui.Tester {
			chosen := 0
			return bench(dark, func(c *ui.Context, k *Tokens) {
				ui.Row(c).Children(func() { segmented(c, k, "theme", &chosen, []string{"System", "Light", "Dark"}, nil) })
			})
		}, []string{"System", "Light", "Dark"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			themes(t, func(t *testing.T, dark bool) {
				tt := tc.open(t, dark)
				for _, l := range tc.labels {
					holdsStill(t, tt, l, tc.labels...)
				}
			})
		})
	}
}

// A row of items (tabs, the sidebar, Settings' nav) keeps each item's box
// whichever of them, or whichever screen, is chosen.
func TestItemsKeepTheirBoxesWhicheverIsChosen(t *testing.T) {
	cases := []struct {
		name string
		// open shows the items; choices are the steps that choose one,
		// each named.
		open    func(t *testing.T) (*ui.Tester, map[string]func())
		items   []string
		ordered []string
	}{
		{"search tabs", func(t *testing.T) (*ui.Tester, map[string]func()) {
			a, _ := newTestApp(remoteSettings())
			s := a.Store
			s.SetOmniText("one last kiss")
			s.WantSearch("one last kiss")
			hit := func(typ, id, name string) *client.HitGroup {
				return &client.HitGroup{Items: []client.SearchHit{{Type: typ, ID: id, Name: name, ArtistName: "Utada"}}}
			}
			s.Search.Result = &client.SearchResult{Albums: hit("album", "1", "One Last Kiss"), Songs: hit("song", "2", "Kiss"),
				Playlists: hit("playlist", "3", "Kisses"), Artists: hit("artist", "4", "Utada Hikaru")}
			s.Search.Status = store.SearchReady
			tt := stillTester(a.View, 1280, 800)
			a.FocusField()
			tt.Frame()
			a.focusGroup("albums")
			tt.Frame()
			choose := map[string]func(){}
			for _, g := range []string{"songs", "playlists", "artists", "albums"} {
				choose[g] = func() { a.focusGroup(g); tt.Frame() }
			}
			return tt, choose
		}, []string{"Albums", "Songs", "Playlists", "Artists"}, []string{"songs", "playlists", "artists", "albums"}},
		{"sidebar", func(t *testing.T) (*ui.Tester, map[string]func()) {
			a, run := demoApp(t)
			a.Screen = ScreenQueue
			run(func() bool { return a.Store.Loaded })
			tt := stillTester(a.View, 1280, 800)
			choose := map[string]func(){}
			for _, s := range []Screen{ScreenHome, ScreenSettings, ScreenQueue} {
				choose[fmt.Sprint("screen ", s)] = func() {
					a.Go(s)
					for range 3 {
						tt.Frame()
					}
				}
			}
			return tt, choose
		}, []string{"Home", "Queue", "Connection settings"}, []string{fmt.Sprint("screen ", ScreenHome), fmt.Sprint("screen ", ScreenSettings), fmt.Sprint("screen ", ScreenQueue)}},
		{"settings nav", func(t *testing.T) (*ui.Tester, map[string]func()) {
			_, tt, _ := hooksApp(t)
			choose := map[string]func(){}
			for _, l := range []string{"Account", "Quality", "Hooks", "Logs"} {
				choose[l] = func() {
					if err := tt.Click(l); err != nil {
						t.Fatal(err)
					}
					for range 8 {
						tt.Frame()
					}
				}
			}
			return tt, choose
		}, []string{"Account", "Quality", "Hooks", "Logs"}, []string{"Account", "Quality", "Hooks", "Logs"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tt, choose := tc.open(t)
			before := map[string]ui.Rect{}
			for _, l := range tc.items {
				before[l] = findAny(t, tt, l)
			}
			for _, c := range tc.ordered {
				step, ok := choose[c]
				if !ok {
					t.Fatalf("no step %q", c)
				}
				step()
				for _, l := range tc.items {
					sameBox(t, l+" with "+c+" chosen", before[l], findAny(t, tt, l))
				}
			}
		})
	}
}

// Hover recolors, never moves (hoverSweep), on the screens with the most
// controls.
func TestPointerNeverMovesAnything(t *testing.T) {
	t.Run("queue", func(t *testing.T) {
		a, run := demoApp(t)
		a.Screen = ScreenQueue
		run(func() bool { return a.Store.Loaded })
		a.Tab = int(store.TabAll)
		tt := stillTester(a.View, 1280, 800)
		hoverSweep(t, tt)
	})
	for _, page := range []string{"quality", "lyrics_artwork", "hooks"} {
		t.Run("settings "+page, func(t *testing.T) {
			a, tt, _ := hooksApp(t)
			a.GoSettings(page)
			for range 4 {
				tt.Frame()
			}
			hoverSweep(t, tt)
		})
	}
}

func TestSettingsSwitchKeepsItsSize(t *testing.T) {
	_, tt, _ := hooksApp(t)
	// Switching hooks off changes what the section says; the switch and
	// the button beside it stay.
	toggle, add := findAny(t, tt, "Run hooks"), findAny(t, tt, "Add hook")
	for range 2 {
		if err := tt.Click("Run hooks"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		sameBox(t, "the switch", toggle, findAny(t, tt, "Run hooks"))
		sameBox(t, "Add hook", add, findAny(t, tt, "Add hook"))
	}
}

func TestSetOptionsKeepTheirSizeWhenChosen(t *testing.T) {
	a, tt, run := hooksApp(t)
	a.GoSettings("lyrics_artwork")
	for range 4 {
		tt.Frame()
	}
	labels := []string{"LRC", "TTML"}
	for _, l := range labels {
		if _, ok := tt.Find(l); !ok {
			t.Fatalf("no option %q in %q", l, tt.Texts())
		}
	}
	before := snapFrame(tt)
	for _, l := range labels {
		if err := tt.Click(l); err != nil {
			t.Fatal(err)
		}
		tt.Move(3, 3)
		tt.Frame()
		if d := moved(snapFrame(tt), before); d != "" {
			t.Errorf("choosing %q (asked): %s", l, d)
		}
		run(func() bool { return len(a.Store.Config.Saving) == 0 })
		tt.Frame()
		if d := moved(snapFrame(tt), before); d != "" {
			t.Errorf("choosing %q (saved): %s", l, d)
		}
		if err := tt.Click(l); err != nil {
			t.Fatal(err)
		}
		run(func() bool { return len(a.Store.Config.Saving) == 0 })
		tt.Move(3, 3)
		tt.Frame()
		before = snapFrame(tt)
	}
}

func TestQueueTabsKeepTheirPlaceAsACountGainsADigit(t *testing.T) {
	a, run := demoApp(t)
	a.Screen = ScreenQueue
	run(func() bool { return a.Store.Loaded })
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	s := a.Store
	set := func(active, done, failed int) {
		s.Counts = domain.JobStatusCounts{Queued: active, Completed: done, Failed: failed, Total: active + done + failed}
		run(func() bool { return !s.Queue.Counting() })
		s.Queue.SetCounts(done, failed)
		tt.Frame()
	}
	tabs := func(active, done, failed int) (ui.Rect, ui.Rect, ui.Rect) {
		return findAny(t, tt, fmt.Sprintf("Active · %d", active)), findAny(t, tt, fmt.Sprintf("Done · %d", done)), findAny(t, tt, fmt.Sprintf("Failed · %d", failed))
	}
	set(9, 9, 9)
	a1, d1, f1 := tabs(9, 9, 9)
	set(10, 9, 9)
	a2, d2, f2 := tabs(10, 9, 9)
	sameBox(t, "the Active tab at 10", a1, a2)
	sameBox(t, "the Done tab after Active went to 10", d1, d2)
	sameBox(t, "the Failed tab after Active went to 10", f1, f2)
	set(10, 99, 9)
	_, d3, f3 := tabs(10, 99, 9)
	sameBox(t, "the Done tab at 99", d2, d3)
	sameBox(t, "the Failed tab after Done went to 99", f2, f3)
	set(10, 99, 100)
	// A third digit is the one the tabs do not reserve room for; it is
	// only the last tab's and the track's.
	findAny(t, tt, "Failed · 100")
}

func TestHeroPercentGainingADigitMovesNothingElse(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Screen = ScreenQueue
	s := a.Store
	s.Loaded = true
	job := &store.Job{Job: domain.Job{ID: "r", Type: "album", Status: domain.JobRunning, Title: "A Title That Fills Out Quite A Few Columns Of The Hero Band", ArtistName: "Someone", TotalItems: 10}, Progress: 0.09}
	s.Jobs = []*store.Job{job}
	s.Counts = domain.JobStatusCounts{Running: 1, Total: 1}
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	before := snapFrame(tt)
	for _, p := range []float64{0.10, 0.99, 1} {
		job.Progress = p
		tt.Frame()
		now := snapFrame(tt)
		for text, r := range before.rects {
			if strings.Contains(text, "·") || text == fmt.Sprint(int(p*100+0.5)) || strings.HasPrefix(text, "9") || text == "10" || text == "99" || text == "100" {
				continue
			}
			sameBox(t, fmt.Sprintf("%q at %d%%", text, int(p*100+0.5)), r, now.rects[text])
		}
	}
}

// The Jobs card keeps its height while its history grows from idle to busy,
// so nothing below it moves.
func TestJobsCardKeepsItsHeightAsItsHistoryGrows(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.GoSettings("")
	sys := &client.SystemInfo{}
	sys.Jobs.Workers = 2
	a.Store.Server.Read = time.Now()
	a.Store.Server.System = sys
	a.Store.Server.Active = []int{0, 0, 0, 2}
	tt := ui.NewTester(a.View, 1280, 1000)
	tt.Frame()
	idle := snapFrame(tt)
	a.Store.Server.Active = []int{0, 0, 2, 2}
	tt.Frame()
	if d := snapFrame(tt).layoutDiffers(idle); d != "" {
		t.Errorf("a second busy sample moves things: %s", d)
	}
}

// A button whose words change to a waiting word while its request is slow
// keeps its words centered where they were.
func TestWaitingWordsKeepTheirButtonWhereItWas(t *testing.T) {
	serving := &client.WrapperStatus{Ready: true, Regions: []string{}, LoginSupported: true,
		Managed: &client.ManagedWrapper{Mode: "serving", Accel: "kvm"}}
	// slowly shows the frame of the request leaving, then the one a moment
	// past statusDelay.
	slowly := func(tt *ui.Tester) {
		tt.Frame()
		tt.Advance(statusDelay + time.Millisecond)
	}
	t.Run("log in", func(t *testing.T) {
		a, _ := newTestApp(remoteSettings())
		a.GoSettings("account")
		a.Store.Server.Read, a.Store.Server.Wrapper = time.Now(), serving
		a.set.login.user, a.set.login.pass = "someone@example.com", "not-a-real-password"
		tt := liveTester(a)
		tt.Frame()
		before := findAny(t, tt, "Log in")
		a.Store.Login.Busy, a.Store.Login.Since = true, tt.Now()
		slowly(tt)
		keepsCenter(t, "Log in", before, findAny(t, tt, "Logging in…"))
	})
	t.Run("verify", func(t *testing.T) {
		a, _ := newTestApp(remoteSettings())
		a.GoSettings("account")
		a.Store.Server.Read, a.Store.Server.Wrapper = time.Now(), serving
		a.Store.Login.LoginID = "code"
		a.set.login.code = "123456"
		tt := liveTester(a)
		tt.Frame()
		before, over := findAny(t, tt, "Verify"), findAny(t, tt, "Start over")
		a.Store.Login.Busy, a.Store.Login.Since = true, tt.Now()
		slowly(tt)
		keepsCenter(t, "Verify", before, findAny(t, tt, "Verifying…"))
		// Start over turns into Cancel in the same box.
		keepsCenter(t, "Start over", over, findAny(t, tt, "Cancel"))
	})
	t.Run("reload", func(t *testing.T) {
		a, _ := newTestApp(remoteSettings())
		a.GoSettings("server")
		a.Store.Server.Read = time.Now()
		a.Store.Config.Values = map[string]any{}
		tt := liveTester(a)
		tt.Frame()
		before := findAny(t, tt, "Reload")
		a.Store.Config.Reloading, a.Store.Config.ReloadSince = true, tt.Now()
		slowly(tt)
		keepsCenter(t, "Reload", before, findAny(t, tt, "Reloading…"))
	})
	t.Run("add a hook", func(t *testing.T) {
		a, tt, _ := openHookSheet(t)
		before, cancel := findAny(t, tt, "Add"), findAny(t, tt, "Cancel")
		a.hookAsk.busy, a.hookAsk.since = true, tt.Now()
		slowly(tt)
		keepsCenter(t, "Add", before, findAny(t, tt, "Adding…"))
		sameBox(t, "Cancel", cancel, findAny(t, tt, "Cancel"))
	})
	t.Run("test a connection", func(t *testing.T) {
		a, _ := newTestApp(remoteSettings())
		a.GoSettings("connection")
		tt := liveTester(a)
		a.set.draft.RemoteURL = "http://nas:18570"
		tt.Frame()
		before := findAny(t, tt, "Test connection")
		a.set.testing, a.set.testFrom = true, tt.Now()
		slowly(tt)
		keepsCenter(t, "Test connection", before, findAny(t, tt, "Test connection", "Testing…"))
	})
}
