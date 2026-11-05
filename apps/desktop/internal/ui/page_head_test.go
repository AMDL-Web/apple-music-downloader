package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/format"
)

// The head's figures stand in four equal columns whatever the state, the
// fourth always the tracks, and the kicker is the type alone.
func TestThePageHeadKeepsItsFourColumnsInEveryState(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	var states [][4]string
	var xs [][4]float32
	look := func(name string, want [4]string) {
		t.Helper()
		tt := stillTester(a.View, 1280, 800)
		tt.Frame()
		var x [4]float32
		for i, w := range want {
			r, ok := tt.Find(w)
			if !ok {
				t.Fatalf("%s: no figure %q in %q", name, w, tt.Texts())
			}
			x[i] = r.X
		}
		for _, text := range tt.Texts() {
			if l := strings.ToLower(text); strings.HasPrefix(l, "album") && strings.Contains(l, "tracks") {
				t.Errorf("%s: the kicker counts the tracks: %q", name, text)
			}
		}
		states, xs = append(states, want), append(xs, x)
	}

	// No download: what the album is.
	a.Go(ScreenQueue)
	a.OpenAlbum(demoAlbum)
	run(func() bool { return s.Search.Album.Status == store.SearchReady })
	look("none", [4]string{"RELEASED", "GENRE", "LENGTH", "TRACKS"})

	// Running: what is live.
	a.Back()
	live := jobOf(s, "", domain.JobRunning)
	if live == nil {
		t.Fatal("the demo has no running download")
	}
	a.OpenJob(live.ID)
	look("running", [4]string{"SIZE", "SPEED", "LEFT", "TRACKS"})

	// Completed: what it made.
	a.Back()
	var done *store.Job
	for _, j := range s.Jobs {
		if j.Status == domain.JobCompleted && j.ResourceKind == domain.ResourceAlbum && j.StartedAt != nil && j.FinishedAt != nil {
			done = j
			break
		}
	}
	if done == nil {
		t.Fatal("the demo has no completed album download")
	}
	a.OpenJob(done.ID)
	look("completed", [4]string{"SIZE", "TOTAL TIME", "REGION", "TRACKS"})

	for i := 1; i < len(xs); i++ {
		if xs[i] != xs[0] {
			t.Errorf("the columns move between states: %v at %v, %v at %v", states[0], xs[0], states[i], xs[i])
		}
	}
}

// A record that is whole with no run of it in the store (the feed lists a
// page of runs) shows the completed figures at once and fills in its size and
// time from the backend, in the same columns.
func TestAWholeRecordWithoutARunReadsItsSizeAndTime(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	done := jobOf(s, "920005", domain.JobCompleted)
	if done == nil {
		t.Fatal("the demo has no finished download of Orchard Lights")
	}
	h := client.SearchHit{Type: "album", ID: "920005", Name: "Orchard Lights", URL: "https://music.apple.com/us/album/orchard-lights/920005"}
	h.Resource = &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "920005", State: domain.RecordComplete,
		TracksDone: done.TotalItems, TracksWanted: done.TotalItems, WantedAll: true}
	s.Jobs = nil
	a.OpenAlbum(h)
	run(func() bool { return s.Search.Album.Status == store.SearchReady })
	tt := stillTester(a.View, 1280, 800)
	tt.Frame()
	for _, want := range []string{"SIZE", "TOTAL TIME", "REGION", "TRACKS"} {
		if !tt.HasText(want) {
			t.Fatalf("a whole record's head lacks %q: %q", want, tt.Texts())
		}
	}
	run(func() bool { a.Store.WatchRun(true); return s.Search.Album.Run.OK })
	tt.Frame()
	r := s.Search.Album.Run
	if r.Size <= 0 || r.Took <= 0 {
		t.Fatalf("the read gave size %d, time %v", r.Size, r.Took)
	}
	num, _, _ := strings.Cut(format.Bytes(r.Size), " ")
	if !tt.HasText(num) || !tt.HasText(format.Duration(r.Took)) {
		t.Errorf("the head does not show %s and %s: %q", num, format.Duration(r.Took), tt.Texts())
	}
}

// valueUnder is the text under the figure named name, in its column.
func valueUnder(t *testing.T, tt *ui.Tester, name string) string {
	t.Helper()
	label, ok := tt.Find(name)
	if !ok {
		t.Fatalf("no figure %q in %q", name, tt.Texts())
	}
	for _, text := range tt.Texts() {
		if r, ok := tt.Find(text); ok && r.X == label.X && r.Y > label.Y && r.Y < label.Y+40 {
			return text
		}
	}
	t.Fatalf("nothing under %q", name)
	return ""
}

// A region's name that does not fit its column gives way to its code, in
// every window: the figure's text never runs wider than the column (Texts
// reports the whole string, so the width is measured).
func TestTheRegionFigureFitsItsColumn(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	for _, page := range []float32{957, 668, 1100, 1500} {
		var v string
		var colW, w float32
		tt := stillTester(func(c *ui.Context) {
			l := pageHero(c, page)
			f := a.regionFigure(c, l, "us")
			size := float32(22)
			if l.compact {
				size = 18
			}
			v = f.value
			w, _ = c.MeasureText(0, ui.Span{Text: v, Font: MonoFont, Size: size, Weight: 600})
			colW = (min(float32(heroBand), l.width) - 3*heroGap) / 4
			ui.Box(c).Size(10, 10)
		}, 1280, 838)
		tt.Frame()
		if w > colW || (v != "United States" && v != "US") {
			t.Errorf("a %v wide page: the region reads %q, %v wide in a column %v wide", page, v, w, colW)
		}
	}
}

// A run that ended shows its share as its tracks figure counts it, whether
// or not its tracks have arrived.
func TestAnEndedRunsPercentIsItsTracksFigure(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	failed := jobOf(s, "920003", domain.JobFailed)
	if failed == nil {
		for _, j := range s.Jobs {
			if j.Status == domain.JobFailed && j.ResourceKind == domain.ResourceAlbum {
				failed = j
				break
			}
		}
	}
	if failed == nil {
		t.Fatal("the demo has no failed album download")
	}
	failed.Items = nil
	failed.TotalItems, failed.CompletedItems, failed.SkippedItems, failed.FailedItems = 10, 8, 0, 2
	a.OpenJob(failed.ID)
	tt := stillTester(a.View, 1189, 838)
	tt.Frame()
	if got := valueUnder(t, tt, "TRACKS"); got != "8 / 10" {
		t.Fatalf("the tracks figure reads %q", got)
	}
	if !tt.HasText("80%") {
		t.Errorf("the share is not 80%% like the tracks figure: %q", tt.Texts())
	}
}
