package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"amdl/internal/domain"
)

// FailUnreadableJobs fails every queued or running run whose overrides do not
// decode (ErrCorruptOverrides): such a run cannot run, and ListRecoverableJobs
// refuses it. In one transaction each one's unfinished items end cancelled with
// message, the run fails with message as its error and keeps its overrides
// column as stored, so a retry still refuses it instead of running with the
// global config, and a job_failed event is recorded. It returns the failed
// runs, complete but for their overrides, and their events.
func (s *Store) FailUnreadableJobs(ctx context.Context, message string, now time.Time) ([]domain.Job, []domain.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var ids []string
	rows, err := tx.QueryContext(ctx, `SELECT `+jobColumns+` FROM runs j WHERE j.status IN (?,?) AND j.overrides <> ''`,
		string(domain.JobQueued), string(domain.JobRunning))
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		job, err := scanJob(rows)
		if errors.Is(err, ErrCorruptOverrides) {
			ids = append(ids, job.ID)
		} else if err != nil {
			rows.Close()
			return nil, nil, err
		}
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}

	var jobs []domain.Job
	var events []domain.Event
	for _, id := range ids {
		if err := cancelUnfinishedItems(ctx, tx, id, message, now); err != nil {
			return nil, nil, err
		}
		job, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM runs j WHERE j.id=?`, id))
		if err != nil && !errors.Is(err, ErrCorruptOverrides) {
			return nil, nil, err
		}
		job.Status = domain.JobFailed
		job.Error = message
		job.UpdatedAt = now
		job.FinishedAt = &now
		if err := updateJob(ctx, tx, job); err != nil {
			return nil, nil, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM run_events WHERE job_id=? AND type=?`, id, string(domain.EventItemProgress)); err != nil {
			return nil, nil, fmt.Errorf("prune item_progress events of job %s: %w", id, err)
		}
		ev, err := addEvent(ctx, tx, domain.Event{JobID: id, Type: domain.EventJobFailed, Message: message,
			Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})})
		if err != nil {
			return nil, nil, err
		}
		jobs = append(jobs, job)
		events = append(events, ev)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return jobs, events, nil
}
