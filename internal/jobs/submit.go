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

// ErrJobFinalizing is returned by Retry, and turned into duplicate_active by
// a resubmit, when the job's row already reads finished but its previous run
// has not fully wound down: the worker's cancel registration is still in
// place, a finalize sequence is dispatching hooks, or a terminal hook still
// runs. Requeueing in the first two windows would let the old worker's
// deferred cleanup delete the new run's cancel entry; in the last, the new
// run would write the files the hook is still working on. The first two are
// tiny; a hook can take as long as its timeout. Callers retry later.
var ErrJobFinalizing = errors.New("job is still finalizing its previous run; retry again shortly")

// SubmitBatch validates and enqueues each url independently, applying
// three-layer dedup: request-internal (canonical key seen earlier in this
// batch), active-job lookup, and the DB's partial unique index as a backstop
// against races.
//
// A canonical key keeps one queue entry: when it has no active job but a
// finished one (completed, failed or cancelled; the newest when an older
// database holds several), that job is queued again under its id
// (requeued=true) with this request's overrides and created_at set to now,
// and the run checks the files before downloading anything. Stations always
// get a new job.
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
		job, result, admitted := m.enqueue(ctx, c.url, c.valid, c.key, overrides, now)
		results[c.index] = result
		if result.Status == domain.SubmitQueueFull {
			queueFull = true
		}
		if admitted {
			m.wake.broadcast()
			logging.FromContext(ctx, m.logger).Info("job queued", "job_id", job.ID, "job_type", job.Type, "storefront", job.Storefront, "requeued", result.Requeued)
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

// admitLocked creates one queued job at the end of the run order, records
// its job_queued event, fires the job_queued hook and adds it to the wait
// list. Caller holds submitMu and m.mu and has checked dedup.
func (m *Manager) admitLocked(ctx context.Context, url string, valid ValidationResult, key string, overrides *config.DownloadOverrides, now time.Time) (domain.Job, domain.SubmitResult, bool) {
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
// again: completed/skipped items keep their state, while every other item
// (failed or cancelled) is reset to queued and re-processed under its
// original item id. The run checks the kept items against the files. The
// overrides and created_at stay; the job_queued hook does not fire.
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
	// The partial unique index on canonical_key allows only one queued/running
	// job per key; refuse the retry when the same input already runs in
	// another row (an older database can hold several rows per key), instead
	// of failing on the index during the status update.
	if _, found, err := m.store.FindActiveJobByKey(ctx, job.CanonicalKey); err != nil {
		return err
	} else if found {
		return db.ErrDuplicateActive
	}
	if _, err := m.requeueLocked(ctx, job, requeue{cause: domain.RequeueRetry, message: "job re-queued to retry unfinished tracks"}); err != nil {
		return err
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
