// Package accounts keeps the accounts table (internal/db) in step with the
// Apple Music accounts the wrapper holds, and hands the result to the token
// provider (internal/usertoken).
//
// Discovery reads the wrapper's /status at start, every discoveryInterval,
// a short while after a failed read, and when Nudge asks (a login, a logout,
// a deleted account). A wrapper-manager lists its accounts in /status
// instances, by instance id; a wrapper that does not (a bare wrapper-lite, an
// older wrapper-manager) holds one implicit account (domain.ImplicitAccountID)
// while it reports a region, and so does catalog.media_user_token alone. An
// account the wrapper stops reporting stays as missing, with its switches.
//
// The first discovery that finds an account in an empty table hands it the
// library-sync anchor and the Apple library cache of a database from before
// accounts (db.AccountDiscovery.Adopt): the account whose current token is the
// one amdl stored before (usertoken.Provider.MatchStored), else the first the
// wrapper lists. It becomes the default with library sync as
// library_sync.enabled had it and the Apple library on, so an upgrade keeps
// syncing; every later account starts with both switches off.
package accounts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/usertoken"
	"amdl/internal/wrapper"
)

// Discovery timing.
const (
	discoveryInterval = 10 * time.Minute
	failureRetry      = 30 * time.Second
	// discoveryTimeout bounds one discovery, the token reads of an adoption
	// included.
	discoveryTimeout = 2 * time.Minute
)

// Wrapper is the slice of the wrapper client discovery and deletion use.
type Wrapper interface {
	Status(ctx context.Context) (wrapper.Status, error)
	LogoutAccount(ctx context.Context, id string) error
}

// errNotReady is a discovery that found the wrapper not ready; it is tried
// again soon.
var errNotReady = errors.New("the wrapper is not ready; its accounts are read again shortly")

// ErrLogoutUnsupported is a delete of an active account the wrapper cannot
// log out: the implicit account of a wrapper without the login API.
var ErrLogoutUnsupported = errors.New("the wrapper cannot log this account out; log it out on the wrapper host, then delete it")

// Service is the accounts table and its discovery. It is safe for concurrent
// use.
type Service struct {
	store   *db.Store
	wrapper Wrapper
	tokens  *usertoken.Provider
	cfg     func() config.Config
	logger  *slog.Logger

	nudge chan struct{}
	// mu serializes discoveries and deletes, so a delete cannot race a
	// discovery that re-adds the account it is deleting.
	mu sync.Mutex
	// listeners hear about every change to the table.
	lmu       sync.Mutex
	listeners []func()
	// lastErr is the last discovery's failure, logged once per change.
	lastErr string
}

// New builds the service. It does no I/O; Run runs discovery.
func New(store *db.Store, w Wrapper, tokens *usertoken.Provider, cfg func() config.Config, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default().With("component", "accounts")
	}
	return &Service{store: store, wrapper: w, tokens: tokens, cfg: cfg, logger: logger, nudge: make(chan struct{}, 1)}
}

// OnChange registers f to run after every change to the accounts table.
func (s *Service) OnChange(f func()) {
	s.lmu.Lock()
	s.listeners = append(s.listeners, f)
	s.lmu.Unlock()
}

func (s *Service) changed() {
	s.lmu.Lock()
	listeners := append([]func(){}, s.listeners...)
	s.lmu.Unlock()
	for _, f := range listeners {
		f()
	}
}

// Load hands the stored accounts to the token provider, so tokens and
// storefronts are known before the first discovery answers.
func (s *Service) Load(ctx context.Context) error {
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	if len(accounts) > 0 {
		s.tokens.SetAccounts(providerAccounts(accounts))
	}
	return nil
}

// Run discovers until ctx ends, as the package comment says.
func (s *Service) Run(ctx context.Context) {
	if err := s.Load(ctx); err != nil && ctx.Err() == nil {
		s.logger.Warn("read the stored accounts", "error", err)
	}
	for {
		wait := discoveryInterval
		if err := s.Discover(ctx); err != nil {
			wait = failureRetry
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-s.nudge:
			timer.Stop()
		}
	}
}

// Nudge asks Run to discover now, without waiting for it.
func (s *Service) Nudge() {
	select {
	case s.nudge <- struct{}{}:
	default: // one is already pending
	}
}

// Discover reads the wrapper's accounts once and applies them. A wrapper that
// does not answer leaves every account as it is.
func (s *Service) Discover(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.discoverLocked(ctx)
	s.noteErr(err)
	return err
}

func (s *Service) discoverLocked(ctx context.Context) error {
	status, err := s.wrapper.Status(ctx)
	if err != nil {
		return fmt.Errorf("read the wrapper's accounts: %w", err)
	}
	if !status.Ready {
		// A wrapper still restoring its accounts (or a managed guest still
		// booting) does not list them all yet.
		return errNotReady
	}
	cfg := s.cfg()
	d := db.AccountDiscovery{Complete: true, AdoptLibrarySync: cfg.LibrarySync.Enabled}
	switch {
	case status.Listed:
		for _, in := range status.Instances {
			d.Seen = append(d.Seen, db.SeenAccount{ID: in.ID, Name: in.Name, Region: in.Region})
		}
	case len(status.Regions) > 0:
		d.Seen = []db.SeenAccount{{ID: domain.ImplicitAccountID, Name: status.Name, Region: status.Regions[0]}}
	case strings.TrimSpace(cfg.Catalog.MediaUserToken) != "":
		d.Seen = []db.SeenAccount{{ID: domain.ImplicitAccountID}}
	}
	before, err := s.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	if len(before) == 0 && len(d.Seen) > 1 {
		ids := make([]string, 0, len(d.Seen))
		for _, seen := range d.Seen {
			ids = append(ids, seen.ID)
		}
		d.Adopt = s.tokens.MatchStored(ctx, ids)
	}
	res, err := s.store.SyncAccounts(ctx, d)
	if err != nil {
		return fmt.Errorf("store the wrapper's accounts: %w", err)
	}
	after, err := s.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	s.tokens.SetAccounts(providerAccounts(after))
	if res.Adopted != "" {
		s.logger.Info("account took over the library state from before accounts", "account", res.Adopted)
	}
	for _, id := range res.Added {
		s.logger.Info("new account", "account", id)
	}
	if !sameAccounts(before, after) {
		s.changed()
	}
	return nil
}

func (s *Service) noteErr(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.lmu.Lock()
	repeat := msg == s.lastErr
	s.lastErr = msg
	s.lmu.Unlock()
	if msg != "" && !repeat {
		s.logger.Warn("account discovery failed; the stored accounts stay", "error", msg)
	}
}

// List returns every account, the default first.
func (s *Service) List(ctx context.Context) ([]domain.Account, error) {
	return s.store.ListAccounts(ctx)
}

// Update changes an account's switches or position (db.AccountPatch).
func (s *Service) Update(ctx context.Context, id string, p db.AccountPatch) (domain.Account, error) {
	s.mu.Lock()
	a, err := s.store.UpdateAccount(ctx, id, p)
	if err == nil {
		err = s.pushLocked(ctx)
	}
	s.mu.Unlock()
	if err != nil {
		return domain.Account{}, err
	}
	s.changed()
	return a, nil
}

// Delete logs an account out of the wrapper and forgets what amdl keeps for
// it: its row, library-sync anchor, Apple library cache and stored token.
// Downloaded files and records stay. A missing account is not logged out;
// an account the wrapper no longer knows is deleted all the same. The
// implicit account of a wrapper without the login API cannot be logged out
// while active (ErrLogoutUnsupported).
func (s *Service) Delete(ctx context.Context, id string) (loggedOut bool, err error) {
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		if err == nil {
			s.changed()
			s.Nudge()
		}
	}()
	a, err := s.store.GetAccount(ctx, id)
	if err != nil {
		return false, err
	}
	if a.State == domain.AccountActive {
		if id == domain.ImplicitAccountID {
			return false, ErrLogoutUnsupported
		}
		switch err := s.wrapper.LogoutAccount(ctx, id); {
		case err == nil:
			loggedOut = true
		case errors.Is(err, wrapper.ErrAccountNotFound):
		default:
			return false, err
		}
	}
	if err := s.tokens.Forget(id); err != nil {
		s.logger.Warn("could not remove the stored copy of a deleted account's token", "account", id, "error", err)
	}
	if err := s.store.DeleteAccount(ctx, id); err != nil {
		return loggedOut, err
	}
	s.logger.Info("account deleted", "account", id, "logged_out", loggedOut)
	return loggedOut, s.pushLocked(ctx)
}

// pushLocked hands the stored accounts to the token provider.
func (s *Service) pushLocked(ctx context.Context) error {
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	s.tokens.SetAccounts(providerAccounts(accounts))
	return nil
}

func providerAccounts(accounts []domain.Account) []usertoken.Account {
	out := make([]usertoken.Account, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, usertoken.Account{ID: a.ID, Region: a.Region, Position: a.Position, Active: a.State == domain.AccountActive})
	}
	return out
}

// sameAccounts reports whether two listings differ in anything but seen_at.
func sameAccounts(a, b []domain.Account) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		x.SeenAt, y.SeenAt = nil, nil
		if x != y {
			return false
		}
	}
	return true
}
