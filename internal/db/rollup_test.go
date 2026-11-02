package db

import (
	"context"
	"testing"

	"amdl/internal/domain"
)

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
