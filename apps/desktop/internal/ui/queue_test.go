package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

func TestTheRowMenuOpensOnAClickAndDeletesOnlyAFinishedJob(t *testing.T) {
	a, run := demoApp(t)
	a.Screen = ScreenQueue
	run(func() bool { return a.Store.Loaded })

	// A job under way is cancelled from its menu, never deleted.
	a.Tab = int(store.TabActive)
	tt := stillTester(a.View, 1280, 800)
	// A record's run is paused, not cancelled.
	cancels := strings.Count(strings.Join(tt.Texts(), "\n"), "Pause")
	if err := tt.Click("More actions"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Show details") || strings.Count(strings.Join(tt.Texts(), "\n"), "Pause") <= cancels || tt.HasText("Delete…") {
		t.Errorf("a running job's menu shows %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)

	// A downloaded record is removed from amdl, after its dialog.
	a.Tab = int(store.TabDone)
	tt.Frame()
	run(func() bool { return a.Store.Queue.List(store.TabDone).Status == store.SearchReady })
	tt.Frame()
	rows, _ := a.Store.QueueRows(store.TabDone)
	if len(rows) == 0 || rows[0].Record == nil {
		t.Fatalf("Done lists %+v", rows)
	}
	done := rows[0].Record
	name := recordName(*done)
	if err := tt.Click("More actions"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Show details", "Download again", "Copy link"} {
		if !tt.HasText(want) {
			t.Errorf("a downloaded record's menu has no %q: %q", want, tt.Texts())
		}
	}
	if tt.HasText("Check files") {
		t.Errorf("a downloaded record's menu offers a disk check: %q", tt.Texts())
	}
	if err := tt.Click("Remove from amdl…"); err != nil {
		t.Fatalf("a downloaded record's menu: %v in %q", err, tt.Texts())
	}
	if !tt.HasText("Remove “" + name + "” from amdl?") {
		t.Fatalf("no remove dialog in %q", tt.Texts())
	}
	if err := tt.Click("Remove"); err != nil {
		t.Fatal(err)
	}
	run(func() bool { return a.Store.Queue.Find(done.Ref) == nil })
	tt.Frame()
	if tt.HasText("Remove “" + name + "” from amdl?") {
		t.Errorf("the dialog stays after the removal: %q", tt.Texts())
	}
	if rows, _ := a.Store.QueueRows(store.TabDone); len(rows) > 0 && rows[0].Record != nil && rows[0].Record.Ref == done.Ref {
		t.Errorf("%s still lists after its removal", name)
	}
}

// Each finished row shows its quality, though the feed lists the jobs
// without the tracks it is read from: the row reads them, not only the
// latest download or one opened this run.
func TestEveryFinishedRowShowsItsQuality(t *testing.T) {
	a, run := demoApp(t)
	a.Screen = ScreenQueue
	run(func() bool { return a.Store.Loaded })
	a.Tab = int(store.TabDone)
	tt := stillTester(a.View, 1280, 1600)
	done := a.Store.Filter(store.TabDone, "")
	if len(done) < 2 {
		t.Fatalf("the demo has %d finished jobs, want the latest and one before it", len(done))
	}
	run(func() bool {
		tt.Frame()
		for _, j := range done {
			if jobQuality(j) == "" {
				return false
			}
		}
		return true
	})
}

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

// A short window keeps three rows of the queue's list in view: the hero
// takes its compact form, and the recent covers go, only as far as that
// needs. The compact cover lines up with the words beside it at the top
// and the bottom, and a page wide enough keeps the percent with Pause and
// Details.
func TestAShortWindowKeepsThreeRowsOfTheQueue(t *testing.T) {
	a, run := demoApp(t)
	run(func() bool { return a.Store.Loaded && a.Store.Now() != nil })
	a.Go(ScreenQueue)
	tt := ui.NewTester(a.View, 1280, 800)
	find := func(s string) ui.Rect {
		t.Helper()
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("no %q: %q", s, tt.Texts())
		}
		return r
	}
	for _, sz := range [][2]int{{1280, 800}, {1280, 690}, {820, 560}} {
		tt.SetSize(sz[0], sz[1])
		for range 10 {
			run(func() bool { return true })
			tt.Advance(50 * time.Millisecond)
		}
		// The third job of Up next.
		if r := find("夏夜信号"); r.Y+r.H > float32(sz[1]) {
			t.Errorf("%v: the third row ends at %.0f, past the window", sz, r.Y+r.H)
		}
		find("Pause")
		find("Details")
		cover := find("Open " + a.Store.Now().Name())
		if cover.X > 300 {
			t.Errorf("%v: the hero's cover is not at its left: %v", sz, cover)
		}
		if sz[1] >= 690 {
			if cover.H != heroCover {
				t.Errorf("%v: the hero's cover is %v, not the full %d", sz, cover, heroCover)
			}
			continue
		}
		// The hero has no label: its title is the top of its words, and
		// the first element showing the job's name.
		top, bottom := find(a.Store.Now().Name()), find(a.heroProgressText(a.Store.Now()))
		if d := top.Y - cover.Y; d < -1 || d > 1 {
			t.Errorf("%v: the compact cover's top is at %.0f, the words' at %.0f", sz, cover.Y, top.Y)
		}
		if d := bottom.Y + bottom.H - cover.Y - cover.H; d < -1 || d > 1 {
			t.Errorf("%v: the compact cover's bottom is at %.0f, the words' at %.0f", sz, cover.Y+cover.H, bottom.Y+bottom.H)
		}
	}
}

// In the light theme the hero's light ends inside the queue's bar, by an
// inward ramp, not at a hard line along its top edge: down a column clear
// of the bar's words, no row differs from the one above by more than a
// few levels.
func TestTheQueueBarLetsTheLightFadeIn(t *testing.T) {
	a, run := demoApp(t)
	a.Theme, a.FlatBackdrop = "light", false
	run(func() bool { return a.Store.Loaded && a.Store.Now() != nil })
	a.Go(ScreenQueue)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetScale(1)
	for range 20 {
		run(func() bool { return true })
		tt.Advance(100 * time.Millisecond)
	}
	all, ok := tt.Find("All")
	if !ok {
		t.Fatalf("no filters: %q", tt.Texts())
	}
	img := tt.Image()
	lum := func(x, y int) float32 {
		o := img.PixOffset(x, y)
		return 0.2126*float32(img.Pix[o]) + 0.7152*float32(img.Pix[o+1]) + 0.0722*float32(img.Pix[o+2])
	}
	for _, x := range []int{1000, 1150, 1260} {
		for y := int(all.Y) - 40; y < int(all.Y+all.H)+20; y++ {
			if d := lum(x, y+1) - lum(x, y); d > 4 || d < -4 {
				t.Errorf("at x %d, row %d differs from row %d by %.0f levels: a hard edge", x, y+1, y, d)
				break
			}
		}
	}
}
