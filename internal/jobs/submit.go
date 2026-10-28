package jobs

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/ids"
	"amdl/internal/logging"
)

var ErrQueueFull = errors.New("job queue is full")

// ErrJobNotRetryable is returned by Retry for jobs that are not in the failed
// terminal status: queued/running jobs are already progressing, and
// completed/cancelled jobs have nothing to retry.
var ErrJobNotRetryable = errors.New("only failed jobs can be retried")

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
// against races. See docs/multi-link-submit-design.md.
//
// overrides, when non-nil, is attached to every job created from this batch,
// overlays the runtime config while those jobs run, and carries their hook
// selection. Callers must validate it (apply to the current config and
// Validate, plus validate hook names) before submitting. A media-user-token
// override is retained only by stations and private playlists, the two job
// kinds that consume it; all other jobs keep the remaining overrides without
// retaining the credential.
func (m *Manager) SubmitBatch(ctx context.Context, urls []string, overrides *config.DownloadOverrides) domain.BatchSubmitResponse {
	results := make([]domain.SubmitResult, len(urls))

	type candidate struct {
		index int
		url   string
		key   string
		valid ValidationResult
	}
	// One destination for the whole batch: overrides are batch-scoped, so
	// every candidate shares this segment and request-internal dedup is
	// unaffected. Resolved once, and before submitMu is taken, because
	// canonicalizing the path touches the filesystem and submitMu serializes
	// every submit in the process. Resolved here rather than when the job
	// runs so the key is stable from submit through retry and post-restart
	// recovery, which is what the unique index depends on.
	destination := destinationSegment(m.baseConfig(), overrides)
	var candidates []candidate
	seenKeys := map[string]bool{}
	for i, url := range urls {
		validated, err := m.processor.ValidateRequest(ctx, url)
		if err != nil {
			results[i] = validationFailure(url, err)
			continue
		}
		key := canonicalKey(validated.Type, validated.Storefront, validated.ID, destination)
		if seenKeys[key] {
			results[i] = domain.SubmitResult{URL: url, Status: domain.SubmitDuplicateInRequest, Error: &domain.SubmitError{
				Code: domain.SubmitCodeDuplicateInRequest, Message: "an earlier URL in this request names the same download",
			}}
			continue
		}
		seenKeys[key] = true
		candidates = append(candidates, candidate{index: i, url: url, key: key, valid: validated})
	}

	// Serialize capacity check, dedup lookup, persistence and enqueue with
	// Retry. Recovered jobs use their separate, unbounded startup backlog, so a
	// free admission-queue slot cannot disappear while this lock is held.
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
		if len(m.queue) >= cap(m.queue) {
			queueFull = true
			results[c.index] = queueFullResult(c.url)
			continue
		}

		jobOverrides := overrides
		if overrides != nil && !needsMediaUserToken(c.valid.Type, c.valid.ID) {
			jobOverrides = overrides.WithoutMediaUserToken()
		}
		job := domain.Job{
			ID: ids.NewID("job"), Input: c.url, Type: c.valid.Type, Storefront: c.valid.Storefront, CanonicalKey: c.key,
			Overrides: jobOverrides, Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now,
		}
		if err := m.store.CreateJob(ctx, job); err != nil {
			if errors.Is(err, db.ErrDuplicateActive) {
				results[c.index] = duplicateActiveResult(c.url, "")
				continue
			}
			results[c.index] = storeFailure(c.url, "create job", err)
			continue
		}
		message := "job queued"
		_ = m.Event(ctx, domain.Event{JobID: job.ID, Type: domain.EventJobQueued, Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})})
		// Fire creation hooks before enqueuing, so the job_queued hook is
		// dispatched before a worker can claim the job and reach a terminal
		// hook. The hook event name matches the job_queued domain event emitted
		// just above, so the two stay semantically aligned. There are no items
		// yet — they are resolved during processing — so the payload carries an
		// empty item list. Dispatch is non-blocking, so calling it while
		// holding submitMu is safe. Ordering between the job_queued hook and a
		// later terminal hook is best-effort only: both are independent async
		// deliveries, so a fast job's terminal webhook may still arrive first.
		m.hooks.Dispatch(string(domain.EventJobQueued), job, nil)
		m.queue <- job.ID
		logging.FromContext(ctx, m.logger).Info("job queued", "job_id", job.ID, "job_type", job.Type, "storefront", job.Storefront)
		accepted := job
		results[c.index] = domain.SubmitResult{URL: c.url, Status: domain.SubmitAccepted, Job: &accepted}
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

// Retry re-queues a failed job so only its unfinished tracks run again:
// completed/skipped items keep their state (ProcessJob reuses those rows and
// never downloads them again), while every other item is reset to queued and
// re-processed under its original item id.
//
// Locking: submitMu serializes the capacity check + enqueue with SubmitBatch
// and closes the race with a concurrent submit of the same canonical key (the
// dedup lookup below and SubmitBatch's both run under it). m.mu, nested inside,
// serializes the status flip with Delete and a worker's startup claim, so the
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
	if job.Status != domain.JobFailed {
		return ErrJobNotRetryable
	}
	// The failed status is persisted before the old worker's deferred cleanup
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
	if len(m.queue) >= cap(m.queue) {
		return ErrQueueFull
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

	job.Status = domain.JobQueued
	job.Error = ""
	job.FinishedAt = nil
	job.UpdatedAt = now
	if err := m.store.UpdateJob(ctx, job); err != nil {
		return err
	}
	// The job row already says queued, so the enqueue below must happen even
	// if recording the event fails — otherwise the job would be stranded in
	// queued with no worker ever picking it up (and further retries refused),
	// until a restart's RecoverUnfinished. Mirrors SubmitBatch, which also
	// treats the job_queued event as best-effort once the row is committed.
	message := "job re-queued to retry failed tracks"
	if err := m.Event(ctx, domain.Event{JobID: jobID, Type: domain.EventJobRetried, Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})}); err != nil {
		m.logger.Error("record job_retried event", "job_id", jobID, "error", err)
	}
	m.queue <- jobID
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

// needsMediaUserToken reports whether a job can consume the per-job credential:
// stations require it for next-tracks, while private playlists use it for
// best-effort library artwork enrichment.
func needsMediaUserToken(jobType, id string) bool {
	return jobType == "station" || (jobType == "playlist" && strings.HasPrefix(id, "pl.u-"))
}
