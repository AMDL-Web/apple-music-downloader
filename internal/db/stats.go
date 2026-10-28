package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"amdl/internal/domain"
)

// Stats is the activity summary behind GET /api/v1/stats. It covers only rows
// that still exist: deleting a job deletes its items and events.
type Stats struct {
	// Totals and Codecs cover every retained job, not only the window.
	Totals StatsTotals
	// Daily has one entry per UTC day of the window, oldest first.
	Daily  []DailyStats
	Codecs []CodecStats
	// Hooks covers hook outcomes recorded inside the window.
	Hooks []HookStats
}

// StatsTotals counts jobs and tracks by final item status.
type StatsTotals struct {
	Jobs            int
	TracksCompleted int
	TracksSkipped   int
	TracksFailed    int
	// BytesWritten sums file_size over completed items (bytes).
	BytesWritten int64
	// DurationMS sums the catalog playback duration of completed items.
	DurationMS int64
}

// DailyStats is one UTC day. A track counts on the day of its last
// item_completed event; a job counts once per job_finished or job_failed
// event, so a failed job that is retried and completes counts on both days.
type DailyStats struct {
	// Date is the UTC calendar date, YYYY-MM-DD.
	Date            string
	TracksCompleted int
	BytesWritten    int64
	JobsCompleted   int
	JobsFailed      int
}

// CodecStats counts completed items with one codec, bit depth and sample
// rate.
type CodecStats struct {
	Codec      string
	BitDepth   int
	SampleRate int
	Tracks     int
}

// HookStats counts one hook's outcomes. Name is the hook entry name, which
// the dispatcher records as the event phase.
type HookStats struct {
	Name         string
	Succeeded    int
	Failed       int
	LastFailedAt *time.Time
}

// statsDateLayout is the YYYY-MM-DD prefix of a formatTime timestamp.
const statsDateLayout = "2006-01-02"

// The window queries of Stats. Each filters job_events on type and a
// created_at range, which idx_job_events_type_created (migration 2) serves;
// TestStatsWindowQueriesUseTypeCreatedIndex checks the query plans.
const (
	// An item that completed more than once (it should not, but events are
	// not unique per item) counts once, on its latest completion.
	statsDailyTracksSQL = `SELECT substr(c.at, 1, 10), COUNT(*), COALESCE(SUM(i.file_size), 0)
		FROM (SELECT item_id, MAX(created_at) AS at FROM job_events
			WHERE type = ? AND created_at >= ? AND created_at < ? GROUP BY item_id) AS c
		JOIN job_items i ON i.id = c.item_id
		WHERE i.status = ?
		GROUP BY 1`
	statsDailyJobsSQL = `SELECT substr(created_at, 1, 10), type, COUNT(*) FROM job_events
		WHERE type IN (?, ?) AND created_at >= ? AND created_at < ?
		GROUP BY 1, 2`
	statsHooksSQL = `SELECT phase,
			COUNT(CASE WHEN type = ? THEN 1 END),
			COUNT(CASE WHEN type = ? THEN 1 END),
			MAX(CASE WHEN type = ? THEN created_at END)
		FROM job_events
		WHERE type IN (?, ?) AND created_at >= ? AND created_at < ?
		GROUP BY phase
		ORDER BY phase`
)

// Stats summarizes the retained history, with per-day figures for the days
// UTC calendar days that end with the day of now (days >= 1).
func (s *Store) Stats(ctx context.Context, now time.Time, days int) (Stats, error) {
	if days < 1 {
		return Stats{}, fmt.Errorf("stats window must be at least one day, got %d", days)
	}
	now = now.UTC()
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	start := end.AddDate(0, 0, -days)
	from, until := formatTime(start), formatTime(end)

	var out Stats
	var err error
	if out.Totals, err = s.statsTotals(ctx); err != nil {
		return Stats{}, fmt.Errorf("stats totals: %w", err)
	}
	if out.Daily, err = s.statsDaily(ctx, start, days, from, until); err != nil {
		return Stats{}, fmt.Errorf("stats daily: %w", err)
	}
	if out.Codecs, err = s.statsCodecs(ctx); err != nil {
		return Stats{}, fmt.Errorf("stats codecs: %w", err)
	}
	if out.Hooks, err = s.statsHooks(ctx, from, until); err != nil {
		return Stats{}, fmt.Errorf("stats hooks: %w", err)
	}
	return out, nil
}

func (s *Store) statsTotals(ctx context.Context) (StatsTotals, error) {
	completed, skipped, failed := string(domain.ItemCompleted), string(domain.ItemSkipped), string(domain.ItemFailed)
	var t StatsTotals
	err := s.db.QueryRowContext(ctx, `SELECT
			(SELECT COUNT(*) FROM jobs),
			COUNT(CASE WHEN status = ? THEN 1 END),
			COUNT(CASE WHEN status = ? THEN 1 END),
			COUNT(CASE WHEN status = ? THEN 1 END),
			COALESCE(SUM(CASE WHEN status = ? THEN file_size END), 0),
			COALESCE(SUM(CASE WHEN status = ? THEN duration_ms END), 0)
		FROM job_items`,
		completed, skipped, failed, completed, completed,
	).Scan(&t.Jobs, &t.TracksCompleted, &t.TracksSkipped, &t.TracksFailed, &t.BytesWritten, &t.DurationMS)
	return t, err
}

func (s *Store) statsDaily(ctx context.Context, start time.Time, days int, from, until string) ([]DailyStats, error) {
	daily := make([]DailyStats, days)
	byDate := make(map[string]*DailyStats, days)
	for i := range daily {
		daily[i].Date = start.AddDate(0, 0, i).Format(statsDateLayout)
		byDate[daily[i].Date] = &daily[i]
	}

	rows, err := s.db.QueryContext(ctx, statsDailyTracksSQL,
		string(domain.EventItemCompleted), from, until, string(domain.ItemCompleted))
	if err != nil {
		return nil, err
	}
	err = scanRows(rows, func() error {
		var (
			date   string
			tracks int
			bytes  int64
		)
		if err := rows.Scan(&date, &tracks, &bytes); err != nil {
			return err
		}
		if d := byDate[date]; d != nil {
			d.TracksCompleted, d.BytesWritten = tracks, bytes
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	finished, failed := string(domain.EventJobFinished), string(domain.EventJobFailed)
	rows, err = s.db.QueryContext(ctx, statsDailyJobsSQL, finished, failed, from, until)
	if err != nil {
		return nil, err
	}
	err = scanRows(rows, func() error {
		var (
			date, eventType string
			n               int
		)
		if err := rows.Scan(&date, &eventType, &n); err != nil {
			return err
		}
		d := byDate[date]
		switch {
		case d == nil:
		case eventType == finished:
			d.JobsCompleted = n
		case eventType == failed:
			d.JobsFailed = n
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return daily, nil
}

func (s *Store) statsCodecs(ctx context.Context) ([]CodecStats, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT codec, bit_depth, sample_rate, COUNT(*) FROM job_items
		WHERE status = ?
		GROUP BY codec, bit_depth, sample_rate
		ORDER BY COUNT(*) DESC, codec, bit_depth, sample_rate`,
		string(domain.ItemCompleted))
	if err != nil {
		return nil, err
	}
	codecs := make([]CodecStats, 0)
	err = scanRows(rows, func() error {
		var c CodecStats
		if err := rows.Scan(&c.Codec, &c.BitDepth, &c.SampleRate, &c.Tracks); err != nil {
			return err
		}
		codecs = append(codecs, c)
		return nil
	})
	return codecs, err
}

func (s *Store) statsHooks(ctx context.Context, from, until string) ([]HookStats, error) {
	succeeded, failed := string(domain.EventHookSucceeded), string(domain.EventHookFailed)
	rows, err := s.db.QueryContext(ctx, statsHooksSQL, succeeded, failed, failed, succeeded, failed, from, until)
	if err != nil {
		return nil, err
	}
	hooks := make([]HookStats, 0)
	err = scanRows(rows, func() error {
		var (
			h          HookStats
			lastFailed sql.NullString
		)
		if err := rows.Scan(&h.Name, &h.Succeeded, &h.Failed, &lastFailed); err != nil {
			return err
		}
		h.LastFailedAt = parseOptionalTime(lastFailed)
		hooks = append(hooks, h)
		return nil
	})
	return hooks, err
}

// scanRows calls scan for each row, then closes rows and returns the first
// error from scan, iteration or close.
func scanRows(rows *sql.Rows, scan func() error) error {
	defer rows.Close()
	for rows.Next() {
		if err := scan(); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return rows.Close()
}
