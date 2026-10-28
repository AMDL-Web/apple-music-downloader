package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/hooks"
	"amdl/internal/limits"
)

type artifactCleanerProcessor struct {
	recoveryProcessor
	mu      sync.Mutex
	cleaned []string
}

func (p *artifactCleanerProcessor) CleanupJobArtifacts(job domain.Job) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleaned = append(p.cleaned, job.ID)
}

func (p *artifactCleanerProcessor) cleanedJobs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.cleaned...)
}

func TestManagerCleansArtifactsForQueuedCancelAndTerminalDelete(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	queued := domain.Job{ID: "job-queued-clean", Input: "queued", Type: "song", Storefront: "cn", Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now}
	failed := domain.Job{ID: "job-failed-clean", Input: "failed", Type: "song", Storefront: "cn", Status: domain.JobFailed, CreatedAt: now, UpdatedAt: now}
	for _, job := range []domain.Job{queued, failed} {
		if err := store.CreateJob(context.Background(), job); err != nil {
			t.Fatal(err)
		}
	}
	processor := &artifactCleanerProcessor{}
	manager := NewManager(store, events.NewHub(), processor, 1, slog.Default())
	if err := manager.Cancel(context.Background(), queued.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Delete(context.Background(), failed.ID); err != nil {
		t.Fatal(err)
	}
	if got := processor.cleanedJobs(); len(got) != 2 || got[0] != queued.ID || got[1] != failed.ID {
		t.Fatalf("cleaned jobs = %v, want queued cancel then failed delete", got)
	}
}

func TestManagerDeleteSurvivesCorruptOverridesRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amdl.db")
	store, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	failed := domain.Job{ID: "job-corrupt-overrides", Input: "failed", Type: "song", Storefront: "cn", Status: domain.JobFailed, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateJob(context.Background(), failed); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE jobs SET overrides='{broken' WHERE id=?`, failed.ID); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	processor := &artifactCleanerProcessor{}
	manager := NewManager(store, events.NewHub(), processor, 1, slog.Default())
	if err := manager.Delete(context.Background(), failed.ID); err != nil {
		t.Fatalf("Delete with corrupt overrides = %v, want success", err)
	}
	if _, err := store.GetJob(context.Background(), failed.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("job still present after delete: %v", err)
	}
	if got := processor.cleanedJobs(); len(got) != 0 {
		t.Fatalf("cleanup ran without decodable metadata: %v", got)
	}
}

type cancelAfterTotalProcessor struct {
	started chan struct{}
	once    sync.Once
}

func (p *cancelAfterTotalProcessor) ValidateRequest(context.Context, string) (ValidationResult, error) {
	return ValidationResult{Type: "artist", Storefront: "cn", ID: "1495777901"}, nil
}

func (p *cancelAfterTotalProcessor) ProcessJob(ctx context.Context, job domain.Job, reporter Reporter) error {
	job.TotalItems = 2
	if err := reporter.SetJob(ctx, &job); err != nil {
		return err
	}
	for i := 1; i <= 2; i++ {
		item := domain.JobItem{
			JobID: job.ID, AdamID: "song", Kind: "song", Index: i, Title: "Song", Status: domain.ItemQueued,
		}
		if err := reporter.AddItem(ctx, &item); err != nil {
			return err
		}
	}
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return ctx.Err()
}

func TestCancelledJobPreservesProcessorUpdatedTotalItems(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	processor := &cancelAfterTotalProcessor{started: make(chan struct{})}
	manager := NewManager(store, events.NewHub(), processor, 1, slog.Default())
	manager.Start(ctx)
	resp := manager.SubmitBatch(ctx, []string{"https://music.apple.com/cn/artist/example/1495777901"}, nil)
	if resp.Accepted != 1 || len(resp.Results) != 1 || resp.Results[0].Status != domain.SubmitAccepted || resp.Results[0].Job == nil {
		t.Fatalf("unexpected submit result: %+v", resp)
	}
	job := *resp.Results[0].Job

	select {
	case <-processor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("processor did not start")
	}
	if err := manager.Cancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	var got domain.Job
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		got, err = store.GetJob(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == domain.JobCancelled && got.Error == "cancelled" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.Status != domain.JobCancelled {
		t.Fatalf("status = %s, want %s", got.Status, domain.JobCancelled)
	}
	if got.TotalItems != 2 {
		t.Fatalf("total_items = %d, want 2", got.TotalItems)
	}
}

// failingProcessor validates like keyedProcessor but every job fails, so
// tests can observe the failed-terminal state.
type failingProcessor struct{ keyedProcessor }

func (failingProcessor) ProcessJob(context.Context, domain.Job, Reporter) error {
	return errors.New("boom")
}

func TestTerminalJobDropsPersistedTokenUnlessRetryable(t *testing.T) {
	newOverrides := func() *config.DownloadOverrides {
		token := "tok"
		return &config.DownloadOverrides{MediaUserToken: &token}
	}
	waitToken := func(t *testing.T, m *Manager, jobID, want string, wantPresent bool) {
		t.Helper()
		var job domain.Job
		deadline := time.Now().Add(2 * time.Second)
		for {
			var err error
			job, err = m.store.GetJob(context.Background(), jobID)
			if err == nil && job.Status.IsTerminal() && !m.FinalizeInFlight(jobID) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("job %s never finalized", jobID)
			}
			time.Sleep(10 * time.Millisecond)
		}
		got := ""
		present := job.Overrides != nil && job.Overrides.MediaUserToken != nil
		if present {
			got = *job.Overrides.MediaUserToken
		}
		if got != want || present != wantPresent {
			t.Fatalf("token after terminal = %q, want %q", got, want)
		}
	}

	t.Run("completed drops the token", func(t *testing.T) {
		manager := newTestManager(t)
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		manager.Start(ctx)
		resp := manager.SubmitBatch(ctx, []string{"station|us|ra.1"}, newOverrides())
		if resp.Accepted != 1 {
			t.Fatalf("submit: %+v", resp.Results)
		}
		waitToken(t, manager, resp.Results[0].Job.ID, "", false)
	})

	t.Run("cancelled drops the token", func(t *testing.T) {
		manager := newTestManager(t)
		resp := manager.SubmitBatch(context.Background(), []string{"station|us|ra.1"}, newOverrides())
		if resp.Accepted != 1 {
			t.Fatalf("submit: %+v", resp.Results)
		}
		jobID := resp.Results[0].Job.ID
		if err := manager.Cancel(context.Background(), jobID); err != nil {
			t.Fatal(err)
		}
		waitToken(t, manager, jobID, "", false)
	})

	t.Run("failed keeps the token for retry", func(t *testing.T) {
		store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
		manager := NewManager(store, events.NewHub(), failingProcessor{}, 1, slog.Default())
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		manager.Start(ctx)
		resp := manager.SubmitBatch(ctx, []string{"station|us|ra.1"}, newOverrides())
		if resp.Accepted != 1 {
			t.Fatalf("submit: %+v", resp.Results)
		}
		waitToken(t, manager, resp.Results[0].Job.ID, "tok", true)
	})
}

func TestJobCompletionDispatchesHook(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		{Name: "on-finish", Type: "webhook", Events: []string{"job_finished"}, URL: server.URL},
	}}
	manager := NewManager(store, events.NewHub(), keyedProcessor{}, 1, slog.Default())
	dispatcher := hooks.NewDispatcher(hooksCfg, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	manager.Start(ctx)

	resp := manager.SubmitBatch(ctx, []string{"song|us|1"}, nil)
	if resp.Accepted != 1 {
		t.Fatalf("submit = %+v, want 1 accepted", resp)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&calls) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	// Drain the dispatcher before asserting so the hook goroutine has fully
	// recorded its result and doesn't outlive the test.
	dispatcher.Shutdown(context.Background())
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("webhook calls = %d, want 1 after job completion", got)
	}
}

func TestCancelQueuedJobDispatchesCancelledHookAndNeverRuns(t *testing.T) {
	var calls int32
	var lastEvent string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Event string `json:"event"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		lastEvent = payload.Event
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
		{Name: "on-cancel", Type: "webhook", Events: []string{"job_cancelled"}, URL: server.URL},
	}}
	// A processor that fails the test if ProcessJob is ever invoked: a
	// cancelled-while-queued job must never actually run.
	processor := &neverRunProcessor{t: t}
	manager := NewManager(store, events.NewHub(), processor, 1, slog.Default())
	dispatcher := hooks.NewDispatcher(hooksCfg, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)

	// Deliberately do not call manager.Start(ctx): the submitted job stays in
	// the in-memory queue channel, never dequeued, so Cancel() must take the
	// "not yet running" path.
	ctx := context.Background()
	resp := manager.SubmitBatch(ctx, []string{"song|us|1"}, nil)
	if resp.Accepted != 1 {
		t.Fatalf("submit = %+v, want 1 accepted", resp)
	}
	jobID := resp.Results[0].Job.ID

	if err := manager.Cancel(ctx, jobID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	got, err := store.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobCancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&calls) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	dispatcher.Shutdown(context.Background())
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("webhook calls = %d, want exactly 1 for the queued-cancel path", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if lastEvent != "job_cancelled" {
		t.Fatalf("event = %q, want job_cancelled", lastEvent)
	}

	// Simulate a worker eventually dequeuing this already-cancelled job: it
	// must not resurrect the job into JobRunning or invoke the processor.
	manager.run(ctx, jobID)
	got, err = store.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobCancelled {
		t.Fatalf("status after late run() = %s, want cancelled (must not resurrect)", got.Status)
	}
}

func TestDeleteRefusesActiveAndFinalizingJobs(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	processor := &cancelAfterTotalProcessor{started: make(chan struct{})}
	manager := NewManager(store, events.NewHub(), processor, 1, slog.Default())

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	manager.Start(ctx)

	resp := manager.SubmitBatch(ctx, []string{"artist|cn|1495777901"}, nil)
	if resp.Accepted != 1 {
		t.Fatalf("submit = %+v, want 1 accepted", resp)
	}
	jobID := resp.Results[0].Job.ID
	<-processor.started

	// Running: the job sits in m.cancels until finalize completes.
	if err := manager.Delete(ctx, jobID); !errors.Is(err, db.ErrJobNotTerminal) {
		t.Fatalf("Delete(running) error = %v, want ErrJobNotTerminal", err)
	}

	// Finalize-in-flight (Cancel's queued path): status row already reads
	// terminal but the marker must still refuse deletion.
	manager.mu.Lock()
	manager.finalizing["job_finalizing"] = true
	manager.mu.Unlock()
	if err := manager.Delete(ctx, "job_finalizing"); !errors.Is(err, db.ErrJobNotTerminal) {
		t.Fatalf("Delete(finalizing) error = %v, want ErrJobNotTerminal", err)
	}

	if err := manager.Cancel(ctx, jobID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	// Delete keeps refusing until run()'s finalize fully completes and clears
	// the m.cancels entry, then succeeds exactly on the row it guarded.
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := manager.Delete(ctx, jobID)
		if err == nil {
			break
		}
		if !errors.Is(err, db.ErrJobNotTerminal) {
			t.Fatalf("Delete() error = %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("Delete() still refused 2s after Cancel")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := store.GetJob(ctx, jobID); err == nil {
		t.Fatal("job row still exists after successful Delete")
	}
}

func TestDeleteRefusesJobWhileHookIsPending(t *testing.T) {
	release := make(chan struct{})
	hookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hookServer.Close()

	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager := NewManager(store, events.NewHub(), recoveryProcessor{}, 1, slog.Default())
	dispatcher := hooks.NewDispatcher(hooks.Config{Enabled: true, Entries: []hooks.Entry{{
		Name: "blocking", Type: "webhook", Events: []string{"job_finished"}, URL: hookServer.URL,
	}}}, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)

	job := domain.Job{ID: "job-with-hook", Input: "song|us|1", Type: "song", CanonicalKey: "song:us:1", Status: domain.JobCompleted}
	if err := store.CreateJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	dispatcher.Dispatch("job_finished", job, nil)
	if err := manager.Delete(context.Background(), job.ID); !errors.Is(err, db.ErrJobNotTerminal) {
		t.Fatalf("Delete() while hook pending error = %v, want ErrJobNotTerminal", err)
	}

	close(release)
	dispatcher.Shutdown(context.Background())
	if err := manager.Delete(context.Background(), job.ID); err != nil {
		t.Fatalf("Delete() after hook completed: %v", err)
	}
}

func TestCancelRunningJobDispatchesCancelledHookExactlyOnce(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		{Name: "on-cancel", Type: "webhook", Events: []string{"job_cancelled"}, URL: server.URL},
	}}
	processor := &cancelAfterTotalProcessor{started: make(chan struct{})}
	manager := NewManager(store, events.NewHub(), processor, 1, slog.Default())
	dispatcher := hooks.NewDispatcher(hooksCfg, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	manager.Start(ctx)

	resp := manager.SubmitBatch(ctx, []string{"song|us|1"}, nil)
	if resp.Accepted != 1 {
		t.Fatalf("submit = %+v, want 1 accepted", resp)
	}
	jobID := resp.Results[0].Job.ID

	select {
	case <-processor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("processor did not start")
	}
	if err := manager.Cancel(ctx, jobID); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var got domain.Job
	for time.Now().Before(deadline) {
		got, err = store.GetJob(ctx, jobID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == domain.JobCancelled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.Status != domain.JobCancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}

	// Give any (incorrect) duplicate dispatch a chance to land before asserting.
	time.Sleep(200 * time.Millisecond)
	dispatcher.Shutdown(context.Background())
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("webhook calls = %d, want exactly 1 (no duplicate dispatch between Cancel and run)", got)
	}
}

// neverRunProcessor fails the test if ProcessJob is ever called.
type neverRunProcessor struct{ t *testing.T }

func (p *neverRunProcessor) ValidateRequest(context.Context, string) (ValidationResult, error) {
	return ValidationResult{Type: "song", Storefront: "us", ID: "1"}, nil
}

func (p *neverRunProcessor) ProcessJob(context.Context, domain.Job, Reporter) error {
	p.t.Fatal("ProcessJob must not run for a job cancelled while queued")
	return nil
}

// cancelThenReturnNilProcessor blocks until its context is cancelled and then
// returns nil, simulating a processor that finishes "successfully" right as a
// cancel arrives (or one that doesn't surface ctx errors).
type cancelThenReturnNilProcessor struct {
	started chan struct{}
	once    sync.Once
}

func (p *cancelThenReturnNilProcessor) ValidateRequest(context.Context, string) (ValidationResult, error) {
	return ValidationResult{Type: "song", Storefront: "us", ID: "1"}, nil
}

func (p *cancelThenReturnNilProcessor) ProcessJob(ctx context.Context, _ domain.Job, _ Reporter) error {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return nil
}

func TestCancelRunningJobWinsOverNilProcessorReturn(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Event string `json:"event"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		seen = append(seen, payload.Event)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	hooksCfg := hooks.Config{Enabled: true, Entries: []hooks.Entry{
		{Name: "terminal", Type: "webhook", Events: []string{"job_finished", "job_cancelled"}, URL: server.URL},
	}}
	processor := &cancelThenReturnNilProcessor{started: make(chan struct{})}
	manager := NewManager(store, events.NewHub(), processor, 1, slog.Default())
	dispatcher := hooks.NewDispatcher(hooksCfg, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	manager.Start(ctx)

	resp := manager.SubmitBatch(ctx, []string{"song|us|1"}, nil)
	if resp.Accepted != 1 {
		t.Fatalf("submit = %+v, want 1 accepted", resp)
	}
	jobID := resp.Results[0].Job.ID

	select {
	case <-processor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("processor did not start")
	}
	if err := manager.Cancel(ctx, jobID); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var got domain.Job
	for time.Now().Before(deadline) {
		got, err = store.GetJob(ctx, jobID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == domain.JobCancelled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Even though ProcessJob returned nil, cancellation must win: the job is
	// cancelled and the hook is job_cancelled, never job_finished.
	if got.Status != domain.JobCancelled {
		t.Fatalf("status = %s, want cancelled even though ProcessJob returned nil", got.Status)
	}

	time.Sleep(200 * time.Millisecond)
	dispatcher.Shutdown(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != "job_cancelled" {
		t.Fatalf("hook events = %v, want exactly [job_cancelled]", seen)
	}
}

func TestFinalizeJobSkipsHookWhenPersistenceFails(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}

	hooksCfg := hooks.Config{Enabled: true, Entries: []hooks.Entry{
		{Name: "on-cancel", Type: "webhook", Events: []string{"job_cancelled"}, URL: server.URL},
	}}
	manager := NewManager(store, events.NewHub(), keyedProcessor{}, 1, slog.Default())
	dispatcher := hooks.NewDispatcher(hooksCfg, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)

	// Close the store so the terminal-status write inside finalizeJob fails.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	job := domain.Job{ID: "job-x", Type: "song", Status: domain.JobCancelled, Error: "cancelled"}
	if err := manager.finalizeJob(context.Background(), job, domain.EventJobCancelled, "job cancelled"); err == nil {
		t.Fatal("finalizeJob returned nil, want a persistence error when the store is closed")
	}

	// The hook must not fire when the terminal status could not be persisted.
	time.Sleep(200 * time.Millisecond)
	dispatcher.Shutdown(context.Background())
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("webhook calls = %d, want 0 when persistence failed", got)
	}
}

func TestTerminalJobEventPayloadCarriesRESTJobSnapshot(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	embed := false
	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Minute)
	job := domain.Job{
		ID: "job-terminal-payload", Input: "song|us|1", Type: "song", Storefront: "us",
		CanonicalKey: "song:us:1", Status: domain.JobRunning, TotalItems: 1,
		Overrides: &config.DownloadOverrides{EmbedCover: &embed}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateItem(ctx, domain.JobItem{ID: "item-1", JobID: job.ID, AdamID: "1", Kind: "song", Index: 1, Status: domain.ItemCompleted, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(store, events.NewHub(), keyedProcessor{}, 1, slog.Default())
	job, err = store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	job.Status = domain.JobCompleted
	job.UpdatedAt = now.Add(30 * time.Second)
	if err := manager.persistTerminal(ctx, job, domain.EventJobFinished, "completed"); err != nil {
		t.Fatal(err)
	}

	recorded, err := store.ListEventsAfterLimit(ctx, job.ID, 0, 0)
	if err != nil || len(recorded) != 1 {
		t.Fatalf("terminal events = %+v, err=%v, want one", recorded, err)
	}
	var snapshot domain.Job
	if err := json.Unmarshal(recorded[0].Payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(recorded[0].Payload, &fields); err != nil {
		t.Fatal(err)
	}
	if snapshot.ID != job.ID || snapshot.Status != domain.JobCompleted || snapshot.CompletedItems != 1 || snapshot.UpdatedAt.IsZero() || fields["message"] != "completed" {
		t.Fatalf("terminal payload = %s, want complete REST job snapshot plus message", recorded[0].Payload)
	}
	if snapshot.FinishedAt == nil || !snapshot.FinishedAt.Equal(job.UpdatedAt) {
		t.Fatalf("terminal payload finished_at = %v, want %v", snapshot.FinishedAt, job.UpdatedAt)
	}
	stored, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.FinishedAt == nil || !stored.FinishedAt.Equal(job.UpdatedAt) {
		t.Fatalf("stored finished_at = %v, want %v", stored.FinishedAt, job.UpdatedAt)
	}
}

// itemsProcessor reports one item per status through the Reporter, the way
// the media pipeline does, and returns nil.
type itemsProcessor struct{ statuses []domain.ItemStatus }

func (itemsProcessor) ValidateRequest(_ context.Context, url string) (ValidationResult, error) {
	return keyedProcessor{}.ValidateRequest(context.Background(), url)
}

func (p itemsProcessor) ProcessJob(ctx context.Context, job domain.Job, reporter Reporter) error {
	job.TotalItems = len(p.statuses)
	if err := reporter.SetJob(ctx, &job); err != nil {
		return err
	}
	for i, status := range p.statuses {
		item := domain.JobItem{JobID: job.ID, AdamID: strconv.Itoa(i + 1), Kind: "song", Index: i + 1, Status: status}
		if err := reporter.AddItem(ctx, &item); err != nil {
			return err
		}
	}
	return nil
}

func waitForTerminal(t *testing.T, store *db.Store, jobID string) domain.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := store.GetJob(context.Background(), jobID)
		if err == nil && job.Status.IsTerminal() {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach a terminal status", jobID)
	return domain.Job{}
}

func TestRunDecidesTerminalStatusFromStoreCounters(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statuses   []domain.ItemStatus
		wantStatus domain.JobStatus
		completed  int
		skipped    int
		failed     int
	}{
		{"all written or present", []domain.ItemStatus{domain.ItemCompleted, domain.ItemSkipped, domain.ItemCompleted}, domain.JobCompleted, 2, 1, 0},
		{"one failed item fails the job", []domain.ItemStatus{domain.ItemCompleted, domain.ItemFailed}, domain.JobFailed, 1, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			manager := NewManager(store, events.NewHub(), itemsProcessor{statuses: tc.statuses}, 1, slog.Default())
			ctx, stop := context.WithCancel(context.Background())
			defer func() {
				stop()
				_ = manager.Wait(context.Background())
			}()
			manager.Start(ctx)

			before := time.Now().UTC().Add(-time.Second)
			resp := manager.SubmitBatch(ctx, []string{"album|us|1"}, nil)
			if resp.Accepted != 1 {
				t.Fatalf("submit = %+v", resp)
			}
			got := waitForTerminal(t, store, resp.Results[0].Job.ID)
			if got.Status != tc.wantStatus || got.TotalItems != len(tc.statuses) ||
				got.CompletedItems != tc.completed || got.SkippedItems != tc.skipped || got.FailedItems != tc.failed || got.CancelledItems != 0 {
				t.Fatalf("job = status %s total %d completed %d skipped %d failed %d cancelled %d, want %s %d %d %d %d 0",
					got.Status, got.TotalItems, got.CompletedItems, got.SkippedItems, got.FailedItems, got.CancelledItems,
					tc.wantStatus, len(tc.statuses), tc.completed, tc.skipped, tc.failed)
			}
			if got.StartedAt == nil || got.FinishedAt == nil || got.StartedAt.Before(before) || got.FinishedAt.Before(*got.StartedAt) {
				t.Fatalf("job timing = started %v finished %v, want both set, started after %v and finished after started", got.StartedAt, got.FinishedAt, before)
			}

			recorded, err := store.ListEventsAfterLimit(context.Background(), got.ID, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			terminal := recorded[len(recorded)-1]
			var snapshot domain.Job
			if err := json.Unmarshal(terminal.Payload, &snapshot); err != nil {
				t.Fatalf("terminal payload %s: %v", terminal.Payload, err)
			}
			if snapshot.CompletedItems != tc.completed || snapshot.SkippedItems != tc.skipped || snapshot.FailedItems != tc.failed {
				t.Fatalf("terminal payload counters = %d/%d/%d, want %d/%d/%d", snapshot.CompletedItems, snapshot.SkippedItems, snapshot.FailedItems, tc.completed, tc.skipped, tc.failed)
			}
		})
	}
}

func TestCancelQueuedJobSetsFinishedAtWithoutStartedAt(t *testing.T) {
	manager := newTestManager(t)
	ctx := context.Background()
	resp := manager.SubmitBatch(ctx, []string{"song|us|1"}, nil)
	if resp.Accepted != 1 {
		t.Fatalf("submit = %+v", resp)
	}
	jobID := resp.Results[0].Job.ID
	if err := manager.Cancel(ctx, jobID); err != nil {
		t.Fatal(err)
	}
	got, err := manager.store.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobCancelled || got.StartedAt != nil || got.FinishedAt == nil {
		t.Fatalf("cancelled queued job = status %s started %v finished %v, want cancelled, never started, finished set", got.Status, got.StartedAt, got.FinishedAt)
	}
}

// raceProcessor either completes quickly or, if cancelled during its short
// work window, returns the context error. This maximizes the variety of
// interleavings between run()'s startup claim and a concurrent Cancel.
type raceProcessor struct{}

func (raceProcessor) ValidateRequest(_ context.Context, url string) (ValidationResult, error) {
	parts := strings.SplitN(url, "|", 3)
	return ValidationResult{Type: parts[0], Storefront: parts[1], ID: parts[2]}, nil
}

func (raceProcessor) ProcessJob(ctx context.Context, _ domain.Job, _ Reporter) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Millisecond):
		return nil
	}
}

// TestCancelRacingStartupDispatchesExactlyOneConsistentHook stresses the exact
// interface race between a worker claiming a queued job (run() marking it
// running) and a concurrent Cancel(). For every job — however the two
// interleave — exactly one terminal hook must fire, and it must match the
// persisted final status. Before the startup claim was serialized under m.mu,
// this window could produce both a job_cancelled hook (from Cancel) and a
// job_finished hook (from a resurrected worker), with the job downloaded
// despite the cancel. Run with -race.
func TestCancelRacingStartupDispatchesExactlyOneConsistentHook(t *testing.T) {
	const jobs = 150

	var mu sync.Mutex
	hookEvents := map[string][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Event string `json:"event"`
			Job   struct {
				ID string `json:"id"`
			} `json:"job"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		hookEvents[payload.Job.ID] = append(hookEvents[payload.Job.ID], payload.Event)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	hooksCfg := hooks.Config{Enabled: true, MaxConcurrent: 8, Entries: []hooks.Entry{
		{Name: "terminal", Type: "webhook", Events: []string{"job_finished", "job_failed", "job_cancelled"}, URL: server.URL},
	}}
	manager := NewManager(store, events.NewHub(), raceProcessor{}, 4, slog.Default())
	dispatcher := hooks.NewDispatcher(hooksCfg, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	manager.Start(ctx)

	jobIDs := make([]string, 0, jobs)
	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		url := "song|us|" + strconv.Itoa(i)
		resp := manager.SubmitBatch(ctx, []string{url}, nil)
		if resp.Accepted != 1 || resp.Results[0].Job == nil {
			t.Fatalf("submit %d = %+v, want 1 accepted", i, resp)
		}
		jobID := resp.Results[0].Job.ID
		jobIDs = append(jobIDs, jobID)
		// Fire the cancel concurrently with the worker picking the job up.
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = manager.Cancel(context.Background(), id)
		}(jobID)
	}
	wg.Wait()

	// Wait for every job to reach a terminal status.
	deadline := time.Now().Add(10 * time.Second)
	for {
		pending := 0
		for _, id := range jobIDs {
			job, err := store.GetJob(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if !job.Status.IsTerminal() {
				pending++
			}
		}
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d jobs did not reach a terminal status in time", pending)
		}
		time.Sleep(20 * time.Millisecond)
	}

	dispatcher.Shutdown(context.Background())

	mu.Lock()
	defer mu.Unlock()
	for _, id := range jobIDs {
		job, err := store.GetJob(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		got := hookEvents[id]
		if len(got) != 1 {
			t.Fatalf("job %s (final status %s): terminal hooks = %v, want exactly one", id, job.Status, got)
		}
		want := hookEventForStatus(job.Status)
		if got[0] != want {
			t.Fatalf("job %s: hook = %q but final status %s maps to %q", id, got[0], job.Status, want)
		}
	}
}

// priorityCaptureProcessor reports the limits priority the manager stamped on
// each job's processing context.
type priorityCaptureProcessor struct {
	keyedProcessor
	priorities chan int64
}

func (p *priorityCaptureProcessor) ProcessJob(ctx context.Context, _ domain.Job, _ Reporter) error {
	priority, ok := limits.PriorityFromContext(ctx)
	if !ok {
		priority = -1
	}
	p.priorities <- priority
	return nil
}

func TestRunStampsPoolPriorityFromJobCreationTime(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	processor := &priorityCaptureProcessor{priorities: make(chan int64, 1)}
	manager := NewManager(store, events.NewHub(), processor, 1, slog.Default())
	manager.Start(context.Background())
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	resp := manager.SubmitBatch(context.Background(), []string{"song|cn|priority"}, nil)
	if resp.Accepted != 1 {
		t.Fatalf("submit = %+v", resp)
	}
	job, err := store.GetJob(context.Background(), resp.Results[0].Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-processor.priorities:
		if want := job.CreatedAt.UnixNano(); got != want {
			t.Fatalf("pool priority = %d, want job CreatedAt %d", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("job was not processed")
	}
}
