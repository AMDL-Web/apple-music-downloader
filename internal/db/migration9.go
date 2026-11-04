package db

import (
	"context"
	"database/sql"
	"strings"
)

// migrateTo9 recomputes the rollup of each record that holds a main slot no
// codec offers (audio_state unavailable): such a track no longer counts as
// wanted (computeRollup), and a record that counted it before stays partial
// until something writes to it. Only the rollup columns (tracks_*, state,
// completed_at) of those records change; no row is added or removed, and a
// second run computes the same values. Frozen like every shipped migration.
func migrateTo9(ctx context.Context, tx *sql.Tx, env migrationEnv) error {
	now := formatTime(env.now)
	for _, stmt := range []string{
		migration9Rollup("albums", "tracks", "album_id", "t.wanted = 1", "album"),
		migration9Rollup("playlists", "playlist_tracks", "playlist_id", "t.removed_at IS NULL", "playlist"),
	} {
		if _, err := tx.ExecContext(ctx, stmt, now); err != nil {
			return err
		}
	}
	return nil
}

// migration9Rollup is the UPDATE of migrateTo9 for one record table: the
// rollup as computeRollup derived it when this migration shipped. Its one
// parameter is the time a record that becomes complete is stamped with.
func migration9Rollup(table, tracks, owner, listed, kind string) string {
	counted := `FROM ` + tracks + ` t WHERE t.` + owner + ` = r.id AND ` + listed + ` AND t.availability = 'available'
		AND NOT EXISTS (SELECT 1 FROM files f WHERE f.owner_kind = '` + kind + `' AND f.owner_id = r.id AND f.song_id = t.song_id
			AND f.slot = 'main' AND f.audio_state = 'unavailable')`
	slot := func(cond string) string {
		return `(SELECT COUNT(*) ` + counted + ` AND EXISTS (SELECT 1 FROM files f WHERE f.owner_kind = '` + kind +
			`' AND f.owner_id = r.id AND f.song_id = t.song_id AND ` + cond + `))`
	}
	state := `CASE WHEN c.wanted = 0 OR c.done = 0 THEN 'none' WHEN ` + table + `.resolved_at IS NULL THEN 'partial'
		WHEN c.done = c.wanted THEN 'complete' ELSE 'partial' END`
	return strings.Join([]string{
		`UPDATE ` + table + ` SET tracks_wanted = c.wanted, tracks_done = c.done, tracks_failed = c.failed,
			tracks_given_up = c.given_up, tracks_missing = c.missing,
			completed_at = CASE WHEN ` + state + ` = 'complete'
				THEN COALESCE(CASE WHEN ` + table + `.state = 'complete' THEN ` + table + `.completed_at END, ?1) END,
			state = ` + state,
		`FROM (SELECT r.id AS id, (SELECT COUNT(*) ` + counted + `) AS wanted,
			` + slot(`f.slot = 'main' AND f.audio_state = 'done'`) + ` AS done,
			` + slot(`f.audio_state = 'failed'`) + ` AS failed,
			` + slot(`f.audio_state = 'given_up'`) + ` AS given_up,
			` + slot(`f.audio_state = 'missing'`) + ` AS missing
			FROM ` + table + ` r WHERE EXISTS (SELECT 1 FROM files u WHERE u.owner_kind = '` + kind + `' AND u.owner_id = r.id
				AND u.slot = 'main' AND u.audio_state = 'unavailable')) AS c`,
		`WHERE ` + table + `.id = c.id`,
	}, "\n")
}
