package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"amdl/internal/domain"
)

// streamEventPageSize bounds each database read while a stream replays from
// an old cursor; the stream loops advance the cursor page by page.
const streamEventPageSize = 512

// eventsExhausted reloads jobID and reports whether its event stream will
// never deliver another event. Reloading is essential for connections opened
// while the job was active: retaining their initial Job snapshot would keep
// them open forever after the job transitions to a terminal state. A terminal
// job.Status alone is not enough: post-download
// hook dispatch is fire-and-forget and can keep recording hook_started/
// hook_succeeded/hook_failed events well after the job itself reached a
// terminal status (see jobs.Manager.HooksPending). And HooksPending alone
// still has a gap: the terminal status becomes visible (Store.FinalizeJob)
// before hook dispatch increments the pending count, so FinalizeInFlight
// must be consulted too — and strictly before HooksPending. Dispatch
// increments pending before the manager drops its finalize marks, so
// observing "not finalizing" guarantees pending already reflects any hooks
// that will fire; the reverse order could read pending before the increment
// and the finalize mark after its removal, wrongly concluding exhausted
// while hook events are still coming.
func (s *Server) eventsExhausted(ctx context.Context, jobID string) bool {
	job, err := s.store.GetJob(ctx, jobID)
	if errors.Is(err, sql.ErrNoRows) {
		// A successful DELETE publishes a final job_deleted tombstone after
		// removing the row. Once the backlog drain has consumed that event there
		// can be no future per-job events, so an already-open stream must close.
		return true
	}
	if err != nil || !job.Status.IsTerminal() {
		return false
	}
	return !s.manager.FinalizeInFlight(jobID) && !s.manager.HooksPending(jobID)
}

// jobEvents is the per-job SSE stream: it replays the job's persisted events
// after the resume cursor (Last-Event-ID or ?cursor=), then streams new ones
// until eventsExhausted says none can arrive. A client that already has
// everything gets 204 No Content, which makes EventSource stop reconnecting.
func (s *Server) jobEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cursor, err := streamCursor(r)
	if err != nil {
		writeCursorError(w, err)
		return
	}
	if _, err := s.store.GetJob(r.Context(), id); err != nil {
		writeJobLookupError(w, id, err)
		return
	}
	lastID := int64(cursor)

	// Subscribe before inspecting state or reading the backlog so a concurrent
	// retry cannot change a failed job back to queued in the gap and leave us
	// waiting on a nil channel. The store remains the source of truth; the hub
	// only wakes a subsequent drain sooner.
	wake, cancel := s.hub.Subscribe(id)
	defer cancel()
	exhausted := s.eventsExhausted(r.Context(), id)

	backlog, err := s.store.ListEventsAfterLimit(r.Context(), id, lastID, streamEventPageSize)
	if err != nil {
		writeStoreError(w, "read job events", err)
		return
	}
	if len(backlog) == 0 && exhausted {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	stream, err := startSSE(w)
	if err != nil {
		return
	}
	ticks, stop := newStreamTicks()
	defer stop()
	ctx, end := s.streamContext(r)
	defer end()
	s.runJobEventStream(ctx, stream, id, lastID, backlog, wake, ticks)
}

// runJobEventStream sends backlog, then every persisted event of jobID after
// it, read from the store on each hub wake and re-drain tick, so delivery
// never depends on the hub: a wake dropped by a full buffer is picked up by
// the next tick. It returns when the client is gone or, after a final drain,
// when eventsExhausted reports that no event can follow.
func (s *Server) runJobEventStream(ctx context.Context, sink sseSink, jobID string, lastID int64, backlog []domain.Event, wake <-chan struct{}, ticks streamTicks) {
	send := func(events []domain.Event) error {
		for _, ev := range events {
			raw, err := json.Marshal(ev)
			if err != nil {
				return fmt.Errorf("encode event %d: %w", ev.ID, err)
			}
			if err := sink.send(strconv.FormatInt(ev.ID, 10), string(ev.Type), raw); err != nil {
				return err
			}
			lastID = ev.ID
		}
		return nil
	}
	drain := func() error {
		for {
			events, err := s.store.ListEventsAfterLimit(ctx, jobID, lastID, streamEventPageSize)
			if err != nil {
				// Keep the stream on a store read error; the next wake or
				// tick retries from the same cursor.
				return nil
			}
			if err := send(events); err != nil {
				return err
			}
			if len(events) < streamEventPageSize {
				return nil
			}
		}
	}
	// done drains and reports whether the stream should end. A hook may record
	// its terminal event after drain's read but before eventsExhausted sees
	// the hook as no longer pending; that event is committed before Pending
	// drops (hooks.Dispatcher.donePending), so one more drain delivers it.
	done := func() bool {
		if drain() != nil {
			return true
		}
		if s.eventsExhausted(ctx, jobID) {
			_ = drain()
			return true
		}
		return false
	}

	if send(backlog) != nil || done() {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-wake:
			if !ok || done() {
				return
			}
		case <-ticks.redrain:
			if done() {
				return
			}
		case <-ticks.keepalive:
			if sink.keepalive() != nil {
				return
			}
		}
	}
}
