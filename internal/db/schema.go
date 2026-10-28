package db

import (
	"context"
	"time"

	"amdl/internal/domain"
)

func (s *Store) initSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			input TEXT NOT NULL,
			type TEXT NOT NULL,
			storefront TEXT,
			title TEXT NOT NULL DEFAULT '',
			artwork_url TEXT NOT NULL DEFAULT '',
			artist_name TEXT NOT NULL DEFAULT '',
			artist_url TEXT NOT NULL DEFAULT '',
			curator_name TEXT NOT NULL DEFAULT '',
			release_date TEXT NOT NULL DEFAULT '',
			genre TEXT NOT NULL DEFAULT '',
			artwork_bg_color TEXT NOT NULL DEFAULT '',
			artwork_text_color1 TEXT NOT NULL DEFAULT '',
			artwork_text_color2 TEXT NOT NULL DEFAULT '',
			artwork_text_color3 TEXT NOT NULL DEFAULT '',
			artwork_text_color4 TEXT NOT NULL DEFAULT '',
			motion_artwork_url TEXT NOT NULL DEFAULT '',
			motion_artwork_tall_url TEXT NOT NULL DEFAULT '',
			motion_artwork_bg_color TEXT NOT NULL DEFAULT '',
			motion_artwork_text_color1 TEXT NOT NULL DEFAULT '',
			motion_artwork_text_color2 TEXT NOT NULL DEFAULT '',
			motion_artwork_text_color3 TEXT NOT NULL DEFAULT '',
			motion_artwork_text_color4 TEXT NOT NULL DEFAULT '',
			motion_artwork_tall_bg_color TEXT NOT NULL DEFAULT '',
			motion_artwork_tall_text_color1 TEXT NOT NULL DEFAULT '',
			motion_artwork_tall_text_color2 TEXT NOT NULL DEFAULT '',
			motion_artwork_tall_text_color3 TEXT NOT NULL DEFAULT '',
			motion_artwork_tall_text_color4 TEXT NOT NULL DEFAULT '',
			canonical_key TEXT NOT NULL,
			force INTEGER NOT NULL DEFAULT 0,
			overrides TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			total_items INTEGER NOT NULL DEFAULT 0,
			done_items INTEGER NOT NULL DEFAULT 0,
			failed_items INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_active_key
			ON jobs(canonical_key)
			WHERE status IN ('queued','running');`,
		`CREATE TABLE IF NOT EXISTS job_items (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL,
			adam_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			idx INTEGER NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			artist TEXT NOT NULL DEFAULT '',
			album TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			artwork_url TEXT NOT NULL DEFAULT '',
			has_lyrics INTEGER NOT NULL DEFAULT 0,
			lyrics_status TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			download_progress REAL NOT NULL DEFAULT 0,
			decrypt_progress REAL NOT NULL DEFAULT 0,
			progress_resolved INTEGER NOT NULL DEFAULT 0,
			progress_remuxed INTEGER NOT NULL DEFAULT 0,
			progress_verified INTEGER NOT NULL DEFAULT 0,
			progress_tagged INTEGER NOT NULL DEFAULT 0,
			progress_saved INTEGER NOT NULL DEFAULT 0,
			codec TEXT NOT NULL DEFAULT '',
			bit_depth INTEGER NOT NULL DEFAULT 0,
			sample_rate INTEGER NOT NULL DEFAULT 0,
			bitrate INTEGER NOT NULL DEFAULT 0,
			file_size INTEGER NOT NULL DEFAULT 0,
			retry_kind TEXT NOT NULL DEFAULT '',
			attempt INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 0,
			status_message TEXT NOT NULL DEFAULT '',
			output_path TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(job_id) REFERENCES jobs(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_job_items_job_id ON job_items(job_id, idx);`,
		`CREATE TABLE IF NOT EXISTS job_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL,
			item_id TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL,
			phase TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_job_events_job_id_id ON job_events(job_id, id);`,
		// The library watcher's whole memory: the newest N library song ids in
		// order, position 0 being the most recently added. It is not user data
		// and carries no foreign keys — dropping every row only makes the
		// watcher re-anchor to the current library on its next tick, which
		// submits nothing. song_name/album_name are stored purely so the rows
		// are legible when someone inspects them.
		`CREATE TABLE IF NOT EXISTS library_sync_anchor (
			position INTEGER PRIMARY KEY,
			song_id TEXT NOT NULL,
			song_name TEXT NOT NULL DEFAULT '',
			album_name TEXT NOT NULL DEFAULT ''
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// Additive migrations for databases created before these columns existed;
	// CREATE TABLE IF NOT EXISTS leaves pre-existing tables untouched.
	for _, col := range []struct{ table, column, decl string }{
		{"jobs", "artwork_url", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "title", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "overrides", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "artist_name", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "artist_url", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "curator_name", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "release_date", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "genre", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "artwork_bg_color", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "artwork_text_color1", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "artwork_text_color2", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "artwork_text_color3", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "artwork_text_color4", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "motion_artwork_url", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "motion_artwork_tall_url", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "motion_artwork_bg_color", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "motion_artwork_text_color1", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "motion_artwork_text_color2", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "motion_artwork_text_color3", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "motion_artwork_text_color4", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "motion_artwork_tall_bg_color", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "motion_artwork_tall_text_color1", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "motion_artwork_tall_text_color2", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "motion_artwork_tall_text_color3", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "motion_artwork_tall_text_color4", "TEXT NOT NULL DEFAULT ''"},
		{"job_items", "artwork_url", "TEXT NOT NULL DEFAULT ''"},
		{"job_items", "duration_ms", "INTEGER NOT NULL DEFAULT 0"},
		{"job_items", "bit_depth", "INTEGER NOT NULL DEFAULT 0"},
		{"job_items", "sample_rate", "INTEGER NOT NULL DEFAULT 0"},
		{"job_items", "bitrate", "INTEGER NOT NULL DEFAULT 0"},
		{"job_items", "file_size", "INTEGER NOT NULL DEFAULT 0"},
		{"job_items", "has_lyrics", "INTEGER NOT NULL DEFAULT 0"},
		{"job_items", "lyrics_status", "TEXT NOT NULL DEFAULT ''"},
		// The per-stage progress breakdown that replaced the single `progress`
		// aggregate. That column is left in place on databases that have it:
		// nothing reads or writes it any more, and it carries a DEFAULT so
		// inserts that omit it succeed. Dropping it would buy nothing and
		// ALTER TABLE DROP COLUMN needs SQLite 3.35+.
		{"job_items", "download_progress", "REAL NOT NULL DEFAULT 0"},
		{"job_items", "decrypt_progress", "REAL NOT NULL DEFAULT 0"},
		{"job_items", "progress_resolved", "INTEGER NOT NULL DEFAULT 0"},
		{"job_items", "progress_remuxed", "INTEGER NOT NULL DEFAULT 0"},
		{"job_items", "progress_verified", "INTEGER NOT NULL DEFAULT 0"},
		{"job_items", "progress_tagged", "INTEGER NOT NULL DEFAULT 0"},
		{"job_items", "progress_saved", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.ensureColumn(ctx, col.table, col.column, col.decl); err != nil {
			return err
		}
	}
	if err := s.backfillItemProgress(ctx); err != nil {
		return err
	}
	// Rewrite variable-width RFC3339Nano timestamps (trailing zeros trimmed)
	// to a fixed 9-digit fractional form so TEXT ORDER BY / range compares
	// match chronological order. Idempotent: already-normalized rows are skipped.
	return s.normalizeTimestampColumns(ctx)
}

// backfillItemProgress gives already-completed rows a breakdown consistent with
// their status. Rows written before the breakdown existed only carried the old
// single aggregate, so the new columns land at zero — which would render a
// finished historical download as "no stage ever ran". A completed item must
// have passed every stage, so mark them.
//
// progress_verified is deliberately left at zero: false there means the
// integrity check did not run, and whether it ran is precisely what an old row
// no longer records. Claiming otherwise would be inventing history.
//
// Only the new columns are touched, and only on rows whose status already says
// the work is done, so no existing information is overwritten. Guarding on
// progress_resolved makes it idempotent and keeps it off rows the current code
// wrote, which set that flag themselves.
func (s *Store) backfillItemProgress(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE job_items
		SET download_progress=1, decrypt_progress=1,
			progress_resolved=1, progress_remuxed=1, progress_tagged=1, progress_saved=1
		WHERE status=? AND progress_resolved=0`, string(domain.ItemCompleted))
	return err
}

func (s *Store) ensureColumn(ctx context.Context, table, column, decl string) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+decl)
	return err
}

func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

// timeLayoutFixed is a fixed-width UTC RFC3339 with exactly 9 fractional
// digits. Unlike time.RFC3339Nano (which trims trailing zeros / omits the
// fraction entirely for whole seconds), this keeps lexicographic TEXT order
// identical to chronological order in SQLite comparisons and ORDER BY.
const timeLayoutFixed = "2006-01-02T15:04:05.000000000Z07:00"

// fixedTimestampGLOB matches formatTime output in SQLite GLOB syntax
// (? = one character). Used to skip already-normalized rows on Open.
const fixedTimestampGLOB = "????-??-??T??:??:??.?????????Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayoutFixed) }

func parseTime(v string) time.Time {
	// Accept both the fixed-width form and legacy variable-width RFC3339Nano
	// rows that have not yet been rewritten by normalizeTimestampColumns.
	if t, err := time.Parse(timeLayoutFixed, v); err == nil {
		return t
	}
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

// normalizeTimestampColumns rewrites persisted timestamps that are not in
// fixed-width form (legacy RFC3339Nano with trimmed fractional seconds).
// Safe to run on every Open: rows already matching fixedTimestampGLOB are
// left untouched.
func (s *Store) normalizeTimestampColumns(ctx context.Context) error {
	rewrites := []struct {
		table string
		idCol string
		col   string
	}{
		{"jobs", "id", "created_at"},
		{"jobs", "id", "updated_at"},
		{"job_items", "id", "created_at"},
		{"job_items", "id", "updated_at"},
		{"job_events", "id", "created_at"},
	}
	for _, rw := range rewrites {
		rows, err := s.db.QueryContext(ctx,
			`SELECT `+rw.idCol+`, `+rw.col+` FROM `+rw.table+` WHERE `+rw.col+` NOT GLOB '`+fixedTimestampGLOB+`'`)
		if err != nil {
			return err
		}
		type pair struct{ id, raw string }
		var batch []pair
		for rows.Next() {
			var p pair
			if err := rows.Scan(&p.id, &p.raw); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, p := range batch {
			if _, err := s.db.ExecContext(ctx,
				`UPDATE `+rw.table+` SET `+rw.col+`=? WHERE `+rw.idCol+`=?`,
				formatTime(parseTime(p.raw)), p.id); err != nil {
				return err
			}
		}
	}
	return nil
}
