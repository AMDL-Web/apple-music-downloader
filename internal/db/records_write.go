package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"amdl/internal/config"
	"amdl/internal/domain"
)

// ErrRunActive is returned by writes a running or queued run of the record
// would race with: reset and delete. Pause the record first.
var ErrRunActive = errors.New("record has an active run")

// AlbumAdd is one add of an album record: an album link, a search hit, the
// library monitor, an artist link, or a song whose album is known.
type AlbumAdd struct {
	ID string
	// WantedAll marks the whole album wanted; otherwise only SongIDs are.
	WantedAll bool
	// SongIDs are tracks wanted one by one. A song not yet in the track list
	// gets a placeholder row the first read fills in or marks unavailable.
	SongIDs []string
	Source  domain.RecordSource
	// Priority is the record's class (domain.PriorityManual or PriorityAuto);
	// zero means manual. An existing record keeps the higher class.
	Priority int
	// Overrides replaces the record's overrides on a manual add and on a new
	// record.
	Overrides *config.DownloadOverrides
	// Paused sets the record's paused flag when non-nil; nil leaves an
	// existing record's flag alone and makes a new one unpaused.
	Paused *bool
	Now    time.Time
}

// PlaylistAdd is one add of a catalog playlist record.
type PlaylistAdd struct {
	ID        string
	Source    domain.RecordSource
	Priority  int
	Overrides *config.DownloadOverrides
	Paused    *bool
	Now       time.Time
}

// AddAlbum creates or extends an album record in one transaction: the record
// row, the wanted tracks, a pending main slot for each wanted, available
// track that has none, the source, and the rollup. Any add other than the
// library monitor's clears the album's tombstone. The record is left
// unresolved when it is new; ApplyAlbumRead resolves it.
func (s *Store) AddAlbum(ctx context.Context, add AlbumAdd) (domain.Album, error) {
	return withTx(ctx, s.db, func(tx *sql.Tx) (domain.Album, error) {
		if err := upsertRecord(ctx, tx, domain.ResourceAlbum, add.ID, add.Source, add.Priority, add.Overrides, add.Paused, add.Now); err != nil {
			return domain.Album{}, err
		}
		stamp := formatTime(add.Now)
		if add.WantedAll {
			if _, err := tx.ExecContext(ctx, `UPDATE albums SET wanted_all = 1 WHERE id = ?`, add.ID); err != nil {
				return domain.Album{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE tracks SET wanted = 1 WHERE album_id = ?`, add.ID); err != nil {
				return domain.Album{}, err
			}
		}
		for _, songID := range add.SongIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO tracks(album_id, song_id, wanted) VALUES(?, ?, 1)
				ON CONFLICT(album_id, song_id) DO UPDATE SET wanted = 1`, add.ID, songID); err != nil {
				return domain.Album{}, fmt.Errorf("want song %s of album %s: %w", songID, add.ID, err)
			}
		}
		if err := ensureMainSlots(ctx, tx, domain.ResourceAlbum, add.ID, stamp); err != nil {
			return domain.Album{}, err
		}
		if err := recomputeRollup(ctx, tx, domain.ResourceAlbum, add.ID, add.Now); err != nil {
			return domain.Album{}, err
		}
		return getAlbum(ctx, tx, add.ID)
	})
}

// AddPlaylist creates or updates a playlist record, as AddAlbum does.
func (s *Store) AddPlaylist(ctx context.Context, add PlaylistAdd) (domain.Playlist, error) {
	return withTx(ctx, s.db, func(tx *sql.Tx) (domain.Playlist, error) {
		if err := upsertRecord(ctx, tx, domain.ResourcePlaylist, add.ID, add.Source, add.Priority, add.Overrides, add.Paused, add.Now); err != nil {
			return domain.Playlist{}, err
		}
		if err := ensureMainSlots(ctx, tx, domain.ResourcePlaylist, add.ID, formatTime(add.Now)); err != nil {
			return domain.Playlist{}, err
		}
		if err := recomputeRollup(ctx, tx, domain.ResourcePlaylist, add.ID, add.Now); err != nil {
			return domain.Playlist{}, err
		}
		return getPlaylist(ctx, tx, add.ID)
	})
}

func upsertRecord(ctx context.Context, tx *sql.Tx, kind domain.ResourceKind, id string, source domain.RecordSource,
	priority int, overrides *config.DownloadOverrides, paused *bool, now time.Time) error {
	if id == "" {
		return fmt.Errorf("add %s: empty id", kind)
	}
	if source == "" {
		return fmt.Errorf("add %s %s: empty source", kind, id)
	}
	table, err := recordTable(kind)
	if err != nil {
		return err
	}
	if priority == 0 {
		priority = domain.PriorityManual
	}
	encoded, err := encodeOverrides(overrides)
	if err != nil {
		return err
	}
	stamp := formatTime(now)
	newPaused := paused != nil && *paused
	_, err = tx.ExecContext(ctx, `INSERT INTO `+table+`(id, paused, priority, overrides, added_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			paused = CASE WHEN ? THEN excluded.paused ELSE paused END,
			priority = MIN(priority, excluded.priority),
			overrides = CASE WHEN ? THEN excluded.overrides ELSE overrides END,
			updated_at = excluded.updated_at`,
		id, newPaused, priority, encoded, stamp, stamp,
		paused != nil, source == domain.SourceManual)
	if err != nil {
		return fmt.Errorf("add %s %s: %w", kind, id, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO resource_sources(resource_kind, resource_id, source, added_at) VALUES(?, ?, ?, ?)`,
		string(kind), id, string(source), stamp); err != nil {
		return fmt.Errorf("add source of %s %s: %w", kind, id, err)
	}
	if source != domain.SourceLibrarySync {
		if _, err := tx.ExecContext(ctx, `DELETE FROM deleted_resources WHERE resource_kind = ? AND resource_id = ?`, string(kind), id); err != nil {
			return fmt.Errorf("clear tombstone of %s %s: %w", kind, id, err)
		}
	}
	return nil
}

// ensureMainSlots gives every wanted, available track of the record a main
// slot, pending, unless it has one.
func ensureMainSlots(ctx context.Context, tx *sql.Tx, kind domain.ResourceKind, id, stamp string) error {
	var query string
	switch kind {
	case domain.ResourceAlbum:
		query = `INSERT OR IGNORE INTO files(owner_kind, owner_id, song_id, slot, audio_state, updated_at)
			SELECT 'album', album_id, song_id, 'main', 'pending', ? FROM tracks
			WHERE album_id = ? AND wanted = 1 AND availability = 'available'`
	case domain.ResourcePlaylist:
		query = `INSERT OR IGNORE INTO files(owner_kind, owner_id, song_id, slot, audio_state, updated_at)
			SELECT 'playlist', playlist_id, song_id, 'main', 'pending', ? FROM playlist_tracks
			WHERE playlist_id = ? AND removed_at IS NULL AND availability = 'available'`
	default:
		return fmt.Errorf("unknown record kind %q", kind)
	}
	if _, err := tx.ExecContext(ctx, query, stamp, id); err != nil {
		return fmt.Errorf("plan main slots of %s %s: %w", kind, id, err)
	}
	return nil
}

// AlbumRead is a track-list read of an album made in the account storefront.
type AlbumRead struct {
	// Album carries the metadata columns: Name through ContentRating,
	// Artwork and ReleaseTrackCount. Its other fields are ignored.
	Album      domain.Album
	Tracks     []domain.Track
	Storefront string
	Now        time.Time
}

// ApplyAlbumRead refreshes an album record from a read in one transaction:
// metadata, the track list (a new track is wanted when the whole album is,
// a listed one is available at its list position, an unlisted one becomes
// unavailable and keeps its row and files), withheld_count, the main slots
// and the rollup. The record becomes resolved and its catalog_state ok.
func (s *Store) ApplyAlbumRead(ctx context.Context, read AlbumRead) error {
	_, err := withTx(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		a := read.Album
		// A track-list read carries no animated cover; keep the one the
		// motion-artwork backfill stored (SetAlbumMotionArtwork).
		if a.Artwork.MotionSquareURL == "" && a.Artwork.MotionTallURL == "" {
			var stored string
			if err := tx.QueryRowContext(ctx, `SELECT artwork FROM albums WHERE id = ?`, a.ID).Scan(&stored); err == nil {
				if prev, err := decodeArtwork(stored); err == nil {
					keepMotion(&a.Artwork, prev)
				}
			}
		}
		artwork, err := json.Marshal(a.Artwork)
		if err != nil {
			return struct{}{}, err
		}
		stamp := formatTime(read.Now)
		withheld := max(a.ReleaseTrackCount-len(read.Tracks), 0)
		res, err := tx.ExecContext(ctx, `UPDATE albums SET name = ?, artist_name = ?, artist_id = ?, artist_url = ?, release_date = ?, genre = ?,
				upc = ?, content_rating = ?, artwork = ?, release_track_count = ?, withheld_count = ?, storefront_read = ?,
				catalog_state = 'ok', resolved_at = ?, refreshed_at = ?, updated_at = ?
			WHERE id = ?`,
			a.Name, a.ArtistName, a.ArtistID, a.ArtistURL, a.ReleaseDate, a.Genre, a.UPC, a.ContentRating, string(artwork),
			a.ReleaseTrackCount, withheld, read.Storefront, stamp, stamp, stamp, a.ID)
		if err := requireRow(res, err); err != nil {
			return struct{}{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tracks SET availability = 'unavailable' WHERE album_id = ?`, a.ID); err != nil {
			return struct{}{}, err
		}
		for i, t := range read.Tracks {
			if _, err := tx.ExecContext(ctx, `INSERT INTO tracks(album_id, song_id, disc_number, track_number, song_number, name, artist_name,
					duration_ms, has_lyrics, isrc, wanted, availability, list_position)
				VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT wanted_all FROM albums WHERE id = ?), 'available', ?)
				ON CONFLICT(album_id, song_id) DO UPDATE SET disc_number = excluded.disc_number, track_number = excluded.track_number,
					song_number = excluded.song_number, name = excluded.name, artist_name = excluded.artist_name,
					duration_ms = excluded.duration_ms, has_lyrics = excluded.has_lyrics, isrc = excluded.isrc,
					availability = 'available', list_position = excluded.list_position`,
				a.ID, t.SongID, t.DiscNumber, t.TrackNumber, t.SongNumber, t.Name, t.ArtistName, t.DurationMS, t.HasLyrics, t.ISRC,
				a.ID, i+1); err != nil {
				return struct{}{}, fmt.Errorf("store track %s of album %s: %w", t.SongID, a.ID, err)
			}
		}
		if err := ensureMainSlots(ctx, tx, domain.ResourceAlbum, a.ID, stamp); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, recomputeRollup(ctx, tx, domain.ResourceAlbum, a.ID, read.Now)
	})
	return err
}

// PlaylistRead is a track-list read of a playlist made in the account
// storefront.
type PlaylistRead struct {
	// Playlist carries Name, CuratorName, Artwork and LastModified.
	Playlist   domain.Playlist
	Tracks     []domain.PlaylistTrack
	Storefront string
	Now        time.Time
}

// ApplyPlaylistRead refreshes a playlist record from a read in one
// transaction, as ApplyAlbumRead does. Every listed song is wanted, at its
// Position (its 1-based index in Tracks when zero), with the availability the
// read gives it (available when empty). A song gone from the list gets removed_at and keeps its row and
// files; one that comes back loses it.
func (s *Store) ApplyPlaylistRead(ctx context.Context, read PlaylistRead) error {
	_, err := withTx(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		p := read.Playlist
		artwork, err := json.Marshal(p.Artwork)
		if err != nil {
			return struct{}{}, err
		}
		stamp := formatTime(read.Now)
		res, err := tx.ExecContext(ctx, `UPDATE playlists SET name = ?, curator_name = ?, artwork = ?, last_modified = ?, storefront_read = ?,
				catalog_state = 'ok', resolved_at = ?, refreshed_at = ?, updated_at = ?
			WHERE id = ?`,
			p.Name, p.CuratorName, string(artwork), p.LastModified, read.Storefront, stamp, stamp, stamp, p.ID)
		if err := requireRow(res, err); err != nil {
			return struct{}{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE playlist_tracks SET removed_at = COALESCE(removed_at, ?) WHERE playlist_id = ?`, stamp, p.ID); err != nil {
			return struct{}{}, err
		}
		for i, t := range read.Tracks {
			availability := t.Availability
			if availability == "" {
				availability = domain.Available
			}
			position := t.Position
			if position == 0 {
				position = i + 1
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_tracks(playlist_id, song_id, position, name, artist_name, album_name,
					duration_ms, has_lyrics, availability, removed_at)
				VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
				ON CONFLICT(playlist_id, song_id) DO UPDATE SET position = excluded.position, name = excluded.name,
					artist_name = excluded.artist_name, album_name = excluded.album_name, duration_ms = excluded.duration_ms,
					has_lyrics = excluded.has_lyrics, availability = excluded.availability, removed_at = NULL`,
				p.ID, t.SongID, position, t.Name, t.ArtistName, t.AlbumName, t.DurationMS, t.HasLyrics, string(availability)); err != nil {
				return struct{}{}, fmt.Errorf("store track %s of playlist %s: %w", t.SongID, p.ID, err)
			}
		}
		if err := ensureMainSlots(ctx, tx, domain.ResourcePlaylist, p.ID, stamp); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, recomputeRollup(ctx, tx, domain.ResourcePlaylist, p.ID, read.Now)
	})
	return err
}

// keepMotion copies prev's animated covers into a.
func keepMotion(a *domain.Artwork, prev domain.Artwork) {
	a.MotionSquareURL, a.MotionTallURL = prev.MotionSquareURL, prev.MotionTallURL
	a.MotionSquareColors, a.MotionTallColors = prev.MotionSquareColors, prev.MotionTallColors
}

// SetAlbumMotionArtwork stores an album's animated covers in its artwork,
// keeping the still cover. ErrRecordNotFound when the album is gone.
func (s *Store) SetAlbumMotionArtwork(ctx context.Context, id string, motion domain.Artwork, now time.Time) error {
	_, err := withTx(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		var stored string
		err := tx.QueryRowContext(ctx, `SELECT artwork FROM albums WHERE id = ?`, id).Scan(&stored)
		if errors.Is(err, sql.ErrNoRows) {
			return struct{}{}, ErrRecordNotFound
		}
		if err != nil {
			return struct{}{}, err
		}
		artwork, err := decodeArtwork(stored)
		if err != nil {
			return struct{}{}, err
		}
		keepMotion(&artwork, motion)
		raw, err := json.Marshal(artwork)
		if err != nil {
			return struct{}{}, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE albums SET artwork = ?, updated_at = ? WHERE id = ?`, string(raw), formatTime(now), id)
		return struct{}{}, err
	})
	return err
}

// SetCoverStates records the outcome of a record's cover.<ext> and, for an
// album, artist.<ext>. An empty state leaves that column as it is; a
// playlist has no artist cover.
func (s *Store) SetCoverStates(ctx context.Context, kind domain.ResourceKind, id string, cover, artist domain.CoverState, now time.Time) error {
	table, err := recordTable(kind)
	if err != nil {
		return err
	}
	set, args := []string{"updated_at = ?"}, []any{formatTime(now)}
	if cover != "" {
		set, args = append(set, "cover_state = ?"), append(args, string(cover))
	}
	if artist != "" && kind == domain.ResourceAlbum {
		set, args = append(set, "artist_cover_state = ?"), append(args, string(artist))
	}
	res, err := s.db.ExecContext(ctx, `UPDATE `+table+` SET `+strings.Join(set, ", ")+` WHERE id = ?`, append(args, id)...)
	return requireRow(res, err)
}

// SetCatalogState records what a required read said about a record.
func (s *Store) SetCatalogState(ctx context.Context, kind domain.ResourceKind, id string, state domain.CatalogState, now time.Time) error {
	table, err := recordTable(kind)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE `+table+` SET catalog_state = ?, updated_at = ? WHERE id = ?`, string(state), formatTime(now), id)
	return requireRow(res, err)
}

// SetPaused sets a record's paused flag. The picker skips paused records.
func (s *Store) SetPaused(ctx context.Context, kind domain.ResourceKind, id string, paused bool, now time.Time) error {
	table, err := recordTable(kind)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE `+table+` SET paused = ?, updated_at = ? WHERE id = ?`, paused, formatTime(now), id)
	return requireRow(res, err)
}

// DeleteRecord removes a record with its tracks, files rows and sources and
// leaves a tombstone, in one transaction. Runs and files on disk stay. A
// record with a queued or running run is refused with ErrRunActive.
func (s *Store) DeleteRecord(ctx context.Context, kind domain.ResourceKind, id string, now time.Time) error {
	_, err := withTx(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		table, err := recordTable(kind)
		if err != nil {
			return struct{}{}, err
		}
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM `+table+` WHERE id = ?`, id).Scan(&name); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return struct{}{}, ErrRecordNotFound
			}
			return struct{}{}, err
		}
		if err := refuseActiveRun(ctx, tx, kind, id); err != nil {
			return struct{}{}, err
		}
		trackTable, ownerColumn := "tracks", "album_id"
		if kind == domain.ResourcePlaylist {
			trackTable, ownerColumn = "playlist_tracks", "playlist_id"
		}
		for _, stmt := range []string{
			`DELETE FROM files WHERE owner_kind = '` + string(kind) + `' AND owner_id = ?`,
			`DELETE FROM ` + trackTable + ` WHERE ` + ownerColumn + ` = ?`,
			`DELETE FROM resource_sources WHERE resource_kind = '` + string(kind) + `' AND resource_id = ?`,
			`DELETE FROM ` + table + ` WHERE id = ?`,
		} {
			if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
				return struct{}{}, fmt.Errorf("delete %s %s: %w", kind, id, err)
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO deleted_resources(resource_kind, resource_id, name, deleted_at) VALUES(?, ?, ?, ?)
			ON CONFLICT(resource_kind, resource_id) DO UPDATE SET name = excluded.name, deleted_at = excluded.deleted_at`,
			string(kind), id, name, formatTime(now))
		return struct{}{}, err
	})
	return err
}

func refuseActiveRun(ctx context.Context, q rowQueryer, kind domain.ResourceKind, id string) error {
	var active bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE resource_kind = ? AND resource_id = ? AND status IN ('queued','running'))`,
		string(kind), id).Scan(&active); err != nil {
		return err
	}
	if active {
		return ErrRunActive
	}
	return nil
}
