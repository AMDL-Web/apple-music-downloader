package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"amdl/internal/domain"
)

// Phase is where a follower's connection stands.
type Phase int

const (
	// Connecting: no answer yet, or reconnecting right after a stream ended.
	Connecting Phase = iota
	// Live: the stream is open and changes arrive as they happen.
	Live
	// Offline: the backend did not answer; RetryAt says when the next try is.
	Offline
)

// ConnState is a follower's connection state, reported on every change.
type ConnState struct {
	Phase Phase
	// Err is why the backend is offline or the stream ended.
	Err error
	// RetryAt is when an offline follower tries again.
	RetryAt time.Time
}

// FeedHandlers receive what FollowFeed sees, all on FollowFeed's goroutine.
type FeedHandlers struct {
	// Snapshot is a fresh copy of the job list, which replaces what the
	// receiver holds: on start, and whenever resuming the feed is impossible.
	Snapshot func(DownloadList)
	// Message is one change after the latest snapshot.
	Message func(domain.DownloadFeedMessage)
	// State reports the connection.
	State func(ConnState)
}

// SnapshotLimit is how many jobs a feed snapshot reads: the API's maximum
// page. Older jobs reach the receiver only as they change.
const SnapshotLimit = 200

// FollowFeed keeps a job list current until ctx ends: it reads a snapshot of
// GET /api/v1/downloads, then follows GET /api/v1/downloads/events from the
// snapshot's cursor. When the stream breaks it resumes from the last event it
// delivered, which replays what happened meanwhile; only an answer that
// rejects the cursor makes it read a new snapshot.
func (c *Client) FollowFeed(ctx context.Context, h FeedHandlers) {
	retry := c.newRetry()
	var cursor int64
	haveSnapshot := false
	state := func(s ConnState) {
		if h.State != nil {
			h.State(s)
		}
	}
	state(ConnState{Phase: Connecting})
	for ctx.Err() == nil {
		if !haveSnapshot {
			list, err := c.Downloads(ctx, ListQuery{Limit: SnapshotLimit})
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				wait := retry.next()
				state(ConnState{Phase: Offline, Err: err, RetryAt: time.Now().Add(wait)})
				sleep(ctx, wait)
				continue
			}
			if h.Snapshot != nil {
				h.Snapshot(list)
			}
			cursor, haveSnapshot = list.EventCursor, true
		}
		opened := time.Time{}
		res, err := c.Stream(ctx, "/api/v1/downloads/events", strconv.FormatInt(cursor, 10),
			func() {
				opened = time.Now()
				state(ConnState{Phase: Live})
			},
			func(ev Event) {
				var msg domain.DownloadFeedMessage
				if json.Unmarshal([]byte(ev.Data), &msg) != nil {
					return
				}
				if msg.EventID > cursor {
					cursor = msg.EventID
				}
				if h.Message != nil {
					h.Message(msg)
				}
			})
		if ctx.Err() != nil {
			return
		}
		switch {
		case res.Status >= 400 && res.Status < 500:
			haveSnapshot = false
			state(ConnState{Phase: Connecting, Err: err})
		case res.Status == 0:
			wait := retry.next()
			state(ConnState{Phase: Offline, Err: err, RetryAt: time.Now().Add(wait)})
			sleep(ctx, wait)
		default:
			// The stream was open and ended: the server shut down, or the
			// watchdog fired. A stream that lasted a while resets the backoff.
			if !opened.IsZero() && time.Since(opened) > 30*time.Second {
				retry.reset()
			}
			state(ConnState{Phase: Connecting, Err: err})
			sleep(ctx, retry.next())
		}
	}
}

// JobHandlers receive what FollowJob sees, all on FollowJob's goroutine.
type JobHandlers struct {
	// Detail is a fresh snapshot of the job, its items and hooks.
	Detail func(DownloadDetail)
	// Event is one event after the latest snapshot, or with FromStart any
	// event of the job's history. Lifecycle events carry the Job's fields in
	// Payload, item events the JobItem's.
	Event func(domain.Event)
	// FromStart replays the job's whole event history after the first
	// snapshot, for an activity log, even when the job has finished. Events
	// up to that snapshot's EventCursor are older than it: a receiver keeps
	// the snapshot's state over theirs.
	FromStart bool
}

// ErrJobGone is returned by FollowJob when the job no longer exists.
var ErrJobGone = errors.New("job not found")

// FollowJob follows one job until it is finished and nothing more will
// arrive for it (nil), the job is deleted (ErrJobGone), or ctx ends. It reads
// GET /api/v1/downloads/{id}, then its event stream from that snapshot's
// cursor, resuming after breaks. When the stream ends cleanly or answers 204
// it reads the snapshot again, which tells a finished job from a broken
// connection.
func (c *Client) FollowJob(ctx context.Context, id string, h JobHandlers) error {
	retry := c.newRetry()
	path := "/api/v1/downloads/" + url.PathEscape(id) + "/events"
	cursor := ""
	needDetail, first := true, true
	for ctx.Err() == nil {
		if needDetail {
			d, err := c.Download(ctx, id)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				var apiErr *APIError
				if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
					return ErrJobGone
				}
				sleep(ctx, retry.next())
				continue
			}
			if h.Detail != nil {
				h.Detail(d)
			}
			cursor, needDetail = strconv.FormatInt(d.EventCursor, 10), false
			if first && h.FromStart {
				cursor = "0"
			} else if Terminal(d.Job.Status) && !hookRunning(d.Hooks) {
				return nil
			}
			first = false
		}
		res, err := c.Stream(ctx, path, cursor, nil, func(ev Event) {
			var e domain.Event
			if json.Unmarshal([]byte(ev.Data), &e) != nil {
				return
			}
			if h.Event != nil {
				h.Event(e)
			}
		})
		cursor = res.LastID
		if ctx.Err() != nil {
			return ctx.Err()
		}
		switch {
		case res.Status == http.StatusNotFound:
			return ErrJobGone
		case res.Status == http.StatusNoContent || (err == nil && res.Status == http.StatusOK):
			needDetail = true
			if res.Events == 0 {
				// Guards against a server that keeps closing at once.
				sleep(ctx, retry.next())
			} else {
				retry.reset()
			}
		default:
			sleep(ctx, retry.next())
		}
	}
	return ctx.Err()
}

func hookRunning(hooks []domain.HookState) bool {
	for _, h := range hooks {
		if h.Status == "running" {
			return true
		}
	}
	return false
}

// retry is an exponential backoff between RetryMin and RetryMax.
type retry struct {
	min, max, cur time.Duration
}

func (c *Client) newRetry() *retry {
	lo, hi := c.RetryMin, c.RetryMax
	if lo <= 0 {
		lo = time.Second
	}
	if hi < lo {
		hi = 30 * time.Second
	}
	return &retry{min: lo, max: hi}
}

func (r *retry) next() time.Duration {
	if r.cur == 0 {
		r.cur = r.min
	} else {
		r.cur = min(r.cur*2, r.max)
	}
	return r.cur
}

func (r *retry) reset() { r.cur = 0 }

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
