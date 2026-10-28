package db

import (
	"context"
	"time"

	"amdl/internal/domain"
)

// UpdateJobStatus sets only a job's status and updated_at, for tests that need
// a row in a given status without building a full Job.
func (s *Store) UpdateJobStatus(ctx context.Context, id string, status domain.JobStatus, updatedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?, updated_at=? WHERE id=?`, string(status), formatTime(updatedAt), id)
	return err
}
