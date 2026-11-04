package db

import (
	"context"
	"database/sql"
	"reflect"
	"strconv"
	"testing"
	"time"

	"amdl/internal/domain"
)

// create8Database is the 1.x fixture migrated to schema 8, plus records the
// way schema 8 rolled them up: an album whose second track no codec offers
// (partial, 1 of 2), an album whose only track no codec offers (none, 0 of
// 1), a complete album with such an extra slot, a playlist like the first
// album, and an untouched partial album.
func create8Database(t *testing.T) string {
	t.Helper()
	path := create1xDatabase(t)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	if err := migrateWith(context.Background(), raw, 8, migrationEnv{downloadsDir: "/music", now: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	const stamp = "2026-11-03T00:00:00.000Z"
	album := func(id, state string, wanted, done int) string {
		return `INSERT INTO albums(id, name, resolved_at, wanted_all, tracks_wanted, tracks_done, state, added_at, updated_at)
			VALUES('` + id + `', 'A', '` + stamp + `', 1, ` + strconv.Itoa(wanted) + `, ` + strconv.Itoa(done) + `, '` + state + `', '` + stamp + `', '` + stamp + `')`
	}
	track := func(album, song string) string {
		return `INSERT INTO tracks(album_id, song_id, wanted, availability) VALUES('` + album + `', '` + song + `', 1, 'available')`
	}
	file := func(kind, owner, song, slot, state string) string {
		return `INSERT INTO files(owner_kind, owner_id, song_id, slot, audio_state, updated_at)
			VALUES('` + kind + `', '` + owner + `', '` + song + `', '` + slot + `', '` + state + `', '` + stamp + `')`
	}
	for _, stmt := range []string{
		album("u1", "partial", 2, 1), track("u1", "1"), track("u1", "2"),
		file("album", "u1", "1", "main", "done"), file("album", "u1", "2", "main", "unavailable"),
		album("u2", "none", 1, 0), track("u2", "3"), file("album", "u2", "3", "main", "unavailable"),
		album("u3", "partial", 2, 1), track("u3", "4"), track("u3", "5"),
		file("album", "u3", "4", "main", "done"), file("album", "u3", "5", "main", "pending"),
		`INSERT INTO playlists(id, name, resolved_at, tracks_wanted, tracks_done, state, added_at, updated_at)
			VALUES('pl.u', 'P', '` + stamp + `', 2, 1, 'partial', '` + stamp + `', '` + stamp + `')`,
		`INSERT INTO playlist_tracks(playlist_id, song_id, position) VALUES('pl.u', '6', 1), ('pl.u', '7', 2)`,
		file("playlist", "pl.u", "6", "main", "done"), file("playlist", "pl.u", "7", "main", "unavailable"),
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	return path
}

// Migration 9 recomputes the rollup of the records that hold a main slot no
// codec offers, as computeRollup now counts it, changes no other value or
// row, and is a no-op on reopen.
func TestMigration9RecountsRecordsWithUnofferedTracks(t *testing.T) {
	path := create8Database(t)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	before := dumpAll(t, raw)
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("open schema 8: %v", err)
	}
	if got := userVersion(t, store.db); got != 9 {
		t.Fatalf("user_version = %d, want 9", got)
	}
	after := dumpAll(t, store.db)
	rollup := map[string]bool{"tracks_wanted": true, "tracks_done": true, "tracks_failed": true, "tracks_given_up": true,
		"tracks_missing": true, "state": true, "completed_at": true}
	recounted := map[string]bool{"albums/u1": true, "albums/u2": true, "playlists/pl.u": true}
	for table, rows := range before {
		if len(after[table]) != len(rows) {
			t.Errorf("%s has %d rows, want %d", table, len(after[table]), len(rows))
		}
		for id, old := range rows {
			for col, value := range old {
				if recounted[table+"/"+id] && rollup[col] {
					continue
				}
				if !reflect.DeepEqual(after[table][id][col], value) {
					t.Errorf("%s %s.%s = %#v, want %#v", table, id, col, after[table][id][col], value)
				}
			}
		}
	}
	ctx := context.Background()
	for _, want := range []struct {
		kind         domain.ResourceKind
		id           string
		state        domain.RecordState
		wanted, done int
		completed    bool
	}{
		{domain.ResourceAlbum, "u1", domain.RecordComplete, 1, 1, true},
		{domain.ResourceAlbum, "u2", domain.RecordNone, 0, 0, false},
		{domain.ResourceAlbum, "u3", domain.RecordPartial, 2, 1, false},
		{domain.ResourcePlaylist, "pl.u", domain.RecordComplete, 1, 1, true},
	} {
		var r domain.Rollup
		var completed sql.NullString
		table, _ := recordTable(want.kind)
		if err := store.db.QueryRowContext(ctx, `SELECT tracks_wanted, tracks_done, state, completed_at FROM `+table+` WHERE id = ?`, want.id).
			Scan(&r.TracksWanted, &r.TracksDone, &r.State, &completed); err != nil {
			t.Fatal(err)
		}
		if r.State != want.state || r.TracksWanted != want.wanted || r.TracksDone != want.done || completed.Valid != want.completed {
			t.Errorf("%s %s = %+v completed %v, want %s %d/%d completed %v", want.kind, want.id, r, completed.Valid, want.state, want.done, want.wanted, want.completed)
		}
		stored := domain.Rollup{TracksWanted: r.TracksWanted, TracksDone: r.TracksDone, State: r.State}
		computed, err := computeRollup(ctx, store.db, want.kind, want.id)
		if err != nil {
			t.Fatal(err)
		}
		computed.TracksFailed, computed.TracksGivenUp, computed.TracksMissing = 0, 0, 0
		if stored != computed {
			t.Errorf("%s %s stored %+v, computeRollup %+v", want.kind, want.id, stored, computed)
		}
	}

	first := dumpAll(t, store.db)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store.Close()
	if second := dumpAll(t, store.db); !reflect.DeepEqual(first, second) {
		t.Fatal("reopening the migrated database changed its rows")
	}
	if _, err := store.db.ExecContext(ctx, `PRAGMA user_version = 8`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, store.db, 9); err != nil {
		t.Fatal(err)
	}
	if third := dumpAll(t, store.db); !reflect.DeepEqual(first, third) {
		t.Fatal("running migration 9 again changed its rows")
	}
}
