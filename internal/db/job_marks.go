package db

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"amdl/internal/domain"
)

// JobMark is the part of a job row that says whether a catalog item is in the
// library or in the queue: the job's id, canonical key, status and times.
type JobMark struct {
	ID           string
	CanonicalKey string
	Status       domain.JobStatus
	CreatedAt    time.Time
	FinishedAt   *time.Time
}

// JobMarks returns the queued, running and completed jobs of types whose
// canonical key holds one of ids after a ":". It is a coarse filter (an id may
// also match inside a longer one): the caller parses each key to match exactly.
// No ids returns nothing without a query.
func (s *Store) JobMarks(ctx context.Context, types, ids []string) ([]JobMark, error) {
	if len(types) == 0 || len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(types)+len(ids)+3)
	for _, t := range types {
		args = append(args, t)
	}
	args = append(args, string(domain.JobQueued), string(domain.JobRunning), string(domain.JobCompleted))
	match := make([]string, len(ids))
	for i, id := range ids {
		match[i] = "instr(canonical_key, ?) > 0"
		args = append(args, ":"+id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, canonical_key, status, created_at, finished_at FROM jobs
		WHERE type IN (`+placeholders(len(types))+`) AND status IN (?,?,?) AND (`+strings.Join(match, " OR ")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobMark
	for rows.Next() {
		var (
			mark     JobMark
			status   string
			created  string
			finished sql.NullString
		)
		if err := rows.Scan(&mark.ID, &mark.CanonicalKey, &status, &created, &finished); err != nil {
			return nil, err
		}
		mark.Status = domain.JobStatus(status)
		mark.CreatedAt = parseTime(created)
		mark.FinishedAt = parseOptionalTime(finished)
		out = append(out, mark)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
