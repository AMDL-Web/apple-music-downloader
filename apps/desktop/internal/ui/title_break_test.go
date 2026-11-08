package ui

import (
	"strings"
	"testing"
	"time"

	"amdl/internal/domain"

	"github.com/egoist/mygo/ui"
)

// A title too wide for one line breaks before the qualifiers it ends in,
// or after its " - ", where both lines fit; without such a place, where
// its two lines are most even; one that fits, or that two lines cannot
// hold, is left alone.
func TestALongTitleBreaksBeforeItsQualifiers(t *testing.T) {
	type row struct{ title, want string }
	var rows []row
	var evenA, evenB, room float32
	var even string
	ui.NewTester(func(c *ui.Context) {
		w := func(s string) float32 { return titleWidth(c, s, 42) }
		check := func(title string, width float32, want string) {
			rows = append(rows, row{title, titleBreak(c, title, 42, width) + "|" + want})
		}
		const love = "First Love (Remastered 2014)"
		check(love, w(love)+4, love)
		check(love, w(love)*0.8, "First Love\n(Remastered 2014)")
		const story = "nameless story - 「とある科学の超電磁砲T」ED"
		check(story, max(w("nameless story -"), w("「とある科学の超電磁砲T」ED"))+2, "nameless story -\n「とある科学の超電磁砲T」ED")
		const moon = "The Dark Side of the Moon (50th Anniversary) [Remastered]"
		check(moon, w("The Dark Side of the Moon (50th Anniversary)")+2, "The Dark Side of the Moon\n(50th Anniversary) [Remastered]")
		check(moon, w("The Dark")+2, moon)
		const mid = "Love (Is) Blind Tonight"
		room = w(mid) * 0.7
		even = titleBreak(c, mid, 42, room)
		if a, b, ok := strings.Cut(even, "\n"); ok {
			evenA, evenB = w(a), w(b)
		}
	}, 400, 300).Frame()
	for _, r := range rows {
		got, want, _ := strings.Cut(r.want, "|")
		if got != want {
			t.Errorf("%s: %q, want %q", r.title, got, want)
		}
	}
	if !strings.Contains(even, "\n") || evenA > room+1 || evenB > room+1 || min(evenA, evenB) < max(evenA, evenB)/2 {
		t.Errorf("a title with nothing to break before is not in two even lines: %q (%.0f and %.0f in %.0f)", even, evenA, evenB, room)
	}
}

// Each queue hero breaks a long title before its qualifiers at rest, the
// latest download's too, which shows while nothing downloads: at every
// window width where its names are narrower than the title but hold each
// half, and at none where they hold the whole of it. The widths are the
// hero's own and the halves are measured in the face the test lays out in,
// the machine's, so the rule holds whatever fonts it has; the sweep
// crosses both kinds of width in any of them.
func TestBothQueueHeroesBreakALongTitleBeforeItsQualifiers(t *testing.T) {
	const title, first, second = "First Love (Remastered 2014)", "First Love", "(Remastered 2014)"
	const broken = first + "\n" + second
	var whole, half float32
	ui.NewTester(func(c *ui.Context) {
		whole = titleWidth(c, title, heroTitle)
		half = max(titleWidth(c, first, heroTitle), titleWidth(c, second, heroTitle))
	}, 400, 300).Frame()
	for _, latest := range []bool{false, true} {
		a, run := demoApp(t)
		s := a.Store
		run(func() bool { return s.Loaded && s.Now() != nil })
		hero := heroNow
		if latest {
			hero = heroLatest
			for _, j := range s.Jobs {
				if j.Status == domain.JobRunning || j.Status == domain.JobQueued {
					s.Cancel(j.ID)
				}
			}
			run(func() bool { return s.Now() == nil && len(s.Finished()) > 0 })
		}
		a.Go(ScreenQueue)
		a.heroJob().Title = title
		tt := ui.NewTester(a.View, 900, 838)
		var breaks, fits int
		for w := 900; w <= 1800; w += 20 {
			tt.SetSize(w, 838)
			for range 4 {
				tt.Advance(50 * time.Millisecond)
			}
			// A width within a pixel or two of the title or of a half is
			// left out: the layout's rounding decides those.
			names := a.heroNamesW[hero]
			_, shown := tt.Find(broken)
			switch {
			case names <= 0:
				t.Fatalf("latest %v at %d: the hero's names were not laid out", latest, w)
			case names > whole+2:
				fits++
				if shown {
					t.Errorf("latest %v at %d: a title that fits the hero's names (%.0f in %.0f) is broken", latest, w, whole, names)
				}
			case names < whole-2 && names > half+2:
				breaks++
				if !shown {
					t.Errorf("latest %v at %d: the title (%.0f, its halves at most %.0f, in %.0f) is not broken before its qualifiers; the window shows %q", latest, w, whole, half, names, tt.Texts())
				}
			}
		}
		if breaks == 0 || fits == 0 {
			t.Errorf("latest %v: no window width from 900 to 1800 has the hero's names %s (title %.0f, halves at most %.0f)", latest, map[bool]string{true: "between the halves and the title", false: "wider than the title"}[breaks == 0], whole, half)
		}
	}
}
