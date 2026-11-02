package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/hooks"
)

// finishJob writes a finished run onto jobID (status, error, started_at and
// finished_at) and takes it off the wait list, as a worker's finish would.
func finishJob(t *testing.T, m *Manager, jobID string, status domain.JobStatus) domain.Job {
	t.Helper()
	ctx := context.Background()
	job, err := m.store.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	delete(m.waiting.ids, jobID)
	m.mu.Unlock()
	started, finished := time.Now().UTC().Add(-time.Minute), time.Now().UTC().Add(-time.Second)
	job.Status, job.Error, job.StartedAt, job.FinishedAt = status, "boom", &started, &finished
	if err := m.store.UpdateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	return job
}

// retriedEvents decodes the job_retried payloads recorded for jobID.
func retriedEvents(t *testing.T, store *db.Store, jobID string) []map[string]any {
	t.Helper()
	evs, err := store.ListEventsAfterLimit(context.Background(), jobID, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, ev := range evs {
		if ev.Type != domain.EventJobRetried {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		out = append(out, payload)
	}
	return out
}

func countJobs(t *testing.T, store *db.Store) int {
	t.Helper()
	_, total, err := store.ListJobs(context.Background(), db.JobListFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// A resubmitted link whose job finished runs again in that job: same id,
// this request's overrides, created_at now, a fresh run, unfinished items
// queued and finished ones kept for the run to check against the files.
func TestResubmitRequeuesFinishedJobUnderItsID(t *testing.T) {
	for _, status := range []domain.JobStatus{domain.JobCompleted, domain.JobFailed, domain.JobCancelled} {
		t.Run(string(status), func(t *testing.T) {
			manager := newTestManager(t)
			ctx := context.Background()
			force := true
			now := time.Now().UTC()
			created := now.Add(-time.Hour)
			started, finished := now.Add(-time.Minute), now.Add(-time.Second)
			jobID := "job-" + string(status)
			if err := manager.store.CreateJob(ctx, domain.Job{
				ID: jobID, Input: "album|us|1", Type: "album", Storefront: "us", CanonicalKey: "album:us:1",
				Overrides: &config.DownloadOverrides{ForceOverwrite: &force}, Status: status, Error: "boom",
				StartedAt: &started, FinishedAt: &finished, CreatedAt: created, UpdatedAt: finished,
			}); err != nil {
				t.Fatal(err)
			}
			for i, itemStatus := range []domain.ItemStatus{domain.ItemCompleted, domain.ItemFailed} {
				item := domain.JobItem{ID: jobID + "-item-" + string(rune('a'+i)), JobID: jobID, AdamID: "s", Index: i + 1, Status: itemStatus, OutputPath: "/x", CreatedAt: now, UpdatedAt: now}
				if err := manager.store.CreateItem(ctx, item); err != nil {
					t.Fatal(err)
				}
			}

			before := time.Now().UTC()
			resp := manager.SubmitBatch(ctx, []string{"album|us|1"}, nil)
			got := resp.Results[0]
			if got.Status != domain.SubmitAccepted || !got.Requeued || got.Job == nil || got.Job.ID != jobID || resp.Accepted != 1 {
				t.Fatalf("resubmit = %+v, want accepted and requeued under %s", resp, jobID)
			}
			job, err := manager.store.GetJob(ctx, jobID)
			if err != nil {
				t.Fatal(err)
			}
			if job.Status != domain.JobQueued || job.Error != "" || job.StartedAt != nil || job.FinishedAt != nil || job.Overrides != nil {
				t.Fatalf("requeued job = %+v, want a queued fresh run without the old overrides", job)
			}
			if job.CreatedAt.Before(before.Add(-time.Second)) || job.CreatedAt.Equal(created) {
				t.Fatalf("created_at = %v, want the resubmit time (>= %v)", job.CreatedAt, before)
			}
			if job.CompletedItems != 1 || job.FailedItems != 0 {
				t.Fatalf("items completed=%d failed=%d, want the finished item kept and the failed one queued", job.CompletedItems, job.FailedItems)
			}
			if !isWaiting(manager, jobID) {
				t.Fatal("requeued job is not on the wait list")
			}
			if total := countJobs(t, manager.store); total != 1 {
				t.Fatalf("jobs = %d, want the one row", total)
			}
			payloads := retriedEvents(t, manager.store, jobID)
			if len(payloads) != 1 || payloads[0]["cause"] != string(domain.RequeueResubmit) || payloads[0]["force_overwrite"] != false || payloads[0]["status"] != string(domain.JobQueued) {
				t.Fatalf("job_retried payloads = %+v, want one with cause resubmit and force_overwrite false", payloads)
			}
		})
	}
}

// Resubmitting while the job is queued or running, or while its previous run
// is still winding down, answers duplicate_active with the job's id.
func TestResubmitWhileActiveOrFinalizingIsDuplicate(t *testing.T) {
	manager := newTestManager(t)
	ctx := context.Background()
	jobID := manager.SubmitBatch(ctx, []string{"song|us|7"}, nil).Results[0].Job.ID

	for _, status := range []domain.JobStatus{domain.JobQueued, domain.JobRunning} {
		job, err := manager.store.GetJob(ctx, jobID)
		if err != nil {
			t.Fatal(err)
		}
		job.Status = status
		if err := manager.store.UpdateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
		got := manager.SubmitBatch(ctx, []string{"song|us|7"}, nil).Results[0]
		if got.Status != domain.SubmitDuplicateActive || got.ExistingJobID != jobID || got.Requeued {
			t.Fatalf("resubmit while %s = %+v, want duplicate_active for %s", status, got, jobID)
		}
	}

	finishJob(t, manager, jobID, domain.JobCompleted)
	for _, mark := range []func(bool){
		func(on bool) {
			if on {
				manager.cancels[jobID] = func() {}
			} else {
				delete(manager.cancels, jobID)
			}
		},
		func(on bool) {
			if on {
				manager.finalizing[jobID] = true
			} else {
				delete(manager.finalizing, jobID)
			}
		},
	} {
		manager.mu.Lock()
		mark(true)
		manager.mu.Unlock()
		got := manager.SubmitBatch(ctx, []string{"song|us|7"}, nil).Results[0]
		manager.mu.Lock()
		mark(false)
		manager.mu.Unlock()
		if got.Status != domain.SubmitDuplicateActive || got.ExistingJobID != jobID {
			t.Fatalf("resubmit while finalizing = %+v, want duplicate_active for %s", got, jobID)
		}
	}
	if payloads := retriedEvents(t, manager.store, jobID); len(payloads) != 0 {
		t.Fatalf("job_retried payloads = %+v, want none from refused resubmits", payloads)
	}
	got := manager.SubmitBatch(ctx, []string{"song|us|7"}, nil).Results[0]
	if !got.Requeued || got.Job.ID != jobID {
		t.Fatalf("resubmit after finalize = %+v, want requeued %s", got, jobID)
	}
}

// A resubmit waits for the previous run's terminal hook, as Delete does.
func TestResubmitWaitsForPendingHooks(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer close(release)

	manager := newTestManager(t)
	dispatcher := hooks.NewDispatcher(hooks.Config{Enabled: true, Entries: []hooks.Entry{
		{Name: "slow", Type: "webhook", Events: []string{"job_finished"}, URL: server.URL},
	}}, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)
	ctx := context.Background()
	jobID := manager.SubmitBatch(ctx, []string{"album|us|9"}, nil).Results[0].Job.ID
	job := finishJob(t, manager, jobID, domain.JobCompleted)
	dispatcher.Dispatch("job_finished", job, nil)
	if !manager.HooksPending(jobID) {
		t.Fatal("hook not pending")
	}

	got := manager.SubmitBatch(ctx, []string{"album|us|9"}, nil).Results[0]
	if got.Status != domain.SubmitDuplicateActive || got.ExistingJobID != jobID {
		t.Fatalf("resubmit with a pending hook = %+v, want duplicate_active for %s", got, jobID)
	}
}

// A retry waits for the previous run's terminal hook too: the hook may still
// be working on the files a new run would write. It answers ErrJobFinalizing
// (409 job_finalizing), and goes through once the hook has ended.
func TestRetryWaitsForPendingHooks(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	var once sync.Once
	releaseHook := func() { once.Do(func() { close(release) }) }
	defer releaseHook()

	manager := newTestManager(t)
	dispatcher := hooks.NewDispatcher(hooks.Config{Enabled: true, Entries: []hooks.Entry{
		{Name: "slow", Type: "webhook", Events: []string{"job_failed"}, URL: server.URL},
	}}, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)
	ctx := context.Background()
	jobID := manager.SubmitBatch(ctx, []string{"album|us|9"}, nil).Results[0].Job.ID
	job := finishJob(t, manager, jobID, domain.JobFailed)
	dispatcher.Dispatch("job_failed", job, nil)
	if !manager.HooksPending(jobID) {
		t.Fatal("hook not pending")
	}

	if err := manager.Retry(ctx, jobID); !errors.Is(err, ErrJobFinalizing) {
		after, _ := manager.store.GetJob(ctx, jobID)
		t.Fatalf("Retry with a pending terminal hook = %v (job now %s), want ErrJobFinalizing", err, after.Status)
	}
	if after, err := manager.store.GetJob(ctx, jobID); err != nil || after.Status != domain.JobFailed {
		t.Fatalf("job after the refused retry = %s, %v; want failed", after.Status, err)
	}

	releaseHook()
	deadline := time.Now().Add(5 * time.Second)
	for manager.HooksPending(jobID) {
		if time.Now().After(deadline) {
			t.Fatal("hook still pending 5s after its webhook answered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := manager.Retry(ctx, jobID); err != nil {
		t.Fatalf("Retry once the hook ended = %v", err)
	}
}

// Retry and a resubmit share the requeue but differ where a user can tell:
// the job_retried cause, created_at, the overrides and the job_queued hook.
func TestRetryAndResubmitRecordTheirCause(t *testing.T) {
	var queuedHooks atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queuedHooks.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	manager := newTestManager(t)
	dispatcher := hooks.NewDispatcher(hooks.Config{Enabled: true, Entries: []hooks.Entry{
		{Name: "on-queued", Type: "webhook", Events: []string{"job_queued"}, URL: server.URL},
	}}, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)
	ctx := context.Background()
	force := true
	jobID := manager.SubmitBatch(ctx, []string{"album|us|5"}, &config.DownloadOverrides{ForceOverwrite: &force}).Results[0].Job.ID
	waitHooks := func(want int32) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && queuedHooks.Load() < want {
			time.Sleep(10 * time.Millisecond)
		}
		// Give a stray extra delivery a moment to show.
		time.Sleep(50 * time.Millisecond)
		if got := queuedHooks.Load(); got != want {
			t.Fatalf("job_queued hook calls = %d, want %d", got, want)
		}
	}
	waitHooks(1)

	failed := finishJob(t, manager, jobID, domain.JobFailed)
	if err := manager.Retry(ctx, jobID); err != nil {
		t.Fatal(err)
	}
	retried, err := manager.store.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if !retried.CreatedAt.Equal(failed.CreatedAt) || retried.Overrides == nil || !*retried.Overrides.ForceOverwrite {
		t.Fatalf("retried job = %+v, want created_at %v and the stored overrides kept", retried, failed.CreatedAt)
	}
	waitHooks(1)

	finishJob(t, manager, jobID, domain.JobCompleted)
	if got := manager.SubmitBatch(ctx, []string{"album|us|5"}, &config.DownloadOverrides{ForceOverwrite: &force}).Results[0]; !got.Requeued {
		t.Fatalf("resubmit = %+v, want requeued", got)
	}
	waitHooks(2)
	dispatcher.Shutdown(context.Background())

	payloads := retriedEvents(t, manager.store, jobID)
	if len(payloads) != 2 {
		t.Fatalf("job_retried payloads = %+v, want two", payloads)
	}
	if payloads[0]["cause"] != string(domain.RequeueRetry) || payloads[0]["force_overwrite"] != true {
		t.Fatalf("retry payload = %+v, want cause retry with the stored force_overwrite", payloads[0])
	}
	if payloads[1]["cause"] != string(domain.RequeueResubmit) || payloads[1]["force_overwrite"] != true {
		t.Fatalf("resubmit payload = %+v, want cause resubmit with this request's force_overwrite", payloads[1])
	}
}

// Every station submission draws fresh tracks, so it gets a new job even
// when an earlier one finished.
func TestStationResubmitCreatesNewJob(t *testing.T) {
	manager := newTestManager(t)
	ctx := context.Background()
	first := manager.SubmitBatch(ctx, []string{"station|us|ra.1"}, nil).Results[0].Job.ID
	finishJob(t, manager, first, domain.JobCompleted)
	got := manager.SubmitBatch(ctx, []string{"station|us|ra.1"}, nil).Results[0]
	if got.Status != domain.SubmitAccepted || got.Requeued || got.Job.ID == first {
		t.Fatalf("station resubmit = %+v, want a new job besides %s", got, first)
	}
	if total := countJobs(t, manager.store); total != 2 {
		t.Fatalf("jobs = %d, want 2", total)
	}
}

// An older database can hold several finished rows for one key: the newest
// is requeued and the others stay untouched.
func TestResubmitReusesNewestOfSeveralFinishedRows(t *testing.T) {
	manager := newTestManager(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	for i, id := range []string{"job-old", "job-new", "job-mid"} {
		created := base.Add(time.Duration([]int{0, 20, 10}[i]) * time.Minute)
		job := domain.Job{ID: id, Input: "in", Type: "album", Storefront: "us", CanonicalKey: "album:us:3", Status: domain.JobCompleted, CreatedAt: created, UpdatedAt: created}
		if err := manager.store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	got := manager.SubmitBatch(ctx, []string{"album|us|3"}, nil).Results[0]
	if !got.Requeued || got.Job.ID != "job-new" {
		t.Fatalf("resubmit = %+v, want job-new requeued", got)
	}
	for _, id := range []string{"job-old", "job-mid"} {
		job, err := manager.store.GetJob(ctx, id)
		if err != nil || job.Status != domain.JobCompleted {
			t.Fatalf("%s = %+v, %v, want untouched", id, job, err)
		}
	}
	if total := countJobs(t, manager.store); total != 3 {
		t.Fatalf("jobs = %d, want all 3 rows kept", total)
	}
}

func TestResubmitIntoFullQueueIsQueueFull(t *testing.T) {
	manager := newTestManager(t)
	ctx := context.Background()
	jobID := manager.SubmitBatch(ctx, []string{"album|us|4"}, nil).Results[0].Job.ID
	finishJob(t, manager, jobID, domain.JobCompleted)
	manager.capacity = 1
	manager.SubmitBatch(ctx, []string{"song|us|40"}, nil)
	got := manager.SubmitBatch(ctx, []string{"album|us|4"}, nil).Results[0]
	if got.Status != domain.SubmitQueueFull {
		t.Fatalf("resubmit into a full queue = %+v, want queue_full", got)
	}
	job, err := manager.store.GetJob(ctx, jobID)
	if err != nil || job.Status != domain.JobCompleted {
		t.Fatalf("job = %+v, %v, want it left completed", job, err)
	}
}

// Two resubmits of the same finished key at once requeue it exactly once;
// the other sees the requeued job as active.
func TestConcurrentResubmitsRequeueOnce(t *testing.T) {
	manager := newTestManager(t)
	ctx := context.Background()
	jobID := manager.SubmitBatch(ctx, []string{"album|us|8"}, nil).Results[0].Job.ID
	finishJob(t, manager, jobID, domain.JobCompleted)

	const n = 8
	results := make([]domain.SubmitResult, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = manager.SubmitBatch(ctx, []string{"album|us|8"}, nil).Results[0]
		}()
	}
	close(start)
	wg.Wait()

	requeued := 0
	for _, r := range results {
		switch {
		case r.Requeued && r.Job.ID == jobID:
			requeued++
		case r.Status == domain.SubmitDuplicateActive && r.ExistingJobID == jobID:
		default:
			t.Fatalf("result = %+v, want requeued or duplicate_active for %s", r, jobID)
		}
	}
	if requeued != 1 {
		t.Fatalf("requeued = %d, want exactly 1: %+v", requeued, results)
	}
	if payloads := retriedEvents(t, manager.store, jobID); len(payloads) != 1 {
		t.Fatalf("job_retried events = %d, want 1", len(payloads))
	}
	if total := countJobs(t, manager.store); total != 1 {
		t.Fatalf("jobs = %d, want 1", total)
	}
}

// A job deleted before the resubmit leaves nothing to requeue: the link gets
// a new job.
func TestResubmitAfterDeleteCreatesNewJob(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager := NewManager(store, events.NewHub(), keyedProcessor{}, 1, slog.Default())
	ctx := context.Background()
	jobID := manager.SubmitBatch(ctx, []string{"album|us|6"}, nil).Results[0].Job.ID
	finishJob(t, manager, jobID, domain.JobCompleted)
	if err := manager.Delete(ctx, jobID); err != nil {
		t.Fatal(err)
	}
	got := manager.SubmitBatch(ctx, []string{"album|us|6"}, nil).Results[0]
	if got.Status != domain.SubmitAccepted || got.Requeued || got.Job.ID == jobID {
		t.Fatalf("resubmit after delete = %+v, want a new job", got)
	}
}
