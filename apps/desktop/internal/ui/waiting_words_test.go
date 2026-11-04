package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/internal/domain"
)

// A wait is said once: a queued job's row by its Queued pill, or by its
// note where a compact row has no pill; a queued track by its Waiting.
func TestAWaitIsSaidOnce(t *testing.T) {
	a, run := demoApp(t)
	run(func() bool { return a.Store.Loaded && a.heroJob() != nil })
	a.Go(ScreenQueue)
	tt := ui.NewTester(a.View, 1280, 800)
	settle := func() {
		for range 10 {
			run(func() bool { return true })
			tt.Advance(50 * time.Millisecond)
		}
	}
	settle()
	queued := false
	for _, j := range a.Store.Jobs {
		queued = queued || j.Status == domain.JobQueued
	}
	if !queued {
		t.Fatal("the demo has no queued job")
	}
	texts := tt.Texts()
	if !slices.Contains(texts, "Queued") || slices.Contains(texts, "waiting for a worker") {
		t.Errorf("a queued row says its wait other than once, by its pill: %q", texts)
	}
	tt.SetSize(700, 800)
	settle()
	if texts := tt.Texts(); slices.Contains(texts, "Queued") || !slices.Contains(texts, "waiting for a worker") {
		t.Errorf("a compact queued row, with no pill, says its wait other than once, by its note: %q", texts)
	}
	tt.SetSize(1280, 800)
	a.OpenJob(a.heroJob().ID)
	settle()
	texts = tt.Texts()
	if !slices.Contains(texts, "Waiting") || slices.Contains(texts, "waiting for a download slot") {
		t.Errorf("a queued track says its wait other than once, by its state: %q", texts)
	}
}
