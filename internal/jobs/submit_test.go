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

func TestSubmitBatchStoresOverridesOnEveryJob(t *testing.T) {
	manager := newTestManager(t)
	force := true
	overrides := &config.DownloadOverrides{CodecAlternative: &force}
	resp := manager.SubmitBatch(context.Background(), []string{
		"station|us|ra.1",
		"playlist|us|pl.u-private",
		"station|us|1",
	}, overrides)
	if resp.Accepted != 3 {
		t.Fatalf("accepted = %d, want 3: %+v", resp.Accepted, resp.Results)
	}
	for _, result := range resp.Results {
		job, err := manager.store.GetJob(context.Background(), result.Job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Overrides == nil || job.Overrides.CodecAlternative == nil || !*job.Overrides.CodecAlternative {
			t.Fatalf("job %s (%s) overrides = %+v, want codec_alternative", result.Job.ID, result.URL, job.Overrides)
		}
	}
}

func TestSubmitBatchDedupesWithinRequest(t *testing.T) {
	manager := newTestManager(t)
	ctx := context.Background()
	resp := manager.SubmitBatch(ctx, []string{
		"album|us|111",
		"station|us|222",
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

	first := manager.SubmitBatch(ctx, []string{"station|us|222"}, nil)
	if first.Accepted != 1 {
		t.Fatalf("first submit = %+v, want 1 accepted", first)
	}
	jobID := first.Results[0].Job.ID

	second := manager.SubmitBatch(ctx, []string{"station|us|222"}, nil)
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

	// A one-off run is never queued twice: the input gets a new run.
	third := manager.SubmitBatch(ctx, []string{"station|us|222"}, nil)
	if got := third.Results[0]; got.Status != domain.SubmitAccepted || got.Job == nil || got.Job.ID == jobID {
		t.Fatalf("third submit = %+v, want a new run after %s completed", got, jobID)
	}
}

func TestSubmitBatchQueueFullMarksRemainingWithoutRollback(t *testing.T) {
	manager := newTestManager(t)
	manager.capacity = 1
	ctx := context.Background()

	resp := manager.SubmitBatch(ctx, []string{"station|us|1", "station|us|2", "station|us|3"}, nil)
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
	resp := manager.SubmitBatch(context.Background(), []string{"station|us|1"}, nil)
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
	resp := manager.SubmitBatch(context.Background(), []string{"station|us|1"}, nil)
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
	if depth := manager.QueueStats().QueueDepth; depth != 0 {
		t.Fatalf("queue depth = %d, want 0 after refused retries", depth)
	}
}

// holdProcessor reports every ProcessJob entry and blocks it until release is
// closed, so a test can see whether a second worker also entered the job.
type holdProcessor struct {
	keyedProcessor
	entered chan string
	release chan struct{}
}

func (p *holdProcessor) ProcessJob(ctx context.Context, job domain.Job, _ Reporter) error {
	p.entered <- job.ID
	select {
	case <-p.release:
	case <-ctx.Done():
	}
	return nil
}

// A job cancelled while queued and retried is one queued job again: two idle
// workers must not both run it.
func TestRetryOfQueuedCancelledJobRunsItOnce(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	processor := &holdProcessor{entered: make(chan string, 4), release: make(chan struct{})}
	manager := NewManager(store, events.NewHub(), processor, 2, slog.Default())
	ctx := context.Background()

	resp := manager.SubmitBatch(ctx, []string{"station|us|1"}, nil)
	if resp.Accepted != 1 {
		t.Fatalf("submit = %+v, want 1 accepted", resp)
	}
	jobID := resp.Results[0].Job.ID
	if err := manager.Cancel(ctx, jobID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Retry(ctx, jobID); err != nil {
		t.Fatal(err)
	}

	manager.Start(ctx)
	select {
	case <-processor.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("no worker started the retried job")
	}
	select {
	case id := <-processor.entered:
		t.Errorf("job %s entered ProcessJob a second time while it was already running", id)
	case <-time.After(300 * time.Millisecond):
	}
	close(processor.release)
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
}
