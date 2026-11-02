package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"amdl/internal/domain"
)

// ErrRecordNotFound is returned when no album or playlist record has the id.
var ErrRecordNotFound = errors.New("record not found")

// Tombstoned returns which of ids were deleted as records of kind and not
// added again since.
func (s *Store) Tombstoned(ctx context.Context, kind domain.ResourceKind, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	args := []any{string(kind)}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT resource_id FROM deleted_resources WHERE resource_kind = ? AND resource_id IN (`+placeholders(len(ids))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// RecordSources returns why a record exists, oldest first.
func (s *Store) RecordSources(ctx context.Context, kind domain.ResourceKind, id string) ([]domain.RecordSource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source FROM resource_sources WHERE resource_kind = ? AND resource_id = ? ORDER BY added_at, source`, string(kind), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.RecordSource
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			return nil, err
		}
		out = append(out, domain.RecordSource(source))
	}
	return out, rows.Err()
}

// GetAlbum returns the album record with id, or ErrRecordNotFound.
func (s *Store) GetAlbum(ctx context.Context, id string) (domain.Album, error) {
	return getAlbum(ctx, s.db, id)
}

// GetPlaylist returns the playlist record with id, or ErrRecordNotFound.
func (s *Store) GetPlaylist(ctx context.Context, id string) (domain.Playlist, error) {
	return getPlaylist(ctx, s.db, id)
}

const albumColumns = `id, name, artist_name, artist_id, artist_url, release_date, genre, upc, content_rating, artwork, resolved_at,
	release_track_count, withheld_count, storefront_read, catalog_state, wanted_all, paused, priority, overrides, cover_state, artist_cover_state,
	tracks_wanted, tracks_done, tracks_failed, tracks_given_up, tracks_missing, state, added_at, refreshed_at, completed_at, updated_at`

func getAlbum(ctx context.Context, q rowQueryer, id string) (domain.Album, error) {
	var (
		a                                               domain.Album
		artwork, catalogState, overrides, cover, artist string
		state, added, updated                           string
		resolved, refreshed, completed                  sql.NullString
	)
	err := q.QueryRowContext(ctx, `SELECT `+albumColumns+` FROM albums WHERE id = ?`, id).Scan(
		&a.ID, &a.Name, &a.ArtistName, &a.ArtistID, &a.ArtistURL, &a.ReleaseDate, &a.Genre, &a.UPC, &a.ContentRating, &artwork, &resolved,
		&a.ReleaseTrackCount, &a.WithheldCount, &a.StorefrontRead, &catalogState, &a.WantedAll, &a.Paused, &a.Priority, &overrides, &cover, &artist,
		&a.TracksWanted, &a.TracksDone, &a.TracksFailed, &a.TracksGivenUp, &a.TracksMissing, &state, &added, &refreshed, &completed, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Album{}, ErrRecordNotFound
	}
	if err != nil {
		return domain.Album{}, err
	}
	if a.Artwork, err = decodeArtwork(artwork); err != nil {
		return domain.Album{}, fmt.Errorf("album %s: %w", id, err)
	}
	if a.Overrides, err = decodeOverrides(overrides); err != nil {
		return domain.Album{}, fmt.Errorf("album %s: %w", id, err)
	}
	a.CatalogState = domain.CatalogState(catalogState)
	a.CoverState, a.ArtistCoverState = domain.CoverState(cover), domain.CoverState(artist)
	a.State = domain.RecordState(state)
	a.ResolvedAt, a.RefreshedAt, a.CompletedAt = parseOptionalTime(resolved), parseOptionalTime(refreshed), parseOptionalTime(completed)
	a.AddedAt, a.UpdatedAt = parseTime(added), parseTime(updated)
	return a, nil
}

const playlistColumns = `id, name, curator_name, artwork, resolved_at, storefront_read, catalog_state, last_modified, paused, priority, overrides, cover_state,
	tracks_wanted, tracks_done, tracks_failed, tracks_given_up, tracks_missing, state, added_at, refreshed_at, completed_at, updated_at`

func getPlaylist(ctx context.Context, q rowQueryer, id string) (domain.Playlist, error) {
	var (
		p                                              domain.Playlist
		artwork, catalogState, overrides, cover, state string
		added, updated                                 string
		resolved, refreshed, completed                 sql.NullString
	)
	err := q.QueryRowContext(ctx, `SELECT `+playlistColumns+` FROM playlists WHERE id = ?`, id).Scan(
		&p.ID, &p.Name, &p.CuratorName, &artwork, &resolved, &p.StorefrontRead, &catalogState, &p.LastModified, &p.Paused, &p.Priority, &overrides, &cover,
		&p.TracksWanted, &p.TracksDone, &p.TracksFailed, &p.TracksGivenUp, &p.TracksMissing, &state, &added, &refreshed, &completed, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Playlist{}, ErrRecordNotFound
	}
	if err != nil {
		return domain.Playlist{}, err
	}
	if p.Artwork, err = decodeArtwork(artwork); err != nil {
		return domain.Playlist{}, fmt.Errorf("playlist %s: %w", id, err)
	}
	if p.Overrides, err = decodeOverrides(overrides); err != nil {
		return domain.Playlist{}, fmt.Errorf("playlist %s: %w", id, err)
	}
	p.CatalogState = domain.CatalogState(catalogState)
	p.CoverState = domain.CoverState(cover)
	p.State = domain.RecordState(state)
	p.ResolvedAt, p.RefreshedAt, p.CompletedAt = parseOptionalTime(resolved), parseOptionalTime(refreshed), parseOptionalTime(completed)
	p.AddedAt, p.UpdatedAt = parseTime(added), parseTime(updated)
	return p, nil
}

// ListAlbumTracks returns an album record's tracks in list order; tracks no
// read has listed yet (placeholders) come first.
func (s *Store) ListAlbumTracks(ctx context.Context, albumID string) ([]domain.Track, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT album_id, song_id, disc_number, track_number, song_number, name, artist_name, duration_ms,
			has_lyrics, isrc, wanted, availability, list_position
		FROM tracks WHERE album_id = ? ORDER BY list_position, song_id`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Track{}
	for rows.Next() {
		var t domain.Track
		var availability string
		if err := rows.Scan(&t.AlbumID, &t.SongID, &t.DiscNumber, &t.TrackNumber, &t.SongNumber, &t.Name, &t.ArtistName, &t.DurationMS,
			&t.HasLyrics, &t.ISRC, &t.Wanted, &availability, &t.ListPosition); err != nil {
			return nil, err
		}
		t.Availability = domain.Availability(availability)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListPlaylistTracks returns a playlist record's songs by position, removed
// ones included.
func (s *Store) ListPlaylistTracks(ctx context.Context, playlistID string) ([]domain.PlaylistTrack, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT playlist_id, song_id, position, name, artist_name, album_name, duration_ms, has_lyrics,
			availability, removed_at
		FROM playlist_tracks WHERE playlist_id = ? ORDER BY position, song_id`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PlaylistTrack{}
	for rows.Next() {
		var t domain.PlaylistTrack
		var availability string
		var removed sql.NullString
		if err := rows.Scan(&t.PlaylistID, &t.SongID, &t.Position, &t.Name, &t.ArtistName, &t.AlbumName, &t.DurationMS, &t.HasLyrics,
			&availability, &removed); err != nil {
			return nil, err
		}
		t.Availability = domain.Availability(availability)
		t.RemovedAt = parseOptionalTime(removed)
		out = append(out, t)
	}
	return out, rows.Err()
}

// RecordRef names a record and the class its runs get.
type RecordRef struct {
	Kind     domain.ResourceKind
	ID       string
	Priority int
}

// ListEligibleRecords returns the records the picker should queue a run for,
// by priority class then age: not paused, catalog state ok, no queued or
// running run, and either unresolved or with a wanted, available track whose
// slot is pending or failed and due at now.
func (s *Store) ListEligibleRecords(ctx context.Context, now time.Time) ([]RecordRef, error) {
	stamp := formatTime(now)
	rows, err := s.db.QueryContext(ctx, `SELECT 'album', a.id, a.priority, a.added_at FROM albums a
			WHERE a.paused = 0 AND a.catalog_state = 'ok'
			AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.resource_kind = 'album' AND r.resource_id = a.id AND r.status IN ('queued','running'))
			AND (a.resolved_at IS NULL OR EXISTS (SELECT 1 FROM files f JOIN tracks t ON t.album_id = f.owner_id AND t.song_id = f.song_id
				WHERE f.owner_kind = 'album' AND f.owner_id = a.id AND t.wanted = 1 AND t.availability = 'available'
				AND (f.audio_state = 'pending' OR (f.audio_state = 'failed' AND (f.next_attempt_at IS NULL OR f.next_attempt_at <= ?)))))
		UNION ALL
		SELECT 'playlist', p.id, p.priority, p.added_at FROM playlists p
			WHERE p.paused = 0 AND p.catalog_state = 'ok'
			AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.resource_kind = 'playlist' AND r.resource_id = p.id AND r.status IN ('queued','running'))
			AND (p.resolved_at IS NULL OR EXISTS (SELECT 1 FROM files f JOIN playlist_tracks t ON t.playlist_id = f.owner_id AND t.song_id = f.song_id
				WHERE f.owner_kind = 'playlist' AND f.owner_id = p.id AND t.removed_at IS NULL AND t.availability = 'available'
				AND (f.audio_state = 'pending' OR (f.audio_state = 'failed' AND (f.next_attempt_at IS NULL OR f.next_attempt_at <= ?)))))
		ORDER BY 3, 4, 2`, stamp, stamp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordRef
	for rows.Next() {
		var ref RecordRef
		var kind, added string
		if err := rows.Scan(&kind, &ref.ID, &ref.Priority, &added); err != nil {
			return nil, err
		}
		ref.Kind = domain.ResourceKind(kind)
		out = append(out, ref)
	}
	return out, rows.Err()
}

func decodeArtwork(raw string) (domain.Artwork, error) {
	var a domain.Artwork
	if strings.TrimSpace(raw) == "" {
		return a, nil
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return a, fmt.Errorf("decode artwork: %w", err)
	}
	return a, nil
}

// requireRow turns an UPDATE that matched nothing into ErrRecordNotFound.
func requireRow(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// withTx runs fn in a transaction and commits when it returns no error.
func withTx[T any](ctx context.Context, database *sql.DB, fn func(tx *sql.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer func() { _ = tx.Rollback() }()
	out, err := fn(tx)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, err
	}
	return out, nil
}
