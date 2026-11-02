package db

import (
	"context"
	"database/sql"
)

// migrateTo5 adds the Apple library cache: the user's Apple Music account
// library (albums, songs, playlists, artists), the track lists of its
// playlists, and the sync's bookkeeping. New tables only; no existing row is
// touched. Everything here is a copy of what Apple holds and is refilled by
// the next full sync, so these tables are not user data in the migration
// sense. Frozen like every shipped migration.
func migrateTo5(ctx context.Context, tx *sql.Tx) error {
	return execAll(ctx, tx, []string{
		`CREATE TABLE IF NOT EXISTS apple_library_items (
			kind TEXT NOT NULL,
			item_id TEXT NOT NULL,
			catalog_id TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			artist_name TEXT NOT NULL DEFAULT '',
			album_name TEXT NOT NULL DEFAULT '',
			artwork_url TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			genre TEXT NOT NULL DEFAULT '',
			release_date TEXT NOT NULL DEFAULT '',
			track_count INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			track_number INTEGER NOT NULL DEFAULT 0,
			disc_number INTEGER NOT NULL DEFAULT 0,
			date_added TEXT NOT NULL DEFAULT '',
			last_modified TEXT NOT NULL DEFAULT '',
			version_hash TEXT NOT NULL DEFAULT '',
			playlist_kind TEXT NOT NULL DEFAULT '',
			add_order INTEGER NOT NULL DEFAULT 0,
			fingerprint TEXT NOT NULL DEFAULT '',
			seen_run TEXT NOT NULL DEFAULT '',
			tracks_version TEXT NOT NULL DEFAULT '',
			tracks_read_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY (kind, item_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_apple_library_items_catalog ON apple_library_items(kind, catalog_id) WHERE catalog_id <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_apple_library_items_order ON apple_library_items(kind, add_order)`,
		`CREATE TABLE IF NOT EXISTS apple_library_playlist_tracks (
			playlist_id TEXT NOT NULL,
			position INTEGER NOT NULL,
			song_id TEXT NOT NULL DEFAULT '',
			catalog_id TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			artist_name TEXT NOT NULL DEFAULT '',
			album_name TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			artwork_url TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (playlist_id, position)
		)`,
		`CREATE TABLE IF NOT EXISTS apple_library_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	})
}
