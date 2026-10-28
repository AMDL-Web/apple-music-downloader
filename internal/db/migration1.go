package db

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateTo1 brings any database written by a 1.x release (user_version 0) to
// the final 1.x schema, and creates that schema in an empty file. It is the
// set of idempotent steps 1.x ran on every open, run once: CREATE ... IF NOT
// EXISTS, the column backfills, the item progress backfill and the timestamp
// normalization. Frozen: later schema changes are new migrations.
func migrateTo1(ctx context.Context, tx *sql.Tx) error {
	if err := execAll(ctx, tx, schemaV1Tables); err != nil {
		return err
	}
	// Databases created before these columns existed; CREATE TABLE IF NOT
	// EXISTS leaves their tables untouched.
	for _, col := range schemaV1Columns {
		if err := ensureColumn(ctx, tx, col.table, col.column, col.decl); err != nil {
			return err
		}
	}
	if err := backfillItemProgress(ctx, tx); err != nil {
		return err
	}
	// Later migrations compare and aggregate created_at as TEXT, which is only
	// chronological once every row is fixed-width.
	return normalizeTimestampColumns(ctx, tx)
}

var schemaV1Tables = []string{
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
	// order, position 0 being the most recently added. Not user data and no
	// foreign keys: losing every row only makes the watcher re-anchor to the
	// current library on its next tick, which submits nothing. song_name and
	// album_name keep the rows legible to someone inspecting them.
	`CREATE TABLE IF NOT EXISTS library_sync_anchor (
		position INTEGER PRIMARY KEY,
		song_id TEXT NOT NULL,
		song_name TEXT NOT NULL DEFAULT '',
		album_name TEXT NOT NULL DEFAULT ''
	);`,
}

// schemaV1Columns are the columns 1.x releases added after their table first
// shipped. A database old enough to lack one gets it with its DEFAULT.
var schemaV1Columns = []struct{ table, column, decl string }{
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
	// aggregate. That legacy column stays on databases that have it: nothing
	// reads or writes it, and its DEFAULT lets inserts omit it.
	{"job_items", "download_progress", "REAL NOT NULL DEFAULT 0"},
	{"job_items", "decrypt_progress", "REAL NOT NULL DEFAULT 0"},
	{"job_items", "progress_resolved", "INTEGER NOT NULL DEFAULT 0"},
	{"job_items", "progress_remuxed", "INTEGER NOT NULL DEFAULT 0"},
	{"job_items", "progress_verified", "INTEGER NOT NULL DEFAULT 0"},
	{"job_items", "progress_tagged", "INTEGER NOT NULL DEFAULT 0"},
	{"job_items", "progress_saved", "INTEGER NOT NULL DEFAULT 0"},
}

// ensureColumn adds column to table unless it exists. decl must carry a
// DEFAULT so existing rows get a value.
func ensureColumn(ctx context.Context, tx *sql.Tx, table, column, decl string) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&count); err != nil {
		return fmt.Errorf("inspect %s.%s: %w", table, column, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+decl); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

// backfillItemProgress gives completed rows written before the per-stage
// breakdown existed a breakdown consistent with their status: a completed item
// passed every stage. progress_verified stays 0, because whether the integrity
// check ran is exactly what an old row no longer records. Only rows whose
// progress_resolved is still 0 are touched, so nothing a newer writer recorded
// is overwritten.
func backfillItemProgress(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE job_items
		SET download_progress=1, decrypt_progress=1,
			progress_resolved=1, progress_remuxed=1, progress_tagged=1, progress_saved=1
		WHERE status='completed' AND progress_resolved=0`)
	if err != nil {
		return fmt.Errorf("backfill item progress: %w", err)
	}
	return nil
}

// normalizeTimestampColumns rewrites timestamps written in the variable-width
// RFC3339Nano form (trailing zeros trimmed) to formatTime's fixed width.
func normalizeTimestampColumns(ctx context.Context, tx *sql.Tx) error {
	rewrites := []struct{ table, idCol, col string }{
		{"jobs", "id", "created_at"},
		{"jobs", "id", "updated_at"},
		{"job_items", "id", "created_at"},
		{"job_items", "id", "updated_at"},
		{"job_events", "id", "created_at"},
	}
	for _, rw := range rewrites {
		rows, err := tx.QueryContext(ctx,
			`SELECT `+rw.idCol+`, `+rw.col+` FROM `+rw.table+` WHERE `+rw.col+` NOT GLOB '`+fixedTimestampGLOB+`'`)
		if err != nil {
			return fmt.Errorf("find legacy %s.%s: %w", rw.table, rw.col, err)
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
			if _, err := tx.ExecContext(ctx,
				`UPDATE `+rw.table+` SET `+rw.col+`=? WHERE `+rw.idCol+`=?`,
				formatTime(parseTime(p.raw)), p.id); err != nil {
				return fmt.Errorf("normalize %s.%s: %w", rw.table, rw.col, err)
			}
		}
	}
	return nil
}
