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
// run's id, as before. A record with every wanted track done answers
// already_downloaded, an add the resolver must read answers pending, and an
// accepted result carries no run when the record waits (a failed slot's
// retry, a full queue the picker drains later).
//
// overrides, when non-nil, becomes the overrides of every record the batch
// adds and of its station runs. Callers validate it
// (DownloadOverrides.ApplyValidated) before submitting.
func (m *Manager) SubmitBatch(ctx context.Context, urls []string, overrides *config.DownloadOverrides) domain.BatchSubmitResponse {
	return m.SubmitBatchWith(ctx, urls, overrides, SubmitOptions{})
}

// SubmitOptions are a batch's choices beyond its overrides.
type SubmitOptions struct {
	// ArtistInclude picks an artist link's albums: "all" (the default) or
	// "albums", which leaves singles out.
	ArtistInclude string
}

// SubmitBatchWith is SubmitBatch with options.
func (m *Manager) SubmitBatchWith(ctx context.Context, urls []string, overrides *config.DownloadOverrides, opts SubmitOptions) domain.BatchSubmitResponse {
	if opts.ArtistInclude == "" {
		opts.ArtistInclude = "all"
	}
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
		result := m.submitOne(ctx, c.url, c.key, c.valid, overrides, opts, now, queueFull)
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
		if r.Taken() {
			resp.Accepted++
		} else {
			resp.Rejected++
		}
	}
	return resp
}

// submitOne adds one validated URL. Caller holds submitMu.
func (m *Manager) submitOne(ctx context.Context, url, key string, valid ValidationResult, overrides *config.DownloadOverrides, opts SubmitOptions, now time.Time, queueFull bool) domain.SubmitResult {
	// A manual add resumes a paused record: the user asked for the download.
	resume := false
	switch valid.Type {
	case string(domain.ResourceAlbum):
		return addResult(url, domain.ResourceAlbum, valid.ID)(m.addAlbum(ctx, db.AlbumAdd{
			ID: valid.ID, WantedAll: true, Source: domain.SourceManual, Overrides: overrides, Paused: &resume, Now: now,
		}, domain.TriggerManual))
	case "song":
		if valid.AlbumID != "" {
			return addResult(url, domain.ResourceAlbum, valid.AlbumID)(m.addAlbum(ctx, db.AlbumAdd{
				ID: valid.AlbumID, SongIDs: []string{valid.ID}, Source: domain.SourceManual, Overrides: overrides, Paused: &resume, Now: now,
			}, domain.TriggerManual))
		}
		return m.pendingResult(ctx, url, domain.PendingAdd{Kind: domain.PendingSong, CatalogID: valid.ID, Source: domain.SourceManual, Overrides: overrides, AddedAt: now})
	case "artist":
		return m.pendingResult(ctx, url, domain.PendingAdd{Kind: domain.PendingArtist, CatalogID: valid.ID, Source: domain.SourceManual, Include: opts.ArtistInclude, Overrides: overrides, AddedAt: now})
	case string(domain.ResourcePlaylist):
		if strings.HasPrefix(valid.ID, "pl.") {
			return addResult(url, domain.ResourcePlaylist, valid.ID)(m.addPlaylist(ctx, db.PlaylistAdd{
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
func addResult(url string, kind domain.ResourceKind, id string) func(AddOutcome, domain.Job, error) domain.SubmitResult {
	return func(outcome AddOutcome, job domain.Job, err error) domain.SubmitResult {
		ref := &domain.ResourceRef{Kind: kind, ID: id}
		var result domain.SubmitResult
		switch {
		case err != nil:
			return storeFailure(url, "add record", err)
		case outcome == AddActive:
			result = duplicateActiveResult(url, job.ID)
		case outcome == AddQueued:
			accepted := job
			result = domain.SubmitResult{URL: url, Status: domain.SubmitAccepted, Job: &accepted}
		case outcome == AddAlreadyDone:
			result = domain.SubmitResult{URL: url, Status: domain.SubmitAlreadyDownloaded}
		default:
			result = domain.SubmitResult{URL: url, Status: domain.SubmitAccepted}
		}
		result.Resource = ref
		return result
	}
}

// pendingResult stores a pending add for the resolver.
func (m *Manager) pendingResult(ctx context.Context, url string, p domain.PendingAdd) domain.SubmitResult {
	if _, err := m.AddPending(ctx, p); err != nil {
		return storeFailure(url, "add pending", err)
	}
	return domain.SubmitResult{URL: url, Status: domain.SubmitPending}
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
	return domain.SubmitResult{URL: url, Status: domain.SubmitPending}, true
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

// Retry is RetryRun without the new run.
func (m *Manager) Retry(ctx context.Context, jobID string) error {
	_, err := m.RetryRun(ctx, jobID)
	return err
}

// RetryRun starts a failed or cancelled run's download again as a new run
// and returns it in Job. For a record run it resumes the record, re-arms its
// failed and given-up slots and queues a run with trigger reset, like
// DownloadRecord: with every wanted track done it queues nothing and sets
// AlreadyDownloaded; with slots left that no run takes now (a full queue,
// which the scheduler fills later, a backoff, a catalog miss, missing files)
// it returns neither. For a one-off run (a station) it queues a new run of
// the same input and overrides, or fails with ErrQueueFull.
// ErrJobNotRetryable refuses a queued, running or completed run;
// db.ErrDuplicateActive a record or input with an active run.
func (m *Manager) RetryRun(ctx context.Context, jobID string) (RecordDownloadResult, error) {
	m.submitMu.Lock()
	defer m.submitMu.Unlock()

	job, err := m.store.GetJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RecordDownloadResult{}, db.ErrJobNotFound
		}
		return RecordDownloadResult{}, err
	}
	if job.Status != domain.JobFailed && job.Status != domain.JobCancelled {
		return RecordDownloadResult{}, ErrJobNotRetryable
	}
	now := time.Now().UTC()
	if job.ResourceID != "" {
		if err := m.store.SetPaused(ctx, job.ResourceKind, job.ResourceID, false, now); err != nil {
			return RecordDownloadResult{}, err
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if _, found, err := m.store.ActiveRunID(ctx, job.ResourceKind, job.ResourceID); err != nil {
			return RecordDownloadResult{}, err
		} else if found {
			return RecordDownloadResult{}, db.ErrDuplicateActive
		}
		delete(m.backoff, recordKey{job.ResourceKind, job.ResourceID})
		if _, err := m.store.ResetFiles(ctx, db.FileReset{
			Kind: job.ResourceKind, ID: job.ResourceID, States: []domain.AudioState{domain.AudioFailed, domain.AudioGivenUp}, Now: now,
		}); err != nil {
			return RecordDownloadResult{}, err
		}
		outcome, run, err := m.afterAddLocked(ctx, job.ResourceKind, job.ResourceID, domain.TriggerReset, now)
		switch {
		case err != nil:
			return RecordDownloadResult{}, err
		case outcome == AddAlreadyDone:
			return RecordDownloadResult{AlreadyDownloaded: true}, nil
		case outcome != AddQueued:
			return RecordDownloadResult{}, nil
		}
		logging.FromContext(ctx, m.logger).Info("job retry queued", "job_id", run.ID, "retried_job_id", jobID)
		return RecordDownloadResult{Job: &run}, nil
	}
	if _, found, err := m.store.FindActiveJobByKey(ctx, job.CanonicalKey); err != nil {
		return RecordDownloadResult{}, err
	} else if found {
		return RecordDownloadResult{}, db.ErrDuplicateActive
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
			return RecordDownloadResult{}, ErrQueueFull
		}
		if result.Status == domain.SubmitDuplicateActive {
			return RecordDownloadResult{}, db.ErrDuplicateActive
		}
		return RecordDownloadResult{}, errors.New(result.Error.Message)
	}
	logging.FromContext(ctx, m.logger).Info("job retry queued", "job_id", run.ID, "retried_job_id", jobID)
	return RecordDownloadResult{Job: &run}, nil
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
