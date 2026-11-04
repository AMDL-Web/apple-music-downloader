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
