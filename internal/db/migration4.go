package db

import (
	"context"
	"database/sql"
)

// migrateTo4 adds jobs.queue_order, the run order of queued jobs
// (domain.Job.QueueOrder). Every existing row gets a value spaced 1<<20
// apart in (created_at, rowid) order, the order the FIFO dispatcher ran
// queued jobs in and GET /downloads lists a batch in, so queued jobs keep
// their place. A partial index serves the claim query, which reads the
// queued rows in that order. No row loses information. Frozen like every
// shipped migration: the spacing and the status are literals.
func migrateTo4(ctx context.Context, tx *sql.Tx) error {
	return execAll(ctx, tx, []string{
		`ALTER TABLE jobs ADD COLUMN queue_order INTEGER NOT NULL DEFAULT 0`,
		`UPDATE jobs SET queue_order = o.n * 1048576
			FROM (SELECT rowid AS rid, row_number() OVER (ORDER BY created_at, rowid) AS n FROM jobs) AS o
			WHERE o.rid = jobs.rowid`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_queued_order ON jobs(queue_order) WHERE status = 'queued'`,
	})
}
