package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
)

// Catalog is the catalog reads the resolver makes to turn a pending add into
// records. Every read is made in the account storefront (design §3.2): a song
// or an artist that storefront does not have is a non-retryable error
// (IsNonRetryable), and the add fails at once.
type Catalog interface {
	// AccountStorefront is the storefront of the account amdl downloads
	// with: the media-user-token's, else the wrapper's region, else
	// catalog.default_storefront.
	AccountStorefront(ctx context.Context) (string, error)
	// SongAlbumID is the catalog id of the album a song is placed in.
	SongAlbumID(ctx context.Context, storefront, songID string) (string, error)
	// ArtistAlbumIDs is the catalog ids of an artist's albums; include is
	// "all" (every album the artist read returns) or "albums" (no singles).
	ArtistAlbumIDs(ctx context.Context, storefront, artistID, include string) ([]string, error)
}

// SetCatalog wires the reads the resolver needs. Without it, adds that need a
// catalog read (songs without their album, artists) stay pending.
func (m *Manager) SetCatalog(c Catalog) {
	m.catalogMu.Lock()
	m.catalog = c
	m.catalogMu.Unlock()
	m.resolveWake.broadcast()
}

func (m *Manager) catalogReads() Catalog {
	m.catalogMu.Lock()
	defer m.catalogMu.Unlock()
	return m.catalog
}

// resolveInterval is how often the resolver looks at pending adds without
// being woken: a catalog wired late, or rows another process added.
const resolveInterval = 60 * time.Second

// resolveRetryMax bounds the wait between two tries of one pending add.
const resolveRetryMax = 30 * time.Second

// resolver turns pending adds into records on its own goroutine from Start
// until ctx ends, woken by adds and resolveInterval.
func (m *Manager) resolver(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		wake := m.resolveWake.wait()
		if ctx.Err() != nil {
			return
		}
		m.resolvePending(ctx)
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-ticker.C:
		}
	}
}

// resolvePending resolves the pending adds one at a time, oldest first within
// each priority class.
func (m *Manager) resolvePending(ctx context.Context) {
	rows, err := m.store.ListPending(ctx, domain.PendingAddPending)
	if err != nil {
		if ctx.Err() == nil {
			m.logger.Error("list pending adds", "error", err)
		}
		return
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		m.resolveOne(ctx, row)
	}
}

// errNoCatalog is why an add that needs a catalog read waits.
var errNoCatalog = errors.New("no catalog reader")

// resolveOne tries one pending add up to download.max_attempts times, a
// non-retryable error once (the required-read retry, design §3.1). Success
// turns it into records and deletes it; the last failure moves it to failed,
// where it waits for the user to retry or remove it.
func (m *Manager) resolveOne(ctx context.Context, row domain.PendingAdd) {
	logger := m.logger.With("pending_id", row.ID, "kind", row.Kind, "catalog_id", row.CatalogID)
	attempts := max(m.config().Download.MaxAttempts, 1)
	var backoff time.Duration
	for attempt := 1; ; attempt++ {
		err := m.resolveRow(ctx, row)
		if err == nil {
			if err := m.store.DeletePending(ctx, row.ID); err != nil && !errors.Is(err, db.ErrPendingNotFound) {
				logger.Error("delete a resolved pending add", "error", err)
			}
			m.pickWake.broadcast()
			return
		}
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errNoCatalog) {
			logger.Debug("pending add waits for a catalog reader")
			return
		}
		giveUp := IsNonRetryable(err) || attempt >= attempts
		if recordErr := m.store.RecordPendingFailure(ctx, row.ID, err.Error(), giveUp); recordErr != nil {
			logger.Error("record a failed resolve", "error", recordErr)
			return
		}
		if giveUp {
			logger.Warn("pending add could not be resolved", "attempts", attempt, "error", err)
			return
		}
		backoff = min(max(2*backoff, time.Second), resolveRetryMax)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// resolveRow makes the records of one pending add and queues their runs. An
// album or playlist row (the 1.x import's stopped jobs) needs no read.
func (m *Manager) resolveRow(ctx context.Context, row domain.PendingAdd) error {
	now := time.Now().UTC()
	trigger := triggerFor(row.Source)
	// A paused row (the 1.x import) makes paused records. A manual add of one
	// song or playlist resumes its record, so the add downloads; an artist's
	// albums keep a pause the user set on one of them.
	var paused *bool
	switch {
	case row.Paused:
		paused = &row.Paused
	case row.Source == domain.SourceManual && row.Kind != domain.PendingArtist:
		resume := false
		paused = &resume
	}
	album := func(id string, wantedAll bool, songIDs []string, source domain.RecordSource) error {
		_, _, err := m.addAlbum(ctx, db.AlbumAdd{
			ID: id, WantedAll: wantedAll, SongIDs: songIDs, Source: source, Priority: row.Priority,
			Overrides: row.Overrides, Paused: paused, Now: now,
		}, triggerFor(source))
		return err
	}
	switch row.Kind {
	case domain.PendingAlbum:
		return album(row.CatalogID, true, nil, row.Source)
	case domain.PendingPlaylist:
		m.mu.Lock()
		defer m.mu.Unlock()
		if _, err := m.store.AddPlaylist(ctx, db.PlaylistAdd{
			ID: row.CatalogID, Source: row.Source, Priority: row.Priority, Overrides: row.Overrides, Paused: paused, Now: now,
		}); err != nil {
			return err
		}
		_, _, err := m.afterAddLocked(ctx, domain.ResourcePlaylist, row.CatalogID, trigger, now)
		return err
	case domain.PendingSong:
		albumID := row.AlbumHint
		if albumID == "" {
			catalog := m.catalogReads()
			if catalog == nil {
				return errNoCatalog
			}
			storefront, err := catalog.AccountStorefront(ctx)
			if err != nil {
				return fmt.Errorf("read the account storefront: %w", err)
			}
			if albumID, err = catalog.SongAlbumID(ctx, storefront, row.CatalogID); err != nil {
				return fmt.Errorf("read song %s in storefront %s: %w", row.CatalogID, storefront, err)
			}
		}
		return album(albumID, false, []string{row.CatalogID}, row.Source)
	case domain.PendingArtist:
		catalog := m.catalogReads()
		if catalog == nil {
			return errNoCatalog
		}
		storefront, err := catalog.AccountStorefront(ctx)
		if err != nil {
			return fmt.Errorf("read the account storefront: %w", err)
		}
		include := row.Include
		if include == "" {
			include = "all"
		}
		albumIDs, err := catalog.ArtistAlbumIDs(ctx, storefront, row.CatalogID, include)
		if err != nil {
			return fmt.Errorf("read the albums of artist %s in storefront %s: %w", row.CatalogID, storefront, err)
		}
		source := row.Source
		if source == domain.SourceManual {
			source = domain.SourceArtist
		}
		for _, id := range albumIDs {
			if err := album(id, true, nil, source); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unknown pending add kind %q", row.Kind)
}

// AddPending stores an add that needs a catalog read and wakes the resolver.
func (m *Manager) AddPending(ctx context.Context, p domain.PendingAdd) (domain.PendingAdd, error) {
	if p.AddedAt.IsZero() {
		p.AddedAt = time.Now().UTC()
	}
	stored, err := m.store.AddPending(ctx, p)
	if err != nil {
		return stored, err
	}
	m.resolveWake.broadcast()
	return stored, nil
}

// RetryPending re-arms a failed pending add and wakes the resolver.
func (m *Manager) RetryPending(ctx context.Context, id int64) error {
	if err := m.store.RetryPending(ctx, id); err != nil {
		return err
	}
	m.resolveWake.broadcast()
	return nil
}
