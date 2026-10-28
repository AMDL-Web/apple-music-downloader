package db

import (
	"context"
	"encoding/json"
	"strings"

	"amdl/internal/domain"
)

const eventColumns = `id,job_id,item_id,type,phase,message,payload,created_at`

func addEvent(ctx context.Context, x execer, event domain.Event) (domain.Event, error) {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now()
	}
	// The payload is bound as a string: a json.RawMessage would bind as []byte
	// and land as a BLOB in the TEXT column.
	res, err := x.ExecContext(ctx, `INSERT INTO job_events(job_id,item_id,type,phase,message,payload,created_at) VALUES(?,?,?,?,?,?,?)`,
		event.JobID, event.ItemID, string(event.Type), event.Phase, event.Message, string(event.Payload), formatTime(event.CreatedAt))
	if err != nil {
		return event, err
	}
	event.ID, _ = res.LastInsertId()
	return event, nil
}

// AddEvent persists event and returns it with its id and created_at set.
func (s *Store) AddEvent(ctx context.Context, event domain.Event) (domain.Event, error) {
	return addEvent(ctx, s.db, event)
}

// LatestEventID returns the id of the most recent event recorded for jobID, or
// 0 if none exist yet. It is the cursor handed to a client alongside a job
// snapshot, so a stream resumed from it skips history the snapshot already
// reflects.
func (s *Store) LatestEventID(ctx context.Context, jobID string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM job_events WHERE job_id=?`, jobID).Scan(&id)
	return id, err
}

// ListEventsAfterLimit returns up to limit of jobID's events with an id
// greater than afterID, oldest first. limit <= 0 means no limit.
func (s *Store) ListEventsAfterLimit(ctx context.Context, jobID string, afterID int64, limit int) ([]domain.Event, error) {
	return s.queryEvents(ctx, `SELECT `+eventColumns+` FROM job_events WHERE job_id=? AND id>? ORDER BY id ASC LIMIT ?`, jobID, afterID, sqlLimit(limit))
}

// LatestGlobalEventID returns the id of the most recent event across all jobs
// (0 if none): the overview feed's cursor for a GET /downloads snapshot.
func (s *Store) LatestGlobalEventID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM job_events`).Scan(&id)
	return id, err
}

// typeInList returns the "?,?,…" fragment and matching args for an
// "type IN (…)" filter over types.
func typeInList(types []domain.EventType) (string, []any) {
	marks := make([]string, len(types))
	args := make([]any, len(types))
	for i, t := range types {
		marks[i] = "?"
		args[i] = string(t)
	}
	return strings.Join(marks, ","), args
}

var (
	milestoneTypePlaceholders, milestoneTypeArgs = typeInList(domain.OverviewMilestones)
	hookTypePlaceholders, hookTypeArgs           = typeInList(domain.HookEventTypes)
)

// ListMilestoneEventsAfterLimit returns, across all jobs, up to limit events
// newer than afterID whose type is in domain.OverviewMilestones, oldest first.
// limit <= 0 means no limit.
func (s *Store) ListMilestoneEventsAfterLimit(ctx context.Context, afterID int64, limit int) ([]domain.Event, error) {
	args := append([]any{afterID}, milestoneTypeArgs...)
	args = append(args, sqlLimit(limit))
	return s.queryEvents(ctx, `SELECT `+eventColumns+` FROM job_events
		WHERE id>? AND type IN (`+milestoneTypePlaceholders+`)
		ORDER BY id ASC LIMIT ?`, args...)
}

// ListHookEvents returns only jobID's hook events (domain.HookEventTypes), so
// summarizing hook status does not scan the far more numerous progress events
// sharing the table.
func (s *Store) ListHookEvents(ctx context.Context, jobID string) ([]domain.Event, error) {
	args := append([]any{jobID}, hookTypeArgs...)
	return s.queryEvents(ctx, `SELECT `+eventColumns+` FROM job_events WHERE job_id=? AND type IN (`+hookTypePlaceholders+`) ORDER BY id ASC`, args...)
}

// sqlLimit maps "no limit" (limit <= 0) to SQLite's LIMIT -1.
func sqlLimit(limit int) int {
	if limit <= 0 {
		return -1
	}
	return limit
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
		var eventType, payload, created string
		if err := rows.Scan(&ev.ID, &ev.JobID, &ev.ItemID, &eventType, &ev.Phase, &ev.Message, &payload, &created); err != nil {
			return nil, err
		}
		ev.Type = domain.EventType(eventType)
		// A payload that is not valid JSON would make the whole event
		// unencodable, so it is dropped rather than passed through.
		if payload != "" && json.Valid([]byte(payload)) {
			ev.Payload = json.RawMessage(payload)
		}
		ev.CreatedAt = parseTime(created)
		out = append(out, ev)
	}
	return out, rows.Err()
}
