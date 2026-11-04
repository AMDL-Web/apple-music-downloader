package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The queue's hero and its row of recent covers carry no label saying what
// is plain from them: the bar and the percent say a download is under way,
// and the covers need no title.
func TestTheQueueHeroCarriesNoObviousLabels(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded && s.Now() != nil && len(s.Finished()) > 1 })
	a.Go(ScreenQueue)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(0.5)
	tt.Advance(time.Second)
	j := a.heroJob()
	if j == nil {
		t.Fatal("the queue has no hero")
	}
	if _, ok := tt.Find("Open " + j.Name()); !ok {
		t.Fatalf("no hero for %s: %q", j.Name(), tt.Texts())
	}
	covers := 0
	for _, f := range s.Finished() {
		if f.ID == j.ID {
			continue
		}
		if _, ok := tt.Find(f.Name()); ok {
			covers++
		}
	}
	if covers == 0 {
		t.Fatalf("no recent covers: %q", tt.Texts())
	}
	// Labels show in capitals (label); the sidebar's card, which says
	// "Downloading now" as its name, is not one.
	for _, x := range []string{"DOWNLOADING NOW", "RECENTLY FINISHED"} {
		if tt.HasText(x) {
			t.Errorf("the queue shows the label %q", x)
		}
	}
}

// The queue hero's words line up with its cover at the top and the bottom:
// the title's box starts where the cover does and the progress line's box
// ends where it does, in a wide window and a narrow one. The sidebar, whose
// card names the download too, is hidden.
func TestTheQueueHerosWordsLineUpWithItsCover(t *testing.T) {
	for _, size := range [][2]int{{1280, 800}, {820, 560}} {
		a, run := demoApp(t)
		s := a.Store
		run(func() bool { return s.Loaded && s.Now() != nil })
		a.Settings.HideSidebar = true
		a.Go(ScreenQueue)
		tt := ui.NewTester(a.View, size[0], size[1])
		tt.SetScale(0.5)
		tt.Advance(time.Second)
		j := a.heroJob()
		if j == nil {
			t.Fatal("the queue has no hero")
		}
		cover, ok := tt.Find("Open " + j.Name())
		title, ok2 := tt.Find(j.Name())
		progress, ok3 := tt.Find(a.heroProgressText(j))
		if !ok || !ok2 || !ok3 {
			t.Fatalf("%v: no hero cover, title or progress line: %q", size, tt.Texts())
		}
		if d := title.Y - cover.Y; d < -4 || d > 4 {
			t.Errorf("%v: the title's top is %.0f DIP from the cover's %v (title %v)", size, d, cover, title)
		}
		if d := progress.Y + progress.H - (cover.Y + cover.H); d < -4 || d > 4 {
			t.Errorf("%v: the progress line's bottom is %.0f DIP from the cover's %v (progress %v)", size, d, cover, progress)
		}
	}
}
