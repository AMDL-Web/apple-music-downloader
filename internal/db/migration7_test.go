package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"amdl/internal/domain"
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
	if got := userVersion(t, store.db); got != 7 {
		t.Fatalf("user_version = %d, want 7", got)
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

// The first discovery hands the rows from before accounts to one account,
// which becomes the default with the single account's switches; later
// accounts start with both off.
func TestSyncAccountsAdoptsUnownedRows(t *testing.T) {
	store, err := Open(create6Database(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	res, err := store.SyncAccounts(ctx, AccountDiscovery{
		Seen:     []SeenAccount{{ID: "jp", Name: "jp@example.com", Region: "JP"}, {ID: "us", Name: "us@example.com", Region: "us"}},
		Adopt:    "us",
		Complete: true, AdoptLibrarySync: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Adopted != "us" || len(res.Added) != 2 {
		t.Fatalf("sync = %+v", res)
	}
	accounts, err := store.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[0].ID != "us" || accounts[0].Position != 0 || !accounts[0].LibrarySync || !accounts[0].AppleLibrary {
		t.Fatalf("default = %+v", accounts)
	}
	if jp := accounts[1]; jp.ID != "jp" || jp.Position != 1 || jp.LibrarySync || jp.AppleLibrary || jp.Region != "jp" || jp.Name != "jp@example.com" {
		t.Fatalf("second = %+v", jp)
	}
	anchor, err := store.LoadLibrarySyncAnchor(ctx, "us")
	if err != nil || len(anchor) != 2 {
		t.Fatalf("us anchor = %v, %v", anchor, err)
	}
	if counts, err := store.AppleLibraryCounts(ctx, "us"); err != nil || counts["albums"] != 1 || counts["playlists"] != 1 {
		t.Fatalf("us counts = %v, %v", counts, err)
	}
	if tracks, _, found, err := store.AppleLibraryPlaylistTracks(ctx, "us", "p.x"); err != nil || !found || len(tracks) != 1 {
		t.Fatalf("us p.x tracks = %v, %v, %v", tracks, found, err)
	}
	if v, err := store.AppleLibraryMeta(ctx, "us", "kind:albums"); err != nil || v == "" {
		t.Fatalf("us meta = %q, %v", v, err)
	}
	for _, table := range accountTables {
		var n int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE account_id = ''`).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s keeps %d unowned rows (%v)", table, n, err)
		}
	}

	// A later discovery that misses one marks it missing and keeps its row;
	// adoption does not run again.
	res, err = store.SyncAccounts(ctx, AccountDiscovery{Seen: []SeenAccount{{ID: "jp", Region: "jp"}, {ID: "kr", Region: "kr"}}, Complete: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Adopted != "" || len(res.Added) != 1 || res.Added[0] != "kr" {
		t.Fatalf("second sync = %+v", res)
	}
	accounts, _ = store.ListAccounts(ctx)
	want := map[string]domain.AccountState{"us": domain.AccountMissing, "jp": domain.AccountActive, "kr": domain.AccountActive}
	for _, a := range accounts {
		if a.State != want[a.ID] {
			t.Errorf("%s state = %s, want %s", a.ID, a.State, want[a.ID])
		}
	}
	if accounts[1].Name != "jp@example.com" {
		t.Errorf("an empty name replaced the stored one: %+v", accounts[1])
	}
	if accounts[2].ID != "kr" || accounts[2].Position != 2 {
		t.Errorf("new account placed at %+v, want position 2", accounts[2])
	}
}

func TestUpdateAndDeleteAccount(t *testing.T) {
	store, err := Open(create6Database(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.SyncAccounts(ctx, AccountDiscovery{Seen: []SeenAccount{{ID: "a"}, {ID: "b"}, {ID: "c"}}, Complete: true}); err != nil {
		t.Fatal(err)
	}
	on, top := true, 0
	got, err := store.UpdateAccount(ctx, "c", AccountPatch{LibrarySync: &on, Position: &top})
	if err != nil || !got.LibrarySync || got.Position != 0 {
		t.Fatalf("update c = %+v, %v", got, err)
	}
	order := func() []string {
		accounts, err := store.ListAccounts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for i, a := range accounts {
			if a.Position != i {
				t.Errorf("%s at position %d, want %d", a.ID, a.Position, i)
			}
			ids = append(ids, a.ID)
		}
		return ids
	}
	if ids := order(); !reflect.DeepEqual(ids, []string{"c", "a", "b"}) {
		t.Fatalf("order = %v", ids)
	}
	if _, err := store.UpdateAccount(ctx, "nobody", AccountPatch{LibrarySync: &on}); err != ErrAccountNotFound {
		t.Fatalf("update unknown = %v", err)
	}

	// "a" adopted the unowned rows; deleting it drops them and moves "b" up.
	if err := store.DeleteAccount(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if ids := order(); !reflect.DeepEqual(ids, []string{"c", "b"}) {
		t.Fatalf("order after delete = %v", ids)
	}
	for _, table := range accountTables {
		var n int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s keeps %d rows after the delete (%v)", table, n, err)
		}
	}
	var albums int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM albums`).Scan(&albums); err != nil || albums != 1 {
		t.Errorf("albums = %d (%v), want the record kept", albums, err)
	}
	if err := store.DeleteAccount(ctx, "a"); err != ErrAccountNotFound {
		t.Fatalf("second delete = %v", err)
	}
}

// A record keeps the pin it was added with; a later add does not move it.
func TestRecordPinIsSetOnceAtAdd(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	if _, err := store.AddAlbum(ctx, AlbumAdd{ID: "9", WantedAll: true, Source: domain.SourceLibrarySync, Storefront: "JP", Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddAlbum(ctx, AlbumAdd{ID: "9", WantedAll: true, Source: domain.SourceManual, Storefront: "us", Now: now}); err != nil {
		t.Fatal(err)
	}
	album, err := store.GetAlbum(ctx, "9")
	if err != nil || album.PinnedStorefront != "jp" {
		t.Fatalf("pin = %q (%v), want jp", album.PinnedStorefront, err)
	}
	if _, err := store.AddPlaylist(ctx, PlaylistAdd{ID: "pl.1", Source: domain.SourceManual, Storefront: "kr", Now: now}); err != nil {
		t.Fatal(err)
	}
	if pl, err := store.GetPlaylist(ctx, "pl.1"); err != nil || pl.PinnedStorefront != "kr" {
		t.Fatalf("playlist pin = %q (%v)", pl.PinnedStorefront, err)
	}
	p, err := store.AddPending(ctx, domain.PendingAdd{Kind: domain.PendingSong, CatalogID: "5", Source: domain.SourceManual, Storefront: "jp", AddedAt: now})
	if err != nil || p.Storefront != "jp" {
		t.Fatalf("pending = %+v, %v", p, err)
	}
	p, err = store.AddPending(ctx, domain.PendingAdd{Kind: domain.PendingSong, CatalogID: "5", Source: domain.SourceManual, Storefront: "us", AddedAt: now})
	if err != nil || p.Storefront != "jp" {
		t.Fatalf("re-added pending = %+v, %v, want the first storefront kept", p, err)
	}
}
