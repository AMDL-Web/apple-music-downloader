package main

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Shutdown budget. Each step waits at most its own timeout and then moves on,
// so their sum bounds how long SIGTERM takes. docker-compose.yml's
// stop_grace_period must stay above that sum, or Docker kills the process
// before the store is closed; TestShutdownBudgetFitsComposeGracePeriod checks
// it.
const (
	// httpDrainTimeout is how long ordinary requests get after the listener
	// closes. An open SSE stream holds http.Server.Shutdown for all of it, so
	// it stays short.
	httpDrainTimeout = 5 * time.Second
	// httpHandlerExitTimeout bounds the wait for handlers, SSE streams
	// included, after their base context is cancelled.
	httpHandlerExitTimeout = 5 * time.Second
	// backgroundStopTimeout bounds the wait for the library watcher and the
	// token refresher; a pass in flight ends with its context.
	backgroundStopTimeout = 10 * time.Second
	// jobShutdownTimeout bounds the wait for workers to return from
	// ProcessJob after they are cancelled with jobs.ErrShutdown.
	jobShutdownTimeout = 45 * time.Second
	// downloaderWaitTimeout bounds the wait for background downloader work
	// (the motion-artwork backfill) that jobs started.
	downloaderWaitTimeout = 15 * time.Second
	// hookDrainTimeout is how long running hook attempts get to finish;
	// retries stop as soon as the drain begins.
	hookDrainTimeout = 15 * time.Second
	// hookCancelTimeout bounds the wait for hook attempts after the drain
	// cancelled them; an exec hook gets its WaitDelay to exit.
	hookCancelTimeout = 10 * time.Second
)

// shutdownTimeouts are the per-step bounds of shutdownSteps; tests shrink them.
type shutdownTimeouts struct {
	httpDrain, httpHandlerExit, backgroundStop, jobShutdown, downloaderWait, hookDrain, hookCancel time.Duration
}

var defaultShutdownTimeouts = shutdownTimeouts{
	httpDrain: httpDrainTimeout, httpHandlerExit: httpHandlerExitTimeout, backgroundStop: backgroundStopTimeout,
	jobShutdown: jobShutdownTimeout, downloaderWait: downloaderWaitTimeout, hookDrain: hookDrainTimeout, hookCancel: hookCancelTimeout,
}

// total is the longest shutdownSteps.run can take.
func (t shutdownTimeouts) total() time.Duration {
	return t.httpDrain + t.httpHandlerExit + t.backgroundStop + t.jobShutdown + t.downloaderWait + t.hookDrain + t.hookCancel
}

// shutdownSteps stops the running components in the order of
// docs/architecture.md section 1. The store, the wrapper client and the log
// system are closed by run's defers after it returns, so nothing that may
// still use them outlives them.
type shutdownSteps struct {
	logger   *slog.Logger
	timeouts shutdownTimeouts

	http interface {
		Shutdown(context.Context) error
	}
	// cancelHTTPBase cancels the base context of every request, which ends
	// the SSE streams.
	cancelHTTPBase   func()
	waitHTTPHandlers func(context.Context) error
	// stopBackground cancels the library watcher and the token refresher.
	stopBackground func()
	waitBackground func(context.Context) error
	jobs           interface {
		Shutdown(context.Context) error
	}
	downloader interface {
		Wait(context.Context) error
	}
	hooks interface {
		Shutdown(context.Context) error
		Wait(context.Context) error
	}
}

func (s shutdownSteps) run() {
	started := time.Now()
	s.logger.Info("shutting down")

	// 1. HTTP: stop accepting and drain ordinary requests, then end the
	// streams that Shutdown does not wait out on its own.
	if err := s.within(s.timeouts.httpDrain, s.http.Shutdown); err != nil {
		s.logger.Info("HTTP drain ended with requests still open; ending streams", "timeout", s.timeouts.httpDrain, "error", err)
	}
	s.cancelHTTPBase()
	s.wait("HTTP handlers", s.timeouts.httpHandlerExit, s.waitHTTPHandlers)

	// 2. Library watcher and token refresher.
	s.stopBackground()
	s.wait("library watcher and token refresher", s.timeouts.backgroundStop, s.waitBackground)

	// 3. Job workers, cancelled with jobs.ErrShutdown: interrupted jobs stay
	// running in the store for recovery on the next start.
	s.wait("job workers", s.timeouts.jobShutdown, s.jobs.Shutdown)

	// 4. Background downloader work the jobs started.
	s.wait("downloader background work", s.timeouts.downloaderWait, s.downloader.Wait)

	// 5. Hooks: drain, then cut what is still running.
	if err := s.within(s.timeouts.hookDrain, s.hooks.Shutdown); err != nil {
		s.logger.Warn("hooks still running after the drain; cancelled them", "timeout", s.timeouts.hookDrain)
		s.wait("cancelled hooks", s.timeouts.hookCancel, s.hooks.Wait)
	}
	s.logger.Info("shutdown complete", "duration_ms", time.Since(started).Milliseconds())
}

// within runs f with a context that expires after timeout.
func (s shutdownSteps) within(timeout time.Duration, f func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return f(ctx)
}

// wait runs one bounded step. When it times out, shutdown logs and moves on:
// a stuck component must not turn SIGTERM into a process that never exits.
func (s shutdownSteps) wait(what string, timeout time.Duration, f func(context.Context) error) {
	if err := s.within(timeout, f); err != nil {
		s.logger.Error("gave up waiting during shutdown; continuing", "step", what, "timeout", timeout, "error", err)
	}
}

// waitGroup waits for wg until ctx ends.
func waitGroup(ctx context.Context, wg *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
