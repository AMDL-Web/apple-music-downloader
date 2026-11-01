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

// Each test here is one cause of a control that moves its neighbors when its
// state changes: an icon that comes and goes, a count that gains a digit,
// words that change to a longer waiting word.

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

func TestWaitingWordsKeepTheirButtonWhereItWas(t *testing.T) {
	serving := &client.WrapperStatus{Ready: true, Regions: []string{}, Accounts: []string{}, LoginSupported: true,
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
		sameBox(t, "Start over", over, findAny(t, tt, "Start over"))
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
