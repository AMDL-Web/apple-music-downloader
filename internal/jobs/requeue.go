package jobs

import (
	"context"
	"errors"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/logging"
)

// stationType is the one job type that never reuses a row: every station
// submission draws a fresh set of tracks, so each gets its own queue entry.
const stationType = "station"

// requeue says how requeueLocked puts a finished job back in the queue.
type requeue struct {
	cause domain.RequeueCause
	// replaceOverrides swaps the stored overrides for overrides (a resubmit
	// carries the new request's); a retry keeps the stored ones.
	replaceOverrides bool
	overrides        *config.DownloadOverrides
	// createdAt, when set, becomes the job's created_at: a resubmit counts as
	// a new submission, a retry does not.
	createdAt *time.Time
	message   string
}

// requeueLocked puts the finished job back at the end of the run order under
// its id, the path a retry and a resubmit share. Caller holds submitMu and mu
// and has checked that no other active job holds the job's canonical key.
//
// It refuses with ErrJobFinalizing while the previous run's worker or
// finalize sequence still knows the job (see Retry), and with ErrQueueFull
// when the wait list is full. Unfinished items return to queued before the
// job becomes claimable; finished items stay, and the next run checks them
// against the files. It records job_retried with the cause and returns the
// requeued row.
func (m *Manager) requeueLocked(ctx context.Context, job domain.Job, r requeue) (domain.Job, error) {
	// The terminal status is persisted before the old worker's deferred
	// cleanup removes its m.cancels entry (and before Cancel's queued path
	// clears its finalizing mark), so a requeue racing that window could let a
	// new worker register its cancel func and then have the old worker's
	// deferred delete remove it, leaving Cancel and Delete blind to the new
	// run. Same guard as Delete.
	if m.cancels[job.ID] != nil || m.finalizing[job.ID] {
		return domain.Job{}, ErrJobFinalizing
	}
	if m.waiting.admitted() >= m.capacity {
		return domain.Job{}, ErrQueueFull
	}
	order, err := m.store.NextQueueOrder(ctx)
	if err != nil {
		return domain.Job{}, err
	}
	// One batch statement rather than per-item updates: this runs while
	// holding both scheduling locks, and a large collection must not stall
	// every other queue operation for hundreds of round-trips.
	now := time.Now().UTC()
	if err := m.store.ResetUnfinishedItems(ctx, job.ID, now); err != nil {
		return domain.Job{}, err
	}
	// A requeue is a new run: its worker sets started_at again, so the run's
	// duration does not count the time the job sat finished. The order and
	// the status land in one statement, so the job is never claimable at its
	// old place.
	if err := m.store.RequeueJob(ctx, job.ID, db.RequeueParams{
		QueueOrder: order, ReplaceOverrides: r.replaceOverrides, Overrides: r.overrides,
		CreatedAt: r.createdAt, Now: now,
	}); err != nil {
		return domain.Job{}, err
	}
	// Read the row back so the event payload and the caller see the counters
	// the reset above changed.
	requeued, err := m.store.GetJob(ctx, job.ID)
	if err != nil {
		return domain.Job{}, err
	}
	m.waiting.add(job.ID, false)
	delete(m.unclaimable, job.ID)
	m.wake.broadcast()
	// The row already says queued and workers may claim it, so a failed event
	// below is only logged, like job_queued in admitLocked. A worker's claim
	// waits for mu, so job_started cannot precede job_retried.
	force := requeued.Overrides != nil && requeued.Overrides.ForceOverwrite != nil && *requeued.Overrides.ForceOverwrite
	payload := domain.MarshalEventPayload(requeued, map[string]any{"message": r.message, "cause": r.cause, "force_overwrite": force})
	if err := m.Event(ctx, domain.Event{JobID: job.ID, Type: domain.EventJobRetried, Message: r.message, Payload: payload}); err != nil {
		logging.FromContext(ctx, m.logger).Error("record job_retried event", "job_id", job.ID, "error", err)
	}
	return requeued, nil
}

// enqueue queues one validated candidate whose key has no active job: it
// requeues the key's newest finished job under its id, or creates a job when
// the key has none or names a station. Caller holds submitMu. mu is held from
// the lookup to the write, so Delete cannot remove the row in between.
func (m *Manager) enqueue(ctx context.Context, url string, valid ValidationResult, key string, overrides *config.DownloadOverrides, now time.Time) (domain.Job, domain.SubmitResult, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if valid.Type == stationType {
		return m.admitLocked(ctx, url, valid, key, overrides, now)
	}
	prev, found, err := m.store.FindLatestFinishedJobByKey(ctx, key)
	if err != nil {
		return domain.Job{}, storeFailure(url, "look up finished jobs", err), false
	}
	if !found {
		return m.admitLocked(ctx, url, valid, key, overrides, now)
	}
	// A terminal hook of the previous run may still be working on its files;
	// the job stays where it is until the hook ends, as Delete waits for it.
	if m.hooks.Pending(prev.ID) {
		return domain.Job{}, duplicateActiveResult(url, prev.ID), false
	}
	job, err := m.requeueLocked(ctx, prev, requeue{
		cause: domain.RequeueResubmit, replaceOverrides: true, overrides: overrides, createdAt: &now,
		message: "job queued again by a new submission",
	})
	switch {
	case errors.Is(err, ErrJobFinalizing):
		return domain.Job{}, duplicateActiveResult(url, prev.ID), false
	case errors.Is(err, ErrQueueFull):
		return domain.Job{}, queueFullResult(url), false
	case errors.Is(err, db.ErrDuplicateActive):
		return domain.Job{}, duplicateActiveResult(url, ""), false
	case err != nil:
		return domain.Job{}, storeFailure(url, "requeue job", err), false
	}
	// Like a new submission, the job_queued hook fires with no items: the run
	// resolves them again.
	m.hooks.Dispatch(string(domain.EventJobQueued), job, nil)
	accepted := job
	return job, domain.SubmitResult{URL: url, Status: domain.SubmitAccepted, Job: &accepted, Requeued: true}, true
}
