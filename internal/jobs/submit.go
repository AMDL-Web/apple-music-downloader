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

// ErrJobNotRetryable is returned by Retry for jobs that are neither failed nor
// cancelled: queued/running jobs are already progressing, and a completed job
// has nothing left to retry.
var ErrJobNotRetryable = errors.New("only failed or cancelled jobs can be retried")

// ErrJobFinalizing is kept for the API's error mapping; nothing returns it
// since a retry is a new run (one row per run). TODO(P3): drop it with the
// API's retry route.
var ErrJobFinalizing = errors.New("job is still finalizing its previous run; retry again shortly")

// SubmitBatch validates and adds each url independently (design §3.1). An
// album, an album-track link and a catalog playlist (pl.) become records, and
// the add queues the record's run in the same step when it needs one; a song
// without its album, an artist and a library playlist's songs become pending
// adds the resolver reads; a station, and a library playlist the Apple
// library cache does not hold, is a one-off run as before. Two URLs of one
// request that name the same thing are refused as duplicate_in_request.
//
// An accepted result carries the record's new run, so a caller can wait for
// it; a record whose run is already active answers duplicate_active with that
// run's id, as before. An accepted result carries no run when the add needs no download
// (every wanted track done, the record paused), when it waits for the
// resolver, and when the queue is full (the picker queues it later).
// TODO(P3): distinct statuses for those (already_downloaded, pending).
//
// overrides, when non-nil, becomes the overrides of every record the batch
// adds and of its station runs. Callers validate it
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

	// Serialize adds with Retry and other submits. Each admission below also
	// holds m.mu, so a worker cannot claim a run before its job_queued event
	// and hook.
	m.submitMu.Lock()
	defer m.submitMu.Unlock()

	now := time.Now().UTC()
	queueFull := false
	for _, c := range candidates {
		result := m.submitOne(ctx, c.url, c.key, c.valid, overrides, now, queueFull)
		if result.Status == domain.SubmitQueueFull {
			queueFull = true
		}
		results[c.index] = result
		if result.Status == domain.SubmitAccepted && result.Job != nil {
			logging.FromContext(ctx, m.logger).Info("job queued", "job_id", result.Job.ID, "job_type", result.Job.Type,
				"resource_id", result.Job.ResourceID, "storefront", result.Job.Storefront)
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

// submitOne adds one validated URL. Caller holds submitMu.
func (m *Manager) submitOne(ctx context.Context, url, key string, valid ValidationResult, overrides *config.DownloadOverrides, now time.Time, queueFull bool) domain.SubmitResult {
	// A manual add resumes a paused record: the user asked for the download.
	resume := false
	switch valid.Type {
	case string(domain.ResourceAlbum):
		return addResult(url)(m.addAlbum(ctx, db.AlbumAdd{
			ID: valid.ID, WantedAll: true, Source: domain.SourceManual, Overrides: overrides, Paused: &resume, Now: now,
		}, domain.TriggerManual))
	case "song":
		if valid.AlbumID != "" {
			return addResult(url)(m.addAlbum(ctx, db.AlbumAdd{
				ID: valid.AlbumID, SongIDs: []string{valid.ID}, Source: domain.SourceManual, Overrides: overrides, Paused: &resume, Now: now,
			}, domain.TriggerManual))
		}
		return m.pendingResult(ctx, url, domain.PendingAdd{Kind: domain.PendingSong, CatalogID: valid.ID, Source: domain.SourceManual, Overrides: overrides, AddedAt: now})
	case "artist":
		return m.pendingResult(ctx, url, domain.PendingAdd{Kind: domain.PendingArtist, CatalogID: valid.ID, Source: domain.SourceManual, Include: "all", Overrides: overrides, AddedAt: now})
	case string(domain.ResourcePlaylist):
		if strings.HasPrefix(valid.ID, "pl.") {
			return addResult(url)(m.addPlaylist(ctx, db.PlaylistAdd{
				ID: valid.ID, Source: domain.SourceManual, Overrides: overrides, Paused: &resume, Now: now,
			}))
		}
		if result, ok := m.addLibraryPlaylist(ctx, url, valid.ID, overrides, now); ok {
			return result
		}
	}
	if queueFull {
		return queueFullResult(url)
	}
	return m.submitOneOff(ctx, url, key, valid, overrides, now)
}

// addResult turns an add's outcome into the URL's submit result.
func addResult(url string) func(AddOutcome, domain.Job, error) domain.SubmitResult {
	return func(outcome AddOutcome, job domain.Job, err error) domain.SubmitResult {
		switch {
		case err != nil:
			return storeFailure(url, "add record", err)
		case outcome == AddActive:
			return duplicateActiveResult(url, job.ID)
		case outcome == AddQueued:
			accepted := job
			return domain.SubmitResult{URL: url, Status: domain.SubmitAccepted, Job: &accepted}
		}
		return domain.SubmitResult{URL: url, Status: domain.SubmitAccepted}
	}
}

// pendingResult stores a pending add for the resolver.
func (m *Manager) pendingResult(ctx context.Context, url string, p domain.PendingAdd) domain.SubmitResult {
	if _, err := m.AddPending(ctx, p); err != nil {
		return storeFailure(url, "add pending", err)
	}
	return domain.SubmitResult{URL: url, Status: domain.SubmitAccepted}
}

// addPlaylist stores a catalog playlist add and queues its run when needed.
func (m *Manager) addPlaylist(ctx context.Context, add db.PlaylistAdd) (AddOutcome, domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.store.AddPlaylist(ctx, add); err != nil {
		return "", domain.Job{}, err
	}
	return m.afterAddLocked(ctx, domain.ResourcePlaylist, add.ID, domain.TriggerManual, add.Now)
}

// addLibraryPlaylist adds the songs of a playlist without a catalog id
// (design §2.6) one by one, from the Apple library cache: it has no record of
// its own. A song without a catalog id is skipped. It reports false when the
// cache does not hold the playlist; the caller downloads it as a one-off run
// then. TODO(P3): report the skipped songs.
func (m *Manager) addLibraryPlaylist(ctx context.Context, url, playlistID string, overrides *config.DownloadOverrides, now time.Time) (domain.SubmitResult, bool) {
	tracks, _, found, err := m.store.AppleLibraryPlaylistTracks(ctx, playlistID)
	if err != nil {
		return storeFailure(url, "read the library playlist", err), true
	}
	if !found {
		return domain.SubmitResult{}, false
	}
	skipped := 0
	for _, track := range tracks {
		if track.CatalogID == "" {
			skipped++
			continue
		}
		if _, err := m.AddPending(ctx, domain.PendingAdd{Kind: domain.PendingSong, CatalogID: track.CatalogID, Source: domain.SourceManual, Overrides: overrides, AddedAt: now}); err != nil {
			return storeFailure(url, "add pending", err), true
		}
	}
	if skipped > 0 {
		logging.FromContext(ctx, m.logger).Info("library playlist songs without a catalog id skipped", "playlist_id", playlistID, "skipped", skipped)
	}
	return domain.SubmitResult{URL: url, Status: domain.SubmitAccepted}, true
}

// submitOneOff queues a run that downloads no record: a station, or a
// library playlist the cache does not hold. An active run of the same input
// answers duplicate_active (canonical_key).
func (m *Manager) submitOneOff(ctx context.Context, url, key string, valid ValidationResult, overrides *config.DownloadOverrides, now time.Time) domain.SubmitResult {
	existing, found, err := m.store.FindActiveJobByKey(ctx, key)
	if err != nil {
		return storeFailure(url, "look up active jobs", err)
	}
	if found {
		return duplicateActiveResult(url, existing.ID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, result, _ := m.admitLocked(ctx, url, valid, key, overrides, now)
	return result
}

// admitLocked creates one queued one-off run at the end of the run order and
// announces it (admittedLocked). Caller holds submitMu and m.mu and has
// checked dedup.
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
		Trigger: domain.TriggerManual, Priority: domain.PriorityManual,
	}
	if err := m.store.CreateJob(ctx, job); err != nil {
		if errors.Is(err, db.ErrDuplicateActive) {
			return domain.Job{}, duplicateActiveResult(url, ""), false
		}
		return domain.Job{}, storeFailure(url, "create job", err), false
	}
	m.admittedLocked(ctx, job)
	accepted := job
	return job, domain.SubmitResult{URL: url, Status: domain.SubmitAccepted, Job: &accepted}, true
}

// Retry starts a failed or cancelled run's download again as a new run. For
// a record run it resumes the record, re-arms its failed and given-up slots
// and queues a run with trigger reset; with nothing left to download it
// queues nothing. For a one-off run (a station) it queues a new run of the
// same input and overrides. ErrJobNotRetryable refuses a queued, running or
// completed run; db.ErrDuplicateActive a record or input with an active run.
func (m *Manager) Retry(ctx context.Context, jobID string) error {
	m.submitMu.Lock()
	defer m.submitMu.Unlock()

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
	now := time.Now().UTC()
	if job.ResourceID != "" {
		if err := m.store.SetPaused(ctx, job.ResourceKind, job.ResourceID, false, now); err != nil {
			return err
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if _, found, err := m.store.ActiveRunID(ctx, job.ResourceKind, job.ResourceID); err != nil {
			return err
		} else if found {
			return db.ErrDuplicateActive
		}
		delete(m.backoff, recordKey{job.ResourceKind, job.ResourceID})
		if _, err := m.store.ResetFiles(ctx, db.FileReset{
			Kind: job.ResourceKind, ID: job.ResourceID, States: []domain.AudioState{domain.AudioFailed, domain.AudioGivenUp}, Now: now,
		}); err != nil {
			return err
		}
		outcome, run, err := m.afterAddLocked(ctx, job.ResourceKind, job.ResourceID, domain.TriggerReset, now)
		if err == nil && outcome == AddQueued {
			logging.FromContext(ctx, m.logger).Info("job retry queued", "job_id", run.ID, "retried_job_id", jobID)
		}
		return err
	}
	if _, found, err := m.store.FindActiveJobByKey(ctx, job.CanonicalKey); err != nil {
		return err
	} else if found {
		return db.ErrDuplicateActive
	}
	typ, id := job.Type, ""
	if t, _, parsedID, ok := ParseCanonicalKey(job.CanonicalKey); ok {
		typ, id = t, parsedID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	run, result, admitted := m.admitLocked(ctx, job.Input, ValidationResult{Type: typ, Storefront: job.Storefront, ID: id}, job.CanonicalKey, job.Overrides, now)
	if !admitted {
		if result.Status == domain.SubmitQueueFull {
			return ErrQueueFull
		}
		if result.Status == domain.SubmitDuplicateActive {
			return db.ErrDuplicateActive
		}
		return errors.New(result.Error.Message)
	}
	logging.FromContext(ctx, m.logger).Info("job retry queued", "job_id", run.ID, "retried_job_id", jobID)
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
