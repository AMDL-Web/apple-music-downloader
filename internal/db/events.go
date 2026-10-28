package db

import (
	"context"
	"strings"

	"amdl/internal/domain"
)

func addEvent(ctx context.Context, x execer, event domain.Event) (domain.Event, error) {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now()
	}
	res, err := x.ExecContext(ctx, `INSERT INTO job_events(job_id,item_id,type,phase,message,payload,created_at) VALUES(?,?,?,?,?,?,?)`,
		event.JobID, event.ItemID, event.Type, event.Phase, event.Message, event.Payload, formatTime(event.CreatedAt))
	if err != nil {
		return event, err
	}
	event.ID, _ = res.LastInsertId()
	return event, nil
}

func (s *Store) AddEvent(ctx context.Context, event domain.Event) (domain.Event, error) {
	return addEvent(ctx, s.db, event)
}

// LatestEventID returns the id of the most recent event recorded for jobID, or
// 0 if none exist yet. Callers use this as the last_event_id to hand a client
// alongside a job/items snapshot, so a subsequent events/ws connection skips
// replaying history already reflected in that snapshot.
func (s *Store) LatestEventID(ctx context.Context, jobID string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM job_events WHERE job_id=?`, jobID).Scan(&id)
	return id, err
}

func (s *Store) ListEventsAfter(ctx context.Context, jobID string, afterID int64) ([]domain.Event, error) {
	return s.queryEvents(ctx, `SELECT id,job_id,item_id,type,phase,message,payload,created_at FROM job_events WHERE job_id=? AND id>? ORDER BY id ASC`, jobID, afterID)
}

// ListEventsAfterLimit is the bounded-page form used by streaming endpoints.
// The unbounded method above remains for existing internal callers/tests.
func (s *Store) ListEventsAfterLimit(ctx context.Context, jobID string, afterID int64, limit int) ([]domain.Event, error) {
	if limit <= 0 {
		return s.ListEventsAfter(ctx, jobID, afterID)
	}
	return s.queryEvents(ctx, `SELECT id,job_id,item_id,type,phase,message,payload,created_at FROM job_events WHERE job_id=? AND id>? ORDER BY id ASC LIMIT ?`, jobID, afterID, limit)
}

// LatestGlobalEventID returns the id of the most recent event across all jobs
// (0 if none). The overview feed hands this to a client alongside the
// GET /downloads snapshot as the cursor to resume from, mirroring how
// LatestEventID pairs with a single job's snapshot.
func (s *Store) LatestGlobalEventID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM job_events`).Scan(&id)
	return id, err
}

// milestoneTypePlaceholders is the "?,?,…" fragment and matching args for
// overview-milestone types, built once from the single source of truth so the
// SQL filter can never drift from IsOverviewMilestone.
var milestoneTypePlaceholders, milestoneTypeArgs = func() (string, []any) {
	types := append([]string{}, domain.PersistedOverviewMilestones...)
	types = append(types, domain.EventDeleted)
	marks := make([]string, len(types))
	args := make([]any, len(types))
	for i, t := range types {
		marks[i] = "?"
		args[i] = t
	}
	return strings.Join(marks, ","), args
}()

// ListMilestoneEventsAfter returns, across all jobs, the events newer than
// afterID whose type changes the GET /downloads list-level view. The overview
// feed uses this to learn which jobs changed since a cursor.
func (s *Store) ListMilestoneEventsAfter(ctx context.Context, afterID int64) ([]domain.Event, error) {
	args := append([]any{afterID}, milestoneTypeArgs...)
	return s.queryEvents(ctx, `SELECT id,job_id,item_id,type,phase,message,payload,created_at FROM job_events
		WHERE id>? AND type IN (`+milestoneTypePlaceholders+`)
		ORDER BY id ASC`, args...)
}

// ListMilestoneEventsAfterLimit is the bounded-page form used by the global
// downloads feed so reconnecting from an old cursor has fixed memory use.
func (s *Store) ListMilestoneEventsAfterLimit(ctx context.Context, afterID int64, limit int) ([]domain.Event, error) {
	if limit <= 0 {
		return s.ListMilestoneEventsAfter(ctx, afterID)
	}
	args := append([]any{afterID}, milestoneTypeArgs...)
	args = append(args, limit)
	return s.queryEvents(ctx, `SELECT id,job_id,item_id,type,phase,message,payload,created_at FROM job_events
		WHERE id>? AND type IN (`+milestoneTypePlaceholders+`)
		ORDER BY id ASC LIMIT ?`, args...)
}

// ListHookEvents returns only jobID's hook lifecycle events, so callers
// summarizing hook status (GET /downloads/{id}) don't have to scan the far
// more numerous progress events sharing the table.
func (s *Store) ListHookEvents(ctx context.Context, jobID string) ([]domain.Event, error) {
	return s.queryEvents(ctx, `SELECT id,job_id,item_id,type,phase,message,payload,created_at FROM job_events WHERE job_id=? AND type IN ('hook_started','hook_succeeded','hook_failed') ORDER BY id ASC`, jobID)
}

func (s *Store) queryEvents(ctx context.Context, query string, args ...any) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Event, 0)
	for rows.Next() {
		var ev domain.Event
		var created string
		if err := rows.Scan(&ev.ID, &ev.JobID, &ev.ItemID, &ev.Type, &ev.Phase, &ev.Message, &ev.Payload, &created); err != nil {
			return nil, err
		}
		ev.CreatedAt = parseTime(created)
		out = append(out, ev)
	}
	return out, rows.Err()
}
