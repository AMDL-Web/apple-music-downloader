package store

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"amdl/apps/desktop/internal/client"
	"amdl/internal/domain"
)

type recorder struct{ finished []string }

func (r *recorder) JobFinished(j *Job) { r.finished = append(r.finished, j.ID+":"+string(j.Status)) }

// newTestStore has no client, so it starts no goroutines: the apply methods
// run as the UI thread would run them.
func newTestStore(t *testing.T) (*Store, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := New(func(fn func()) { fn() }, rec)
	return s, rec
}

func at(min int) time.Time { return time.Date(2026, 10, 30, 10, min, 0, 0, time.Local) }

func TestItemStagesFollowStatus(t *testing.T) {
	cases := []struct {
		name string
		item domain.JobItem
		want float64
	}{
		// A skipped track only reports Resolved, yet nothing is left to do.
		{"skipped", domain.JobItem{Status: domain.ItemSkipped, Progress: domain.ItemProgress{Resolved: true}}, 1},
		// check_integrity off leaves Verified false on a finished track.
		{"completed unverified", domain.JobItem{Status: domain.ItemCompleted, Progress: domain.ItemProgress{
			Resolved: true, Download: 1, Decrypt: 1, Remuxed: true, Tagged: true, Saved: true}}, 1},
		{"queued", domain.JobItem{Status: domain.ItemQueued}, 0},
		// Half the download stage: 4 (resolve) + 56/2.
		{"half downloaded", domain.JobItem{Status: domain.ItemDownloading, Progress: domain.ItemProgress{Resolved: true, Download: 0.5}}, 0.32},
		// Tagging means remux and verify are behind it even with Verified false.
		{"tagging", domain.JobItem{Status: domain.ItemTagging, Progress: domain.ItemProgress{Resolved: true, Remuxed: true}}, 0.95},
	}
	for _, c := range cases {
		if got := ItemStages(c.item).Weighted(); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: weighted %.4f, want %.4f", c.name, got, c.want)
		}
	}
	if !Indeterminate(domain.JobItem{Status: domain.ItemDownloading}) {
		t.Error("a download without a fraction should be indeterminate")
	}
}

func TestSnapshotAndFeedKeepBackendOrder(t *testing.T) {
	s, _ := newTestStore(t)
	// A batch submitted together shares created_at and lists in submission order.
	s.applySnapshot(client.DownloadList{Downloads: []domain.Job{
		{ID: "b1", CreatedAt: at(5)}, {ID: "b2", CreatedAt: at(5)}, {ID: "old", CreatedAt: at(1), Status: domain.JobCompleted},
	}, StatusCounts: domain.JobStatusCounts{Completed: 1, Queued: 2, Total: 3}})
	s.applyMessage(domain.DownloadFeedMessage{Type: "download_upserted", Job: &domain.Job{ID: "new", CreatedAt: at(9), Status: domain.JobQueued},
		StatusCounts: domain.JobStatusCounts{Completed: 1, Queued: 3, Total: 4}})
	var ids []string
	for _, j := range s.Jobs {
		ids = append(ids, j.ID)
	}
	if fmt.Sprint(ids) != "[new b1 b2 old]" {
		t.Errorf("order %v", ids)
	}
	if s.Count(TabActive) != 3 || s.Count(TabAll) != 4 {
		t.Errorf("counts %+v", s.Counts)
	}
	s.applyMessage(domain.DownloadFeedMessage{Type: "download_deleted", JobID: "b1"})
	if s.Job("b1") != nil || len(s.Jobs) != 3 {
		t.Error("deleted job still listed")
	}
}

func TestNotifiesOnlyRunsSeenUnfinished(t *testing.T) {
	s, rec := newTestStore(t)
	s.applySnapshot(client.DownloadList{Downloads: []domain.Job{
		{ID: "done-before", Status: domain.JobCompleted}, {ID: "run", Status: domain.JobRunning},
	}})
	upsert := func(id string, st domain.JobStatus) {
		s.applyMessage(domain.DownloadFeedMessage{Type: "download_upserted", Job: &domain.Job{ID: id, Status: st}})
	}
	upsert("done-before", domain.JobCompleted) // a late motion-cover update of a finished job
	upsert("run", domain.JobCompleted)
	upsert("run", domain.JobCompleted) // the same state again
	// A per-job lifecycle event can carry the end before the feed does.
	s.applyEvent("run", domain.Event{ID: 99, Type: domain.EventJobFinished, Payload: mustJSON(domain.Job{ID: "run", Status: domain.JobCompleted})})
	upsert("run", domain.JobQueued) // retried: a new run
	upsert("run", domain.JobFailed)
	upsert("cancelled", domain.JobRunning)
	upsert("cancelled", domain.JobCancelled)
	if want := "[run:completed run:failed]"; fmt.Sprint(rec.finished) != want {
		t.Errorf("notified %v, want %s", rec.finished, want)
	}
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

func TestEventsOlderThanSnapshotOnlyLog(t *testing.T) {
	s, _ := newTestStore(t)
	s.applySnapshot(client.DownloadList{Downloads: []domain.Job{{ID: "j", Status: domain.JobRunning, TotalItems: 1}}})
	s.applyDetail("j", client.DownloadDetail{
		Job:         domain.Job{ID: "j", Status: domain.JobRunning, TotalItems: 1},
		Items:       []domain.JobItem{{ID: "i", Index: 1, Status: domain.ItemDecrypting, Progress: domain.ItemProgress{Resolved: true, Download: 1, Decrypt: 0.5}}},
		EventCursor: 10,
	})
	old := domain.JobItem{ID: "i", Index: 1, Status: domain.ItemDownloading, Progress: domain.ItemProgress{Resolved: true, Download: 0.2}}
	s.applyEvent("j", domain.Event{ID: 5, ItemID: "i", Type: domain.EventItemProgress, Payload: mustJSON(old)})
	if got := s.Job("j").Items[0].Status; got != domain.ItemDecrypting {
		t.Fatalf("history overwrote the snapshot: status %s", got)
	}
	done := domain.JobItem{ID: "i", Index: 1, Status: domain.ItemCompleted, FileSize: 41_000_000}
	s.applyEvent("j", domain.Event{ID: 11, ItemID: "i", Type: domain.EventItemCompleted, Payload: mustJSON(done)})
	j := s.Job("j")
	if j.Items[0].Status != domain.ItemCompleted || j.Progress != 1 {
		t.Fatalf("item %+v, progress %v", j.Items[0], j.Progress)
	}
	if len(j.Activity) != 1 || j.Activity[0].Text != "Track 1 saved · 41.0 MB" {
		t.Errorf("activity %+v", j.Activity)
	}
}

func TestNowIsTheLatestStartedRunningJob(t *testing.T) {
	s, _ := newTestStore(t)
	early, late := at(1), at(7)
	s.applySnapshot(client.DownloadList{Downloads: []domain.Job{
		{ID: "a", Status: domain.JobRunning, StartedAt: &early, CreatedAt: at(9)},
		{ID: "b", Status: domain.JobRunning, StartedAt: &late, CreatedAt: at(0)},
		{ID: "c", Status: domain.JobQueued, CreatedAt: at(10)},
	}})
	if n := s.Now(); n == nil || n.ID != "b" {
		t.Fatalf("now = %v", n)
	}
}

func TestSummaryCountsToday(t *testing.T) {
	s, _ := newTestStore(t)
	s.now = func() time.Time { return at(30) }
	yesterday, today := at(30).Add(-24*time.Hour), at(2)
	s.applySnapshot(client.DownloadList{Downloads: []domain.Job{
		{ID: "r", Status: domain.JobRunning, TotalItems: 4, CompletedItems: 1, SkippedItems: 1},
		{ID: "f1", Status: domain.JobFailed, FinishedAt: &today},
		{ID: "f0", Status: domain.JobFailed, FinishedAt: &yesterday},
		{ID: "q", Status: domain.JobQueued},
	}})
	sum := s.Summary()
	if sum.Running != 1 || sum.Queued != 1 || sum.TracksDone != 2 || sum.TracksTotal != 4 || sum.FailedToday != 1 {
		t.Errorf("summary %+v", sum)
	}
	if math.Abs(sum.Progress-0.5) > 1e-9 {
		t.Errorf("progress %v, want 0.5 from the counters", sum.Progress)
	}
}

func TestFindLinksAndRunningMatch(t *testing.T) {
	text := "IGOR https://music.apple.com/us/album/igor/1463409338, and\nhttps://music.apple.com/cn/album/1858184006 again https://music.apple.com/us/album/igor/1463409338"
	links := FindLinks(text)
	if fmt.Sprint(links) != "[https://music.apple.com/us/album/igor/1463409338 https://music.apple.com/cn/album/1858184006]" {
		t.Fatalf("links %q", links)
	}
	s, _ := newTestStore(t)
	s.applySnapshot(client.DownloadList{Downloads: []domain.Job{
		{ID: "j", Status: domain.JobRunning, Input: "https://music.apple.com/cn/album/x/1858184006?i=1858184010"},
	}})
	l := &Link{State: LinkReady, Lookup: client.Lookup{Type: "album", ID: "1858184006"}}
	if j := s.Running(l); j == nil || j.ID != "j" {
		t.Errorf("running = %v", j)
	}
	l.Lookup.ID = "1463409338"
	if s.Running(l) != nil {
		t.Error("an album nobody downloads matched")
	}
}

func TestQualityOverrides(t *testing.T) {
	if QualityDefault.overrides(false) != nil {
		t.Error("the default preset should keep the backend's settings")
	}
	o := QualityLossless.overrides(true)
	if o == nil || (*o.QualityPriority)[0] != "alac" || *o.ALACMaxSampleRate != 48000 || !*o.ForceOverwrite {
		t.Errorf("lossless %+v", o)
	}
}

func TestRemainingFromRate(t *testing.T) {
	var samples []sample
	samples = addSample(samples, at(0), 0.2)
	samples = addSample(samples, at(0).Add(10*time.Second), 0.4)
	if got := remaining(samples); got != 30*time.Second {
		t.Errorf("remaining %v, want 30s", got)
	}
	samples = addSample(samples, at(0).Add(11*time.Second), 0.1) // a retry went back
	if remaining(samples) != 0 {
		t.Error("a step back should drop the old rate")
	}
}
