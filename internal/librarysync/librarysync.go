// Package librarysync watches the Apple Music library of each account whose
// library_sync switch is on (internal/accounts) and adds the albums of newly
// added songs as album records, which download like records added by hand.
// library_sync.enabled is the master switch and library_sync.interval_minutes
// the schedule of every account; each account has its own anchor, token and
// status, and its records are pinned to the storefront of the catalog link
// its library gave.
//
// # Why this cannot be done with timestamps
//
// Apple exposes no per-song add time. library-songs has no dateAdded attribute
// (neither extend= nor fields[] produces one), and library-albums.dateAdded is
// the moment that album's *first* song entered, so it never moves when a later
// song joins the same album — an album added in 2024 that gained a track today
// still reports 2024. sort=-dateModified is rejected outright.
//
// What is dependable is the ORDER returned by sort=-dateAdded. So the watermark
// is positional, not temporal: remember the newest anchorSize song ids, and on
// each pass read newest-first until the oldest remembered id reappears.
// Anything above it that is not remembered was added since. This needs no
// clock, survives clock skew, and is exact rather than approximate.
package librarysync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/jobs"
	"amdl/internal/usertoken"
)

// Accounts lists the accounts (internal/accounts.Service.List).
type Accounts interface {
	List(ctx context.Context) ([]domain.Account, error)
}

const (
	// anchorSize is how many song ids are remembered. It only has to exceed
	// what can be added between two passes; at the 15-minute default that is a
	// wide margin, and it keeps a single request enough for a quiet pass.
	anchorSize = 50
	// margin is read beyond the anchor so ordinary additions still resolve in
	// one request: anchorSize+margin is within Apple's 100-per-page ceiling.
	margin = 25
	// scanCap bounds a pass that never finds its anchor, so a library whose
	// remembered songs were all deleted cannot pull the whole collection.
	scanCap = 2000
)

// Submitter is the record service's album add (jobs.Manager.AddAlbums). The
// monitor adds unresolved album records with source library_sync and the
// auto priority class, pinned to storefront, makes no catalog read for them,
// and gets back what became of each id; an album removed from amdl comes
// back tombstoned and is not added again.
type Submitter interface {
	AddAlbums(ctx context.Context, source domain.RecordSource, storefront string, albumIDs []string) (map[string]jobs.AddOutcome, error)
}

// AnchorStore persists each account's positional watermark across restarts.
type AnchorStore interface {
	LoadLibrarySyncAnchor(ctx context.Context, accountID string) ([]domain.LibraryAnchorEntry, error)
	ReplaceLibrarySyncAnchor(ctx context.Context, accountID string, entries []domain.LibraryAnchorEntry) error
	ClearLibrarySyncAnchor(ctx context.Context, accountID string) (int64, error)
}

// Library is the slice of the Apple Music client this package needs.
type Library interface {
	LibrarySongsNewestFirst(ctx context.Context, mediaUserToken string, offset, limit int) ([]applemusic.LibrarySong, bool, error)
	LibraryAlbumCatalogURL(ctx context.Context, mediaUserToken, libraryAlbumID string) (string, bool, error)
}

// Status is the watcher's view of itself, for GET /api/v1/library-sync:
// the master switch and schedule, and one entry per account.
type Status struct {
	Enabled         bool            `json:"enabled"`
	IntervalMinutes int             `json:"interval_minutes"`
	Accounts        []AccountStatus `json:"accounts"`
}

// AccountStatus is one account's library sync. Enabled is the account's
// library_sync switch; it polls while that and the master switch are on and
// the account is active.
type AccountStatus struct {
	AccountID      string     `json:"account_id"`
	Name           string     `json:"name"`
	Region         string     `json:"region"`
	Enabled        bool       `json:"enabled"`
	AnchorSize     int        `json:"anchor_size"`
	LastPollAt     *time.Time `json:"last_poll_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	LastSubmitted  int        `json:"last_submitted"`
	TotalSubmitted int        `json:"total_submitted"`
}

// Watcher polls the libraries on a schedule taken from the live config and
// the accounts table, so enabling, disabling, and re-timing all take effect
// without a restart.
type Watcher struct {
	cfg      *config.Store
	tokens   *usertoken.Provider
	accounts Accounts
	library  Library
	store    AnchorStore
	jobs     Submitter
	logger   *slog.Logger

	mu     sync.Mutex
	states map[string]*accountState
}

// accountState is one account's watcher state.
type accountState struct {
	id     string
	logger *slog.Logger

	// anchorMu makes ResetAnchor's clear and a pass's "was the anchor reset
	// since I loaded it?" check plus save one step each, so a pass that was in
	// flight during a reset cannot write its anchor back over the reset.
	anchorMu sync.Mutex
	// generation counts resets; guarded by anchorMu.
	generation uint64

	// lastRun is when the poll loop last ran a pass; only the loop uses it.
	lastRun time.Time

	mu             sync.Mutex
	lastPollAt     *time.Time
	lastError      string
	lastSubmitted  int
	totalSubmitted int
	anchorSize     int
	// idleReason is remembered so an account that cannot run (no token) says
	// so once per distinct reason rather than every tick.
	idleReason string
	// clientErrorPasses counts, per library album id, the consecutive passes
	// whose catalog lookup failed with a non-retryable client error; see
	// giveUpOnAlbum.
	clientErrorPasses map[string]int
}

// IdleNoToken is the watcher's last_error while amdl has no media-user-token
// for the account.
const IdleNoToken = "no media-user-token: amdl cannot read one from the wrapper; the library cannot be read without one"

// pollTick is how often the poll loop checks whether a pass is due.
const pollTick = 15 * time.Second

// New builds a watcher over the live config. tokens picks each account's
// media-user-token; nil has none. accounts lists
// the accounts; nil watches the token provider's default account alone,
// with its switch on. It does no I/O; Run runs it.
func New(cfg *config.Store, tokens *usertoken.Provider, accounts Accounts, library Library, store AnchorStore, jobs Submitter, logger *slog.Logger) *Watcher {
	return &Watcher{cfg: cfg, tokens: tokens, accounts: accounts, library: library, store: store, jobs: jobs, logger: logger,
		states: map[string]*accountState{}}
}

// listAccounts lists the accounts, or the default one alone without
// Accounts.
func (w *Watcher) listAccounts(ctx context.Context) ([]domain.Account, error) {
	if w.accounts == nil {
		return []domain.Account{{ID: w.tokens.DefaultAccount(), LibrarySync: true, State: domain.AccountActive}}, nil
	}
	return w.accounts.List(ctx)
}

// state returns an account's state, made on first use.
func (w *Watcher) state(id string) *accountState {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.states[id]
	if st == nil {
		st = &accountState{id: id, logger: w.logger.With("account", id)}
		w.states[id] = st
	}
	return st
}

// Run polls until ctx is cancelled and returns once the loop, including a pass
// in flight, has stopped. The caller runs it on its own goroutine and waits for
// it before closing the store.
//
// The loop always ticks at a fixed short cadence and decides per tick whether a
// pass is due. Sleeping for the configured interval instead would mean a change
// from 24 hours to 5 minutes only took effect a day later.
func (w *Watcher) Run(ctx context.Context) {
	w.run(ctx, pollTick)
}

func (w *Watcher) run(ctx context.Context, tick time.Duration) {
	timer := time.NewTicker(tick)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		w.tick(ctx)
	}
}

// tick runs the passes that are due, one account after another.
func (w *Watcher) tick(ctx context.Context) {
	cfg := w.cfg.Get()
	accounts, err := w.listAccounts(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Warn("library sync could not list the accounts", "error", err)
		}
		return
	}
	for _, a := range accounts {
		if ctx.Err() != nil {
			return
		}
		st := w.state(a.ID)
		if !cfg.LibrarySync.Enabled || !a.LibrarySync || a.State != domain.AccountActive {
			st.lastRun = time.Time{} // re-enabling polls promptly, not a full interval later
			st.noteIdle("")
			continue
		}
		if !st.lastRun.IsZero() && time.Since(st.lastRun) < cfg.LibrarySync.Interval() {
			continue
		}
		if w.tokens.Account(cfg, a.ID).Value == "" {
			// Look again next tick, not an interval later: a wrapper login
			// supplies the token at any moment.
			st.noteIdle(IdleNoToken)
			continue
		}
		st.lastRun = time.Now()
		w.runOnce(ctx, st, a.Region)
	}
}

// RunOnce performs a single pass for an account. Exported so tests can
// drive it directly.
func (w *Watcher) RunOnce(ctx context.Context, accountID string) {
	region := ""
	if accounts, err := w.listAccounts(ctx); err == nil {
		for _, a := range accounts {
			if a.ID == accountID {
				region = a.Region
			}
		}
	}
	w.runOnce(ctx, w.state(accountID), region)
}

func (w *Watcher) runOnce(ctx context.Context, st *accountState, region string) {
	token := w.tokens.Account(w.cfg.Get(), st.id).Value
	if token == "" {
		// Not an error worth repeating every tick: nothing is broken, the
		// watcher simply has no credential that can read a personal library.
		st.noteIdle(IdleNoToken)
		return
	}
	st.noteIdle("")
	submitted, err := w.pass(ctx, st, token, region)
	now := time.Now()
	st.mu.Lock()
	st.lastPollAt = &now
	st.lastSubmitted = submitted
	st.totalSubmitted += submitted
	if err != nil {
		st.lastError = err.Error()
	} else {
		st.lastError = ""
	}
	st.mu.Unlock()
	if applemusic.IsUserTokenRejected(err) {
		w.tokens.NoteRejected(token)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		st.logger.Warn("library sync pass failed", "error", err)
	}
}

func (w *Watcher) pass(ctx context.Context, st *accountState, token, region string) (int, error) {
	generation, anchor, err := w.loadAnchor(ctx, st)
	if err != nil {
		return 0, fmt.Errorf("load anchor: %w", err)
	}

	// No anchor: this is a first run or a reset. Record where the library
	// stands and submit nothing — everything present at that moment counts as
	// already had, and back-filling an entire library was never the intent.
	if len(anchor) == 0 {
		songs, _, err := w.library.LibrarySongsNewestFirst(ctx, token, 0, anchorSize)
		if err != nil {
			return 0, fmt.Errorf("read library: %w", err)
		}
		if err := w.saveAnchor(ctx, st, generation, songs); err != nil {
			return 0, err
		}
		st.logger.Info("library sync anchored", "songs", len(songs),
			"newest", firstName(songs), "submitted", 0)
		return 0, nil
	}

	oldest := anchor[len(anchor)-1].SongID
	scanned, foundAnchor, err := w.scanUntil(ctx, token, oldest, len(anchor)+margin)
	if err != nil {
		return 0, fmt.Errorf("read library: %w", err)
	}
	if !foundAnchor {
		// The oldest remembered song is gone from the library, so there is no
		// boundary to measure against. Treating the whole scan as new would
		// queue hundreds of albums, so re-anchor quietly instead.
		st.logger.Warn("library sync anchor not found; re-anchoring without submitting",
			"scanned", len(scanned), "oldest_anchor_song", oldest)
		return 0, w.saveAnchor(ctx, st, generation, head(scanned, anchorSize))
	}

	// Only songs ahead of the oldest remembered one can be new. The margin read
	// beyond it is older than the anchor by construction; those songs are
	// absent from the anchor merely because it holds a fixed number of entries.
	cut := len(scanned)
	for i, song := range scanned {
		if song.ID == oldest {
			cut = i
			break
		}
	}
	known := make(map[string]bool, len(anchor))
	for _, e := range anchor {
		known[e.SongID] = true
	}
	var added []applemusic.LibrarySong
	for _, song := range scanned[:cut] {
		if !known[song.ID] {
			added = append(added, song)
		}
	}

	submitted := 0
	var pending []string
	if len(added) > 0 {
		submitted, pending = w.submitAlbums(ctx, st, token, region, added)
	}
	if len(pending) > 0 {
		// Keeping the previous anchor leaves every song above it "new" for the
		// next pass, which retries the albums that failed here. Albums this pass
		// did submit come round again too: an active job answers
		// duplicate_active, and a finished one skips the files already on disk.
		st.anchorMu.Lock()
		reset := st.generation != generation
		if !reset {
			st.setAnchorSize(len(anchor))
		}
		st.anchorMu.Unlock()
		if reset {
			// A reset forgets history, unsubmitted albums included: the next
			// pass re-anchors and submits nothing, so there is nothing to retry.
			return submitted, nil
		}
		return submitted, fmt.Errorf("%d new album(s) not submitted, retrying next pass: %s",
			len(pending), strings.Join(head(pending, maxReportedPending), "; "))
	}
	if err := w.saveAnchor(ctx, st, generation, head(scanned, anchorSize)); err != nil {
		return submitted, err
	}
	return submitted, nil
}

// maxReportedPending bounds how many unsubmitted albums the pass error names.
const maxReportedPending = 3

// loadAnchor reads the stored anchor together with the reset generation it
// belongs to.
func (w *Watcher) loadAnchor(ctx context.Context, st *accountState) (uint64, []domain.LibraryAnchorEntry, error) {
	st.anchorMu.Lock()
	defer st.anchorMu.Unlock()
	anchor, err := w.store.LoadLibrarySyncAnchor(ctx, st.id)
	return st.generation, anchor, err
}

// saveAnchor stores songs as the new anchor unless ResetAnchor ran since the
// pass loaded generation's anchor: the reset wins, and the next pass
// re-anchors from scratch.
func (w *Watcher) saveAnchor(ctx context.Context, st *accountState, generation uint64, songs []applemusic.LibrarySong) error {
	st.anchorMu.Lock()
	defer st.anchorMu.Unlock()
	if st.generation != generation {
		st.logger.Info("library sync anchor was reset during the pass; not saving it")
		return nil
	}
	if err := w.store.ReplaceLibrarySyncAnchor(ctx, st.id, toEntries(songs)); err != nil {
		return fmt.Errorf("save anchor: %w", err)
	}
	st.setAnchorSize(len(songs))
	st.clearClientErrors()
	return nil
}

// scanUntil reads newest-first until untilID reappears, the library ends, or
// scanCap is reached.
func (w *Watcher) scanUntil(ctx context.Context, token, untilID string, want int) ([]applemusic.LibrarySong, bool, error) {
	var out []applemusic.LibrarySong
	for {
		limit := want - len(out)
		if limit <= 0 {
			limit = 100
		}
		page, hasMore, err := w.library.LibrarySongsNewestFirst(ctx, token, len(out), limit)
		if err != nil {
			return out, false, err
		}
		out = append(out, page...)
		for _, song := range page {
			if song.ID == untilID {
				return out, true, nil
			}
		}
		if len(page) == 0 || !hasMore || len(out) >= scanCap {
			return out, false, nil
		}
	}
}

// ResetAnchor forgets an account's watermark and reports how many entries
// went.
//
// This forgets history rather than replaying it: the next pass finds no anchor,
// re-anchors to the library as it stands then, and submits nothing. Use it when
// the watcher's idea of "already had" has drifted from reality — not to
// re-download something, which is what submitting the album directly is for.
//
// A pass already in flight still submits what it found, but does not save its
// anchor afterwards, so the reset holds.
func (w *Watcher) ResetAnchor(ctx context.Context, accountID string) (int64, error) {
	st := w.state(accountID)
	st.anchorMu.Lock()
	defer st.anchorMu.Unlock()
	cleared, err := w.store.ClearLibrarySyncAnchor(ctx, accountID)
	if err != nil {
		return 0, err
	}
	st.generation++
	st.setAnchorSize(0)
	st.logger.Info("library sync anchor reset", "cleared", cleared)
	return cleared, nil
}

// Forget drops a deleted account's in-memory state; its anchor rows go with
// the account (db.Store.DeleteAccount).
func (w *Watcher) Forget(accountID string) {
	w.mu.Lock()
	delete(w.states, accountID)
	w.mu.Unlock()
}

// Status reports the watcher's current state, merging live config and the
// accounts with what each account's last pass observed.
func (w *Watcher) Status(ctx context.Context) (Status, error) {
	cfg := w.cfg.Get().LibrarySync
	out := Status{Enabled: cfg.Enabled, IntervalMinutes: cfg.IntervalMinutes, Accounts: []AccountStatus{}}
	accounts, err := w.listAccounts(ctx)
	if err != nil {
		return out, err
	}
	for _, a := range accounts {
		st := w.state(a.ID)
		st.mu.Lock()
		out.Accounts = append(out.Accounts, AccountStatus{
			AccountID: a.ID, Name: a.Name, Region: a.Region, Enabled: a.LibrarySync,
			AnchorSize:     st.anchorSize,
			LastPollAt:     st.lastPollAt,
			LastError:      st.lastError,
			LastSubmitted:  st.lastSubmitted,
			TotalSubmitted: st.totalSubmitted,
		})
		st.mu.Unlock()
	}
	return out, nil
}

func (st *accountState) setAnchorSize(n int) {
	st.mu.Lock()
	st.anchorSize = n
	st.mu.Unlock()
}

// noteIdle logs a reason for not polling only when it changes, so an account
// left enabled without a token does not fill the log.
func (st *accountState) noteIdle(reason string) {
	st.mu.Lock()
	changed := st.idleReason != reason
	st.idleReason = reason
	if reason != "" {
		st.lastError = reason
	}
	st.mu.Unlock()
	if changed && reason != "" {
		st.logger.Warn("library sync idle", "reason", reason)
	}
}

func toEntries(songs []applemusic.LibrarySong) []domain.LibraryAnchorEntry {
	out := make([]domain.LibraryAnchorEntry, 0, len(songs))
	for _, song := range songs {
		out = append(out, domain.LibraryAnchorEntry{
			SongID: song.ID, Name: song.Name, AlbumName: song.AlbumName,
		})
	}
	return out
}

func head[T any](items []T, n int) []T {
	if len(items) <= n {
		return items
	}
	return items[:n]
}

func firstName(songs []applemusic.LibrarySong) string {
	if len(songs) == 0 {
		return ""
	}
	return songs[0].Name
}
