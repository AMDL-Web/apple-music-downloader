package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Event is one Server-Sent Event.
type Event struct {
	// ID is the event's id field, the resume cursor of the amdl streams.
	ID string
	// Name is the event field: the EventType on a per-job stream, empty on
	// the overview feed.
	Name string
	Data string
}

// ErrWatchdog ends a stream that sent nothing for Client.Watchdog.
var ErrWatchdog = errors.New("stream silent past the watchdog")

// StreamResult says how one SSE connection ended.
type StreamResult struct {
	// Status is the HTTP status of the answer, 0 when none came.
	Status int
	// LastID is the id of the last event delivered, or the cursor the
	// connection started from when none was.
	LastID string
	// Events counts the events delivered.
	Events int
}

// Stream opens one SSE connection to path, resuming after cursor when it is
// not empty. open, when not nil, runs once the server has answered 200; fn
// runs for each event in order. Both run on the calling goroutine. Stream
// returns when the server ends the stream, ctx ends, or nothing arrives for
// c.Watchdog (ErrWatchdog). A 204 answer, which the per-job stream sends once
// a finished job has nothing left to deliver, returns Status 204 and no error.
func (c *Client) Stream(ctx context.Context, path, cursor string, open func(), fn func(Event)) (StreamResult, error) {
	res := StreamResult{LastID: cursor}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	watchdog := c.Watchdog
	if watchdog <= 0 {
		watchdog = 45 * time.Second
	}
	timer := time.AfterFunc(watchdog, func() { cancel(ErrWatchdog) })
	defer timer.Stop()

	q := url.Values{}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path, q), nil)
	if err != nil {
		return res, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := c.stream.Do(req)
	if err != nil {
		return res, streamErr(ctx, err)
	}
	defer resp.Body.Close()
	res.Status = resp.StatusCode
	if resp.StatusCode == http.StatusNoContent {
		return res, nil
	}
	if resp.StatusCode != http.StatusOK {
		var raw [4096]byte
		n, _ := resp.Body.Read(raw[:])
		return res, decodeAPIError(resp.StatusCode, raw[:n])
	}
	if open != nil {
		open()
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	var ev Event
	var data strings.Builder
	hasData := false
	for sc.Scan() {
		timer.Reset(watchdog)
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" {
			if hasData {
				ev.Data = data.String()
				if ev.ID != "" {
					res.LastID = ev.ID
				}
				res.Events++
				fn(ev)
			}
			ev, hasData = Event{}, false
			data.Reset()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			ev.Name = value
		case "id":
			ev.ID = value
		case "data":
			if hasData {
				data.WriteByte('\n')
			}
			data.WriteString(value)
			hasData = true
		}
	}
	if err := sc.Err(); err != nil {
		return res, streamErr(ctx, err)
	}
	return res, streamErr(ctx, nil)
}

// streamErr prefers the watchdog or the caller's cancellation over the read
// error it caused.
func streamErr(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		if errors.Is(cause, ErrWatchdog) {
			return ErrWatchdog
		}
		return cause
	}
	if err != nil {
		return fmt.Errorf("stream: %w", err)
	}
	return nil
}
