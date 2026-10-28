package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
)

// resumableProcessor stands in for the media pipeline across a restart. Its
// first run reports one item mid-download and blocks until its context ends;
// a later run finishes that item.
type resumableProcessor struct {
	keyedProcessor
	calls   atomic.Int32
	started chan struct{}
	exited  chan struct{}
	resumed chan domain.Job
}

func newResumableProcessor() *resumableProcessor {
	return &resumableProcessor{started: make(chan struct{}), exited: make(chan struct{}), resumed: make(chan domain.Job, 1)}
}

func (p *resumableProcessor) ProcessJob(ctx context.Context, job domain.Job, reporter Reporter) error {
	if p.calls.Add(1) == 1 {
		job.TotalItems = 1
		if err := reporter.SetJob(ctx, &job); err != nil {
			return err
		}
		item := domain.JobItem{JobID: job.ID, AdamID: "1", Kind: "song", Index: 1, Status: domain.ItemDownloading}
		if err := reporter.AddItem(ctx, &item); err != nil {
			return err
		}
		close(p.started)
		<-ctx.Done()
		close(p.exited)
		return ctx.Err()
	}
	p.resumed <- job
	items, err := reporter.ListItems(ctx, job.ID)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].Status = domain.ItemCompleted
		if err := reporter.UpdateItem(ctx, &items[i]); err != nil {
			return err
		}
	}
	return nil
}

// terminalHookServer counts every terminal hook delivery.
func terminalHookServer(t *testing.T) (*httptest.Server, *atomic.Int32, hooks.Config) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	cfg := hooks.Config{Enabled: true, Entries: []hooks.Entry{
		{Name: "terminal", Type: "webhook", Events: []string{"job_finished", "job_failed", "job_cancelled"}, URL: server.URL},
	}}
	return server, &calls, cfg
}

func eventTypes(t *testing.T, store *db.Store, jobID string) []domain.EventType {
	t.Helper()
	recorded, err := store.ListEventsAfterLimit(context.Background(), jobID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	types := make([]domain.EventType, 0, len(recorded))
	for _, ev := range recorded {
		types = append(types, ev.Type)
	}
	return types
}

func containsEventType(types []domain.EventType, want domain.EventType) bool {
	for _, got := range types {
		if got == want {
			return true
		}
	}
	return false
}

// A job interrupted by Shutdown is not over: it stays running with its items
// and media-user-token, records no terminal event and fires no hook, and a new
// Manager over the same store recovers it (job_recovered) and completes it.
func TestShutdownInterruptsJobAndRestartResumesIt(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, hookCalls, hooksCfg := terminalHookServer(t)
	processor := newResumableProcessor()
	ctx := context.Background()

	first := NewManager(store, events.NewHub(), processor, 1, slog.Default())
	firstHooks := hooks.NewDispatcher(hooksCfg, first.Event, slog.Default())
	first.SetHooks(firstHooks)
	first.Start(context.Background())
	token := "persisted-token"
	resp := first.SubmitBatch(ctx, []string{"station|us|ra.1"}, &config.DownloadOverrides{MediaUserToken: &token})
	if resp.Accepted != 1 {
		t.Fatalf("submit = %+v", resp)
	}
	jobID := resp.Results[0].Job.ID
	select {
	case <-processor.started:
	case <-time.After(3 * time.Second):
		t.Fatal("processor did not start")
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := first.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case <-processor.exited:
	default:
		t.Fatal("Shutdown returned before ProcessJob exited")
	}
	if err := firstHooks.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("hooks Shutdown: %v", err)
	}

	interrupted, err := store.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.Status != domain.JobRunning || interrupted.FinishedAt != nil || interrupted.Error != "" || interrupted.StartedAt == nil {
		t.Fatalf("interrupted job = status %s finished %v error %q started %v, want running, unfinished, no error, started",
			interrupted.Status, interrupted.FinishedAt, interrupted.Error, interrupted.StartedAt)
	}
	if interrupted.Overrides == nil || interrupted.Overrides.MediaUserToken == nil || *interrupted.Overrides.MediaUserToken != token {
		t.Fatalf("interrupted job overrides = %+v, want the media-user-token kept for the resumed run", interrupted.Overrides)
	}
	items, err := store.ListItems(ctx, jobID)
	if err != nil || len(items) != 1 || items[0].Status != domain.ItemDownloading {
		t.Fatalf("interrupted items = %+v, err=%v, want the one item left downloading", items, err)
	}
	for _, terminal := range []domain.EventType{domain.EventJobFinished, domain.EventJobFailed, domain.EventJobCancelled} {
		if containsEventType(eventTypes(t, store, jobID), terminal) {
			t.Fatalf("interrupted job recorded %s", terminal)
		}
	}
	if got := hookCalls.Load(); got != 0 {
		t.Fatalf("terminal hook calls after shutdown = %d, want 0", got)
	}

	second := NewManager(store, events.NewHub(), processor, 1, slog.Default())
	secondHooks := hooks.NewDispatcher(hooksCfg, second.Event, slog.Default())
	second.SetHooks(secondHooks)
	if recovered, err := second.RecoverUnfinished(ctx); err != nil || recovered != 1 {
		t.Fatalf("RecoverUnfinished = (%d, %v), want (1, nil)", recovered, err)
	}
	if !containsEventType(eventTypes(t, store, jobID), domain.EventJobRecovered) {
		t.Fatal("restart recorded no job_recovered event")
	}
	second.Start(context.Background())
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = second.Shutdown(stopCtx)
		_ = secondHooks.Shutdown(stopCtx)
	}()
	select {
	case resumed := <-processor.resumed:
		if resumed.Overrides == nil || resumed.Overrides.MediaUserToken == nil || *resumed.Overrides.MediaUserToken != token {
			t.Fatalf("resumed run overrides = %+v, want the media-user-token", resumed.Overrides)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("recovered job did not run")
	}
	done := waitForTerminal(t, store, jobID)
	if done.Status != domain.JobCompleted || done.CompletedItems != 1 {
		t.Fatalf("resumed job = status %s completed %d, want completed with its one item", done.Status, done.CompletedItems)
	}
	if done.StartedAt == nil || !done.StartedAt.Equal(*interrupted.StartedAt) {
		t.Fatalf("started_at = %v, want the first run's %v kept across recovery", done.StartedAt, interrupted.StartedAt)
	}
}

// panickingProcessor panics for inputs containing "panic" and succeeds
// otherwise.
type panickingProcessor struct{ keyedProcessor }

func (panickingProcessor) ProcessJob(_ context.Context, job domain.Job, _ Reporter) error {
	if strings.Contains(job.Input, "panic") {
		var items []domain.JobItem
		_ = items[3]
	}
	return nil
}

// A panic inside ProcessJob fails that job (retryable) and leaves the worker
// running: the next job on the single worker still completes.
func TestProcessorPanicFailsJobAndWorkerSurvives(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager := NewManager(store, events.NewHub(), panickingProcessor{}, 1, slog.Default())
	manager.Start(context.Background())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
	}()

	ctx := context.Background()
	resp := manager.SubmitBatch(ctx, []string{"song|us|panic", "song|us|fine"}, nil)
	if resp.Accepted != 2 {
		t.Fatalf("submit = %+v", resp)
	}
	panicked := waitForTerminal(t, store, resp.Results[0].Job.ID)
	if panicked.Status != domain.JobFailed || !strings.HasPrefix(panicked.Error, "internal error: panic: ") || !strings.Contains(panicked.Error, "index out of range") {
		t.Fatalf("panicked job = status %s error %q, want failed with an internal error naming the panic", panicked.Status, panicked.Error)
	}
	if fine := waitForTerminal(t, store, resp.Results[1].Job.ID); fine.Status != domain.JobCompleted {
		t.Fatalf("job after the panic = %s, want completed: the worker must survive", fine.Status)
	}
	if err := manager.Retry(ctx, panicked.ID); err != nil {
		t.Fatalf("Retry of the panicked job: %v, want it retryable", err)
	}
}

// A user cancel of a queued job ends its unfinished items too (a recovered
// job carries rows from the interrupted run), and the terminal event and hook
// payload count them.
func TestCancelQueuedJobCancelsItsUnfinishedItems(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var (
		mu          sync.Mutex
		hookPayload map[string]any
	)
	hookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Job map[string]any `json:"job"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		hookPayload = body.Job
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer hookServer.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	job := domain.Job{ID: "job-recovered", Input: "album|us|1", Type: "album", Storefront: "us", CanonicalKey: "album:us:1",
		Status: domain.JobQueued, TotalItems: 4, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	statuses := map[string]domain.ItemStatus{
		"item-queued": domain.ItemQueued, "item-downloading": domain.ItemDownloading,
		"item-completed": domain.ItemCompleted, "item-failed": domain.ItemFailed,
	}
	index := 0
	for id, status := range statuses {
		index++
		if err := store.CreateItem(ctx, domain.JobItem{ID: id, JobID: job.ID, AdamID: id, Kind: "song", Index: index, Status: status, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}

	manager := NewManager(store, events.NewHub(), &neverRunProcessor{t: t}, 1, slog.Default())
	dispatcher := hooks.NewDispatcher(hooks.Config{Enabled: true, Entries: []hooks.Entry{
		{Name: "on-cancel", Type: "webhook", Events: []string{"job_cancelled"}, URL: hookServer.URL},
	}}, manager.Event, slog.Default())
	manager.SetHooks(dispatcher)
	if err := manager.Cancel(ctx, job.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := dispatcher.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}

	items, err := store.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]domain.ItemStatus{
		"item-queued": domain.ItemCancelled, "item-downloading": domain.ItemCancelled,
		"item-completed": domain.ItemCompleted, "item-failed": domain.ItemFailed,
	}
	for _, item := range items {
		if item.Status != want[item.ID] {
			t.Errorf("item %s = %s, want %s", item.ID, item.Status, want[item.ID])
		}
		if item.Status == domain.ItemCancelled && item.StatusMessage != cancelledItemMessage {
			t.Errorf("item %s status message = %q, want %q", item.ID, item.StatusMessage, cancelledItemMessage)
		}
	}

	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobCancelled || got.CancelledItems != 2 || got.CompletedItems != 1 || got.FailedItems != 1 {
		t.Fatalf("job = status %s cancelled %d completed %d failed %d, want cancelled 2/1/1", got.Status, got.CancelledItems, got.CompletedItems, got.FailedItems)
	}
	recorded, err := store.ListEventsAfterLimit(ctx, job.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var terminal *domain.Event
	for i := range recorded {
		if recorded[i].Type == domain.EventJobCancelled {
			terminal = &recorded[i]
		}
	}
	if terminal == nil {
		t.Fatal("no job_cancelled event recorded")
	}
	var snapshot domain.Job
	if err := json.Unmarshal(terminal.Payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.CancelledItems != 2 {
		t.Fatalf("job_cancelled payload cancelled_items = %d, want 2", snapshot.CancelledItems)
	}
	mu.Lock()
	defer mu.Unlock()
	if hookPayload["cancelled_items"] != float64(2) || hookPayload["finished_at"] == nil {
		t.Fatalf("hook job payload = %v, want cancelled_items 2 and finished_at", hookPayload)
	}
}

// Cancelling the context passed to Start is a user cancel, not a shutdown:
// only Manager.Shutdown sets ErrShutdown.
func TestStartContextCancelIsNotShutdown(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	processor := newResumableProcessor()
	manager := NewManager(store, events.NewHub(), processor, 1, slog.Default())
	ctx, stop := context.WithCancel(context.Background())
	manager.Start(ctx)
	resp := manager.SubmitBatch(context.Background(), []string{"album|us|1"}, nil)
	if resp.Accepted != 1 {
		t.Fatalf("submit = %+v", resp)
	}
	select {
	case <-processor.started:
	case <-time.After(3 * time.Second):
		t.Fatal("processor did not start")
	}
	stop()
	waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Wait(waitCtx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait: %v", err)
	}
	if got := waitForTerminal(t, store, resp.Results[0].Job.ID); got.Status != domain.JobCancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}
}
