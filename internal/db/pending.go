package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"amdl/internal/domain"
)

// ErrPendingNotFound is returned when no pending add has the id.
var ErrPendingNotFound = errors.New("pending add not found")

const pendingColumns = `id, kind, catalog_id, album_hint, source, priority, paused, include, overrides, added_at, attempts, last_error, state`

func scanPending(row jobScanner) (domain.PendingAdd, error) {
	var (
		p                                     domain.PendingAdd
		kind, source, overrides, added, state string
	)
	if err := row.Scan(&p.ID, &kind, &p.CatalogID, &p.AlbumHint, &source, &p.Priority, &p.Paused, &p.Include, &overrides,
		&added, &p.Attempts, &p.LastError, &state); err != nil {
		return domain.PendingAdd{}, err
	}
	var err error
	if p.Overrides, err = decodeOverrides(overrides); err != nil {
		return domain.PendingAdd{}, fmt.Errorf("pending add %d: %w", p.ID, err)
	}
	p.Kind, p.Source, p.State = domain.PendingAddKind(kind), domain.RecordSource(source), domain.PendingAddState(state)
	p.AddedAt = parseTime(added)
	return p, nil
}

// AddPending stores an add that needs a catalog read, one row per (kind,
// catalog id). Adding an existing one re-arms it (pending, no attempts),
// keeps the higher priority class, unpauses it unless the new add is paused,
// and takes the new add's overrides and album hint when it has them. It
// returns the stored row.
func (s *Store) AddPending(ctx context.Context, p domain.PendingAdd) (domain.PendingAdd, error) {
	if p.Kind == "" || p.CatalogID == "" || p.Source == "" {
		return domain.PendingAdd{}, fmt.Errorf("add pending: kind, catalog id and source are required")
	}
	if p.Priority == 0 {
		p.Priority = domain.PriorityManual
	}
	overrides, err := encodeOverrides(p.Overrides)
	if err != nil {
		return domain.PendingAdd{}, err
	}
	return withTx(ctx, s.db, func(tx *sql.Tx) (domain.PendingAdd, error) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pending_adds(kind, catalog_id, album_hint, source, priority, paused, include, overrides, added_at, state)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')
			ON CONFLICT(kind, catalog_id) DO UPDATE SET
				album_hint = CASE WHEN excluded.album_hint <> '' THEN excluded.album_hint ELSE album_hint END,
				priority = MIN(priority, excluded.priority),
				paused = paused AND excluded.paused,
				include = CASE WHEN excluded.include <> '' THEN excluded.include ELSE include END,
				overrides = CASE WHEN excluded.overrides <> '' THEN excluded.overrides ELSE overrides END,
				attempts = 0, last_error = '', state = 'pending'`,
			string(p.Kind), p.CatalogID, p.AlbumHint, string(p.Source), p.Priority, p.Paused, p.Include, overrides, formatTime(p.AddedAt)); err != nil {
			return domain.PendingAdd{}, fmt.Errorf("add pending %s %s: %w", p.Kind, p.CatalogID, err)
		}
		return scanPending(tx.QueryRowContext(ctx, `SELECT `+pendingColumns+` FROM pending_adds WHERE kind = ? AND catalog_id = ?`, string(p.Kind), p.CatalogID))
	})
}

// ListPending returns the pending adds in state, oldest first; an empty state
// lists every row.
func (s *Store) ListPending(ctx context.Context, state domain.PendingAddState) ([]domain.PendingAdd, error) {
	query, args := `SELECT `+pendingColumns+` FROM pending_adds`, []any{}
	if state != "" {
		query += ` WHERE state = ?`
		args = append(args, string(state))
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY priority, added_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PendingAdd{}
	for rows.Next() {
		p, err := scanPending(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePending removes a pending add: the resolver did it, or the user
// dropped it.
func (s *Store) DeletePending(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM pending_adds WHERE id = ?`, id)
	return requirePendingRow(res, err)
}

// RecordPendingFailure counts a failed resolve of a pending add and, when
// giveUp is set, moves it to failed.
func (s *Store) RecordPendingFailure(ctx context.Context, id int64, errText string, giveUp bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE pending_adds SET attempts = attempts + 1, last_error = ?,
		state = CASE WHEN ? THEN 'failed' ELSE state END WHERE id = ?`, errText, giveUp, id)
	return requirePendingRow(res, err)
}

// RetryPending re-arms a failed pending add.
func (s *Store) RetryPending(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE pending_adds SET attempts = 0, last_error = '', state = 'pending' WHERE id = ?`, id)
	return requirePendingRow(res, err)
}

func requirePendingRow(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrPendingNotFound
	}
	return nil
}
