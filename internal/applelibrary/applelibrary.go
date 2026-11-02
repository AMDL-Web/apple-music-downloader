// Package applelibrary keeps a local copy of the signed-in user's Apple Music
// account library (albums, songs, playlists with their tracks, artists) in the
// SQLite store, so GET /api/v1/apple-library and the in_apple_library mark on
// search hits never wait on Apple. It is not the on-disk library layout.
//
// # Syncing without a change feed
//
// Apple offers no change feed and no per-song add time (see internal/librarysync),
// only a dependable newest-first order for songs and albums, meta.total per
// collection, and lastModifiedDate / playParams.versionHash per playlist. So:
//
//   - An incremental pass reads songs and albums newest-first until an id
//     already cached, the whole playlist list, and the artist total. A kind
//     whose total then disagrees with the cache (something was removed) is
//     read in full, at most once per mismatchFullEvery.
//   - A full pass reads every page of every kind, tags each row it saw with the
//     run id and removes the untagged rows of a kind only when every page of it
//     was read and meta.total did not move during the read.
//   - A playlist's track list is re-read when its lastModifiedDate or
//     versionHash differs from the one its cached tracks were read at; a full
//     pass also refreshes the stalest lists.
//
// Every pass is bounded (deadline, page caps), idempotent and safe to cut
// short: an interrupted pass removes nothing. A list that reaches its page cap
// (30,000 songs, 5,000 tracks of one playlist) is cached in part and named in
// Run.Capped; the pass still finishes the other lists. A sync never touches download
// jobs; reading the library is an optional read, so without a usable
// media-user-token the engine idles and says so in Status.
package applelibrary

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/usertoken"
)

// Apple is the slice of the Apple Music client the engine reads with.
type Apple interface {
	LibraryItems(ctx context.Context, mediaUserToken string, kind applemusic.LibraryKind, newestFirst bool, offset, limit int) (applemusic.LibraryPage, error)
	LibraryPlaylistTracks(ctx context.Context, mediaUserToken, playlistID string, offset, limit int) (applemusic.LibraryPage, error)
	AccountStorefront(ctx context.Context, mediaUserToken string) (string, error)
}

// Sync modes and triggers, the values of Run.Mode and Run.Trigger.
const (
	ModeFull        = "full"
	ModeIncremental = "incremental"
	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
)

// Reasons Status.Reason gives for available: false, also the details.reason
// of the apple_library_unavailable error.
const (
	ReasonNoToken      = "no_token"
	ReasonTokenRefused = "token_refused"
)

// ErrRunning is returned by Trigger while a pass runs.
var ErrRunning = errors.New("an Apple library sync is already running")

// UnavailableError is returned when the library cannot be read: there is no
// usable media-user-token and nothing is cached.
type UnavailableError struct{ Reason string }

func (e UnavailableError) Error() string {
	if e.Reason == ReasonTokenRefused {
		return "Apple refused the media-user-token, so the Apple Music library cannot be read"
	}
	return "no media-user-token: the Apple Music library cannot be read without one (sign in to the wrapper)"
}

// Run is one finished or running sync pass (the AppleLibrarySyncRun schema).
type Run struct {
	ID         string     `json:"id"`
	Mode       string     `json:"mode"`
	Trigger    string     `json:"trigger"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	OK         bool       `json:"ok"`
	Error      string     `json:"error,omitempty"`
	// Requests counts the Apple requests the pass made.
	Requests int `json:"requests"`
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Changed  int `json:"changed"`
	// Capped names the lists a page cap cut short in this pass: a kind
	// ("songs") or "playlist_tracks:<library id>". The pass is still ok; the
	// rows read are cached and nothing was removed for those lists.
	Capped []string `json:"capped,omitempty"`
}

// Progress is the pass under way (the AppleLibrarySyncProgress schema).
type Progress struct {
	ID        string    `json:"id"`
	Mode      string    `json:"mode"`
	Trigger   string    `json:"trigger"`
	StartedAt time.Time `json:"started_at"`
	Kind      string    `json:"kind"`
	PagesRead int       `json:"pages_read"`
	Requests  int       `json:"requests"`
}

// Status is the AppleLibraryStatus schema.
type Status struct {
	Available           bool           `json:"available"`
	Reason              string         `json:"reason,omitempty"`
	State               string         `json:"state"`
	Current             *Progress      `json:"current,omitempty"`
	LastSync            *Run           `json:"last_sync,omitempty"`
	LastFullSyncAt      *time.Time     `json:"last_full_sync_at,omitempty"`
	NextSyncAt          *time.Time     `json:"next_sync_at,omitempty"`
	Counts              map[string]int `json:"counts"`
	SyncIntervalMinutes int            `json:"sync_interval_minutes"`
	FullSyncHours       int            `json:"full_sync_hours"`
}

// Query selects a page of one kind; see db.AppleLibraryQuery.
type Query = db.AppleLibraryQuery

// Page is one page of a kind (the AppleLibraryPage schema). SyncedAt is when
// the kind last completed a full read, nil before the first.
type Page struct {
	Kind     string                    `json:"kind"`
	Items    []domain.AppleLibraryItem `json:"items"`
	Total    int                       `json:"total"`
	Offset   int                       `json:"offset"`
	SyncedAt *time.Time                `json:"synced_at,omitempty"`
}

// Tracks is a cached playlist's track list (the AppleLibraryTracks schema).
// ReadAt is nil while the list has not been read yet.
type Tracks struct {
	PlaylistID string                     `json:"playlist_id"`
	Items      []domain.AppleLibraryTrack `json:"items"`
	Total      int                        `json:"total"`
	ReadAt     *time.Time                 `json:"read_at,omitempty"`
}

// ErrPlaylistNotFound is returned by PlaylistTracks for a playlist that is
// not cached.
var ErrPlaylistNotFound = errors.New("playlist not in the cached Apple library")

// kindState is persisted per kind under the meta key "kind:<kind>".
type kindState struct {
	Total  int        `json:"total"`
	FullAt *time.Time `json:"full_at,omitempty"`
	// Capped is set when the last full read hit its page cap: the cache holds
	// the first pages only.
	Capped bool `json:"capped,omitempty"`
}

// Meta keys of apple_library_meta.
const (
	metaLastRun    = "last_run"
	metaLastFullAt = "last_full_at"
)

// tick is how often the loop checks whether a pass is due; failureBackoff is
// how long the schedule waits after a failed pass before it tries again.
const (
	tick           = 15 * time.Second
	failureBackoff = 10 * time.Minute
)

// Engine syncs the cache on the schedule in the live config and on request.
type Engine struct {
	cfg    *config.Store
	tokens *usertoken.Provider
	apple  Apple
	store  *db.Store
	logger *slog.Logger
	now    func() time.Time

	mu      sync.Mutex
	base    context.Context
	running *Progress
	lastRun *Run
	// lastFullAt is the end of the last successful full pass.
	lastFullAt *time.Time
	// refused is the digest of the token Apple last refused; a different
	// token clears it.
	refused [sha256.Size]byte
	// capNoted holds the lists whose page cap is already logged.
	capNoted map[string]bool
	loaded   bool
	passes   sync.WaitGroup
}

// New builds the engine. tokens picks the media-user-token; nil uses
// catalog.media_user_token alone. It does no I/O; Run runs the schedule.
func New(cfg *config.Store, tokens *usertoken.Provider, apple Apple, store *db.Store, logger *slog.Logger) *Engine {
	return &Engine{cfg: cfg, tokens: tokens, apple: apple, store: store, logger: logger, now: time.Now, capNoted: map[string]bool{}}
}

// Run drives the schedule until ctx is cancelled and returns once every pass,
// a manual one included, has stopped. The caller runs it on its own goroutine
// and waits for it before closing the store.
func (e *Engine) Run(ctx context.Context) {
	e.mu.Lock()
	e.base = ctx
	e.mu.Unlock()
	e.load(ctx)
	e.loop(ctx, tick)
	e.passes.Wait()
}

func (e *Engine) loop(ctx context.Context, every time.Duration) {
	timer := time.NewTicker(every)
	defer timer.Stop()
	for {
		if mode := e.due(); mode != "" {
			if token := e.token(); token != "" && !e.isRefused(token) {
				if e.begin(mode, TriggerSchedule) == nil {
					e.passes.Add(1)
					e.pass(ctx, token)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

// load reads the last run and full-sync time once, so the schedule and the
// status survive a restart.
func (e *Engine) load(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loaded {
		return
	}
	e.loaded = true
	if raw, err := e.store.AppleLibraryMeta(ctx, metaLastRun); err == nil && raw != "" {
		var run Run
		if json.Unmarshal([]byte(raw), &run) == nil {
			e.lastRun = &run
		}
	}
	if raw, err := e.store.AppleLibraryMeta(ctx, metaLastFullAt); err == nil && raw != "" {
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			e.lastFullAt = &t
		}
	}
}

// due names the pass the schedule wants now, or "". The first full pass runs
// whatever full_sync_hours says, since until then there is nothing to browse.
func (e *Engine) due() string {
	cfg := e.cfg.Get().AppleLibrary
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running != nil {
		return ""
	}
	now := e.now()
	if e.lastRun != nil && !e.lastRun.OK && e.lastRun.FinishedAt != nil && now.Sub(*e.lastRun.FinishedAt) < failureBackoff {
		return ""
	}
	if e.lastFullAt == nil {
		return ModeFull
	}
	if cfg.FullSyncHours > 0 && now.Sub(*e.lastFullAt) >= time.Duration(cfg.FullSyncHours)*time.Hour {
		return ModeFull
	}
	if cfg.SyncIntervalMinutes > 0 && (e.lastRun == nil || e.lastRun.FinishedAt == nil ||
		now.Sub(*e.lastRun.FinishedAt) >= time.Duration(cfg.SyncIntervalMinutes)*time.Minute) {
		return ModeIncremental
	}
	return ""
}

func (e *Engine) token() string { return e.tokens.Token(e.cfg.Get()).Value }

func (e *Engine) isRefused(token string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.refused == sha256.Sum256([]byte(token))
}

// begin claims the single pass slot.
func (e *Engine) begin(mode, trigger string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running != nil {
		return ErrRunning
	}
	e.running = &Progress{ID: newRunID(), Mode: mode, Trigger: trigger, StartedAt: e.now().UTC()}
	return nil
}

// Trigger starts a pass of mode in the background and returns the status
// with the pass as current. It returns ErrRunning while a pass runs and an
// UnavailableError without a usable token.
func (e *Engine) Trigger(ctx context.Context, mode string) (Status, error) {
	e.load(ctx)
	token := e.token()
	if token == "" {
		return e.Status(ctx), UnavailableError{Reason: ReasonNoToken}
	}
	if err := e.begin(mode, TriggerManual); err != nil {
		return e.Status(ctx), err
	}
	e.mu.Lock()
	base := e.base
	e.mu.Unlock()
	if base == nil {
		base = context.Background()
	}
	e.passes.Add(1)
	go e.pass(base, token)
	return e.Status(ctx), nil
}

// SyncNow runs a pass of mode to the end on the caller's goroutine and
// returns it; ErrRunning while another pass runs.
func (e *Engine) SyncNow(ctx context.Context, mode string) (Run, error) {
	e.load(ctx)
	token := e.token()
	if token == "" {
		return Run{}, UnavailableError{Reason: ReasonNoToken}
	}
	if err := e.begin(mode, TriggerManual); err != nil {
		return Run{}, err
	}
	e.passes.Add(1)
	return e.pass(ctx, token), nil
}

// Running reports the id of the pass under way, "" when idle.
func (e *Engine) Running() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running == nil {
		return ""
	}
	return e.running.ID
}

// Status reports availability, the pass under way, the last pass and the
// cached counts.
func (e *Engine) Status(ctx context.Context) Status {
	e.load(ctx)
	cfg := e.cfg.Get().AppleLibrary
	token := e.token()
	counts, err := e.store.AppleLibraryCounts(ctx)
	if err != nil {
		counts = map[string]int{}
	}
	for _, kind := range applemusic.LibraryKinds {
		if _, ok := counts[string(kind)]; !ok {
			counts[string(kind)] = 0
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	st := Status{Available: true, State: "idle", Counts: counts, LastFullSyncAt: e.lastFullAt,
		SyncIntervalMinutes: cfg.SyncIntervalMinutes, FullSyncHours: cfg.FullSyncHours}
	switch {
	case token == "":
		st.Available, st.Reason = false, ReasonNoToken
	case e.refused == sha256.Sum256([]byte(token)):
		st.Available, st.Reason = false, ReasonTokenRefused
	}
	if e.lastRun != nil {
		run := *e.lastRun
		st.LastSync = &run
		if !run.OK {
			st.State = "failed"
		}
	}
	if e.running != nil {
		p := *e.running
		st.Current, st.State = &p, "running"
	}
	st.NextSyncAt = e.nextSyncLocked(cfg)
	return st
}

func (e *Engine) nextSyncLocked(cfg config.AppleLibraryConfig) *time.Time {
	if e.lastFullAt == nil || e.lastRun == nil || e.lastRun.FinishedAt == nil {
		return nil
	}
	var next *time.Time
	consider := func(t time.Time) {
		if next == nil || t.Before(*next) {
			next = &t
		}
	}
	if cfg.FullSyncHours > 0 {
		consider(e.lastFullAt.Add(time.Duration(cfg.FullSyncHours) * time.Hour))
	}
	if cfg.SyncIntervalMinutes > 0 {
		consider(e.lastRun.FinishedAt.Add(time.Duration(cfg.SyncIntervalMinutes) * time.Minute))
	}
	if next != nil && !e.lastRun.OK {
		if retry := e.lastRun.FinishedAt.Add(failureBackoff); next.Before(retry) {
			next = &retry
		}
	}
	return next
}

// List returns a page of one kind from the cache. It returns an
// UnavailableError only when nothing is cached and no usable token can fill
// the cache.
func (e *Engine) List(ctx context.Context, q Query) (Page, error) {
	items, total, err := e.store.ListAppleLibrary(ctx, q)
	if err != nil {
		return Page{}, err
	}
	state := e.kindState(ctx, q.Kind)
	if state.FullAt == nil && total == 0 && q.Q == "" {
		if st := e.Status(ctx); !st.Available {
			if counts := st.Counts; counts["albums"]+counts["songs"]+counts["playlists"]+counts["artists"] == 0 {
				return Page{}, UnavailableError{Reason: st.Reason}
			}
		}
	}
	return Page{Kind: q.Kind, Items: items, Total: total, Offset: q.Offset, SyncedAt: state.FullAt}, nil
}

// PlaylistTracks returns a cached playlist's tracks in order.
func (e *Engine) PlaylistTracks(ctx context.Context, playlistID string) (Tracks, error) {
	tracks, readAt, found, err := e.store.AppleLibraryPlaylistTracks(ctx, playlistID)
	if err != nil {
		return Tracks{}, err
	}
	if !found {
		return Tracks{}, ErrPlaylistNotFound
	}
	out := Tracks{PlaylistID: playlistID, Items: tracks, Total: len(tracks)}
	if readAt != "" {
		if t, err := time.Parse("2006-01-02T15:04:05.000000000Z07:00", readAt); err == nil {
			out.ReadAt = &t
		}
	}
	return out, nil
}

// Membership reports, for catalog ids of one search group (songs, albums,
// playlists, artists), which are in the cached library. It returns nil when
// that kind has not completed a full read, so the caller leaves the mark out
// rather than claiming "not in the library"; a kind whose full read hit its
// page cap answers only for the ids it has cached.
func (e *Engine) Membership(ctx context.Context, kind string, catalogIDs []string) map[string]bool {
	state := e.kindState(ctx, kind)
	if len(catalogIDs) == 0 || state.FullAt == nil {
		return nil
	}
	in, err := e.store.AppleLibraryMembership(ctx, kind, catalogIDs)
	if err != nil {
		e.logger.Warn("apple library membership lookup failed", "kind", kind, "error", err)
		return nil
	}
	out := make(map[string]bool, len(catalogIDs))
	for _, id := range catalogIDs {
		// A capped kind is cached only in part, so an id that is not cached
		// may still be in the library: only the found ones are answered.
		if in[id] || !state.Capped {
			out[id] = in[id]
		}
	}
	return out
}

func (e *Engine) kindState(ctx context.Context, kind string) kindState {
	var st kindState
	if raw, err := e.store.AppleLibraryMeta(ctx, "kind:"+kind); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &st) // a damaged value reads as never synced
	}
	return st
}

func (e *Engine) saveKindState(ctx context.Context, kind string, st kindState) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return e.store.SetAppleLibraryMeta(ctx, "kind:"+kind, string(raw))
}

func newRunID() string {
	var b [6]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read does not fail on supported platforms
	return "als_" + hex.EncodeToString(b[:])
}
