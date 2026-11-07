package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"amdl/internal/domain"
)

// openStore opens a fresh store in a temporary directory, with file paths
// stored relative to /music, and closes it when the test ends.
func openStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenWith(filepath.Join(t.TempDir(), "amdl.db"), Options{DownloadsDir: "/music"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// createRawDatabase writes a database by running scripts on a plain sqlite
// connection, without any migration, and returns its path. It stands for a
// file an older release left behind.
func createRawDatabase(t *testing.T, scripts ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "amdl.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	for _, script := range scripts {
		if _, err := raw.Exec(script); err != nil {
			t.Fatalf("build database: %v", err)
		}
	}
	return path
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// assertRollupsConsistent checks that every record's stored rollup equals the
// one its tracks and files give.
func assertRollupsConsistent(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	for _, kind := range []domain.ResourceKind{domain.ResourceAlbum, domain.ResourcePlaylist} {
		table, _ := recordTable(kind)
		rows, err := store.db.QueryContext(ctx, `SELECT id, tracks_wanted, tracks_done, tracks_failed, tracks_given_up, tracks_missing, state, completed_at IS NOT NULL FROM `+table)
		if err != nil {
			t.Fatal(err)
		}
		type stored struct {
			id        string
			rollup    domain.Rollup
			completed bool
		}
		var all []stored
		for rows.Next() {
			var s stored
			var state string
			if err := rows.Scan(&s.id, &s.rollup.TracksWanted, &s.rollup.TracksDone, &s.rollup.TracksFailed, &s.rollup.TracksGivenUp,
				&s.rollup.TracksMissing, &state, &s.completed); err != nil {
				t.Fatal(err)
			}
			s.rollup.State = domain.RecordState(state)
			all = append(all, s)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		for _, s := range all {
			want, err := computeRollup(ctx, store.db, kind, s.id)
			if err != nil {
				t.Fatal(err)
			}
			if s.rollup != want {
				t.Errorf("%s %s stored rollup %+v, recomputed %+v", kind, s.id, s.rollup, want)
			}
			if s.completed != (want.State == domain.RecordComplete) {
				t.Errorf("%s %s completed_at set = %v with state %s", kind, s.id, s.completed, want.State)
			}
		}
	}
}
