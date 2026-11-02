package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"amdl/internal/domain"
)

// QueueOrderGap is the distance between neighboring queue_order values the
// store hands out: a new job lands this far past the last one, and a
// renumbering spaces the queue this far apart, so a move between two
// neighbors finds a free value 20 times in a row before the queue must be
// renumbered.
const QueueOrderGap int64 = 1 << 20

// nextQueueOrderSelect selects the queue_order that puts a job behind every
// other.
var nextQueueOrderSelect = fmt.Sprintf(`SELECT COALESCE(MAX(queue_order), 0) + %d FROM runs`, QueueOrderGap)

// queuedRunOrder is the claim order of queued runs: lowest priority class
// first (domain.PriorityNow, then manual, then auto), then lowest
// queue_order, then insertion order. The literal status lets the partial
// index idx_runs_queued_order serve it.
const queuedRunOrder = `status = '` + string(domain.JobQueued) + `' ORDER BY priority ASC, queue_order ASC, rowid ASC`

// ErrJobNotQueued is returned by MoveQueuedJob when the moved job or the
// anchor it is placed next to is not queued: running and finished jobs have
// no place in the run order.
var ErrJobNotQueued = errors.New("job is not queued")

// NextQueueOrder returns the queue_order that puts a job behind every job in
// the store, the place of a new or retried job.
func (s *Store) NextQueueOrder(ctx context.Context) (int64, error) {
	var order int64
	err := s.db.QueryRowContext(ctx, nextQueueOrderSelect).Scan(&order)
	return order, err
}

// SetQueueOrder writes one job's queue_order and nothing else.
func (s *Store) SetQueueOrder(ctx context.Context, jobID string, order int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET queue_order=? WHERE id=?`, order, jobID)
	return err
}

// NextQueuedJobID returns the id of the queued job that runs next, skipping
// the ids in skip; false when there is none.
func (s *Store) NextQueuedJobID(ctx context.Context, skip map[string]bool) (string, bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM runs WHERE `+queuedRunOrder)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", false, err
		}
		if !skip[id] {
			return id, true, nil
		}
	}
	return "", false, rows.Err()
}

// QueuedJobIDs returns the ids of the queued jobs in run order.
func (s *Store) QueuedJobIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM runs WHERE `+queuedRunOrder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Placement says where MoveQueuedJob puts a job in the run order. Exactly one
// field is set: Before or After name another queued job, Edge is EdgeFirst or
// EdgeLast.
type Placement struct {
	Before string
	After  string
	Edge   string
}

// Placement edges.
const (
	EdgeFirst = "first"
	EdgeLast  = "last"
)

type queuedRow struct {
	id    string
	order int64
}

// MoveQueuedJob moves the queued job jobID to place in the run order and
// returns the job as stored with the job_moved events it recorded, which the
// caller publishes. Usually only the moved job's queue_order changes, to a
// value between its new neighbors; when none is free the whole queue is
// renumbered QueueOrderGap apart and every job whose value changed gets its
// own job_moved, so a client that sorts by queue_order stays right without
// rereading the list. A move that leaves the job where it is writes nothing.
// The reads and writes run in one transaction. ErrJobNotFound reports a
// missing jobID; an error wrapping ErrJobNotQueued names the job, moved or
// anchor, that is not queued.
func (s *Store) MoveQueuedJob(ctx context.Context, jobID string, place Placement) (domain.Job, []domain.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Job{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	queue, err := queuedRows(ctx, tx)
	if err != nil {
		return domain.Job{}, nil, err
	}
	from := indexOfQueued(queue, jobID)
	if from < 0 {
		if err := jobExists(ctx, tx, jobID); err != nil {
			return domain.Job{}, nil, err
		}
		return domain.Job{}, nil, fmt.Errorf("job %s: %w", jobID, ErrJobNotQueued)
	}
	moved := queue[from]
	rest := append(append([]queuedRow{}, queue[:from]...), queue[from+1:]...)
	to := len(rest)
	switch {
	case place.Before != "" || place.After != "":
		anchor := place.Before
		if anchor == "" {
			anchor = place.After
		}
		at := indexOfQueued(rest, anchor)
		if at < 0 {
			if anchor == jobID {
				// Before or after itself: it stays where it is.
				at = from
				if place.After != "" {
					at--
				}
			} else {
				return domain.Job{}, nil, fmt.Errorf("job %s: %w", anchor, ErrJobNotQueued)
			}
		}
		to = at
		if place.After != "" {
			to = at + 1
		}
	case place.Edge == EdgeFirst:
		to = 0
	}

	var events []domain.Event
	if to != from {
		changed := map[string]int64{}
		switch {
		case len(rest) == 0:
		case to == 0:
			changed[jobID] = rest[0].order - QueueOrderGap
		case to == len(rest):
			changed[jobID] = rest[len(rest)-1].order + QueueOrderGap
		case rest[to].order-rest[to-1].order >= 2:
			changed[jobID] = rest[to-1].order + (rest[to].order-rest[to-1].order)/2
		default:
			order := append(append(append([]queuedRow{}, rest[:to]...), moved), rest[to:]...)
			for i, row := range order {
				if next := int64(i+1) * QueueOrderGap; next != row.order {
					changed[row.id] = next
				}
			}
		}
		// Record the moved job's event last, so a stream that applies events
		// in order ends on the move itself.
		ids := make([]string, 0, len(changed))
		for _, row := range queue {
			if _, ok := changed[row.id]; ok && row.id != jobID {
				ids = append(ids, row.id)
			}
		}
		if _, ok := changed[jobID]; ok {
			ids = append(ids, jobID)
		}
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE runs SET queue_order=? WHERE id=?`, changed[id], id); err != nil {
				return domain.Job{}, nil, err
			}
		}
		for _, id := range ids {
			job, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM runs j WHERE j.id=?`, id))
			if err != nil {
				return domain.Job{}, nil, err
			}
			message := "job moved in the queue"
			if id != jobID {
				message = "queue renumbered"
			}
			ev, err := addEvent(ctx, tx, domain.Event{JobID: id, Type: domain.EventJobMoved, Message: message,
				Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})})
			if err != nil {
				return domain.Job{}, nil, err
			}
			events = append(events, ev)
		}
	}
	job, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM runs j WHERE j.id=?`, jobID))
	if err != nil {
		return domain.Job{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Job{}, nil, err
	}
	return job, events, nil
}

func queuedRows(ctx context.Context, tx *sql.Tx) ([]queuedRow, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, queue_order FROM runs WHERE `+queuedRunOrder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []queuedRow
	for rows.Next() {
		var row queuedRow
		if err := rows.Scan(&row.id, &row.order); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func indexOfQueued(rows []queuedRow, id string) int {
	for i, row := range rows {
		if row.id == id {
			return i
		}
	}
	return -1
}

// jobExists returns nil when jobID has a row, ErrJobNotFound when it has none.
func jobExists(ctx context.Context, tx *sql.Tx, jobID string) error {
	var one int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM runs WHERE id=?`, jobID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrJobNotFound
	}
	return err
}
