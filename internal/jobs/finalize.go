package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/limits"
	"amdl/internal/logging"
)

// ErrJobNotCancellable is returned by Cancel for a job that already ended
// completed or failed: there is nothing left to stop. Cancelling a cancelled
// job succeeds again.
var ErrJobNotCancellable = errors.New("the job already finished; only a queued or running job can be cancelled")

// Cancel stops a queued or running job.
func (m *Manager) Cancel(ctx context.Context, jobID string) error {
	m.mu.Lock()
	if cancel := m.cancels[jobID]; cancel != nil {
		m.mu.Unlock()
		// The job is actively running under a worker. Cancelling its context
		// is enough: run() observes ctx.Err() once ProcessJob returns and
		// finalizes the job (status, event, hook dispatch) exactly once.
		// Writing the terminal state here too would race with that write
		// and double-fire the job_cancelled hook.
		cancel()
		logging.FromContext(ctx, m.logger).Info("job cancellation requested", "job_id", jobID)
		return nil
	}

	// The job is not running under a worker. Read and write its terminal
	// status while still holding m.mu: run()'s startup claim takes the same
	// lock and performs its status-check + mark-running under it, so this
	// critical section is mutually exclusive with a worker claiming the job.
	// Without this, a worker could observe "queued" and mark the job running
	// in the window between our map read and status write, resurrecting a
	// job we just cancelled.
	job, err := m.store.GetJob(ctx, jobID)
	if err != nil {
		m.mu.Unlock()
		if errors.Is(err, sql.ErrNoRows) {
			return db.ErrJobNotFound
		}
		return err
	}
	if job.Status.IsTerminal() {
		m.mu.Unlock()
		if job.Status == domain.JobCancelled {
			return nil
		}
		return ErrJobNotCancellable
	}
	// From here on the cancel is decided, and the caller's context (an HTTP
	// request) must not stop it half-way: items written cancelled with the job
	// row still queued would be reset and downloaded by the next worker.
	ctx = context.WithoutCancel(ctx)
	// No worker holds the job, so nothing else ends its items. A recovered job
	// can carry rows a previous process left mid-download; end them before the
	// terminal write, then re-read the job so the counters in its row snapshot,
	// terminal event and hook payload include them.
	now := time.Now().UTC()
	if err := m.store.CancelUnfinishedItems(ctx, jobID, cancelledItemMessage, now); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("cancel unfinished items of job %s: %w", jobID, err)
	}
	if job, err = m.store.GetJob(ctx, jobID); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("reload cancelled job %s: %w", jobID, err)
	}
	job.Status = domain.JobCancelled
	job.Error = "cancelled"
	job.UpdatedAt = now
	job, persistErr := m.persistTerminal(ctx, job, domain.EventJobCancelled, "job cancelled")
	if persistErr == nil {
		// Mark the finalize sequence in flight before releasing m.mu: the
		// status row (and its terminal event, written atomically alongside
		// it) now say cancelled, but hook dispatch below still reads the
		// job's rows. Delete must not remove them in that window.
		m.finalizing[jobID] = true
	}
	m.mu.Unlock()

	// Only report success and dispatch hooks once the terminal status is
	// durably persisted.
	if persistErr != nil {
		return persistErr
	}
	if cleaner, ok := m.processor.(ArtifactCleaner); ok {
		cleaner.CleanupJobArtifacts(job)
	}
	m.dispatchHooks(ctx, job)
	m.mu.Lock()
	delete(m.finalizing, jobID)
	m.mu.Unlock()
	m.wakeStreams(jobID)
	logging.FromContext(ctx, m.logger).Info("queued job cancelled", "job_id", jobID)
	return nil
}

// Delete removes a terminal job and its items/events from the store. It
// refuses while the job is running, finalizing, or still executing a hook:
// deleting earlier would orphan late hook events and make an active per-job
// stream impossible to exhaust cleanly. The check and delete both happen
// under m.mu, which run()'s claim and Cancel's queued path also hold, so a job
// cannot start (or finish) finalizing between the check and the delete.
func (m *Manager) Delete(ctx context.Context, jobID string) error {
	m.mu.Lock()
	if m.cancels[jobID] != nil || m.finalizing[jobID] || m.hooks.Pending(jobID) {
		m.mu.Unlock()
		return db.ErrJobNotTerminal
	}
	// The full row is only needed to locate leftover artifacts. A terminal row
	// whose persisted overrides no longer decode must stay deletable — its
	// cleanup metadata is unavailable either way — so a scan failure degrades
	// to deleting without artifact cleanup instead of wedging the job forever.
	job, jobErr := m.store.GetJob(ctx, jobID)
	if jobErr != nil && errors.Is(jobErr, sql.ErrNoRows) {
		m.mu.Unlock()
		return db.ErrJobNotFound
	}
	deleted, err := m.store.DeleteJob(ctx, jobID)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if jobErr != nil {
		logging.FromContext(ctx, m.logger).Warn("job deleted without artifact cleanup", "job_id", jobID, "error", jobErr)
	} else if cleaner, ok := m.processor.(ArtifactCleaner); ok {
		cleaner.CleanupJobArtifacts(job)
	}
	// Broadcast the persisted tombstone so live overview subscribers can drop the
	// job immediately; missed broadcasts are replayed from job_events by cursor.
	m.hub.Publish(deleted)
	logging.FromContext(ctx, m.logger).Info("job deleted", "job_id", jobID)
	return nil
}

func (m *Manager) run(parent context.Context, jobID string) {
	ctx, cancel := context.WithCancel(parent)

	// Claim the job atomically under m.mu: read its current status, and only
	// if it is still startable, mark it running and register the cancel func.
	// Cancel() takes the same lock for its queued-cancel transition, so the
	// two cannot interleave — either we mark running first (and a concurrent
	// Cancel then finds the cancel func and cancels our context) or Cancel
	// marks cancelled first (and we observe the terminal status here and bail).
	m.mu.Lock()
	job, err := m.store.GetJob(parent, jobID)
	if err != nil {
		m.mu.Unlock()
		cancel()
		m.logClaimFailure(parent, jobID, "load job", err)
		return
	}
	if job.Status != domain.JobQueued {
		// A stale delivery. A job cancelled while queued keeps its id in the
		// worker channel, and a retry enqueues it again, so one job can be
		// delivered twice: Cancel() already finalized it (its terminal hook was
		// dispatched, do not resurrect it), or another worker already claimed it
		// and the row says running. Either way this delivery has nothing to run.
		m.mu.Unlock()
		cancel()
		return
	}
	job.Status = domain.JobRunning
	job.UpdatedAt = time.Now().UTC()
	if job.StartedAt == nil {
		startedAt := job.UpdatedAt
		job.StartedAt = &startedAt
	}
	if err := m.store.UpdateJob(ctx, job); err != nil {
		m.mu.Unlock()
		cancel()
		m.logClaimFailure(parent, jobID, "mark job running", err)
		return
	}
	m.cancels[jobID] = cancel
	m.mu.Unlock()
	started := time.Now()
	jobLogger := m.logger.With("job_id", job.ID, "job_type", job.Type, "storefront", job.Storefront)
	ctx = logging.NewContext(ctx, jobLogger)
	// Rank every pool acquire this job makes by its submission time, so when
	// the process-wide pools are contended the oldest unfinished job drains
	// first instead of interleaving with later ones. Recovered jobs keep their
	// original CreatedAt and therefore their place in line across restarts.
	ctx = limits.WithPriority(ctx, job.CreatedAt.UnixNano())
	jobLogger.Info("job started")

	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.cancels, jobID)
		m.mu.Unlock()
		m.wakeStreams(jobID)
	}()

	// A download's decrypt/remux pipeline allocates on the order of a track's
	// size per parallel track; once the job returns, that memory is garbage but
	// Go's scavenger only hands it back to the OS lazily (MADV_FREE), so RSS
	// appears stuck at the peak long after the download finished. Force a
	// scavenge once the worker pool goes idle so a machine idling between jobs
	// actually reclaims it. Gating on the active count (rather than an
	// unconditional per-job call) means a burst of concurrent completions under
	// max_running_jobs>1 triggers a single stop-the-world GC when the last job
	// finishes, not one per job that would stall the others still downloading.
	m.active.Add(1)
	defer func() {
		if m.active.Add(-1) == 0 {
			debug.FreeOSMemory()
		}
	}()

	message := "job started"
	if err := m.Event(ctx, domain.Event{JobID: job.ID, Type: domain.EventJobStarted, Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})}); err != nil {
		jobLogger.Warn("record job_started event", "error", err)
	}

	err = m.processRecovered(ctx, job, jobLogger)
	panicked := errors.Is(err, errProcessorPanic)
	if !panicked && errors.Is(context.Cause(ctx), ErrShutdown) {
		// Interrupted, not finished: the row stays running with its items,
		// checkpoints and media-user-token, and no terminal event or hook
		// fires, so RecoverUnfinished resumes it on the next start. A panic is
		// excluded because recovery would only run into it again. A nil error
		// counts as interrupted too: ProcessJob need not surface ctx errors, so
		// nil does not prove every item ended, and a resumed job that had
		// finished only resolves again and skips its finished items.
		jobLogger.Info("job interrupted by shutdown; it resumes on the next start", "duration_ms", time.Since(started).Milliseconds())
		return
	}
	// The reload carries the item counters the terminal decision and the
	// terminal event payload use, derived by the store from job_items.
	latest, loadErr := m.store.GetJob(context.Background(), job.ID)
	if loadErr == nil {
		job = latest
	} else {
		jobLogger.Error("reload job before finalize", "error", loadErr)
	}

	switch {
	case panicked:
		// A panic is a defect, not a verdict on the input: fail the job so
		// Retry stays available, whatever else happened meanwhile.
		job.Status = domain.JobFailed
		job.Error = err.Error()
		m.finalizeLogged(job, domain.EventJobFailed, err.Error())
	case errors.Is(ctx.Err(), context.Canceled):
		// Cancellation takes priority over ProcessJob's return value: a job
		// cancelled mid-flight must never be reported as completed, even if
		// ProcessJob happened to return nil (e.g. it finished its last item
		// just as the cancel arrived, or it doesn't surface ctx errors).
		job.Status = domain.JobCancelled
		job.Error = "cancelled"
		m.finalizeLogged(job, domain.EventJobCancelled, "job cancelled")
	case err != nil:
		job.Status = domain.JobFailed
		job.Error = err.Error()
		m.finalizeLogged(job, domain.EventJobFailed, err.Error())
	case loadErr != nil:
		// Without the reload the counters are the claim-time ones, so success
		// cannot be judged. Failing keeps Retry available, and a retry skips the
		// items that did finish.
		job.Status = domain.JobFailed
		job.Error = fmt.Sprintf("read final item states: %v", loadErr)
		m.finalizeLogged(job, domain.EventJobFailed, job.Error)
	default:
		if job.FailedItems > 0 {
			job.Status = domain.JobFailed
		} else {
			job.Status = domain.JobCompleted
		}
		m.finalizeLogged(job, domain.EventJobFinished, string(job.Status))
	}
	attrs := []any{
		"status", string(job.Status), "duration_ms", time.Since(started).Milliseconds(),
		"total_items", job.TotalItems, "completed_items", job.CompletedItems, "skipped_items", job.SkippedItems,
		"failed_items", job.FailedItems, "cancelled_items", job.CancelledItems,
	}
	if err != nil {
		attrs = append(attrs, "error", err)
		if body := logging.UpstreamBody(err); body != "" {
			attrs = append(attrs, "upstream_body", body)
		}
	}
	switch job.Status {
	case domain.JobFailed:
		jobLogger.Error("job finished", attrs...)
	case domain.JobCancelled:
		jobLogger.Warn("job finished", attrs...)
	default:
		jobLogger.Info("job finished", attrs...)
	}
}

// logClaimFailure logs a worker that could not claim jobID. Losing the race
// with Shutdown is expected: the row is still queued, and the next start
// recovers it.
func (m *Manager) logClaimFailure(parent context.Context, jobID, op string, err error) {
	if errors.Is(context.Cause(parent), ErrShutdown) {
		m.logger.Info("job left queued by shutdown; recovery runs it on the next start", "job_id", jobID)
		return
	}
	m.logger.Error(op, "job_id", jobID, "error", err)
}

// errProcessorPanic wraps the value of a panic inside Processor.ProcessJob, so
// the job fails with "internal error: panic: <value>" instead of taking the
// process down.
var errProcessorPanic = errors.New("internal error: panic")

// processRecovered runs ProcessJob and turns a panic into an error wrapping
// errProcessorPanic, logging the stack while it is still available. The worker
// goroutine survives and the job finalizes like any other failure.
func (m *Manager) processRecovered(ctx context.Context, job domain.Job, logger *slog.Logger) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("job processor panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			err = fmt.Errorf("%w: %v", errProcessorPanic, r)
		}
	}()
	return m.processor.ProcessJob(ctx, job, m)
}

// cancelledItemMessage is the status message of an item whose job the user
// cancelled. internal/media writes the same text for items a running job ends.
const cancelledItemMessage = "Cancelled"

// finalizeJob persists a job's terminal status, emits the corresponding
// domain event, and dispatches any post-download hooks subscribed to that
// status. Shared by run() and Cancel() so every path that reaches a terminal
// status goes through hook dispatch exactly once.
//
// The hook is dispatched only after the terminal status is durably persisted:
// an external system must not be told a job completed/cancelled when the
// backend could not even record that fact. A persistence failure is returned
// so callers can propagate or log it, and no hook fires.
func (m *Manager) finalizeJob(ctx context.Context, job domain.Job, eventType domain.EventType, message string) error {
	job.UpdatedAt = time.Now().UTC()
	job, err := m.persistTerminal(ctx, job, eventType, message)
	if err != nil {
		return err
	}
	m.dispatchHooks(ctx, job)
	return nil
}

// persistTerminal atomically persists job's terminal status, with finished_at
// set to job.UpdatedAt, together with its terminal domain event
// (Store.FinalizeJob), then publishes the stored event to the hub, and returns
// job as persisted. The atomicity matters to the per-job streams: they read a
// job's status and its events table independently, so if these two writes
// ever landed as separate statements, such a reader could observe the
// terminal status with the terminal event still missing and wrongly conclude
// no more events are pending.
func (m *Manager) persistTerminal(ctx context.Context, job domain.Job, eventType domain.EventType, message string) (domain.Job, error) {
	finishedAt := job.UpdatedAt
	job.FinishedAt = &finishedAt
	ev := domain.Event{JobID: job.ID, Type: eventType, Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})}
	stored, err := m.store.FinalizeJob(ctx, job, ev)
	if err != nil {
		return job, err
	}
	m.hub.Publish(stored)
	return job, nil
}

// dispatchHooks fires any post-download hooks subscribed to job's terminal
// status. Must be called only after persistTerminal has durably committed the
// terminal status and its event, and never while holding m.mu (Dispatch is
// non-blocking, but the store read here should not extend the lock).
func (m *Manager) dispatchHooks(ctx context.Context, job domain.Job) {
	if hookEvent := hookEventForStatus(job.Status); hookEvent != "" {
		items, err := m.store.ListItems(ctx, job.ID)
		if err != nil {
			// The hook still fires: its job counters are right, and a
			// refresh-style hook needs no items. Only the per-item list and
			// AMDL_OUTPUT_PATHS are missing.
			m.logger.Error("list items for hook payload; dispatching without items", "job_id", job.ID, "hook_event", hookEvent, "error", err)
		}
		m.hooks.Dispatch(hookEvent, job, items)
	}
}

// finalizeLogged is the worker-path wrapper around finalizeJob: there is no
// caller to return an error to, so a persistence failure is logged (and the
// hook is correctly skipped by finalizeJob).
func (m *Manager) finalizeLogged(job domain.Job, eventType domain.EventType, message string) {
	if err := m.finalizeJob(context.Background(), job, eventType, message); err != nil {
		m.logger.Error("finalize job", "job_id", job.ID, "status", string(job.Status), "error", err)
	}
}

// hookEventForStatus maps a job's final status to the hook event name hook
// entries subscribe to in hooks.yaml. Returns "" for non-terminal statuses.
func hookEventForStatus(status domain.JobStatus) string {
	switch status {
	case domain.JobCompleted:
		return string(domain.EventJobFinished)
	case domain.JobFailed:
		return string(domain.EventJobFailed)
	case domain.JobCancelled:
		return string(domain.EventJobCancelled)
	default:
		return ""
	}
}
