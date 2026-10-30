package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// stepRecorder records the order in which shutdown reaches each component.
type stepRecorder struct {
	mu    sync.Mutex
	steps []string
}

func (r *stepRecorder) add(step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step)
}

func (r *stepRecorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.steps...)
}

type fakeStep struct {
	rec  *stepRecorder
	name string
	err  error
	// block makes the call wait for its context before returning its error.
	block bool
}

func (f fakeStep) call(ctx context.Context) error {
	f.rec.add(f.name)
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.err
}

type fakeHTTP struct{ fakeStep }

func (f fakeHTTP) Shutdown(ctx context.Context) error { return f.call(ctx) }

type fakeJobs struct{ fakeStep }

func (f fakeJobs) Shutdown(ctx context.Context) error { return f.call(ctx) }

type fakeDownloader struct{ fakeStep }

func (f fakeDownloader) Wait(ctx context.Context) error { return f.call(ctx) }

type fakeHooks struct {
	shutdown fakeStep
	wait     fakeStep
}

func (f fakeHooks) Shutdown(ctx context.Context) error { return f.shutdown.call(ctx) }
func (f fakeHooks) Wait(ctx context.Context) error     { return f.wait.call(ctx) }

func newFakeShutdown(rec *stepRecorder, timeouts shutdownTimeouts) shutdownSteps {
	return shutdownSteps{
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		timeouts:         timeouts,
		http:             fakeHTTP{fakeStep{rec: rec, name: "http.Shutdown"}},
		cancelHTTPBase:   func() { rec.add("cancel HTTP base context") },
		waitHTTPHandlers: fakeStep{rec: rec, name: "wait HTTP handlers"}.call,
		stopBackground:   func() { rec.add("stop watcher and refresher") },
		waitBackground:   fakeStep{rec: rec, name: "wait watcher and refresher"}.call,
		jobs:             fakeJobs{fakeStep{rec: rec, name: "jobs.Shutdown"}},
		downloader:       fakeDownloader{fakeStep{rec: rec, name: "downloader.Wait"}},
		hooks: fakeHooks{
			shutdown: fakeStep{rec: rec, name: "hooks.Shutdown"},
			wait:     fakeStep{rec: rec, name: "hooks.Wait"},
		},
	}
}

// The order is docs/architecture.md section 1: HTTP drains before the
// streams' base context is cancelled, background loops stop before the job
// workers, downloader background work after them, hooks last. The store
// closes after run returns.
func TestShutdownStepsRunInOrder(t *testing.T) {
	rec := &stepRecorder{}
	newFakeShutdown(rec, defaultShutdownTimeouts).run()
	want := []string{
		"http.Shutdown", "cancel HTTP base context", "wait HTTP handlers",
		"stop watcher and refresher", "wait watcher and refresher",
		"jobs.Shutdown", "downloader.Wait", "hooks.Shutdown",
	}
	if got := rec.got(); !reflect.DeepEqual(got, want) {
		t.Fatalf("shutdown order =\n%q\nwant\n%q", got, want)
	}
}

// A step that never finishes costs its own timeout and no more: every later
// component still stops, and hooks still running after the drain are waited
// for once more after being cancelled.
func TestShutdownStepsMovePastStuckSteps(t *testing.T) {
	rec := &stepRecorder{}
	short := shutdownTimeouts{
		httpDrain: 20 * time.Millisecond, httpHandlerExit: 20 * time.Millisecond, backgroundStop: 20 * time.Millisecond,
		jobShutdown: 20 * time.Millisecond, downloaderWait: 20 * time.Millisecond, hookDrain: 20 * time.Millisecond, hookCancel: 20 * time.Millisecond,
	}
	steps := newFakeShutdown(rec, short)
	steps.http = fakeHTTP{fakeStep{rec: rec, name: "http.Shutdown", block: true}}
	steps.waitHTTPHandlers = fakeStep{rec: rec, name: "wait HTTP handlers", block: true}.call
	steps.jobs = fakeJobs{fakeStep{rec: rec, name: "jobs.Shutdown", block: true}}
	steps.hooks = fakeHooks{
		shutdown: fakeStep{rec: rec, name: "hooks.Shutdown", block: true},
		wait:     fakeStep{rec: rec, name: "hooks.Wait", err: errors.New("still running")},
	}

	done := make(chan struct{})
	go func() {
		steps.run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish with every step bounded")
	}
	want := []string{
		"http.Shutdown", "cancel HTTP base context", "wait HTTP handlers",
		"stop watcher and refresher", "wait watcher and refresher",
		"jobs.Shutdown", "downloader.Wait", "hooks.Shutdown", "hooks.Wait",
	}
	if got := rec.got(); !reflect.DeepEqual(got, want) {
		t.Fatalf("shutdown order =\n%q\nwant\n%q", got, want)
	}
}

// docker-compose.yml's stop_grace_period must outlast the whole shutdown
// budget, or Docker kills the backend before it closes the store.
func TestShutdownBudgetFitsComposeGracePeriod(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			StopGracePeriod string `yaml:"stop_grace_period"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatal(err)
	}
	grace, err := time.ParseDuration(compose.Services["amdl-backend"].StopGracePeriod)
	if err != nil {
		t.Fatalf("amdl-backend stop_grace_period: %v", err)
	}
	if budget := defaultShutdownTimeouts.total(); budget >= grace {
		t.Fatalf("shutdown budget %s does not fit stop_grace_period %s", budget, grace)
	}
}
