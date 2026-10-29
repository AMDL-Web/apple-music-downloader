package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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

func TestSubmitBatchStoresTokenOnlyForJobsThatNeedIt(t *testing.T) {
	manager := newTestManager(t)
	token := "batch-token"
	embedLyrics := false
	overrides := &config.DownloadOverrides{MediaUserToken: &token, EmbedLyrics: &embedLyrics}
	resp := manager.SubmitBatch(context.Background(), []string{
		"station|us|ra.1",
		"playlist|us|pl.u-private",
		"playlist|us|pl.editorial",
		"song|us|1",
	}, overrides)
	if resp.Accepted != 4 {
		t.Fatalf("accepted = %d, want 4: %+v", resp.Accepted, resp.Results)
	}
	want := []string{"batch-token", "batch-token", "", ""}
	for i, result := range resp.Results {
		job, err := manager.store.GetJob(context.Background(), result.Job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Overrides == nil || job.Overrides.EmbedLyrics == nil || *job.Overrides.EmbedLyrics {
			t.Fatalf("job %s (%s) lost non-token overrides: %+v", result.Job.ID, result.URL, job.Overrides)
		}
		got := ""
		if job.Overrides.MediaUserToken != nil {
			got = *job.Overrides.MediaUserToken
		}
		if got != want[i] {
			t.Fatalf("job %s (%s) token = %q, want %q", result.Job.ID, result.URL, got, want[i])
		}
		if want[i] == "" && job.Overrides.MediaUserToken != nil {
			t.Fatalf("job %s (%s) retained a token pointer for an unrelated job", result.Job.ID, result.URL)
		}
	}
	if overrides.MediaUserToken == nil || *overrides.MediaUserToken != token {
		t.Fatalf("SubmitBatch mutated the caller's overrides: %+v", overrides)
	}
}

func TestSubmitBatchDedupesWithinRequest(t *testing.T) {
	manager := newTestManager(t)
	ctx := context.Background()
	resp := manager.SubmitBatch(ctx, []string{
		"album|us|111",
		"song|us|222",
		"album|us|111", // same canonical key as the first entry
	}, nil)
	if len(resp.Results) != 3 {
		t.Fatalf("results = %+v, want 3", resp.Results)
	}
	if resp.Results[0].Status != domain.SubmitAccepted || resp.Results[1].Status != domain.SubmitAccepted {
		t.Fatalf("first two results = %+v, want accepted", resp.Results[:2])
	}
	if resp.Results[2].Status != domain.SubmitDuplicateInRequest || resp.Results[2].Error == nil || resp.Results[2].Error.Code != "duplicate_in_request" {
		t.Fatalf("third result = %+v, want duplicate_in_request with its error code", resp.Results[2])
	}
	if resp.Accepted != 2 || resp.Rejected != 1 {
		t.Fatalf("resp = %+v, want 2 accepted / 1 rejected", resp)
	}
}

func TestSubmitBatchRejectsActiveDuplicateButAllowsAfterCompletion(t *testing.T) {
	manager := newTestManager(t)
	ctx := context.Background()

	first := manager.SubmitBatch(ctx, []string{"song|us|222"}, nil)
	if first.Accepted != 1 {
		t.Fatalf("first submit = %+v, want 1 accepted", first)
	}
	jobID := first.Results[0].Job.ID

	second := manager.SubmitBatch(ctx, []string{"song|us|222"}, nil)
	if second.Results[0].Status != domain.SubmitDuplicateActive || second.Results[0].ExistingJobID != jobID ||
		second.Results[0].Error == nil || second.Results[0].Error.Code != "duplicate_active" {
		t.Fatalf("second submit = %+v, want duplicate_active for %s", second.Results[0], jobID)
	}

	done, err := manager.store.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	done.Status = domain.JobCompleted
	if err := manager.store.UpdateJob(ctx, done); err != nil {
		t.Fatal(err)
	}

	third := manager.SubmitBatch(ctx, []string{"song|us|222"}, nil)
	if third.Results[0].Status != domain.SubmitAccepted {
		t.Fatalf("third submit = %+v, want accepted after completion", third.Results[0])
	}
}

func TestSubmitBatchQueueFullMarksRemainingWithoutRollback(t *testing.T) {
	manager := newTestManager(t)
	manager.queue = make(chan string, 1)
	ctx := context.Background()

	resp := manager.SubmitBatch(ctx, []string{"song|us|1", "song|us|2", "song|us|3"}, nil)
	if resp.Results[0].Status != domain.SubmitAccepted {
		t.Fatalf("first = %+v, want accepted", resp.Results[0])
	}
	for _, result := range resp.Results[1:] {
		if result.Status != domain.SubmitQueueFull || result.Error == nil || result.Error.Code != "queue_full" {
			t.Fatalf("remaining = %+v, want queue_full with its error code", resp.Results[1:])
		}
	}
	if resp.Accepted != 1 || resp.Rejected != 2 {
		t.Fatalf("resp = %+v, want 1 accepted / 2 rejected", resp)
	}
}

func TestSubmitBatchInvalidURLReportsError(t *testing.T) {
	manager := newTestManager(t)
	resp := manager.SubmitBatch(context.Background(), []string{"bad:not-a-url"}, nil)
	got := resp.Results[0]
	if got.Status != domain.SubmitInvalid || got.Error == nil || got.Error.Code != "invalid_url" || got.Error.Message != "bad test url" || got.Error.Details != nil {
		t.Fatalf("result = %+v, want invalid with the processor's code and message", got)
	}
}

// A store failure is not a verdict on the URL: it is reported as status
// error with store_error, which tells the client to retry, and never as
// invalid, which tells it the input is bad.
func TestSubmitBatchStoreFailureIsErrorNotInvalid(t *testing.T) {
	manager := newTestManager(t)
	if err := manager.store.Close(); err != nil {
		t.Fatal(err)
	}
	resp := manager.SubmitBatch(context.Background(), []string{"song|us|1"}, nil)
	got := resp.Results[0]
	if got.Status != domain.SubmitServerError || got.Error == nil || got.Error.Code != "store_error" {
		t.Fatalf("result = %+v, want status error with store_error", got)
	}
	if resp.Accepted != 0 || resp.Rejected != 1 {
		t.Fatalf("resp = %+v, want 0 accepted / 1 rejected", resp)
	}
}

func TestRequestErrorSubmitErrorCarriesStorefrontDetails(t *testing.T) {
	err := &RequestError{Code: "unsupported_storefront", Message: "storefront \"jp\" is not supported", Storefront: "jp", SupportedStorefronts: []string{"us", "cn"}}
	got := err.SubmitError()
	raw, marshalErr := json.Marshal(got)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	want := `{"code":"unsupported_storefront","message":"storefront \"jp\" is not supported","details":{"storefront":"jp","supported_storefronts":["us","cn"]}}`
	if string(raw) != want {
		t.Fatalf("submit error = %s, want %s", raw, want)
	}
}

func TestJobQueuedDispatchesHook(t *testing.T) {
	var calls int32
	var mu sync.Mutex
	var gotEvent, gotStatus string
	var gotTotal, gotItems int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Event string `json:"event"`
			Job   struct {
				Status     string `json:"status"`
				TotalItems int    `json:"total_items"`
			} `json:"job"`
			Items []struct{} `json:"items"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		gotEvent = payload.Event
		gotStatus = payload.Job.Status
		gotTotal = payload.Job.TotalItems
		gotItems = len(payload.Items)
		mu.Unlock()
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	hooksCfg := hooks.Config{Enabled: true, Entries: []hooks.Entry{
		{Name: "on-queued", Type: "webhook", Events: []string{"job_queued"}, URL: server.URL},
	}}
	manager := NewManager(store, events.NewHub(), keyedProcessor{}, 1, slog.Default())
	dispatcher := hooks.NewDispatcher(hooksCfg, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)

	// Deliberately do NOT start workers: the creation hook fires from
	// SubmitBatch itself, so the job stays queued and only job_queued can fire.
	resp := manager.SubmitBatch(context.Background(), []string{"song|us|1"}, nil)
	if resp.Accepted != 1 {
		t.Fatalf("submit = %+v, want 1 accepted", resp)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&calls) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	dispatcher.Shutdown(context.Background())

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("webhook calls = %d, want 1 after job creation", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotEvent != "job_queued" {
		t.Fatalf("event = %q, want job_queued", gotEvent)
	}
	if gotStatus != string(domain.JobQueued) {
		t.Fatalf("job status = %q, want %q", gotStatus, domain.JobQueued)
	}
	if gotTotal != 0 || gotItems != 0 {
		t.Fatalf("creation payload total_items=%d items=%d, want 0 and 0 (no tracks resolved yet)", gotTotal, gotItems)
	}
}

func TestRetryRequeuesFailedJobAndResetsOnlyUnfinishedItems(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	started, finished := now.Add(-time.Minute), now.Add(-time.Second)
	job := domain.Job{ID: "job-failed", Input: "https://music.apple.com/cn/album/example/1", Type: "album", Storefront: "cn", CanonicalKey: "album:cn:1", Status: domain.JobFailed, TotalItems: 2, Error: "boom", StartedAt: &started, FinishedAt: &finished, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	donePR := domain.ItemProgress{Download: 1, Decrypt: 1, Resolved: true, Remuxed: true, Verified: true, Tagged: true, Saved: true}
	completed := domain.JobItem{ID: "item-done", JobID: job.ID, AdamID: "song-1", Kind: "song", Index: 1, Status: domain.ItemCompleted, Progress: donePR, Codec: "alac", CreatedAt: now, UpdatedAt: now}
	failed := domain.JobItem{ID: "item-failed", JobID: job.ID, AdamID: "song-2", Kind: "song", Index: 2, Status: domain.ItemFailed, Progress: domain.ItemProgress{Download: 0.4, Resolved: true}, Codec: "alac", RetryKind: "download", Attempt: 3, MaxAttempts: 3, StatusMessage: "ALAC failed", Error: "boom", CreatedAt: now, UpdatedAt: now}
	for _, item := range []domain.JobItem{completed, failed} {
		if err := store.CreateItem(ctx, item); err != nil {
			t.Fatal(err)
		}
	}

	manager := NewManager(store, events.NewHub(), recoveryProcessor{}, 1, slog.Default())
	if err := manager.Retry(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobQueued || got.Error != "" {
		t.Fatalf("job after retry = status %s error %q, want queued with empty error", got.Status, got.Error)
	}
	// A retry is a new run: both timestamps belong to the previous one, so the
	// next run's duration does not count the time the job sat failed.
	if got.StartedAt != nil || got.FinishedAt != nil {
		t.Fatalf("job timing after retry = started %v finished %v, want both cleared", got.StartedAt, got.FinishedAt)
	}
	items, err := store.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].Status != domain.ItemCompleted || items[0].Progress != donePR || items[0].Codec != "alac" {
		t.Fatalf("completed item was touched by retry: %+v", items[0])
	}
	reset := items[1]
	if reset.Status != domain.ItemQueued || reset.Progress != (domain.ItemProgress{}) || reset.Codec != "" || reset.Error != "" ||
		reset.RetryKind != "" || reset.Attempt != 0 || reset.MaxAttempts != 0 || reset.StatusMessage != "" {
		t.Fatalf("failed item was not reset to queued: %+v", reset)
	}
	if len(manager.queue) != 1 || <-manager.queue != job.ID {
		t.Fatal("retry did not enqueue the job")
	}
	events, err := store.ListEventsAfterLimit(ctx, job.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "job_retried" {
		t.Fatalf("events = %+v, want one job_retried", events)
	}
}

// A cancelled job is retried like a failed one: its cancelled and failed items
// run again, finished items stay done, and the job emits the same job_retried
// event, so an operator who cancelled a batch by mistake can bring it back.
func TestRetryRequeuesCancelledJob(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	started, finished := now.Add(-time.Minute), now.Add(-time.Second)
	token := "tok"
	job := domain.Job{ID: "job-cancelled", Input: "in", Type: "album", Storefront: "cn", CanonicalKey: "album:cn:c1", Status: domain.JobCancelled, TotalItems: 3, Error: "cancelled", StartedAt: &started, FinishedAt: &finished, Overrides: &config.DownloadOverrides{MediaUserToken: &token}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	for i, status := range []domain.ItemStatus{domain.ItemCompleted, domain.ItemCancelled, domain.ItemFailed} {
		item := domain.JobItem{ID: "item-" + strconv.Itoa(i), JobID: job.ID, AdamID: "song-" + strconv.Itoa(i), Kind: "song", Index: i + 1, Status: status, StatusMessage: "Cancelled", CreatedAt: now, UpdatedAt: now}
		if err := store.CreateItem(ctx, item); err != nil {
			t.Fatal(err)
		}
	}

	manager := NewManager(store, events.NewHub(), recoveryProcessor{}, 1, slog.Default())
	if err := manager.Retry(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobQueued || got.Error != "" || got.StartedAt != nil || got.FinishedAt != nil {
		t.Fatalf("job after retry = %+v, want queued with error, started_at and finished_at cleared", got)
	}
	if got.Overrides == nil || got.Overrides.MediaUserToken == nil || *got.Overrides.MediaUserToken != token {
		t.Fatalf("overrides after retry = %+v, want the media-user-token kept", got.Overrides)
	}
	items, err := store.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Status != domain.ItemCompleted || items[0].StatusMessage != "Cancelled" {
		t.Fatalf("completed item touched by retry: %+v", items[0])
	}
	for _, it := range items[1:] {
		if it.Status != domain.ItemQueued || it.StatusMessage != "" {
			t.Fatalf("unfinished item not reset to queued: %+v", it)
		}
	}
	if got.CompletedItems != 1 || got.CancelledItems != 0 || got.FailedItems != 0 {
		t.Fatalf("counters after retry = completed %d cancelled %d failed %d, want 1/0/0", got.CompletedItems, got.CancelledItems, got.FailedItems)
	}
	if len(manager.queue) != 1 || <-manager.queue != job.ID {
		t.Fatal("retry did not enqueue the job")
	}
	evs, err := store.ListEventsAfterLimit(ctx, job.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != domain.EventJobRetried {
		t.Fatalf("events = %+v, want one job_retried", evs)
	}
}

func TestRetryRefusesActiveCompletedMissingAndDuplicateJobs(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	manager := NewManager(store, events.NewHub(), recoveryProcessor{}, 1, slog.Default())

	for i, status := range []domain.JobStatus{domain.JobQueued, domain.JobRunning, domain.JobCompleted} {
		job := domain.Job{ID: "job-" + string(status), Input: "in", Type: "song", CanonicalKey: "song:cn:" + strconv.Itoa(i), Status: status, CreatedAt: now, UpdatedAt: now}
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
		if err := manager.Retry(ctx, job.ID); !errors.Is(err, ErrJobNotRetryable) {
			t.Fatalf("Retry(%s job) = %v, want ErrJobNotRetryable", status, err)
		}
	}

	if err := manager.Retry(ctx, "no-such-job"); !errors.Is(err, db.ErrJobNotFound) {
		t.Fatalf("Retry(missing job) = %v, want ErrJobNotFound", err)
	}

	// The same canonical key already has an active job (resubmitted by the
	// user): retrying the old failed job must be refused, not trip the
	// partial unique index.
	failed := domain.Job{ID: "job-old-failed", Input: "in", Type: "song", CanonicalKey: "song:cn:dup", Status: domain.JobFailed, CreatedAt: now, UpdatedAt: now}
	active := domain.Job{ID: "job-new-active", Input: "in", Type: "song", CanonicalKey: "song:cn:dup", Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now}
	for _, job := range []domain.Job{failed, active} {
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Retry(ctx, failed.ID); !errors.Is(err, db.ErrDuplicateActive) {
		t.Fatalf("Retry(failed job with active duplicate) = %v, want ErrDuplicateActive", err)
	}
	if len(manager.queue) != 0 {
		t.Fatalf("queue length = %d, want 0 after refused retries", len(manager.queue))
	}
}

// TestRetryRefusesWhilePreviousRunIsFinalizing covers the window where the
// job row already reads failed but the old worker's deferred cleanup has not
// removed its cancels entry yet (or Cancel's queued path is still dispatching
// hooks under a finalizing mark). Retrying inside that window would let the
// old worker's deferred delete remove the new run's cancel registration.
func TestRetryRefusesWhilePreviousRunIsFinalizing(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	job := domain.Job{ID: "job-finalizing", Input: "in", Type: "song", CanonicalKey: "song:cn:fin", Status: domain.JobFailed, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store, events.NewHub(), recoveryProcessor{}, 1, slog.Default())

	manager.mu.Lock()
	manager.cancels[job.ID] = func() {}
	manager.mu.Unlock()
	if err := manager.Retry(ctx, job.ID); !errors.Is(err, ErrJobFinalizing) {
		t.Fatalf("Retry(with lingering cancels entry) = %v, want ErrJobFinalizing", err)
	}

	manager.mu.Lock()
	delete(manager.cancels, job.ID)
	manager.finalizing[job.ID] = true
	manager.mu.Unlock()
	if err := manager.Retry(ctx, job.ID); !errors.Is(err, ErrJobFinalizing) {
		t.Fatalf("Retry(while finalizing) = %v, want ErrJobFinalizing", err)
	}

	manager.mu.Lock()
	delete(manager.finalizing, job.ID)
	manager.mu.Unlock()
	if err := manager.Retry(ctx, job.ID); err != nil {
		t.Fatalf("Retry(after finalize finished) = %v, want success", err)
	}
	if len(manager.queue) != 1 || <-manager.queue != job.ID {
		t.Fatal("retry after finalize did not enqueue the job")
	}
}
