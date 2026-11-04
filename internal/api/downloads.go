package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/jobs"
	"amdl/internal/progress"
)

// maxBatchSubmitURLs caps the number of URLs accepted in a single batch
// submit request.
const maxBatchSubmitURLs = 100

// urlSplitPattern splits a pasted textarea blob of URLs on newlines,
// whitespace, commas, and semicolons (ASCII and full-width variants).
var urlSplitPattern = regexp.MustCompile(`[\r\n\s,;，；]+`)

func (s *Server) createDownload(w http.ResponseWriter, r *http.Request) {
	var req domain.DownloadRequest
	// Unknown fields are rejected rather than ignored: a typo inside
	// overrides would otherwise submit successfully and run the whole batch
	// without the intended settings.
	if !decodeJSONBody(w, r, &req, true) {
		return
	}
	s.submit(w, r, req.URLs, req.Overrides, jobs.SubmitOptions{}, false)
}

// submit adds the links of POST /downloads and POST /library/add. With now,
// each run it queues moves to the front of the run order.
func (s *Server) submit(w http.ResponseWriter, r *http.Request, raws []string, overrides *config.DownloadOverrides, opts jobs.SubmitOptions, now bool) {
	var urls []string
	for _, raw := range raws {
		for _, entry := range urlSplitPattern.Split(raw, -1) {
			entry = strings.TrimSpace(entry)
			if entry != "" {
				urls = append(urls, entry)
			}
		}
	}
	if len(urls) == 0 {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "urls is required")
		return
	}
	if len(urls) > maxBatchSubmitURLs {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("too many urls: max %d per request", maxBatchSubmitURLs))
		return
	}
	if overrides != nil {
		// Validate the overlay against the same rules the runtime config must
		// satisfy, applied to the config these jobs would actually run under.
		if _, err := overrides.ApplyValidated(s.currentConfig()); err != nil {
			writeError(w, http.StatusUnprocessableEntity, CodeInvalidOverrides, "invalid overrides: "+err.Error())
			return
		}
	}
	resp := s.manager.SubmitBatchWith(r.Context(), urls, overrides, opts)
	if now {
		for i, result := range resp.Results {
			if result.Job == nil || result.Job.Status != domain.JobQueued {
				continue
			}
			if moved, err := s.manager.DownloadNow(r.Context(), result.Job.ID); err == nil {
				resp.Results[i].Job = &moved
			}
		}
	}
	if resp.Accepted > 0 {
		writeJSON(w, http.StatusAccepted, resp)
		return
	}
	// Nothing was queued. A server-side failure means the same batch may
	// succeed when retried, so it is a 500, not a verdict on the input; the
	// per-URL results go out in details either way.
	var serverCode ErrorCode
	for _, result := range resp.Results {
		if result.Status != domain.SubmitServerError {
			continue
		}
		if result.Error != nil && result.Error.Code == domain.SubmitCodeInternalError {
			if serverCode == "" {
				serverCode = CodeInternalError
			}
			continue
		}
		serverCode = CodeStoreError
		break
	}
	if serverCode != "" {
		writeErrorDetails(w, http.StatusInternalServerError, serverCode, "no URL was accepted: the server failed; retry the submit", resp)
		return
	}
	writeErrorDetails(w, http.StatusUnprocessableEntity, CodeNoURLsAccepted, "no URL was accepted; see details.results", resp)
}

func (s *Server) listDownloads(w http.ResponseWriter, r *http.Request) {
	filter, err := parseJobListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
		return
	}
	// Read the global cursor before the job snapshot, for the same reason
	// getDownload reads its per-job cursor first: an event committing between
	// this read and the snapshot is already reflected in the snapshot, so the
	// cursor a client resumes the overview feed from never runs ahead of what
	// this response shows.
	eventCursor, err := s.store.LatestGlobalEventID(r.Context())
	if err != nil {
		writeStoreError(w, "read event cursor", err)
		return
	}
	jobs, total, err := s.store.ListJobs(r.Context(), filter)
	if err != nil {
		writeStoreError(w, "list jobs", err)
		return
	}
	if err := s.fillProgress(r.Context(), jobs); err != nil {
		writeStoreError(w, "read item progress", err)
		return
	}
	counts, err := s.store.CountJobsByStatus(r.Context(), filter)
	if err != nil {
		writeStoreError(w, "count jobs", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"downloads":     jobs,
		"status_counts": makeJobStatusCounts(counts, total),
		"total":         total, "limit": filter.Limit, "offset": filter.Offset, "event_cursor": eventCursor,
	})
}

func makeJobStatusCounts(counts map[domain.JobStatus]int, total int) domain.JobStatusCounts {
	return domain.JobStatusCounts{
		Queued:    counts[domain.JobQueued],
		Running:   counts[domain.JobRunning],
		Completed: counts[domain.JobCompleted],
		Failed:    counts[domain.JobFailed],
		Cancelled: counts[domain.JobCancelled],
		Total:     total,
	}
}

// jobSnapshot returns a job as the GET /downloads list and getDownload return
// it; the store fills its item counters. A sql.ErrNoRows error means the job
// no longer exists (e.g. deleted between a milestone event and this read); the
// overview feed treats any other error as transient and retries.
func (s *Server) jobSnapshot(ctx context.Context, id string) (*domain.Job, error) {
	job, err := s.store.GetJob(ctx, id)
	if err := forDisplay(err); err != nil {
		return nil, err
	}
	jobs := []domain.Job{job}
	if err := s.fillProgress(ctx, jobs); err != nil {
		return nil, err
	}
	return &jobs[0], nil
}

// fillProgress sets each job's Progress from its items, read in one query.
func (s *Server) fillProgress(ctx context.Context, jobs []domain.Job) error {
	ids := make([]string, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID
	}
	items, err := s.store.ItemProgress(ctx, ids)
	if err != nil {
		return err
	}
	for i := range jobs {
		p := jobProgress(jobs[i].Status, items[jobs[i].ID])
		jobs[i].Progress = &p
	}
	return nil
}

// jobProgress is progress.Overall to three decimals, so a client and the
// overview feed compare it without float noise.
func jobProgress(status domain.JobStatus, items []domain.JobItem) float64 {
	return math.Round(progress.Overall(status, items)*1000) / 1000
}

func (s *Server) cancelDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.manager.Cancel(r.Context(), id); err != nil {
		if errors.Is(err, jobs.ErrJobNotCancellable) {
			writeError(w, http.StatusConflict, CodeJobNotCancellable, err.Error())
			return
		}
		writeJobLookupError(w, id, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// retryDownload starts a failed or cancelled run's download again as a new
// run. For a record run the record's failed, given-up and missing slots are
// re-armed; what is done stays done. The answer names the new run, or, like
// POST /library/{kind}/{id}/download, says the record has nothing left to
// download or waits.
func (s *Server) retryDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	res, err := s.manager.RetryRun(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrJobNotRetryable), errors.Is(err, jobs.ErrJobUnreadable):
			writeError(w, http.StatusConflict, CodeJobNotRetryable, err.Error())
		case errors.Is(err, db.ErrDuplicateActive):
			writeError(w, http.StatusConflict, CodeDuplicateActive, "an active job already downloads this input")
		case errors.Is(err, jobs.ErrQueueFull):
			writeError(w, http.StatusServiceUnavailable, CodeQueueFull, err.Error())
		default:
			writeJobLookupError(w, id, err)
		}
		return
	}
	switch {
	case res.Job != nil:
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "job": res.Job})
	case res.AlreadyDownloaded:
		writeJSON(w, http.StatusOK, map[string]any{"status": "already_downloaded"})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"status": "waiting"})
	}
}

// moveDownload places a queued job elsewhere in the run order: before or
// after another queued job, or first or last. Exactly one of before, after
// and to is set.
func (s *Server) moveDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req domain.MoveRequest
	if !decodeJSONBody(w, r, &req, true) {
		return
	}
	set := 0
	for _, field := range []string{req.Before, req.After, req.To} {
		if field != "" {
			set++
		}
	}
	if set != 1 {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "set exactly one of before, after and to")
		return
	}
	if req.To != "" && req.To != db.EdgeFirst && req.To != db.EdgeLast {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("to must be %s or %s", db.EdgeFirst, db.EdgeLast))
		return
	}
	job, err := s.manager.Move(r.Context(), id, db.Placement{Before: req.Before, After: req.After, Edge: req.To})
	if err != nil {
		if errors.Is(err, db.ErrJobNotQueued) {
			writeError(w, http.StatusConflict, CodeJobNotQueued, err.Error()+"; only queued jobs have a place in the run order")
			return
		}
		writeJobLookupError(w, id, err)
		return
	}
	jobs := []domain.Job{job}
	if err := s.fillProgress(r.Context(), jobs); err != nil {
		jobs[0].Progress = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": jobs[0]})
}

func (s *Server) getDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Read the event cursor before the job/items snapshot below. If an event
	// commits concurrently between this read and the snapshot reads, its
	// effect is guaranteed to already be visible in the snapshot — so the
	// cursor never races ahead of what the response actually reflects.
	// Reading it after the snapshot instead would let a concurrent event land
	// in between: the cursor would include it, but the snapshot wouldn't,
	// and the stream's cursor filters strictly by id, so a client would
	// skip that event and never see its effect until a later one arrives.
	eventCursor, err := s.store.LatestEventID(r.Context(), id)
	if err != nil {
		writeStoreError(w, "read event cursor", err)
		return
	}
	job, err := s.store.GetJob(r.Context(), id)
	if err := forDisplay(err); err != nil {
		writeJobLookupError(w, id, err)
		return
	}
	items, err := s.store.ListItems(r.Context(), id)
	if err != nil {
		writeStoreError(w, "list items", err)
		return
	}
	p := jobProgress(job.Status, items)
	job.Progress = &p
	// The snapshot and the SSE stream are two access modes of one state, so
	// hook outcomes — which the stream pushes as hook_* events — must be
	// visible here too, derived from those same persisted events.
	hookEvents, err := s.store.ListHookEvents(r.Context(), id)
	if err != nil {
		writeStoreError(w, "list hook events", err)
		return
	}
	hooks := domain.SummarizeHooks(hookEvents, s.manager.HooksPending(id))
	writeJSON(w, http.StatusOK, map[string]any{
		"job": job, "items": items, "hooks": hooks, "event_cursor": eventCursor,
	})
}

func (s *Server) deleteDownload(w http.ResponseWriter, r *http.Request) {
	// Deletion goes through the manager, not the store, so a job whose
	// finalize sequence (terminal event + hook dispatch) is still in flight
	// is refused even though its status row already reads terminal.
	id := r.PathValue("id")
	var record bool
	switch r.URL.Query().Get("record") {
	case "", "false":
	case "true":
		record = true
	default:
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "record must be true or false")
		return
	}
	if record {
		job, err := s.store.GetJob(r.Context(), id)
		if err := forDisplay(err); err != nil {
			writeJobLookupError(w, id, err)
			return
		}
		if job.ResourceID != "" {
			if err := s.manager.DeleteRecord(r.Context(), job.ResourceKind, job.ResourceID); err != nil {
				writeRecordError(w, job.ResourceKind, job.ResourceID, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "deleted": "record",
				"resource": domain.ResourceRef{Kind: job.ResourceKind, ID: job.ResourceID}})
			return
		}
	}
	if err := s.manager.Delete(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrJobNotTerminal) {
			writeError(w, http.StatusConflict, CodeJobActive, fmt.Sprintf("job %s is still running, finalizing or running a hook", id))
			return
		}
		writeJobLookupError(w, id, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "deleted": "job"})
}
