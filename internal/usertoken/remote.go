package usertoken

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"amdl/internal/wrapper"
)

// Endpoint is the wrapper's GET /token source (see the package comment).
type Endpoint struct {
	// Fetch asks the wrapper for its token, bounded by the wrapper client's
	// request timeout (wrapper.Client.MusicToken).
	Fetch func(context.Context) (wrapper.TokenReply, error)
	// Dir holds amdl's copy of the token (StoreDir).
	Dir string
	// Address is wrapper.address; a stored copy from another address is not
	// used.
	Address string
	Logger  *slog.Logger
}

// Refresh timing. The wrapper refreshes its token every
// --token-refresh-interval (1800 s by default) and says when next; amdl asks
// again a few seconds after that, so it gets the new token. The floor keeps a
// next_refresh_at in the past (a wrapper whose own refresh keeps failing)
// from turning into a loop, and the ceiling keeps a far-future one from
// stalling.
const (
	refreshFloor     = time.Minute
	refreshCeiling   = time.Hour
	jitterMin        = 5 * time.Second
	jitterSpan       = 10 * time.Second
	loggedOutRetry   = time.Minute
	unsupportedRetry = 30 * time.Minute
	backoffMin       = 5 * time.Second
	backoffMax       = 5 * time.Minute
	nudgeSpacing     = 30 * time.Second
)

// endpoint state as last answered, for logging changes only.
const (
	stateUnknown = iota
	stateOK
	stateLoggedOut
	stateUnsupported
	stateFailing
)

type remote struct {
	fetch   func(context.Context) (wrapper.TokenReply, error)
	store   tokenStore
	address string
	logger  *slog.Logger
	now     func() time.Time
	jitter  func() time.Duration
	nudge   chan struct{}

	floor, ceiling, loggedOutRetry, unsupportedRetry time.Duration
	backoffMin, backoffMax, nudgeSpacing             time.Duration

	mu     sync.Mutex
	live   string // from a 200 in this run
	stored string // loaded from Dir at start, until the wrapper answers
	state  int
	fails  int
}

func newRemote(e Endpoint) *remote {
	logger := e.Logger
	if logger == nil {
		logger = slog.Default().With("component", "usertoken")
	}
	r := &remote{
		fetch:            e.Fetch,
		store:            tokenStore{dir: e.Dir},
		address:          e.Address,
		logger:           logger,
		now:              time.Now,
		jitter:           func() time.Duration { return jitterMin + rand.N(jitterSpan) },
		nudge:            make(chan struct{}, 1),
		floor:            refreshFloor,
		ceiling:          refreshCeiling,
		loggedOutRetry:   loggedOutRetry,
		unsupportedRetry: unsupportedRetry,
		backoffMin:       backoffMin,
		backoffMax:       backoffMax,
		nudgeSpacing:     nudgeSpacing,
	}
	v, err := r.store.load(e.Address)
	if err != nil {
		logger.Warn("amdl's stored copy of the wrapper's token is unusable; ignoring it", "error", err.Error())
	}
	r.stored = v
	return r
}

// liveToken is the token from this run's last 200, and storedToken the copy
// from an earlier run, kept until the wrapper answers.
func (r *remote) liveToken() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.live
}

func (r *remote) storedToken() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stored
}

func (r *remote) requestRefresh() {
	if r == nil {
		return
	}
	select {
	case r.nudge <- struct{}{}:
	default: // one is already pending
	}
}

// run fetches at once, then whenever the last answer says to, or on a nudge
// at most once per nudgeSpacing; a nudge is ignored while the wrapper has no
// /token.
func (r *remote) run(ctx context.Context) {
	var last time.Time
	delay := time.Duration(0)
	for ctx.Err() == nil {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-r.nudge:
			timer.Stop()
			r.mu.Lock()
			unsupported := r.state == stateUnsupported
			r.mu.Unlock()
			if unsupported {
				delay = max(0, r.unsupportedRetry-r.now().Sub(last))
				continue
			}
			if wait := r.nudgeSpacing - r.now().Sub(last); !last.IsZero() && wait > 0 {
				delay = wait
				continue
			}
		}
		last = r.now()
		delay = r.fetchOnce(ctx)
	}
}

// fetchOnce asks the wrapper once, applies the answer and returns the wait
// before the next fetch.
func (r *remote) fetchOnce(ctx context.Context) time.Duration {
	reply, err := r.fetch(ctx)
	if ctx.Err() != nil {
		return 0
	}
	var v string
	if err == nil {
		v, err = Validate(reply.MusicToken)
	}
	switch {
	case err == nil:
		return r.gotToken(v, reply)
	case errors.Is(err, wrapper.ErrNoToken):
		r.forget(stateLoggedOut, "the wrapper has no logged-in account; its token is no longer used")
		return r.loggedOutRetry
	case errors.Is(err, wrapper.ErrTokenUnsupported):
		r.forget(stateUnsupported, "the wrapper has no GET /token; its token is read from files only")
		return r.unsupportedRetry
	default:
		return r.failed(err)
	}
}

func (r *remote) gotToken(v string, reply wrapper.TokenReply) time.Duration {
	now := r.now()
	r.mu.Lock()
	prev := r.state
	r.live, r.stored, r.state, r.fails = v, "", stateOK, 0
	r.mu.Unlock()
	next := time.Unix(reply.NextRefreshAt, 0)
	if reply.NextRefreshAt <= 0 {
		next = time.Time{}
	}
	if err := r.store.save(storedToken{
		Address: r.address, Region: reply.Region, MusicToken: v,
		RefreshedAt: reply.RefreshedAt, NextRefreshAt: reply.NextRefreshAt, FetchedAt: now.Unix(),
	}); err != nil {
		r.logger.Warn("could not store a copy of the wrapper's token; it is used from memory", "dir", r.store.dir, "error", err.Error())
	}
	wait := r.afterToken(now, next)
	level := slog.LevelDebug
	if prev != stateOK {
		level = slog.LevelInfo
	}
	r.logger.Log(context.Background(), level, "using the wrapper's token from GET /token",
		"region", reply.Region, "next_refresh_at", unixTime(reply.NextRefreshAt), "next_fetch_in", wait.Round(time.Second).String())
	return wait
}

// afterToken is the wait after a 200 at now whose next_refresh_at is next
// (zero when the reply had none): next plus jitter, clamped to the floor and
// the ceiling.
func (r *remote) afterToken(now, next time.Time) time.Duration {
	if next.IsZero() || !next.After(now) {
		return r.floor
	}
	return min(max(next.Sub(now)+r.jitter(), r.floor), r.ceiling)
}

// forget drops the live token and the stored copy after an answer that says
// the wrapper cannot vouch for any token.
func (r *remote) forget(state int, msg string) {
	r.mu.Lock()
	prev := r.state
	had := r.live != "" || r.stored != ""
	r.live, r.stored, r.state, r.fails = "", "", state, 0
	r.mu.Unlock()
	if err := r.store.drop(); err != nil {
		r.logger.Warn("could not remove the stored copy of the wrapper's token", "dir", r.store.dir, "error", err.Error())
	}
	if prev != state {
		level := slog.LevelInfo
		if state == stateLoggedOut {
			level = slog.LevelWarn
		}
		r.logger.Log(context.Background(), level, msg, "dropped_previous", had)
	}
}

// failed keeps whatever token there is, so amdl works on through a wrapper
// restart, and backs off.
func (r *remote) failed(err error) time.Duration {
	r.mu.Lock()
	prev := r.state
	r.state = stateFailing
	r.fails++
	n := r.fails
	kept := r.live != "" || r.stored != ""
	r.mu.Unlock()
	wait := r.backoffMin
	for i := 1; i < n && wait < r.backoffMax; i++ {
		wait *= 2
	}
	wait = min(wait, r.backoffMax)
	if prev != stateFailing {
		r.logger.Warn("could not fetch the wrapper's token from GET /token; retrying",
			"error", err.Error(), "kept_previous", kept, "retry_in", wait.String())
	}
	return wait
}

func unixTime(sec int64) string {
	if sec <= 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}
