package jobs

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/ids"
	"amdl/internal/logging"
)

var ErrQueueFull = errors.New("job queue is full")

// ErrJobNotRetryable is returned by Retry for jobs that are neither failed nor
// cancelled: queued/running jobs are already progressing, and a completed job
// has nothing left to retry.
var ErrJobNotRetryable = errors.New("only failed or cancelled jobs can be retried")

// ErrJobFinalizing is returned by Retry when the job's row already reads
// failed but its previous run has not fully wound down (the worker's cancel
// registration is still in place, or a finalize sequence is dispatching
// hooks). Retrying in that window would let the old worker's deferred
// cleanup delete the new run's cancel entry. The window is tiny; callers can
// simply retry shortly.
var ErrJobFinalizing = errors.New("job is still finalizing its previous run; retry again shortly")

// SubmitBatch validates and enqueues each url independently, applying
// three-layer dedup: request-internal (canonical key seen earlier in this
// batch), active-job lookup, and the DB's partial unique index as a backstop
// against races.
//
// overrides, when non-nil, is attached to every job created from this batch
// and overlays the runtime config while those jobs run. Callers validate it
// (DownloadOverrides.ApplyValidated) before submitting.
func (m *Manager) SubmitBatch(ctx context.Context, urls []string, overrides *config.DownloadOverrides) domain.BatchSubmitResponse {
	results := make([]domain.SubmitResult, len(urls))

	type candidate struct {
		index int
		url   string
		key   string
		valid ValidationResult
	}
	var candidates []candidate
	seenKeys := map[string]bool{}
	for i, url := range urls {
		validated, err := m.processor.ValidateRequest(ctx, url)
		if err != nil {
			results[i] = validationFailure(url, err)
			continue
		}
		key := canonicalKey(validated.Type, validated.Storefront, validated.ID)
		if seenKeys[key] {
			results[i] = domain.SubmitResult{URL: url, Status: domain.SubmitDuplicateInRequest, Error: &domain.SubmitError{
				Code: domain.SubmitCodeDuplicateInRequest, Message: "an earlier URL in this request names the same download",
			}}
			continue
		}
		seenKeys[key] = true
		candidates = append(candidates, candidate{index: i, url: url, key: key, valid: validated})
	}

	// Serialize the dedup lookup with Retry and other submits. Each admission
	// below (capacity check, insert, job_queued, wait-list entry) also holds
	// m.mu, so a worker cannot claim the job before its job_queued event and
	// hook, and a free slot cannot disappear half-way.
	m.submitMu.Lock()
	defer m.submitMu.Unlock()

	now := time.Now().UTC()
	queueFull := false
	for _, c := range candidates {
		if queueFull {
			results[c.index] = queueFullResult(c.url)
			continue
		}
		existing, found, err := m.store.FindActiveJobByKey(ctx, c.key)
		if err != nil {
			results[c.index] = storeFailure(c.url, "look up active jobs", err)
			continue
		}
		if found {
			results[c.index] = duplicateActiveResult(c.url, existing.ID)
			continue
		}
		job, result, admitted := m.admit(ctx, c.url, c.valid, c.key, overrides, now)
		results[c.index] = result
		if result.Status == domain.SubmitQueueFull {
			queueFull = true
		}
		if admitted {
			m.wake.broadcast()
			logging.FromContext(ctx, m.logger).Info("job queued", "job_id", job.ID, "job_type", job.Type, "storefront", job.Storefront)
		}
	}

	resp := domain.BatchSubmitResponse{Results: results}
	for _, r := range results {
		if r.Status == domain.SubmitAccepted {
			resp.Accepted++
		} else {
			resp.Rejected++
		}
	}
	return resp
}

// admit creates one queued job at the end of the run order, records its
// job_queued event, fires the job_queued hook and adds it to the wait list,
// all under m.mu. Caller holds submitMu and has checked dedup.
func (m *Manager) admit(ctx context.Context, url string, valid ValidationResult, key string, overrides *config.DownloadOverrides, now time.Time) (domain.Job, domain.SubmitResult, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.waiting.admitted() >= m.capacity {
		return domain.Job{}, queueFullResult(url), false
	}
	order, err := m.store.NextQueueOrder(ctx)
	if err != nil {
		return domain.Job{}, storeFailure(url, "read queue order", err), false
	}
	job := domain.Job{
		ID: ids.NewID("job"), Input: url, Type: valid.Type, Storefront: valid.Storefront, CanonicalKey: key,
		Overrides: overrides, Status: domain.JobQueued, QueueOrder: &order, CreatedAt: now, UpdatedAt: now,
	}
	if err := m.store.CreateJob(ctx, job); err != nil {
		if errors.Is(err, db.ErrDuplicateActive) {
			return domain.Job{}, duplicateActiveResult(url, ""), false
		}
		return domain.Job{}, storeFailure(url, "create job", err), false
	}
	// Best-effort once the row is committed, like job_retried in Retry: the
	// job must still join the wait list, or it would sit queued until restart.
	message := "job queued"
	if err := m.Event(ctx, domain.Event{JobID: job.ID, Type: domain.EventJobQueued, Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})}); err != nil {
		logging.FromContext(ctx, m.logger).Error("record job_queued event", "job_id", job.ID, "error", err)
	}
	// Fire creation hooks while still holding m.mu, so the job_queued hook is
	// dispatched before a worker can claim the job and reach a terminal hook.
	// The hook event name matches the job_queued domain event emitted just
	// above, so the two stay semantically aligned. There are no items yet —
	// they are resolved during processing — so the payload carries an empty
	// item list. Dispatch is non-blocking and takes only the dispatcher's own
	// lock, so calling it under m.mu is safe. Ordering between the job_queued
	// hook and a later terminal hook is best-effort only: both are independent
	// async deliveries, so a fast job's terminal webhook may still arrive
	// first.
	m.hooks.Dispatch(string(domain.EventJobQueued), job, nil)
	m.waiting.add(job.ID, false)
	accepted := job
	return job, domain.SubmitResult{URL: url, Status: domain.SubmitAccepted, Job: &accepted}, true
}

// Retry re-queues a failed or cancelled job so only its unfinished tracks run
// again: completed/skipped items keep their state (ProcessJob reuses those rows
// and never downloads them again), while every other item (failed or
// cancelled) is reset to queued and re-processed under its original item id.
//
// The retried job goes to the end of the run order.
//
// Locking: submitMu closes the race with a concurrent submit of the same
// canonical key (the dedup lookup below and SubmitBatch's both run under it).
// m.mu, nested inside, serializes the capacity check, the status flip and the
// wait-list entry with admission, Delete, Move and a worker's claim, so the
// job cannot be deleted between the status read and the requeue.
func (m *Manager) Retry(ctx context.Context, jobID string) error {
	m.submitMu.Lock()
	defer m.submitMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()

	job, err := m.store.GetJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.ErrJobNotFound
		}
		return err
	}
	if job.Status != domain.JobFailed && job.Status != domain.JobCancelled {
		return ErrJobNotRetryable
	}
	// The terminal status is persisted before the old worker's deferred cleanup
	// removes its m.cancels entry (and before Cancel's queued path clears its
	// finalizing mark), so a retry racing that window could requeue the job,
	// let a new worker register its cancel func, and then have the old
	// worker's deferred delete remove the new entry — leaving Cancel/Delete
	// blind to the running retry. Same guard as Delete: refuse while either
	// map still knows the job.
	if m.cancels[jobID] != nil || m.finalizing[jobID] {
		return ErrJobFinalizing
	}
	// The partial unique index on canonical_key allows only one queued/running
	// job per key; refuse the retry when the same input was already
	// resubmitted, instead of failing on the index during the status update.
	if _, found, err := m.store.FindActiveJobByKey(ctx, job.CanonicalKey); err != nil {
		return err
	} else if found {
		return db.ErrDuplicateActive
	}
	if m.waiting.admitted() >= m.capacity {
		return ErrQueueFull
	}
	order, err := m.store.NextQueueOrder(ctx)
	if err != nil {
		return err
	}

	// Reset unfinished items before the job becomes claimable, so a worker
	// picking it up right after the enqueue never sees stale failed state.
	// One batch statement rather than per-item updates: this runs while
	// holding both scheduling locks, and a large collection must not stall
	// every other queue operation for hundreds of round-trips.
	now := time.Now().UTC()
	if err := m.store.ResetUnfinishedItems(ctx, jobID, now); err != nil {
		return err
	}

	// A retry is a new run: its worker sets started_at again, so the run's
	// duration does not count the time the job sat failed.
	job.Status = domain.JobQueued
	job.Error = ""
	// The reset above turned every failed and cancelled item back to queued;
	// the job_retried payload below must carry the counters GET now derives.
	job.FailedItems = 0
	job.CancelledItems = 0
	job.StartedAt = nil
	job.FinishedAt = nil
	job.UpdatedAt = now
	job.QueueOrder = &order
	// The order first: the status write below makes the job claimable.
	if err := m.store.SetQueueOrder(ctx, jobID, order); err != nil {
		return err
	}
	if err := m.store.UpdateJob(ctx, job); err != nil {
		return err
	}
	m.waiting.add(jobID, false)
	delete(m.unclaimable, jobID)
	m.wake.broadcast()
	// The job row already says queued and workers may claim it, so a failed
	// event below is only logged. Mirrors SubmitBatch, which also treats the
	// job_queued event as best-effort once the row is committed. A worker's
	// claim waits for m.mu, so job_started cannot precede job_retried.
	message := "job re-queued to retry unfinished tracks"
	if err := m.Event(ctx, domain.Event{JobID: jobID, Type: domain.EventJobRetried, Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})}); err != nil {
		m.logger.Error("record job_retried event", "job_id", jobID, "error", err)
	}
	logging.FromContext(ctx, m.logger).Info("job retry queued", "job_id", jobID)
	return nil
}

// validationFailure is the result for a URL the processor refused. A
// RequestError is a verdict on the URL (status invalid); any other error is a
// failure of the validator itself (status error), which a retry may not hit.
func validationFailure(url string, err error) domain.SubmitResult {
	var reqErr *RequestError
	if errors.As(err, &reqErr) {
		return domain.SubmitResult{URL: url, Status: domain.SubmitInvalid, Error: reqErr.SubmitError()}
	}
	return domain.SubmitResult{URL: url, Status: domain.SubmitServerError, Error: &domain.SubmitError{
		Code: domain.SubmitCodeInternalError, Message: "validate url: " + err.Error(),
	}}
}

// storeFailure is the result for a URL whose submit failed in the job store:
// status error with store_error, so a client retries instead of dropping it.
func storeFailure(url, op string, err error) domain.SubmitResult {
	return domain.SubmitResult{URL: url, Status: domain.SubmitServerError, Error: &domain.SubmitError{
		Code: domain.SubmitCodeStoreError, Message: op + ": " + err.Error(),
	}}
}

func queueFullResult(url string) domain.SubmitResult {
	return domain.SubmitResult{URL: url, Status: domain.SubmitQueueFull, Error: &domain.SubmitError{
		Code: domain.SubmitCodeQueueFull, Message: ErrQueueFull.Error(),
	}}
}

// duplicateActiveResult is the result for a URL whose download already runs
// in a queued or running job. existingJobID is empty when the store's unique
// index caught the race instead of the lookup.
func duplicateActiveResult(url, existingJobID string) domain.SubmitResult {
	return domain.SubmitResult{URL: url, Status: domain.SubmitDuplicateActive, ExistingJobID: existingJobID, Error: &domain.SubmitError{
		Code: domain.SubmitCodeDuplicateActive, Message: "an active job already downloads this input",
	}}
}
