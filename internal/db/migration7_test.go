package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// create6Database is a database at schema 6 holding a row in every table
// migration 7 changes, the Apple library cache and the anchor included.
func create6Database(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "amdl.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	ctx := context.Background()
	if err := migrateWith(ctx, raw, 6, migrationEnv{now: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO albums(id, name, storefront_read, added_at, updated_at) VALUES('1001', 'Album', 'us', '2026-11-01T00:00:00.000Z', '2026-11-01T00:00:00.000Z')`,
		`INSERT INTO playlists(id, name, added_at, updated_at) VALUES('pl.a', 'Playlist', '2026-11-01T00:00:00.000Z', '2026-11-01T00:00:00.000Z')`,
		`INSERT INTO pending_adds(kind, catalog_id, source, added_at) VALUES('song', '2002', 'manual', '2026-11-01T00:00:00.000Z')`,
		`INSERT INTO library_sync_anchor(position, song_id, song_name, album_name) VALUES(0, 'i.new', 'New', 'A'), (1, 'i.old', 'Old', 'B')`,
		`INSERT INTO apple_library_items(kind, item_id, catalog_id, name, add_order, fingerprint, seen_run, updated_at)
			VALUES('albums', 'l.a', '1001', 'Album', 2, 'fp', 'als_1', '2026-11-01T00:00:00.000Z'),
			      ('playlists', 'p.x', '', 'Mine', 1, 'fp2', 'als_1', '2026-11-01T00:00:00.000Z')`,
		`INSERT INTO apple_library_playlist_tracks(playlist_id, position, song_id, catalog_id, name) VALUES('p.x', 0, 'i.1', '2002', 'Song')`,
		`INSERT INTO apple_library_meta(key, value) VALUES('last_run', '{}'), ('kind:albums', '{"total":1}')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	return path
}

func dumpV6(t *testing.T, database *sql.DB) map[string]tableDump {
	t.Helper()
	return map[string]tableDump{
		"albums":                        dumpTable(t, database, "albums", "id"),
		"playlists":                     dumpTable(t, database, "playlists", "id"),
		"pending_adds":                  dumpTable(t, database, "pending_adds", "kind || '/' || catalog_id"),
		"library_sync_anchor":           dumpTable(t, database, "library_sync_anchor", "position"),
		"apple_library_items":           dumpTable(t, database, "apple_library_items", "kind || '/' || item_id"),
		"apple_library_playlist_tracks": dumpTable(t, database, "apple_library_playlist_tracks", "playlist_id || '/' || position"),
		"apple_library_meta":            dumpTable(t, database, "apple_library_meta", "key"),
	}
}

func TestMigration7KeepsEveryRowAndAddsAccounts(t *testing.T) {
	path := create6Database(t)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	before := dumpV6(t, raw)
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("open schema 6: %v", err)
	}
	if got := userVersion(t, store.db); got != latestSchemaVersion() {
		t.Fatalf("user_version = %d, want %d", got, latestSchemaVersion())
	}
	after := dumpV6(t, store.db)
	for table, rows := range before {
		if len(after[table]) != len(rows) {
			t.Errorf("%s has %d rows, want %d", table, len(after[table]), len(rows))
		}
		// An unset timestamp has no instant to compare; it must stay unset.
		unset := func(key, col string) bool {
			old := rows[key][col]
			if isTimestampColumn(col) && (old == nil || old == "") {
				if now := after[table][key][col]; now != old {
					t.Errorf("%s %s.%s = %v, want %v", table, key, col, now, old)
				}
				return true
			}
			return false
		}
		assertRowsSurvived(t, table, rows, after[table], unset)
	}
	for _, table := range accountTables {
		for key, row := range after[table] {
			if row["account_id"] != "" {
				t.Errorf("%s %s account_id = %v, want empty", table, key, row["account_id"])
			}
		}
	}
	for table, column := range map[string]string{"albums": "pinned_storefront", "playlists": "pinned_storefront", "pending_adds": "storefront"} {
		for key, row := range after[table] {
			if row[column] != "" {
				t.Errorf("%s %s.%s = %v, want empty", table, key, column, row[column])
			}
		}
	}
	for _, index := range []string{"idx_apple_library_items_catalog", "idx_apple_library_items_order", "idx_pending_adds_catalog"} {
		if !indexExists(t, store.db, index) {
			t.Errorf("index %s missing", index)
		}
	}
	if accounts, err := store.ListAccounts(context.Background()); err != nil || len(accounts) != 0 {
		t.Fatalf("accounts = %v, %v, want none", accounts, err)
	}
	first := dumpV6(t, store.db)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store.Close()
	if second := dumpV6(t, store.db); !reflect.DeepEqual(first, second) {
		t.Fatal("reopening the migrated database changed its rows")
	}
}
