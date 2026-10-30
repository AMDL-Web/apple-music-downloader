package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
)

// downloadsFeed is the SSE overview feed: it pushes one DownloadFeedMessage
// per change to the GET /downloads list — download_upserted (the affected
// job's latest snapshot) as jobs are queued, start, progress and finish, and
// download_deleted as jobs are removed. A client first reads GET /downloads
// and resumes from its event_cursor (Last-Event-ID or ?cursor=).
func (s *Server) downloadsFeed(w http.ResponseWriter, r *http.Request) {
	cursor, err := streamCursor(r)
	if err != nil {
		writeCursorError(w, err)
		return
	}
	// Subscribe before the first drain so a milestone published between the
	// backlog read and registration still wakes a subsequent drain.
	wake, cancel := s.hub.SubscribeAll()
	defer cancel()
	stream, err := startSSE(w)
	if err != nil {
		return
	}
	ticks, stop := newStreamTicks()
	defer stop()
	progressTicker := time.NewTicker(feedProgressInterval)
	defer progressTicker.Stop()
	ticks.progress = progressTicker.C
	ctx, end := s.streamContext(r)
	defer end()
	s.runDownloadsFeed(ctx, stream, int64(cursor), wake, ticks)
}

// feedProgressJobs bounds how many running jobs one progress tick reads; no
// more run at once than download.max_running_jobs allows.
const feedProgressJobs = 64

// runDownloadsFeed drives the overview feed loop. It drains milestone events
// newer than lastID into one message per affected job (deduped to the latest
// action and snapshot) on every wake and re-drain tick. A write error (client
// gone) ends the loop. The feed never ends on its own.
func (s *Server) runDownloadsFeed(ctx context.Context, sink sseSink, lastID int64, wake <-chan struct{}, ticks streamTicks) {
	write := func(msg domain.DownloadFeedMessage) error {
		raw, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("encode feed message: %w", err)
		}
		return sink.send(strconv.FormatInt(msg.EventID, 10), msg.Type, raw)
	}
	drain := func() error {
		for {
			events, err := s.store.ListMilestoneEventsAfterLimit(ctx, lastID, streamEventPageSize)
			if err != nil {
				return nil // keep the connection; the next wake or tick retries
			}
			if len(events) == 0 {
				return nil
			}
			counts, err := s.store.CountJobsByStatus(ctx, db.JobListFilter{})
			if err != nil {
				return nil // do not advance the cursor until counts can accompany the message
			}
			total := 0
			for _, count := range counts {
				total += count
			}
			statusCounts := makeJobStatusCounts(counts, total)
			type pendingMessage struct {
				eventID int64
				deleted bool
			}
			// One upsert per job in this bounded page, carrying that job's own
			// highest milestone id rather than a page-wide cursor.
			latest := map[string]pendingMessage{}
			var order []string
			for _, ev := range events {
				if _, seen := latest[ev.JobID]; !seen {
					order = append(order, ev.JobID)
				}
				if ev.ID > latest[ev.JobID].eventID {
					latest[ev.JobID] = pendingMessage{eventID: ev.ID, deleted: ev.Type == domain.EventJobDeleted}
				}
			}
			// Send in ascending per-job cursor order so Last-Event-ID only moves
			// forward and a mid-page disconnect never skips a later message.
			sort.Slice(order, func(i, j int) bool { return latest[order[i]].eventID < latest[order[j]].eventID })
			for _, jobID := range order {
				pending := latest[jobID]
				if pending.deleted {
					if err := write(domain.DownloadFeedMessage{Type: "download_deleted", JobID: jobID, EventID: pending.eventID, StatusCounts: statusCounts}); err != nil {
						return err
					}
					lastID = pending.eventID
					continue
				}
				snap, err := s.jobSnapshot(ctx, jobID)
				if errors.Is(err, sql.ErrNoRows) {
					// The job is gone although its latest event here is not the
					// tombstone: the tombstone is in a later page, or an event was
					// filed after it (a motion artwork lookup racing the delete).
					// Say so now; a later tombstone repeats it, and clients apply
					// download_deleted idempotently.
					if err := write(domain.DownloadFeedMessage{Type: "download_deleted", JobID: jobID, EventID: pending.eventID, StatusCounts: statusCounts}); err != nil {
						return err
					}
					lastID = pending.eventID
					continue
				}
				if err != nil {
					// A transient store error must not advance the cursor past a
					// milestone that may never repeat (e.g. job_finished). Stop
					// this round; the next wake or tick retries the same page.
					return nil
				}
				if err := write(domain.DownloadFeedMessage{Type: "download_upserted", Job: snap, EventID: pending.eventID, StatusCounts: statusCounts}); err != nil {
					return err
				}
				lastID = pending.eventID
			}
			if len(events) < streamEventPageSize {
				return nil
			}
		}
	}

	// pushProgress sends each running job whose progress moved since it was
	// last sent, at the cursor already reached: it is no event, so a client
	// resuming from it misses nothing.
	sent := map[string]float64{}
	pushProgress := func() error {
		running, _, err := s.store.ListJobs(ctx, db.JobListFilter{Statuses: []domain.JobStatus{domain.JobRunning}, Limit: feedProgressJobs})
		if err != nil || s.fillProgress(ctx, running) != nil {
			return nil // the next tick tries again
		}
		live := make(map[string]bool, len(running))
		var moved []domain.Job
		for _, job := range running {
			live[job.ID] = true
			if last, ok := sent[job.ID]; !ok || last != *job.Progress {
				moved = append(moved, job)
			}
		}
		for id := range sent {
			if !live[id] {
				delete(sent, id)
			}
		}
		if len(moved) == 0 {
			return nil
		}
		counts, err := s.store.CountJobsByStatus(ctx, db.JobListFilter{})
		if err != nil {
			return nil
		}
		total := 0
		for _, count := range counts {
			total += count
		}
		statusCounts := makeJobStatusCounts(counts, total)
		for i := range moved {
			job := moved[i]
			if err := write(domain.DownloadFeedMessage{Type: "download_upserted", Job: &job, EventID: lastID, StatusCounts: statusCounts}); err != nil {
				return err
			}
			sent[job.ID] = *job.Progress
		}
		return nil
	}

	if err := drain(); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-wake:
			if !ok || drain() != nil {
				return
			}
		case <-ticks.redrain:
			if drain() != nil {
				return
			}
		case <-ticks.progress:
			if pushProgress() != nil {
				return
			}
		case <-ticks.keepalive:
			if sink.keepalive() != nil {
				return
			}
		}
	}
}
