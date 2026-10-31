package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/domain"
	"amdl/internal/progress"
)

// longActivity is a retry line as the backend wrote it before its messages
// got plainer: one long token the column has to break.
const longActivity = `Track 2: retrying — lyrics attempt failed: wrapper /lyrics: Get "http://127.0.0.1:12340/lyrics?adamId=1858184007&storefront=cn&language=zh-Hans-CN": context deadline exceeded`

// detailFixture is a 13-track album under way: finished and skipped tracks,
// two adjacent downloads, one decrypting, one resolving, waiting and failed
// ones, and an activity log with a long retry line.
func detailFixture(now time.Time) *store.Job {
	started := now.Add(-3 * time.Minute)
	j := &store.Job{Job: domain.Job{ID: "job", Type: "album", Status: domain.JobRunning, Title: "Midnight Archive", ArtistName: "The Long Signal",
		TotalItems: 13, CompletedItems: 5, SkippedItems: 1, FailedItems: 1, CreatedAt: started, StartedAt: &started,
		ArtworkBgColor: "2b2350", ArtworkTextColor1: "f4eefc", ArtworkTextColor2: "d9cff0", ArtworkTextColor3: "c4b6e8", ArtworkTextColor4: "b0a2d6"}}
	item := func(i int, st domain.ItemStatus, p domain.ItemProgress) domain.JobItem {
		return domain.JobItem{ID: fmt.Sprintf("it%d", i), Index: i, Title: fmt.Sprintf("Track Number %d", i), Status: st, Progress: p,
			Codec: "alac", BitDepth: 24, SampleRate: 96000}
	}
	done := domain.ItemProgress{Resolved: true, Download: 1, Decrypt: 1, Remuxed: true, Verified: true, Tagged: true, Saved: true}
	for i := 1; i <= 5; i++ {
		it := item(i, domain.ItemCompleted, done)
		it.FileSize = 92_000_000
		j.Items = append(j.Items, it)
	}
	j.Items = append(j.Items,
		item(6, domain.ItemSkipped, domain.ItemProgress{Resolved: true}),
		item(7, domain.ItemDownloading, domain.ItemProgress{Resolved: true, Download: 0.42}),
		item(8, domain.ItemDownloading, domain.ItemProgress{Resolved: true, Download: 0.18}),
		item(9, domain.ItemDecrypting, domain.ItemProgress{Resolved: true, Download: 1, Decrypt: 0.6}),
		item(10, domain.ItemResolving, domain.ItemProgress{}),
		item(11, domain.ItemWaitingDownload, domain.ItemProgress{Resolved: true}),
		item(12, domain.ItemFailed, domain.ItemProgress{Resolved: true}),
		item(13, domain.ItemQueued, domain.ItemProgress{}),
	)
	j.Items[11].StatusMessage = "wrapper /lyrics: context deadline exceeded"
	j.Items[1].Attempt, j.Items[1].MaxAttempts = 2, 3
	j.Activity = []store.Activity{
		{At: now.Add(-5 * time.Second), Text: longActivity, Kind: store.ActivityWarn},
		{At: now.Add(-20 * time.Second), Text: "Track 12 failed — wrapper /lyrics: context deadline exceeded", Kind: store.ActivityFailed},
		{At: now.Add(-40 * time.Second), Text: "Track 5 saved · 92 MB", Kind: store.ActivityDone},
		{At: now.Add(-2 * time.Minute), Text: "Picked ALAC 24/96", Kind: store.ActivityInfo},
		{At: now.Add(-3 * time.Minute), Text: "Found 13 tracks", Kind: store.ActivityInfo},
		{At: now.Add(-3 * time.Minute), Text: "Started", Kind: store.ActivityInfo},
	}
	return j
}

// detailView is the main window's column showing j as the detail screen,
// without the sidebar: the backdrop, the title strip and the page.
func detailView(a *App, j *store.Job) func(c *ui.Context) {
	return func(c *ui.Context) {
		k := a.tokens(c)
		applyTheme(c, k)
		c.Root().Background(k.Ground)
		ui.Box(c).Fill().Background(k.Ground).Clip().Children(func() {
			paletteBackdrop(c, k, jobPalette(j, false))
			ui.Column(c.Key("content")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() {
				ui.Row(c).Height(52)
				ui.Column(c).Grow(1).MinHeight(0).Children(func() { a.detailJob(c, k, j) })
			})
		})
		a.scrollbars(c, k)
	}
}

// tickDetail moves the running tracks on, as their progress events do.
func tickDetail(j *store.Job, i int) {
	for n := range j.Items {
		it := &j.Items[n]
		switch it.Status {
		case domain.ItemDownloading:
			it.Progress.Download = float64((i*7+n*13)%100) / 100
		case domain.ItemDecrypting:
			it.Progress.Decrypt = float64((i*5+n*11)%100) / 100
		}
	}
}

func newDetailTester(t *testing.T, w, h int) (*App, *store.Job, *ui.Tester) {
	t.Helper()
	a, _ := newTestApp(remoteSettings())
	a.Settings.HideSidebar = true
	j := detailFixture(time.Now())
	return a, j, ui.NewTester(detailView(a, j), w, h)
}

func TestTrackStatesSayWhatRuns(t *testing.T) {
	for _, st := range []domain.ItemStatus{domain.ItemQueued, domain.ItemResolving, domain.ItemWaitingDownload, domain.ItemDownloading,
		domain.ItemWaitingDecrypt, domain.ItemDecrypting, domain.ItemRemuxing, domain.ItemTagging, domain.ItemSaving} {
		_, text, _ := stateLabel(&domain.JobItem{Status: st, Progress: domain.ItemProgress{Download: 0.42, Decrypt: 0.5}})
		if text == "Working" || len(text) > 9 {
			t.Errorf("%s says %q", st, text)
		}
	}
	if _, text, working := stateLabel(&domain.JobItem{Status: domain.ItemResolving}); text != "Preparing" || !working {
		t.Errorf("a resolving track says %q (working %v)", text, working)
	}
	if got := trackSub(&domain.JobItem{Status: domain.ItemResolving}); got == "" {
		t.Error("a resolving track has no line under its title")
	}
}

func TestStageLegendNeverOverlaps(t *testing.T) {
	for _, weights := range [][progress.StageCount]float64{
		progress.Weights,
		{0.04, 0.80, 0.04, 0.04, 0.03, 0.03, 0.02},
		{1. / 7, 1. / 7, 1. / 7, 1. / 7, 1. / 7, 1. / 7, 1. / 7},
		{0.01, 0.01, 0.94, 0.01, 0.01, 0.01, 0.01},
	} {
		for _, width := range []float32{160, 220, 300} {
			legend := stageLegend(width, weights)
			for i, l := range legend {
				// After the last segment comes the row's gap before the
				// state column.
				room := l.width + legendSpill
				if i < len(legend)-1 {
					room = l.width + stageGap
				}
				if w := float32(len(l.text)) * legendAdvance; w > room+0.01 {
					t.Errorf("weights %v at %v: %q is %v wide in %v", weights, width, l.text, w, room)
				}
				if l.text != "" && l.text != stageWords[i] && l.text != stageInitials[i] {
					t.Errorf("weights %v at %v: label %q", weights, width, l.text)
				}
				if l.text == stageInitials[i] && float32(len(stageWords[i]))*legendAdvance <= l.width {
					t.Errorf("weights %v at %v: %q shortened though %q fits", weights, width, l.text, stageWords[i])
				}
			}
		}
	}
	if got := stageLegend(300, progress.Weights)[progress.StageDownload].text; got != "Download" {
		t.Errorf("the download segment says %q", got)
	}
}

func TestActivityWrapsLongLines(t *testing.T) {
	const w = 1280
	_, _, tt := newDetailTester(t, w, 800)
	r, ok := tt.Find(longActivity)
	if !ok {
		t.Fatalf("no long activity line in %q", tt.Texts())
	}
	// The column's padding and the scroll gutter stay clear.
	if right := r.X + r.W; right > w-20-scrollGutter+0.5 {
		t.Errorf("the line runs to %v, past the column's text edge %v", right, w-20-scrollGutter)
	}
	if r.H < 40 {
		t.Errorf("the line is %v tall: not wrapped", r.H)
	}
}

func TestRunningRowsKeepTheRhythmAndStandApart(t *testing.T) {
	a, j, tt := newDetailTester(t, 1280, 1000)
	y := func(i int) float32 {
		r, ok := tt.Find(j.Items[i-1].Title)
		if !ok {
			t.Fatalf("no track %d in %q", i, tt.Texts())
		}
		return r.Y
	}
	idle, active := y(5)-y(4), y(8)-y(7)
	if idle != active {
		t.Errorf("an idle row is %v tall, a running one %v", idle, active)
	}
	// Between tracks 7 and 8, both running, a band of the list's ground
	// shows through: the highlights do not touch.
	img := tt.Image()
	k := &DarkTokens
	ground := detailGround(k)
	x := 72
	gap := 0
	for py := int(y(7)); py < int(y(8)); py++ {
		c := img.RGBAAt(x, py)
		if absDiff(c.R, ground.R) <= 2 && absDiff(c.G, ground.G) <= 2 && absDiff(c.B, ground.B) <= 2 {
			gap++
		}
	}
	if gap < 5 {
		t.Errorf("only %d rows of ground between two running rows", gap)
	}
	// A row turning idle keeps its place.
	before := y(9)
	j.Items[6].Status = domain.ItemCompleted
	tt.Frame()
	if after := y(9); after != before {
		t.Errorf("track 9 moved from %v to %v when track 7 finished", before, after)
	}
	_ = a
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func TestScrollingHoldsTheStripes(t *testing.T) {
	_, j, tt := newDetailTester(t, 1280, 800)
	tt.Advance(time.Second)
	if !tt.FramePending() {
		t.Fatal("running stripes ask for no frames")
	}
	r, _ := tt.Find(j.Items[0].Title)
	tt.Scroll(r.X+20, r.Y+4, 0, 40)
	if tt.FramePending() {
		t.Error("the stripes still move while the list scrolls")
	}
	tt.Advance(scrollHold + scrollFadeOut + 100*time.Millisecond)
	if !tt.FramePending() {
		t.Error("the stripes did not move again after scrolling")
	}
}
