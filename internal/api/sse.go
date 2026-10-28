package api

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// sseKeepaliveInterval is how often every stream writes a ": keepalive"
	// comment, so an idle proxy keeps the connection open and a client that
	// went away surfaces as a write error that ends the stream.
	sseKeepaliveInterval = 15 * time.Second
	// storeRedrainInterval is how often the job event streams re-read the
	// store without a hub wake. The hub drops a wake when a subscriber's
	// buffer is full, so this bounds how long such an event waits. It is not
	// the keepalive: one bounds latency, the other keeps the connection open.
	storeRedrainInterval = 10 * time.Second
)

// streamTicks are a stream loop's timers; tests pass channels they control.
type streamTicks struct {
	redrain   <-chan time.Time
	keepalive <-chan time.Time
}

// newStreamTicks starts the production timers; call stop when the stream ends.
func newStreamTicks() (ticks streamTicks, stop func()) {
	redrain := time.NewTicker(storeRedrainInterval)
	keepalive := time.NewTicker(sseKeepaliveInterval)
	return streamTicks{redrain: redrain.C, keepalive: keepalive.C}, func() {
		redrain.Stop()
		keepalive.Stop()
	}
}

// sseSink is what a stream loop writes to; *sseStream in production. Every
// method flushes, and an error means the client is gone and the loop must end.
type sseSink interface {
	send(id, name string, data []byte) error
	keepalive() error
}

// sseStream writes Server-Sent Events to one client.
type sseStream struct {
	w  io.Writer
	rc *http.ResponseController
}

// startSSE writes the response head of a stream and flushes it, so the
// client's EventSource opens even when there is nothing to send yet. Callers
// answer every error status (400, 404, 204) before calling it.
func startSSE(w http.ResponseWriter) (*sseStream, error) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	// nginx buffers proxied responses by default, which would hold events
	// back until its buffer fills.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	stream := &sseStream{w: w, rc: http.NewResponseController(w)}
	if err := stream.rc.Flush(); err != nil {
		return nil, fmt.Errorf("flush stream head: %w", err)
	}
	return stream, nil
}

// send writes one named event; data must be a single line (compact JSON).
func (s *sseStream) send(id, name string, data []byte) error {
	if _, err := fmt.Fprintf(s.w, "id: %s\nevent: %s\ndata: %s\n\n", id, name, data); err != nil {
		return err
	}
	return s.rc.Flush()
}

func (s *sseStream) keepalive() error {
	if _, err := io.WriteString(s.w, ": keepalive\n\n"); err != nil {
		return err
	}
	return s.rc.Flush()
}

// streamCursor returns the resume cursor of a stream request: the
// Last-Event-ID header when it is set, else the cursor query parameter, else
// 0 (no cursor). The header wins because EventSource sends it on every
// automatic reconnect, so it is newer than a ?cursor= written into the URL at
// the first connect.
func streamCursor(r *http.Request) (uint64, error) {
	if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
		return parseCursor("Last-Event-ID", raw)
	}
	return queryCursor(r)
}

// queryCursor returns the cursor query parameter, or 0 when it is absent.
func queryCursor(r *http.Request) (uint64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if raw == "" {
		return 0, nil
	}
	return parseCursor("cursor", raw)
}

// parseCursor accepts a non-negative integer that fits an event id (int64).
func parseCursor(name, raw string) (uint64, error) {
	cursor, err := strconv.ParseUint(raw, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return cursor, nil
}

// writeCursorError answers a malformed resume cursor.
func writeCursorError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
}
