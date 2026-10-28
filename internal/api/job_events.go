package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"amdl/internal/domain"

	"github.com/coder/websocket"
)

// streamEventPageSize bounds each database allocation while old SSE/WS
// cursors are replayed. Handlers advance the persisted event id page by page.
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

// eventsWS is the WebSocket twin of events: it streams the same persisted job
// events — one domain.Event encoded as a JSON text message, identical to the
// SSE data payload — over a WebSocket connection. Resume works via the
// ?last_event_id= query parameter (WebSocket has no Last-Event-ID header
// convention); clients pass the id of the last event they saw. Delivery uses
// the same store-drain pattern as events: the hub only wakes the drain sooner,
// and the ticker bounds tail latency for events dropped by a full hub buffer.
// The ticker also pings the peer so half-open mobile connections are detected
// and torn down instead of holding the goroutine forever.
func (s *Server) eventsWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.GetJob(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	lastID, _ := strconv.ParseInt(r.URL.Query().Get("last_event_id"), 10, 64)

	// Subscribe before inspecting state or reading the backlog so a concurrent
	// retry cannot change a failed job back to queued in the gap and leave us
	// waiting on a nil channel. The store remains the source of truth; the hub
	// only wakes a subsequent drain sooner.
	ch, cancel := s.hub.Subscribe(id)
	defer cancel()
	exhausted := s.eventsExhausted(r.Context(), id)

	pending, err := s.store.ListEventsAfterLimit(r.Context(), id, lastID, streamEventPageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(pending) == 0 && exhausted {
		// Nothing left to replay and the job will never emit another event:
		// refuse the subscription instead of holding a socket open forever.
		terminalJob, _ := s.store.GetJob(r.Context(), id)
		writeError(w, http.StatusConflict, fmt.Errorf("job %s is already %s: no further events will be emitted", id, terminalJob.Status))
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return // Accept already wrote the handshake failure response
	}
	defer conn.CloseNow()

	// CloseRead answers control frames (ping/pong/close) in the background and
	// cancels the returned context when the client goes away. The protocol is
	// server-push only, so a client data frame also terminates the connection.
	ctx := conn.CloseRead(r.Context())

	drain := func() error {
		for {
			events, err := s.store.ListEventsAfterLimit(ctx, id, lastID, streamEventPageSize)
			if err != nil {
				// Keep the connection on a store read error; the next hub wake or
				// tick retries, mirroring the SSE drain.
				return nil
			}
			for _, ev := range events {
				raw, _ := json.Marshal(ev)
				if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
					return err
				}
				lastID = ev.ID
			}
			if len(events) < streamEventPageSize {
				return nil
			}
		}
	}

	for _, ev := range pending {
		raw, _ := json.Marshal(ev)
		if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
			return
		}
		lastID = ev.ID
	}
	if err := drain(); err != nil {
		return
	}
	if s.eventsExhausted(ctx, id) {
		// Backlog has been delivered in full and nothing more will ever
		// arrive (job event or hook event), so close instead of idling
		// forever. A hook may have recorded its terminal event after the
		// backlog read but before the HooksPending check just above; that
		// event is committed before Pending drops (see
		// hooks.Dispatcher.donePending), so one final drain is guaranteed to
		// deliver it before the connection closes.
		_ = drain()
		return
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			if err := drain(); err != nil {
				return
			}
			if s.eventsExhausted(ctx, id) {
				// A pending hook (the reason this loop was entered instead of
				// closing right after the initial backlog) has now recorded
				// its terminal event: nothing more will ever arrive. That
				// event may have been committed after drain's read, so drain
				// once more before closing.
				_ = drain()
				return
			}
		case <-ticker.C:
			if err := drain(); err != nil {
				return
			}
			if s.eventsExhausted(ctx, id) {
				_ = drain()
				return
			}
			pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pingCtx)
			pingCancel()
			if err != nil {
				return
			}
		}
	}
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.GetJob(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	lastID, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)

	// Subscribe before inspecting state or reading the backlog so a concurrent
	// retry cannot change a failed job back to queued in the gap and leave us
	// waiting on a nil channel. The store remains the source of truth; the hub
	// only wakes a subsequent drain sooner.
	ch, cancel := s.hub.Subscribe(id)
	defer cancel()
	exhausted := s.eventsExhausted(r.Context(), id)

	pending, err := s.store.ListEventsAfterLimit(r.Context(), id, lastID, streamEventPageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(pending) == 0 && exhausted {
		// Nothing left to replay and the job will never emit another event:
		// refuse the subscription instead of holding a connection open forever.
		terminalJob, _ := s.store.GetJob(r.Context(), id)
		writeError(w, http.StatusConflict, fmt.Errorf("job %s is already %s: no further events will be emitted", id, terminalJob.Status))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}

	// drain streams every persisted event newer than lastID directly from the
	// store, so delivery never depends on the in-memory hub channel. The hub
	// only wakes us to drain sooner; any event dropped by a full channel buffer
	// is still picked up on the next drain, and the ticker bounds the tail
	// latency for a dropped final event that has no successor to wake us.
	drain := func() {
		for {
			events, err := s.store.ListEventsAfterLimit(r.Context(), id, lastID, streamEventPageSize)
			if err != nil {
				return
			}
			for _, ev := range events {
				writeSSE(w, ev)
				lastID = ev.ID
			}
			flusher.Flush()
			if len(events) < streamEventPageSize {
				return
			}
		}
	}

	for _, ev := range pending {
		writeSSE(w, ev)
		lastID = ev.ID
	}
	flusher.Flush()
	drain()
	if s.eventsExhausted(r.Context(), id) {
		// Backlog has been delivered in full and nothing more will ever
		// arrive (job event or hook event), so close instead of idling
		// forever. A hook may have recorded its terminal event after the
		// backlog read but before the HooksPending check just above; that
		// event is committed before Pending drops (see
		// hooks.Dispatcher.donePending), so one final drain is guaranteed to
		// deliver it before the stream closes.
		drain()
		return
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			drain()
			if s.eventsExhausted(r.Context(), id) {
				// A pending hook (the reason this loop was entered instead of
				// closing right after the initial backlog) has now recorded
				// its terminal event: nothing more will ever arrive. That
				// event may have been committed after drain's read, so drain
				// once more before closing.
				drain()
				return
			}
		case <-ticker.C:
			drain()
			if s.eventsExhausted(r.Context(), id) {
				drain()
				return
			}
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev domain.Event) {
	raw, _ := json.Marshal(ev)
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, raw)
}
