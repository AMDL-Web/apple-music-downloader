package applelibrary

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/usertoken"
)

// Accounts lists the accounts (internal/accounts.Service.List).
type Accounts interface {
	List(ctx context.Context) ([]domain.Account, error)
}

// ErrAccountNotFound is a request for an account the accounts table does
// not have.
var ErrAccountNotFound = errors.New("no such account")

// SetStatus is the AppleLibraryStatus schema: the schedule every account
// shares and one entry per account.
type SetStatus struct {
	SyncIntervalMinutes int             `json:"sync_interval_minutes"`
	FullSyncHours       int             `json:"full_sync_hours"`
	Accounts            []AccountStatus `json:"accounts"`
}

// AccountStatus is one account's entry (the AppleLibraryAccountStatus
// schema): the account, whether it is the default, its apple_library switch
// and its Engine's Status.
type AccountStatus struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
	Region    string `json:"region"`
	Default   bool   `json:"default"`
	Enabled   bool   `json:"enabled"`
	Status
}

// accountsRefresh is how often Run looks for accounts without an engine.
const accountsRefresh = 15 * time.Second

// Set runs one Engine per account. Requests name an account; an empty one is
// the default account (position 0).
type Set struct {
	cfg      *config.Store
	tokens   *usertoken.Provider
	apple    Apple
	store    *db.Store
	logger   *slog.Logger
	accounts Accounts

	mu      sync.Mutex
	run     context.Context
	engines map[string]*setEngine
	known   map[string]domain.Account
	loops   sync.WaitGroup
}

type setEngine struct {
	*Engine
	stop context.CancelFunc
}

// NewSet builds the set. It does no I/O; Run runs the engines' schedules.
func NewSet(cfg *config.Store, tokens *usertoken.Provider, apple Apple, store *db.Store, accounts Accounts, logger *slog.Logger) *Set {
	return &Set{cfg: cfg, tokens: tokens, apple: apple, store: store, logger: logger, accounts: accounts,
		engines: map[string]*setEngine{}, known: map[string]domain.Account{}}
}

// Run starts an engine for every account as it appears and returns once all
// of them, and every pass, have stopped.
func (s *Set) Run(ctx context.Context) {
	s.mu.Lock()
	s.run = ctx
	for _, e := range s.engines {
		s.startLocked(e)
	}
	s.mu.Unlock()
	ticker := time.NewTicker(accountsRefresh)
	defer ticker.Stop()
	for {
		s.Refresh(ctx)
		select {
		case <-ctx.Done():
			s.loops.Wait()
			return
		case <-ticker.C:
		}
	}
}

// Refresh reads the accounts and makes an engine for each new one; call it
// after a change to the accounts table.
func (s *Set) Refresh(ctx context.Context) {
	accounts, err := s.accounts.List(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Warn("apple library could not list the accounts", "error", err)
		}
		return
	}
	if len(accounts) == 0 {
		// Before the first discovery the default (implicit) account stands
		// in, with its library on, as the single account had it.
		accounts = []domain.Account{{ID: s.tokens.DefaultAccount(), AppleLibrary: true, State: domain.AccountActive}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	for _, a := range accounts {
		seen[a.ID] = true
		s.known[a.ID] = a
		if s.engines[a.ID] == nil {
			s.engines[a.ID] = &setEngine{Engine: s.newEngineLocked(a.ID)}
			s.startLocked(s.engines[a.ID])
		}
	}
	for id, e := range s.engines {
		if !seen[id] {
			if e.stop != nil {
				e.stop()
			}
			delete(s.engines, id)
			delete(s.known, id)
		}
	}
}

func (s *Set) newEngineLocked(id string) *Engine {
	enabled := func() bool {
		s.mu.Lock()
		a, ok := s.known[id]
		s.mu.Unlock()
		return ok && a.AppleLibrary && a.State == domain.AccountActive
	}
	return newAccountEngine(s.cfg, s.tokens, s.apple, s.store, s.logger.With("account", id), id, enabled)
}

func (s *Set) startLocked(e *setEngine) {
	if s.run == nil || e.stop != nil {
		return
	}
	ctx, stop := context.WithCancel(s.run)
	e.stop = stop
	s.loops.Go(func() { e.Run(ctx) })
}

// engine returns the engine of account ("" is the default account),
// reading the accounts once when it is not known yet.
func (s *Set) engine(ctx context.Context, account string) (*Engine, error) {
	if account == "" {
		account = s.tokens.DefaultAccount()
	}
	s.mu.Lock()
	e := s.engines[account]
	s.mu.Unlock()
	if e == nil {
		s.Refresh(ctx)
		s.mu.Lock()
		e = s.engines[account]
		s.mu.Unlock()
	}
	if e == nil {
		return nil, ErrAccountNotFound
	}
	return e.Engine, nil
}

// List returns a page of one kind of an account's cache.
func (s *Set) List(ctx context.Context, account string, q Query) (Page, error) {
	e, err := s.engine(ctx, account)
	if err != nil {
		return Page{}, err
	}
	return e.List(ctx, q)
}

// PlaylistTracks returns the tracks of a playlist in an account's cache.
func (s *Set) PlaylistTracks(ctx context.Context, account, playlistID string) (Tracks, error) {
	e, err := s.engine(ctx, account)
	if err != nil {
		return Tracks{}, err
	}
	return e.PlaylistTracks(ctx, playlistID)
}

// Trigger starts a pass of an account in the background (Engine.Trigger).
func (s *Set) Trigger(ctx context.Context, account, mode string) (AccountStatus, error) {
	e, err := s.engine(ctx, account)
	if err != nil {
		return AccountStatus{}, err
	}
	st, err := e.Trigger(ctx, mode)
	return s.accountStatus(e, st), err
}

// Running reports the id of an account's pass under way, "" when idle.
func (s *Set) Running(ctx context.Context, account string) string {
	e, err := s.engine(ctx, account)
	if err != nil {
		return ""
	}
	return e.Running()
}

// Status reports every account's sync.
func (s *Set) Status(ctx context.Context) SetStatus {
	s.Refresh(ctx)
	cfg := s.cfg.Get().AppleLibrary
	out := SetStatus{SyncIntervalMinutes: cfg.SyncIntervalMinutes, FullSyncHours: cfg.FullSyncHours, Accounts: []AccountStatus{}}
	s.mu.Lock()
	ids := make([]domain.Account, 0, len(s.known))
	for _, a := range s.known {
		ids = append(ids, a)
	}
	s.mu.Unlock()
	sortByPosition(ids)
	for _, a := range ids {
		e, err := s.engine(ctx, a.ID)
		if err != nil {
			continue
		}
		out.Accounts = append(out.Accounts, s.accountStatus(e, e.Status(ctx)))
	}
	return out
}

func sortByPosition(accounts []domain.Account) {
	for i := 1; i < len(accounts); i++ {
		for j := i; j > 0 && (accounts[j].Position < accounts[j-1].Position ||
			accounts[j].Position == accounts[j-1].Position && accounts[j].ID < accounts[j-1].ID); j-- {
			accounts[j], accounts[j-1] = accounts[j-1], accounts[j]
		}
	}
}

// SyncNow runs a pass of an account to the end on the caller's goroutine
// (Engine.SyncNow).
func (s *Set) SyncNow(ctx context.Context, account, mode string) (Run, error) {
	e, err := s.engine(ctx, account)
	if err != nil {
		return Run{}, err
	}
	return e.SyncNow(ctx, mode)
}

func (s *Set) accountStatus(e *Engine, st Status) AccountStatus {
	s.mu.Lock()
	a := s.known[e.account]
	s.mu.Unlock()
	return AccountStatus{AccountID: e.account, Name: a.Name, Region: a.Region, Default: a.Position == 0,
		Enabled: a.AppleLibrary, Status: st}
}

// Membership answers for the account that serves storefront
// (usertoken.Provider.AccountFor): which catalog ids of a search group are in
// its cached library; nil when that cache cannot tell.
func (s *Set) Membership(ctx context.Context, storefront, kind string, catalogIDs []string) map[string]bool {
	e, err := s.engine(ctx, s.tokens.AccountFor(storefront))
	if err != nil || !e.isEnabled() {
		return nil
	}
	return e.Membership(ctx, kind, catalogIDs)
}
