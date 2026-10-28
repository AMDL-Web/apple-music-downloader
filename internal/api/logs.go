package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"amdl/internal/logging"
)

func (s *Server) listLogs(w http.ResponseWriter, r *http.Request) {
	filter, err := parseLogFilter(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.logStore == nil {
		writeJSON(w, http.StatusOK, map[string]any{"logs": []logging.Entry{}, "next_cursor": 0, "oldest_sequence": 0, "truncated": false})
		return
	}
	page := s.logStore.List(filter)
	writeJSON(w, http.StatusOK, map[string]any{
		"logs": page.Entries, "next_cursor": page.NextCursor,
		"oldest_sequence": page.OldestSequence, "truncated": page.Truncated,
	})
}

func (s *Server) streamLogs(w http.ResponseWriter, r *http.Request) {
	filter, err := parseLogFilter(r, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.logStore == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("log buffer is not configured"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming is not supported"))
		return
	}
	backlog, live, unsubscribe := s.logStore.Subscribe(filter)
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	for _, entry := range backlog {
		if err := writeLogEvent(w, entry); err != nil {
			return
		}
	}
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	write := func(entry logging.Entry) error {
		if err := writeLogEvent(w, entry); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	keepalive := func() error {
		if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	runLogStream(r.Context(), live, heartbeat.C, write, keepalive)
}

// runLogStream drives the live phase of the SSE log endpoint.
// Callers replay their own backlog first, then hand the subscription here: each
// published record goes out through write, and every tick calls keepalive so an
// idle connection stays open and a dead peer is detected. A closed live channel
// means the store dropped this slow subscriber, so the client must reconnect
// with its last sequence to fill the gap.
func runLogStream(
	ctx context.Context,
	live <-chan logging.Entry,
	tick <-chan time.Time,
	write func(logging.Entry) error,
	keepalive func() error,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-live:
			if !ok || write(entry) != nil {
				return
			}
		case <-tick:
			if keepalive() != nil {
				return
			}
		}
	}
}

func writeLogEvent(w io.Writer, entry logging.Entry) error {
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: log\ndata: %s\n\n", entry.Sequence, raw)
	return err
}

func parseLogFilter(r *http.Request, stream bool) (logging.Filter, error) {
	q := r.URL.Query()
	filter := logging.Filter{
		Component: strings.TrimSpace(q.Get("component")),
		RequestID: strings.TrimSpace(q.Get("request_id")),
		JobID:     strings.TrimSpace(q.Get("job_id")),
		Query:     strings.TrimSpace(q.Get("q")),
		Limit:     200,
	}
	if stream {
		filter.Limit = 500
	}
	for _, level := range strings.Split(q.Get("level"), ",") {
		if level = strings.TrimSpace(strings.ToLower(level)); level != "" {
			switch level {
			case "debug", "info", "warn", "error":
				filter.Levels = append(filter.Levels, level)
			default:
				return filter, fmt.Errorf("level %q is not supported", level)
			}
		}
	}
	if raw := strings.TrimSpace(q.Get("after")); raw != "" {
		after, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return filter, errors.New("after must be a non-negative integer")
		}
		filter.After = after
	} else if stream {
		if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
			after, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				return filter, errors.New("Last-Event-ID must be a non-negative integer")
			}
			filter.After = after
		}
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 1000 {
			return filter, errors.New("limit must be an integer between 1 and 1000")
		}
		filter.Limit = limit
	}
	var err error
	if filter.Since, err = parseLogTime(q.Get("since")); err != nil {
		return filter, fmt.Errorf("since: %w", err)
	}
	if filter.Until, err = parseLogTime(q.Get("until")); err != nil {
		return filter, fmt.Errorf("until: %w", err)
	}
	if !filter.Since.IsZero() && !filter.Until.IsZero() && filter.Since.After(filter.Until) {
		return filter, errors.New("since must be before or equal to until")
	}
	return filter, nil
}

func parseLogTime(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, errors.New("must be an RFC3339 timestamp")
	}
	return parsed, nil
}
