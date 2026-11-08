package db

import (
	"context"
	"database/sql"
)

// migrateTo7 adds accounts: the accounts table (one row per Apple Music
// account the wrapper holds, with its per-account switches), the storefront a
// record or pending add must be read in (pinned_storefront, storefront), and
// an account_id on the library-sync anchor and the Apple library cache, which
// become per account. Every existing row is kept: the anchor and cache rows
// are copied with an empty account_id, which the first account discovery
// hands to one account (Store.AdoptUnownedAccountData), and an empty pin
// means "the default account", as before. Frozen like every shipped
// migration.
func migrateTo7(ctx context.Context, tx *sql.Tx, _ migrationEnv) error {
	return execAll(ctx, tx, schemaV7)
}

var schemaV7 = []string{
	// position 0 is the default account. state is active or missing.
	`CREATE TABLE accounts (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		region TEXT NOT NULL DEFAULT '',
		position INTEGER NOT NULL DEFAULT 0,
		library_sync INTEGER NOT NULL DEFAULT 0,
		apple_library INTEGER NOT NULL DEFAULT 0,
		state TEXT NOT NULL DEFAULT 'active',
		added_at TEXT NOT NULL,
		seen_at TEXT
	)`,
	`ALTER TABLE albums ADD COLUMN pinned_storefront TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE playlists ADD COLUMN pinned_storefront TEXT NOT NULL DEFAULT ''`,
	// A read hint, not identity: idx_pending_adds_catalog stays (kind, catalog_id).
	`ALTER TABLE pending_adds ADD COLUMN storefront TEXT NOT NULL DEFAULT ''`,

	`CREATE TABLE library_sync_anchor_v7 (
		account_id TEXT NOT NULL DEFAULT '',
		position INTEGER NOT NULL,
		song_id TEXT NOT NULL,
		song_name TEXT NOT NULL DEFAULT '',
		album_name TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (account_id, position)
	)`,
	`INSERT INTO library_sync_anchor_v7 (account_id, position, song_id, song_name, album_name)
		SELECT '', position, song_id, song_name, album_name FROM library_sync_anchor`,
	`DROP TABLE library_sync_anchor`,
	`ALTER TABLE library_sync_anchor_v7 RENAME TO library_sync_anchor`,

	`CREATE TABLE apple_library_items_v7 (
		account_id TEXT NOT NULL DEFAULT '',
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
		PRIMARY KEY (account_id, kind, item_id)
	)`,
	`INSERT INTO apple_library_items_v7 (account_id, kind, item_id, catalog_id, url, name, artist_name, album_name,
		artwork_url, description, genre, release_date, track_count, duration_ms, track_number, disc_number, date_added,
		last_modified, version_hash, playlist_kind, add_order, fingerprint, seen_run, tracks_version, tracks_read_at, updated_at)
		SELECT '', kind, item_id, catalog_id, url, name, artist_name, album_name,
		artwork_url, description, genre, release_date, track_count, duration_ms, track_number, disc_number, date_added,
		last_modified, version_hash, playlist_kind, add_order, fingerprint, seen_run, tracks_version, tracks_read_at, updated_at
		FROM apple_library_items`,
	`DROP TABLE apple_library_items`,
	`ALTER TABLE apple_library_items_v7 RENAME TO apple_library_items`,
	`CREATE INDEX idx_apple_library_items_catalog ON apple_library_items(account_id, kind, catalog_id) WHERE catalog_id <> ''`,
	`CREATE INDEX idx_apple_library_items_order ON apple_library_items(account_id, kind, add_order)`,

	`CREATE TABLE apple_library_playlist_tracks_v7 (
		account_id TEXT NOT NULL DEFAULT '',
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
		PRIMARY KEY (account_id, playlist_id, position)
	)`,
	`INSERT INTO apple_library_playlist_tracks_v7 (account_id, playlist_id, position, song_id, catalog_id, url, name,
		artist_name, album_name, duration_ms, artwork_url)
		SELECT '', playlist_id, position, song_id, catalog_id, url, name, artist_name, album_name, duration_ms, artwork_url
		FROM apple_library_playlist_tracks`,
	`DROP TABLE apple_library_playlist_tracks`,
	`ALTER TABLE apple_library_playlist_tracks_v7 RENAME TO apple_library_playlist_tracks`,

	`CREATE TABLE apple_library_meta_v7 (
		account_id TEXT NOT NULL DEFAULT '',
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		PRIMARY KEY (account_id, key)
	)`,
	`INSERT INTO apple_library_meta_v7 (account_id, key, value) SELECT '', key, value FROM apple_library_meta`,
	`DROP TABLE apple_library_meta`,
	`ALTER TABLE apple_library_meta_v7 RENAME TO apple_library_meta`,
}
