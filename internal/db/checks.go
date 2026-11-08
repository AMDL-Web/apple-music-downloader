package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"amdl/internal/domain"
)

const checkColumns = `id, trigger, scope, started_at, finished_at, checked, missing, aborted_reason`

func scanCheckRun(row jobScanner) (domain.CheckRun, error) {
	var (
		c                domain.CheckRun
		trigger, started string
		finished         sql.NullString
	)
	if err := row.Scan(&c.ID, &trigger, &c.Scope, &started, &finished, &c.Checked, &c.Missing, &c.AbortedReason); err != nil {
		return domain.CheckRun{}, err
	}
	c.Trigger = domain.CheckTrigger(trigger)
	c.StartedAt, c.FinishedAt = parseTime(started), parseOptionalTime(finished)
	return c, nil
}

// StartCheckRun records a disk check that starts now. A check row left
// unfinished by a stopped server is closed as interrupted first; the caller
// runs one check at a time.
func (s *Store) StartCheckRun(ctx context.Context, trigger domain.CheckTrigger, scope string, now time.Time) (domain.CheckRun, error) {
	return withTx(ctx, s.db, func(tx *sql.Tx) (domain.CheckRun, error) {
		stamp := formatTime(now)
		if _, err := tx.ExecContext(ctx, `UPDATE check_runs SET finished_at = ?, aborted_reason = ? WHERE finished_at IS NULL`,
			stamp, domain.CheckAbortedInterrupted); err != nil {
			return domain.CheckRun{}, err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO check_runs(trigger, scope, started_at) VALUES(?, ?, ?)`, string(trigger), scope, stamp)
		if err != nil {
			return domain.CheckRun{}, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return domain.CheckRun{}, err
		}
		return scanCheckRun(tx.QueryRowContext(ctx, `SELECT `+checkColumns+` FROM check_runs WHERE id = ?`, id))
	})
}

// FinishCheckRun records a check's counts and end; aborted is empty for a
// check that marked what it found.
func (s *Store) FinishCheckRun(ctx context.Context, id int64, checked, missing int, aborted string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE check_runs SET finished_at = ?, checked = ?, missing = ?, aborted_reason = ? WHERE id = ?`,
		formatTime(now), checked, missing, aborted, id)
	return err
}

// LastCheckRun returns the newest disk check and whether there is one. A
// finished check of a record that has been removed since is skipped: what
// it found belongs to nothing in the library.
func (s *Store) LastCheckRun(ctx context.Context) (domain.CheckRun, bool, error) {
	c, err := scanCheckRun(s.db.QueryRowContext(ctx, `SELECT `+checkColumns+` FROM check_runs
		WHERE finished_at IS NULL OR scope = 'all'
			OR (scope LIKE 'album:%' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = substr(check_runs.scope, 7)))
			OR (scope LIKE 'playlist:%' AND EXISTS (SELECT 1 FROM playlists p WHERE p.id = substr(check_runs.scope, 10)))
		ORDER BY id DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CheckRun{}, false, nil
	}
	return c, err == nil, err
}

// LastCheckedAt returns when a finished, not aborted check last covered the
// record: a whole-library check or one of the record. Nil when none did.
func (s *Store) LastCheckedAt(ctx context.Context, kind domain.ResourceKind, id string) (*time.Time, error) {
	var finished sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT MAX(finished_at) FROM check_runs
		WHERE aborted_reason = '' AND finished_at IS NOT NULL AND scope IN ('all', ?)`, string(kind)+":"+id).Scan(&finished)
	if err != nil {
		return nil, err
	}
	return parseOptionalTime(finished), nil
}

// DoneFile is a done slot and its stored path, resolved against the
// downloads dir.
type DoneFile struct {
	Key  domain.FileKey
	Path string
}

// DoneFiles returns every done slot with a path, of one record when kind is
// set, else of the whole library.
func (s *Store) DoneFiles(ctx context.Context, kind domain.ResourceKind, id string) ([]DoneFile, error) {
	query := `SELECT owner_kind, owner_id, song_id, slot, path FROM files WHERE audio_state = 'done' AND path <> ''`
	var args []any
	if kind != "" {
		query += ` AND owner_kind = ? AND owner_id = ?`
		args = append(args, string(kind), id)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY owner_kind, owner_id, song_id, slot`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dir := s.DownloadsDir()
	var out []DoneFile
	for rows.Next() {
		var f DoneFile
		var ownerKind string
		if err := rows.Scan(&ownerKind, &f.Key.OwnerID, &f.Key.SongID, &f.Key.Slot, &f.Path); err != nil {
			return nil, err
		}
		f.Key.OwnerKind = domain.ResourceKind(ownerKind)
		f.Path = domain.ResolveStoredPath(dir, f.Path)
		out = append(out, f)
	}
	return out, rows.Err()
}

// ResumeOwners lists who may own resume checkpoints: every record as
// "<kind>:<id>", and each queued or running run without a record by its id.
func (s *Store) ResumeOwners(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT 'album:' || id FROM albums UNION ALL SELECT 'playlist:' || id FROM playlists
		UNION ALL SELECT id FROM runs WHERE status IN ('queued','running') AND resource_id IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return nil, err
		}
		out = append(out, owner)
	}
	return out, rows.Err()
}
