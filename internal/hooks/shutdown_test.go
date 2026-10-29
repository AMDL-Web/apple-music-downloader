package hooks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/domain"
)

// Shutdown stops a failing hook from retrying instead of waiting out every
// remaining attempt, and the hook still records its failure.
func TestShutdownStopsRetries(t *testing.T) {
	var attempts atomic.Int32
	firstAttempt := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			close(firstAttempt)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	collector := newEventCollector(2) // hook_started + hook_failed
	d := NewDispatcher(Config{Enabled: true, Entries: []Entry{
		{Name: "flaky", Type: "webhook", Events: []string{"job_finished"}, URL: server.URL, MaxAttempts: 10},
	}}, collector.record, discardLogger())
	d.Dispatch("job_finished", domain.Job{ID: "job-1", Type: "song"}, nil)
	select {
	case <-firstAttempt:
	case <-time.After(3 * time.Second):
		t.Fatal("hook never ran")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= retryDelay {
		t.Errorf("Shutdown took %s; it should not wait out the %s retry delay", elapsed, retryDelay)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1: no retry may start once Shutdown has begun", got)
	}
	evs := collector.wait(t)
	last := evs[len(evs)-1]
	if last.Type != domain.EventHookFailed || !strings.Contains(last.Message, "not retried") {
		t.Fatalf("last event = %s %q, want hook_failed saying it was not retried", last.Type, last.Message)
	}
}

// When Shutdown's wait expires it cuts the running attempt, which records
// hook_failed, so a later Wait returns promptly instead of after the hook's
// own timeout.
func TestShutdownDeadlineAbortsRunningAttempts(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release) // before server.Close, which waits for this handler

	collector := newEventCollector(2) // hook_started + hook_failed
	d := NewDispatcher(Config{Enabled: true, Entries: []Entry{
		{Name: "hanging", Type: "webhook", Events: []string{"job_finished"}, URL: server.URL},
	}}, collector.record, discardLogger())
	d.Dispatch("job_finished", domain.Job{ID: "job-1", Type: "song"}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := d.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want deadline exceeded", err)
	}
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelWait()
	if err := d.Wait(waitCtx); err != nil {
		t.Fatalf("Wait after the Shutdown deadline: %v; the running attempt was not cancelled", err)
	}
	evs := collector.wait(t)
	if last := evs[len(evs)-1]; last.Type != domain.EventHookFailed {
		t.Fatalf("last event = %s, want hook_failed", last.Type)
	}
}

// A recorder failure is logged, and the execution still finishes so Pending
// drops and Shutdown returns.
func TestRecorderFailureDoesNotWedgeExecution(t *testing.T) {
	runner := &recordingRunner{}
	const runnerType = "recorder-failure-test"

	var recorded atomic.Int32
	failing := func(context.Context, domain.Event) error {
		recorded.Add(1)
		return errors.New("store is closed")
	}
	d := NewDispatcher(Config{Enabled: true, Entries: []Entry{
		{Name: "h", Type: runnerType, Events: []string{"job_finished"}},
	}}, failing, discardLogger())
	d.runner = runner
	d.Dispatch("job_finished", domain.Job{ID: "job-1", Type: "song"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := recorded.Load(); got != 2 {
		t.Fatalf("recorder calls = %d, want 2 (hook_started, hook_succeeded)", got)
	}
	if d.Pending("job-1") {
		t.Fatal("Pending = true after the execution finished")
	}
}
