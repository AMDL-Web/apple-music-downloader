package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
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

// detailView is the main window's column showing the album screen, without
// the sidebar: the backdrop, the title strip and the page.
func detailView(a *App) func(c *ui.Context) {
	return func(c *ui.Context) {
		k := a.tokens(c)
		applyTheme(c, k)
		c.Root().Background(k.Ground)
		w, _ := c.Size()
		ui.Box(c).Fill().Background(k.Ground).Clip().Children(func() {
			paletteBackdrop(k, a.pagePalette(c), false).Show(c)
			ui.Column(c.Key("content")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() {
				ui.Row(c).Height(52)
				ui.Column(c).Grow(1).MinHeight(0).Children(func() { a.albumScreen(c, k, w) })
			})
		})
		a.scrollbars(c, k)
	}
}

// openFixture puts j in the store, as the feed would, and opens its page.
func openFixture(a *App, j *store.Job) *store.Job {
	a.Store.ApplySnapshot(client.DownloadList{Downloads: []domain.Job{j.Job}})
	held := a.Store.Job(j.ID)
	held.Items, held.Activity = j.Items, j.Activity
	a.OpenJob(j.ID)
	return held
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
	j := openFixture(a, detailFixture(time.Now()))
	return a, j, stillTester(detailView(a), w, h)
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
	tt.SetPreferences(ui.Preferences{})
	tt.Advance(100 * time.Millisecond)
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

// A run's page shows its record's row of the queue, so its Delete removes
// the record, as the row's Remove from amdl does; a station run's goes alone.
func TestARunPagesDeleteRemovesItsRecord(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		job  domain.Job
		want string
	}{
		{domain.Job{ID: "r", Type: "album", Status: domain.JobCompleted, Title: "Glass Weather", ResourceKind: domain.ResourceAlbum, ResourceID: "7", CreatedAt: now, FinishedAt: &now},
			"Remove “Glass Weather” from amdl?"},
		{domain.Job{ID: "s", Type: "station", Status: domain.JobCompleted, Title: "Chill Radio", CreatedAt: now, FinishedAt: &now},
			"Delete “Chill Radio”?"},
	} {
		a, _ := newTestApp(remoteSettings())
		openFixture(a, &store.Job{Job: tc.job})
		tt := ui.NewTester(a.View, 1280, 800)
		tt.Frame()
		if err := tt.Click("Delete this download"); err != nil {
			t.Fatalf("%s: no Delete: %v in %q", tc.job.Type, err, tt.Texts())
		}
		tt.Frame()
		if !tt.HasText(tc.want) {
			t.Errorf("%s: no %q in %q", tc.job.Type, tc.want, tt.Texts())
		}
	}
}

// An album downloaded in part opens on the album's page, with its record's
// share and the rest to download, not on the page of the run that holds
// only the songs added by hand ("1 / 1 finished"); a whole album opens on
// its run as before.
func TestAPartAlbumOpensItsRecordsPageNotItsRun(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name string
		mark domain.ResourceMark
		part bool
	}{
		{"part", domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "7", State: domain.RecordComplete, TracksDone: 1, TracksWanted: 1}, true},
		{"whole", domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "7", State: domain.RecordComplete, TracksDone: 6, TracksWanted: 6, WantedAll: true}, false},
	} {
		a, _ := newTestApp(remoteSettings())
		a.Store.ApplySnapshot(client.DownloadList{Downloads: []domain.Job{{ID: "r", Type: "album", Status: domain.JobCompleted, Title: "Glass Weather",
			ResourceKind: domain.ResourceAlbum, ResourceID: "7", TotalItems: 1, CreatedAt: now, FinishedAt: &now}}})
		mark := tc.mark
		a.OpenAlbum(client.SearchHit{Type: "album", ID: "7", Name: "Glass Weather", URL: "https://music.apple.com/us/album/x/7", TrackCount: 6, Resource: &mark})
		tt := ui.NewTester(a.View, 1280, 800)
		tt.Frame()
		if got := tt.HasText("Delete this download"); got == tc.part {
			t.Errorf("%s: the run's page shows %v, want %v: %q", tc.name, got, !tc.part, tt.Texts())
		}
		if got := tt.HasText("1 of 6 downloaded"); got != tc.part {
			t.Errorf("%s: the share shows %v: %q", tc.name, got, tt.Texts())
		}
		if _, ok := tt.Find("Download the rest of Glass Weather"); ok != tc.part {
			t.Errorf("%s: the rest's button shows %v: %q", tc.name, ok, tt.Texts())
		}
	}
}

// A song's run holds that one song of its album; the album's other songs
// are not waiting with it, whatever else the page knows of them.
func TestASongRunsPageLeavesTheOtherSongsOfItsAlbumOffTheQueue(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Settings.HideSidebar = true
	now := time.Now()
	j := &store.Job{Job: domain.Job{ID: "run", Type: "song", Status: domain.JobRunning, Title: "Two", ArtistName: "Someone",
		ResourceKind: domain.ResourceAlbum, ResourceID: "7", TotalItems: 1, CreatedAt: now, StartedAt: &now},
		Items: []domain.JobItem{{ID: "i2", Index: 1, AdamID: "b", Title: "Two", Status: domain.ItemDownloading, Progress: domain.ItemProgress{Resolved: true, Download: 0.3}}}}
	openFixture(a, j)
	p := &a.Store.Search.Album
	p.Status, p.Album = store.SearchReady, client.Album{ID: "7", Title: "Album", ArtistName: "Someone", Tracks: []client.AlbumTrack{
		{ID: "a", TrackNumber: 1, Name: "One", DurationMS: 61_000},
		{ID: "b", TrackNumber: 2, Name: "Two", DurationMS: 62_000},
		{ID: "c", TrackNumber: 3, Name: "Three", DurationMS: 63_000},
	}}
	tt := ui.NewTester(detailView(a), 1280, 800)
	tt.Frame()
	if tt.HasText("Waiting") {
		t.Errorf("a song's run shows songs it does not hold as waiting: %q", tt.Texts())
	}
	if !tt.HasText("30%") {
		t.Errorf("the run's own song does not show its progress: %q", tt.Texts())
	}
}
