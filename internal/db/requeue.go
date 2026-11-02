package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"amdl/internal/config"
	"amdl/internal/domain"
)

// ErrJobNotFinished is returned by RequeueJob when the job is still queued or
// running: only a finished job (completed, failed or cancelled) goes back to
// the queue.
var ErrJobNotFinished = errors.New("job is not finished")

// FindLatestFinishedJobByKey returns the newest completed, failed or
// cancelled job whose canonical_key is canonicalKey: the latest created_at,
// then the latest inserted row. Databases from before one row per key may
// hold several finished rows for a key; only the newest is reused, the others
// stay as they are.
func (s *Store) FindLatestFinishedJobByKey(ctx context.Context, canonicalKey string) (domain.Job, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM runs j
		WHERE j.canonical_key=? AND j.status IN (?,?,?)
		ORDER BY j.created_at DESC, j.rowid DESC LIMIT 1`,
		canonicalKey, string(domain.JobCompleted), string(domain.JobFailed), string(domain.JobCancelled))
	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Job{}, false, nil
		}
		return domain.Job{}, false, err
	}
	return job, true, nil
}

// RequeueParams are the columns RequeueJob writes besides the status reset.
type RequeueParams struct {
	// QueueOrder is the job's new place in the run order.
	QueueOrder int64
	// ReplaceOverrides writes Overrides (nil stores none); otherwise the
	// stored overrides stay.
	ReplaceOverrides bool
	Overrides        *config.DownloadOverrides
	// CreatedAt, when set, becomes the job's created_at.
	CreatedAt *time.Time
	Now       time.Time
}

// RequeueJob puts a finished job back in the queue in one statement: status
// queued, error cleared, started_at and finished_at unset, queue_order and
// updated_at from p, and overrides and created_at when p says so. The order
// and the status land together, so the job is never claimable at a stale
// place. Items are not touched (see ResetUnfinishedItems).
//
// It returns ErrJobNotFound when no job has id, ErrJobNotFinished when the
// job is queued or running, and ErrDuplicateActive when another queued or
// running job holds the same canonical_key.
func (s *Store) RequeueJob(ctx context.Context, id string, p RequeueParams) error {
	overrides, err := encodeOverrides(p.Overrides)
	if err != nil {
		return err
	}
	var createdAt any
	if p.CreatedAt != nil {
		createdAt = formatTime(*p.CreatedAt)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE runs SET status=?, error='', started_at=NULL, finished_at=NULL,
			queue_order=?, updated_at=?,
			overrides=CASE WHEN ? THEN ? ELSE overrides END,
			created_at=COALESCE(?, created_at)
		WHERE id=? AND status IN (?,?,?)`,
		string(domain.JobQueued), p.QueueOrder, formatTime(p.Now),
		p.ReplaceOverrides, overrides,
		createdAt,
		id, string(domain.JobCompleted), string(domain.JobFailed), string(domain.JobCancelled))
	if err != nil {
		if isUniqueConstraintErr(err) {
			return ErrDuplicateActive
		}
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	var status string
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM runs WHERE id=?`, id).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrJobNotFound
		}
		return err
	}
	return ErrJobNotFinished
}
