package ui

import (
	"strings"
	"testing"

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
