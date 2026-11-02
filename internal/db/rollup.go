package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"amdl/internal/domain"
)

// rowQueryer is satisfied by *sql.DB and *sql.Tx.
type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// recordTable is the table that holds records of kind.
func recordTable(kind domain.ResourceKind) (string, error) {
	switch kind {
	case domain.ResourceAlbum:
		return "albums", nil
	case domain.ResourcePlaylist:
		return "playlists", nil
	}
	return "", fmt.Errorf("unknown record kind %q", kind)
}

// rollupCountsSQL counts a record's wanted, available tracks and, of those,
// the ones whose main slot is done and the ones with any slot failed, given
// up or missing. Its one parameter is the record id.
var rollupCountsSQL = map[domain.ResourceKind]string{
	domain.ResourceAlbum: `SELECT COUNT(*),
			COALESCE(SUM(EXISTS(SELECT 1 FROM files f WHERE f.owner_kind = 'album' AND f.owner_id = t.album_id AND f.song_id = t.song_id
				AND f.slot = 'main' AND f.audio_state = 'done')), 0),
			COALESCE(SUM(EXISTS(SELECT 1 FROM files f WHERE f.owner_kind = 'album' AND f.owner_id = t.album_id AND f.song_id = t.song_id
				AND f.audio_state = 'failed')), 0),
			COALESCE(SUM(EXISTS(SELECT 1 FROM files f WHERE f.owner_kind = 'album' AND f.owner_id = t.album_id AND f.song_id = t.song_id
				AND f.audio_state = 'given_up')), 0),
			COALESCE(SUM(EXISTS(SELECT 1 FROM files f WHERE f.owner_kind = 'album' AND f.owner_id = t.album_id AND f.song_id = t.song_id
				AND f.audio_state = 'missing')), 0)
		FROM tracks t WHERE t.album_id = ? AND t.wanted = 1 AND t.availability = 'available'`,
	domain.ResourcePlaylist: `SELECT COUNT(*),
			COALESCE(SUM(EXISTS(SELECT 1 FROM files f WHERE f.owner_kind = 'playlist' AND f.owner_id = t.playlist_id AND f.song_id = t.song_id
				AND f.slot = 'main' AND f.audio_state = 'done')), 0),
			COALESCE(SUM(EXISTS(SELECT 1 FROM files f WHERE f.owner_kind = 'playlist' AND f.owner_id = t.playlist_id AND f.song_id = t.song_id
				AND f.audio_state = 'failed')), 0),
			COALESCE(SUM(EXISTS(SELECT 1 FROM files f WHERE f.owner_kind = 'playlist' AND f.owner_id = t.playlist_id AND f.song_id = t.song_id
				AND f.audio_state = 'given_up')), 0),
			COALESCE(SUM(EXISTS(SELECT 1 FROM files f WHERE f.owner_kind = 'playlist' AND f.owner_id = t.playlist_id AND f.song_id = t.song_id
				AND f.audio_state = 'missing')), 0)
		FROM playlist_tracks t WHERE t.playlist_id = ? AND t.removed_at IS NULL AND t.availability = 'available'`,
}

// computeRollup derives a record's rollup from its tracks and files. The
// stored columns must always equal it (recomputeRollup).
func computeRollup(ctx context.Context, q rowQueryer, kind domain.ResourceKind, id string) (domain.Rollup, error) {
	table, err := recordTable(kind)
	if err != nil {
		return domain.Rollup{}, err
	}
	var resolved bool
	if err := q.QueryRowContext(ctx, `SELECT resolved_at IS NOT NULL FROM `+table+` WHERE id = ?`, id).Scan(&resolved); err != nil {
		return domain.Rollup{}, fmt.Errorf("read %s %s: %w", kind, id, err)
	}
	var r domain.Rollup
	if err := q.QueryRowContext(ctx, rollupCountsSQL[kind], id).Scan(
		&r.TracksWanted, &r.TracksDone, &r.TracksFailed, &r.TracksGivenUp, &r.TracksMissing); err != nil {
		return domain.Rollup{}, fmt.Errorf("count tracks of %s %s: %w", kind, id, err)
	}
	switch {
	case r.TracksWanted == 0 || r.TracksDone == 0:
		r.State = domain.RecordNone
	case !resolved:
		// Files an unresolved record already holds (the 1.x import) count,
		// but its track list is not read yet, so it cannot be complete.
		r.State = domain.RecordPartial
	case r.TracksDone == r.TracksWanted:
		r.State = domain.RecordComplete
	default:
		r.State = domain.RecordPartial
	}
	return r, nil
}

// recomputeRollup stores the record's rollup. Every write to a record's
// tracks or files calls it in the same transaction. completed_at is set when
// the record becomes complete and cleared when it stops being complete.
func recomputeRollup(ctx context.Context, tx *sql.Tx, kind domain.ResourceKind, id string, now time.Time) error {
	r, err := computeRollup(ctx, tx, kind, id)
	if err != nil {
		return err
	}
	table, _ := recordTable(kind)
	_, err = tx.ExecContext(ctx, `UPDATE `+table+` SET tracks_wanted = ?, tracks_done = ?, tracks_failed = ?, tracks_given_up = ?, tracks_missing = ?,
			completed_at = CASE WHEN ? = 'complete' THEN COALESCE(CASE WHEN state = 'complete' THEN completed_at END, ?) END,
			state = ?
		WHERE id = ?`,
		r.TracksWanted, r.TracksDone, r.TracksFailed, r.TracksGivenUp, r.TracksMissing,
		string(r.State), formatTime(now), string(r.State), id)
	if err != nil {
		return fmt.Errorf("store rollup of %s %s: %w", kind, id, err)
	}
	return nil
}
