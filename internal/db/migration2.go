package db

import (
	"context"
	"database/sql"
)

// migrateTo2 is the 2.0 schema. Frozen like every shipped migration, so its
// SQL spells event types and statuses as literals rather than referencing
// constants that may change later.
//
//   - jobs.started_at / jobs.finished_at (NULL = unset) are added and
//     backfilled from job_events: started_at from the first job_started of
//     the latest run (after the job's last job_retried, by event id),
//     finished_at, for terminal jobs only, from the latest terminal event.
//   - jobs.force is folded into overrides.force_overwrite, which is where the
//     pipeline reads forcing from; a row whose overrides are not a JSON
//     object is left as it is, since the job cannot load them anyway.
//   - jobs.force, jobs.done_items and jobs.failed_items are dropped: item
//     counters are derived from job_items on every read.
//   - job_events(type, created_at) is indexed for type and time-window scans.
//   - item_progress events of terminal jobs are deleted. They are live
//     telemetry; each item row keeps its final state, and Store.FinalizeJob
//     prunes them the same way from now on.
func migrateTo2(ctx context.Context, tx *sql.Tx) error {
	return execAll(ctx, tx, []string{
		`ALTER TABLE jobs ADD COLUMN started_at TEXT`,
		`ALTER TABLE jobs ADD COLUMN finished_at TEXT`,
		`DELETE FROM job_events
			WHERE type = 'item_progress'
			AND job_id IN (SELECT id FROM jobs WHERE status IN ('completed','failed','cancelled'))`,
		`UPDATE jobs SET started_at = s.started_at
			FROM (SELECT e.job_id, MIN(e.created_at) AS started_at FROM job_events e
				WHERE e.type = 'job_started'
				AND e.id > COALESCE((SELECT MAX(r.id) FROM job_events r
					WHERE r.job_id = e.job_id AND r.type = 'job_retried'), 0)
				GROUP BY e.job_id) AS s
			WHERE s.job_id = jobs.id`,
		`UPDATE jobs SET finished_at = f.finished_at
			FROM (SELECT job_id, MAX(created_at) AS finished_at FROM job_events
				WHERE type IN ('job_finished','job_failed','job_cancelled') GROUP BY job_id) AS f
			WHERE f.job_id = jobs.id AND jobs.status IN ('completed','failed','cancelled')`,
		`UPDATE jobs
			SET overrides = json_set(CASE WHEN overrides = '' THEN '{}' ELSE overrides END, '$.force_overwrite', json('true'))
			WHERE force <> 0 AND CASE
				WHEN overrides = '' THEN 1
				WHEN json_valid(overrides) THEN json_type(overrides) = 'object'
				ELSE 0 END`,
		`ALTER TABLE jobs DROP COLUMN force`,
		`ALTER TABLE jobs DROP COLUMN done_items`,
		`ALTER TABLE jobs DROP COLUMN failed_items`,
		`CREATE INDEX IF NOT EXISTS idx_job_events_type_created ON job_events(type, created_at)`,
	})
}
