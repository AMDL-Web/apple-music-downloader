package hooks

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"amdl/internal/domain"
)

// failingRunner fails every attempt, counting them.
type failingRunner struct{ attempts atomic.Int32 }

func (r *failingRunner) Run(context.Context, Entry, Payload) error {
	r.attempts.Add(1)
	return errors.New("upstream answered 500")
}

// hangingRunner blocks every attempt until its context ends.
type hangingRunner struct{}

func (hangingRunner) Run(ctx context.Context, _ Entry, _ Payload) error {
	<-ctx.Done()
	return ctx.Err()
}

// A failing hook is retried after retryDelay each time, up to max_attempts,
// and then records hook_failed.
func TestDispatchRetriesAfterTheRetryDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runner := &failingRunner{}
		collector := newEventCollector(2) // hook_started + hook_failed
		d := NewDispatcher(Config{Enabled: true, Entries: []Entry{
			{Name: "flaky", Type: "fake", Events: []string{"job_finished"}, MaxAttempts: 3},
		}}, collector.record, discardLogger())
		d.runner = runner
		start := time.Now()
		d.Dispatch("job_finished", domain.Job{ID: "job-1", Type: "song"}, nil)
		evs := collector.wait(t)
		if elapsed := time.Since(start); elapsed != 2*retryDelay {
			t.Errorf("three attempts took %s, want two retry delays (%s)", elapsed, 2*retryDelay)
		}
		if err := d.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := runner.attempts.Load(); got != 3 {
			t.Fatalf("attempts = %d, want 3 (max_attempts = 3)", got)
		}
		if last := evs[len(evs)-1]; last.Type != domain.EventHookFailed || !strings.Contains(string(last.Payload), `"attempts":3`) {
			t.Fatalf("last event = %s %s, want hook_failed after 3 attempts", last.Type, last.Payload)
		}
	})
}

// Shutdown stops a failing hook from retrying instead of waiting out every
// remaining attempt, and the hook still records its failure.
func TestShutdownStopsRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runner := &failingRunner{}
		collector := newEventCollector(2) // hook_started + hook_failed
		d := NewDispatcher(Config{Enabled: true, Entries: []Entry{
			{Name: "flaky", Type: "fake", Events: []string{"job_finished"}, MaxAttempts: 10},
		}}, collector.record, discardLogger())
		d.runner = runner
		d.Dispatch("job_finished", domain.Job{ID: "job-1", Type: "song"}, nil)
		synctest.Wait() // the first attempt failed; the hook waits to retry

		start := time.Now()
		if err := d.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
		if elapsed := time.Since(start); elapsed != 0 {
			t.Errorf("Shutdown took %s; it should not wait out the %s retry delay", elapsed, retryDelay)
		}
		if got := runner.attempts.Load(); got != 1 {
			t.Errorf("attempts = %d, want 1: no retry may start once Shutdown has begun", got)
		}
		evs := collector.wait(t)
		last := evs[len(evs)-1]
		if last.Type != domain.EventHookFailed || !strings.Contains(last.Message, "not retried") {
			t.Fatalf("last event = %s %q, want hook_failed saying it was not retried", last.Type, last.Message)
		}
	})
}

// When Shutdown's wait expires it cuts the running attempt, which records
// hook_failed, so a later Wait returns at once instead of after the hook's
// own timeout.
func TestShutdownDeadlineAbortsRunningAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		collector := newEventCollector(2) // hook_started + hook_failed
		d := NewDispatcher(Config{Enabled: true, Entries: []Entry{
			{Name: "hanging", Type: "fake", Events: []string{"job_finished"}},
		}}, collector.record, discardLogger())
		d.runner = hangingRunner{}
		d.Dispatch("job_finished", domain.Job{ID: "job-1", Type: "song"}, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if err := d.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Shutdown error = %v, want deadline exceeded", err)
		}
		start := time.Now()
		if err := d.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed != 0 {
			t.Fatalf("Wait after the Shutdown deadline took %s; the running attempt was not cancelled", elapsed)
		}
		evs := collector.wait(t)
		if last := evs[len(evs)-1]; last.Type != domain.EventHookFailed {
			t.Fatalf("last event = %s, want hook_failed", last.Type)
		}
	})
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
