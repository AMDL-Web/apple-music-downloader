package jobs

import (
	"context"
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

type tokenRecoveryProcessor struct {
	jobs chan domain.Job
}

func (p *tokenRecoveryProcessor) ValidateRequest(context.Context, string) (ValidationResult, error) {
	return ValidationResult{Type: "station", Storefront: "us", ID: "ra.1"}, nil
}

func (p *tokenRecoveryProcessor) ProcessJob(_ context.Context, job domain.Job, _ Reporter) error {
	p.jobs <- job
	return errors.New("keep retryable")
}

func TestRecoverUnfinishedRequeuesQueuedAndRunningJobs(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	queued := domain.Job{ID: "job-queued", Input: "https://music.apple.com/cn/song/queued/1", Type: "song", Storefront: "cn", CanonicalKey: "song:cn:1", Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now}
	startedAt := now.Add(time.Second)
	running := domain.Job{ID: "job-running", Input: "https://music.apple.com/cn/song/running/2", Type: "song", Storefront: "cn", CanonicalKey: "song:cn:2", Status: domain.JobRunning, StartedAt: &startedAt, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	completed := domain.Job{ID: "job-completed", Input: "https://music.apple.com/cn/song/completed/3", Type: "song", Storefront: "cn", CanonicalKey: "song:cn:3", Status: domain.JobCompleted, CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)}
	for _, job := range []domain.Job{queued, running, completed} {
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}

	manager := NewManager(store, events.NewHub(), recoveryProcessor{}, 1, slog.Default())
	recovered, err := manager.RecoverUnfinished(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if recovered != 2 {
		t.Fatalf("recovered = %d, want 2", recovered)
	}
	gotRunning, err := store.GetJob(ctx, running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotRunning.Status != domain.JobQueued {
		t.Fatalf("running job status = %s, want %s", gotRunning.Status, domain.JobQueued)
	}
	if gotRunning.StartedAt == nil || !gotRunning.StartedAt.Equal(startedAt) || gotRunning.FinishedAt != nil {
		t.Fatalf("recovered job timing = started %v finished %v, want started_at kept at %v", gotRunning.StartedAt, gotRunning.FinishedAt, startedAt)
	}
	gotCompleted, err := store.GetJob(ctx, completed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotCompleted.Status != domain.JobCompleted {
		t.Fatalf("completed job status = %s, want %s", gotCompleted.Status, domain.JobCompleted)
	}
	manager.recoveryMu.Lock()
	recoveryQueue := append([]string(nil), manager.recoveryQueue...)
	manager.recoveryMu.Unlock()
	if len(recoveryQueue) != 2 {
		t.Fatalf("recovery queue length = %d, want 2", len(recoveryQueue))
	}
	if first, second := recoveryQueue[0], recoveryQueue[1]; first != queued.ID || second != running.ID {
		t.Fatalf("queued ids = [%s, %s], want [%s, %s]", first, second, queued.ID, running.ID)
	}
	events, err := store.ListEventsAfterLimit(ctx, running.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "job_recovered" {
		t.Fatalf("running job recovery events = %+v, want one job_recovered", events)
	}
}

func TestRecoverUnfinishedRestoresMediaUserTokenOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amdl.db")
	store, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	token := "persisted-token"
	now := time.Now().UTC()
	job := domain.Job{
		ID: "station-recover", Input: "station|us|ra.1", Type: "station", Storefront: "us", CanonicalKey: "station:us:ra.1",
		Overrides: &config.DownloadOverrides{MediaUserToken: &token}, Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(context.Background(), job); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	processor := &tokenRecoveryProcessor{jobs: make(chan domain.Job, 1)}
	manager := NewManager(store, events.NewHub(), processor, 1, slog.Default())
	if recovered, err := manager.RecoverUnfinished(context.Background()); err != nil || recovered != 1 {
		t.Fatalf("RecoverUnfinished() = (%d, %v), want (1, nil)", recovered, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	manager.Start(ctx)
	select {
	case recovered := <-processor.jobs:
		if recovered.Overrides == nil || recovered.Overrides.MediaUserToken == nil || *recovered.Overrides.MediaUserToken != token {
			t.Fatalf("recovered token override = %+v, want %q", recovered.Overrides, token)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("recovered job did not run")
	}
	shutdownCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

type recoveryBacklogProcessor struct {
	total   int32
	started chan struct{}
	release chan struct{}
	done    chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func (p *recoveryBacklogProcessor) ValidateRequest(context.Context, string) (ValidationResult, error) {
	return ValidationResult{Type: "song", Storefront: "cn"}, nil
}

func (p *recoveryBacklogProcessor) ProcessJob(ctx context.Context, _ domain.Job, _ Reporter) error {
	p.started <- struct{}{}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.release:
	}
	if p.calls.Add(1) == p.total {
		p.once.Do(func() { close(p.done) })
	}
	return nil
}

func TestRecoverUnfinishedHandlesMoreThanQueueCapacityWithoutLoss(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	const backlog = 300
	for i := 0; i < backlog; i++ {
		id := strconv.Itoa(i)
		job := domain.Job{
			ID: "recover-" + id, Input: "song-" + id, Type: "song", Storefront: "cn",
			CanonicalKey: "song:cn:" + id, Status: domain.JobQueued, CreatedAt: now.Add(time.Duration(i) * time.Microsecond), UpdatedAt: now,
		}
		if err := store.CreateJob(ctx, job); err != nil {
			t.Fatalf("create recoverable job %d: %v", i, err)
		}
	}

	processor := &recoveryBacklogProcessor{
		total: backlog, started: make(chan struct{}, backlog), release: make(chan struct{}), done: make(chan struct{}),
	}
	manager := NewManager(store, events.NewHub(), processor, 8, slog.Default())
	recovered, err := manager.RecoverUnfinished(ctx)
	if err != nil {
		t.Fatalf("RecoverUnfinished: %v", err)
	}
	if recovered != backlog {
		t.Fatalf("recovered = %d, want %d", recovered, backlog)
	}
	manager.recoveryMu.Lock()
	queued := len(manager.recoveryQueue)
	manager.recoveryMu.Unlock()
	if queued != backlog {
		t.Fatalf("recovery queue length = %d, want %d", queued, backlog)
	}
	if len(manager.queue) != 0 {
		t.Fatalf("submission queue length = %d, want 0", len(manager.queue))
	}

	manager.Start(context.Background())
	for i := 0; i < 8; i++ {
		select {
		case <-processor.started:
		case <-time.After(2 * time.Second):
			t.Fatal("workers did not begin draining recovered backlog")
		}
	}
	close(processor.release)
	select {
	case <-processor.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("processed %d/%d recovered jobs", processor.calls.Load(), backlog)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		recoverable, err := store.ListRecoverableJobs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(recoverable) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d recovered jobs remain unfinished", len(recoverable))
		}
		time.Sleep(10 * time.Millisecond)
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := processor.calls.Load(); got != backlog {
		t.Fatalf("processor calls = %d, want %d", got, backlog)
	}
}

// TestRecoverUnfinishedDoesNotDispatchJobQueuedHook guards the documented
// design decision that recovery re-enqueues jobs without re-firing the
// job_queued creation hook: those jobs were created before the restart, so
// dispatching job_queued again would double-notify external systems.
func TestRecoverUnfinishedDoesNotDispatchJobQueuedHook(t *testing.T) {
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

	ctx := context.Background()
	now := time.Now().UTC()
	queued := domain.Job{ID: "job-recover", Input: "https://music.apple.com/cn/song/q/1", Type: "song", Storefront: "cn", CanonicalKey: "song:cn:1", Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateJob(ctx, queued); err != nil {
		t.Fatal(err)
	}

	hooksCfg := hooks.Config{Enabled: true, Entries: []hooks.Entry{
		{Name: "on-queued", Type: "webhook", Events: []string{"job_queued"}, URL: server.URL},
	}}
	manager := NewManager(store, events.NewHub(), recoveryProcessor{}, 1, slog.Default())
	dispatcher := hooks.NewDispatcher(hooksCfg, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)

	// Do NOT start workers: recovery itself must not fire the creation hook.
	if _, err := manager.RecoverUnfinished(ctx); err != nil {
		t.Fatal(err)
	}
	// Dispatch launches its webhook goroutine synchronously, so any erroneous
	// dispatch during recovery is already counted in the WaitGroup that
	// Shutdown drains — no arbitrary sleep needed.
	dispatcher.Shutdown(context.Background())
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("job_queued hook calls after recovery = %d, want 0", got)
	}
}

func TestNilHooksDispatcherIsNoop(t *testing.T) {
	manager := newTestManager(t)
	manager.Start(context.Background())
	resp := manager.SubmitBatch(context.Background(), []string{"song|us|1"}, nil)
	if resp.Accepted != 1 {
		t.Fatalf("submit = %+v, want 1 accepted", resp)
	}
	deadline := time.Now().Add(2 * time.Second)
	var job domain.Job
	for time.Now().Before(deadline) {
		job, _ = manager.store.GetJob(context.Background(), resp.Results[0].Job.ID)
		if job.Status == domain.JobCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.Status != domain.JobCompleted {
		t.Fatalf("job status = %s, want completed (manager.hooks is nil until SetHooks is called)", job.Status)
	}
}

// TestHooksPendingIsNilSafe mirrors SetHooks/Dispatch's existing nil-receiver
// contract: server.go's events/eventsWS handlers call HooksPending on every
// terminal job regardless of whether SetHooks was ever called, and must not
// need a nil *Manager check either.
func TestHooksPendingIsNilSafe(t *testing.T) {
	var m *Manager
	if m.HooksPending("job-1") {
		t.Fatal("nil *Manager HooksPending = true, want false")
	}

	manager := newTestManager(t) // hooks left unset (nil dispatcher)
	if manager.HooksPending("job-1") {
		t.Fatal("HooksPending with unset hooks dispatcher = true, want false")
	}
}

// TestFinalizeInFlightTracksFinalizeMarks pins the contract eventsExhausted
// (internal/api) relies on to close the pre-dispatch gap: a job's terminal
// status becomes visible before hook dispatch increments the dispatcher's
// pending count, and in that window the job is still covered by either its
// m.cancels entry (run's finalize path) or its m.finalizing mark (Cancel's
// queued path). FinalizeInFlight must report true for both.
func TestFinalizeInFlightTracksFinalizeMarks(t *testing.T) {
	var nilManager *Manager
	if nilManager.FinalizeInFlight("job-1") {
		t.Fatal("nil *Manager FinalizeInFlight = true, want false")
	}

	manager := newTestManager(t)
	if manager.FinalizeInFlight("job-1") {
		t.Fatal("FinalizeInFlight with no marks = true, want false")
	}

	manager.mu.Lock()
	manager.cancels["job-1"] = func() {}
	manager.mu.Unlock()
	if !manager.FinalizeInFlight("job-1") {
		t.Fatal("FinalizeInFlight with a cancels entry = false, want true")
	}

	manager.mu.Lock()
	delete(manager.cancels, "job-1")
	manager.finalizing["job-1"] = true
	manager.mu.Unlock()
	if !manager.FinalizeInFlight("job-1") {
		t.Fatal("FinalizeInFlight with a finalizing mark = false, want true")
	}

	manager.mu.Lock()
	delete(manager.finalizing, "job-1")
	manager.mu.Unlock()
	if manager.FinalizeInFlight("job-1") {
		t.Fatal("FinalizeInFlight after marks cleared = true, want false")
	}
}
