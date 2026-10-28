package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"amdl/internal/logging"
)

func (s *Server) listLogs(w http.ResponseWriter, r *http.Request) {
	filter, err := parseLogFilter(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
		return
	}
	if s.logStore == nil {
		s.writeLogsUnavailable(w)
		return
	}
	page := s.logStore.List(filter)
	writeJSON(w, http.StatusOK, map[string]any{
		"logs": page.Entries, "next_cursor": page.NextCursor,
		"oldest_sequence": page.OldestSequence, "truncated": page.Truncated,
	})
}

// streamLogs is the SSE log tail: it replays the matching entries still in
// the buffer, then streams new ones as event "log" with the sequence as id.
func (s *Server) streamLogs(w http.ResponseWriter, r *http.Request) {
	filter, err := parseLogFilter(r, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
		return
	}
	if s.logStore == nil {
		s.writeLogsUnavailable(w)
		return
	}
	backlog, live, unsubscribe := s.logStore.Subscribe(filter)
	defer unsubscribe()
	stream, err := startSSE(w)
	if err != nil {
		return
	}
	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()
	runLogStream(r.Context(), stream, backlog, live, keepalive.C)
}

// writeLogsUnavailable answers a log route that has no log history to serve.
func (s *Server) writeLogsUnavailable(w http.ResponseWriter) {
	if s.logsOff {
		writeError(w, http.StatusServiceUnavailable, CodeFeatureUnavailable, "in-memory log retention is off (logging.buffer_size: 0)")
		return
	}
	writeFeatureUnavailable(w, "the log buffer")
}

// runLogStream sends backlog, then each record published on live, plus a
// keepalive comment on every tick. A closed live channel means the store
// dropped this slow subscriber, so the stream ends and the client reconnects
// with its last sequence to fill the gap.
func runLogStream(ctx context.Context, sink sseSink, backlog []logging.Entry, live <-chan logging.Entry, keepalive <-chan time.Time) {
	for _, entry := range backlog {
		if sendLogEntry(sink, entry) != nil {
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-live:
			if !ok || sendLogEntry(sink, entry) != nil {
				return
			}
		case <-keepalive:
			if sink.keepalive() != nil {
				return
			}
		}
	}
}

// sendLogEntry writes one entry. An entry that cannot be encoded is skipped
// rather than ending the stream, which would replay it on every reconnect.
func sendLogEntry(sink sseSink, entry logging.Entry) error {
	raw, err := json.Marshal(entry)
	if err != nil {
		return nil
	}
	return sink.send(strconv.FormatUint(entry.Sequence, 10), "log", raw)
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
	cursor := queryCursor
	if stream {
		cursor = streamCursor
	}
	after, err := cursor(r)
	if err != nil {
		return filter, err
	}
	filter.After = after
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 1000 {
			return filter, errors.New("limit must be an integer between 1 and 1000")
		}
		filter.Limit = limit
	}
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
