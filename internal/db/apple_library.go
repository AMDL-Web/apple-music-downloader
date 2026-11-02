package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"amdl/internal/domain"
)

// AppleLibraryQuery selects a page of one kind of the cached Apple library.
// Sort is "added", "name" or "artist"; Q filters by a substring of the name,
// artist or album name (case-insensitive for ASCII letters).
type AppleLibraryQuery struct {
	Kind   string
	Sort   string
	Desc   bool
	Q      string
	Limit  int
	Offset int
}

// AppleLibraryUpsert reports what UpsertAppleLibraryItems changed.
type AppleLibraryUpsert struct {
	Added   int
	Changed int
}

// AppleLibraryPlaylistState is what the sync needs to decide whether a cached
// playlist's track list is stale.
type AppleLibraryPlaylistState struct {
	ID            string
	Version       string
	TracksVersion string
	TracksReadAt  string
}

const appleLibraryColumns = `kind,item_id,catalog_id,url,name,artist_name,album_name,artwork_url,description,
	genre,release_date,track_count,duration_ms,track_number,disc_number,date_added,last_modified,version_hash,
	playlist_kind,add_order`

func scanAppleLibraryItem(rows interface{ Scan(...any) error }) (domain.AppleLibraryItem, error) {
	var it domain.AppleLibraryItem
	err := rows.Scan(&it.Kind, &it.ID, &it.CatalogID, &it.URL, &it.Name, &it.ArtistName, &it.AlbumName, &it.ArtworkURL,
		&it.Description, &it.Genre, &it.ReleaseDate, &it.TrackCount, &it.DurationMS, &it.TrackNumber, &it.DiscNumber,
		&it.DateAdded, &it.LastModified, &it.VersionHash, &it.PlaylistKind, &it.AddOrder)
	return it, err
}

// appleLibraryFingerprint is a digest of every field a client shows, so an
// upsert can tell a changed row from an unchanged one. An artwork link without
// the {w}x{h} template (an own playlist's) is pre-signed and differs on every
// read, so it is left out; the row still stores the latest one.
func appleLibraryFingerprint(it domain.AppleLibraryItem) string {
	artwork := it.ArtworkURL
	if !strings.Contains(artwork, "{w}") {
		artwork = ""
	}
	h := sha256.New()
	for _, v := range []string{it.CatalogID, it.URL, it.Name, it.ArtistName, it.AlbumName, artwork, it.Description,
		it.Genre, it.ReleaseDate, strconv.Itoa(it.TrackCount), strconv.Itoa(it.DurationMS), strconv.Itoa(it.TrackNumber),
		strconv.Itoa(it.DiscNumber), it.DateAdded, it.LastModified, it.VersionHash, it.PlaylistKind} {
		h.Write([]byte(v))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}

// UpsertAppleLibraryItems writes items of one kind in one transaction, marking
// each as seen by runID. setOrder writes each item's AddOrder; otherwise an
// existing row keeps its order and a new row takes the item's.
func (s *Store) UpsertAppleLibraryItems(ctx context.Context, kind string, items []domain.AppleLibraryItem, runID string, setOrder bool) (AppleLibraryUpsert, error) {
	var out AppleLibraryUpsert
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := formatTime(now())
	for _, it := range items {
		fp := appleLibraryFingerprint(it)
		var oldFP string
		err := tx.QueryRowContext(ctx, `SELECT fingerprint FROM apple_library_items WHERE kind=? AND item_id=?`, kind, it.ID).Scan(&oldFP)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			out.Added++
		case err != nil:
			return out, err
		case oldFP != fp:
			out.Changed++
		}
		orderUpdate := `add_order`
		if setOrder {
			orderUpdate = `excluded.add_order`
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO apple_library_items (`+appleLibraryColumns+`,fingerprint,seen_run,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(kind,item_id) DO UPDATE SET catalog_id=excluded.catalog_id, url=excluded.url, name=excluded.name,
				artist_name=excluded.artist_name, album_name=excluded.album_name, artwork_url=excluded.artwork_url,
				description=excluded.description, genre=excluded.genre, release_date=excluded.release_date,
				track_count=excluded.track_count, duration_ms=excluded.duration_ms, track_number=excluded.track_number,
				disc_number=excluded.disc_number, date_added=excluded.date_added, last_modified=excluded.last_modified,
				version_hash=excluded.version_hash, playlist_kind=excluded.playlist_kind, add_order=`+orderUpdate+`,
				fingerprint=excluded.fingerprint, seen_run=excluded.seen_run,
				updated_at=CASE WHEN fingerprint=excluded.fingerprint THEN updated_at ELSE excluded.updated_at END`,
			kind, it.ID, it.CatalogID, it.URL, it.Name, it.ArtistName, it.AlbumName, it.ArtworkURL, it.Description,
			it.Genre, it.ReleaseDate, it.TrackCount, it.DurationMS, it.TrackNumber, it.DiscNumber, it.DateAdded,
			it.LastModified, it.VersionHash, it.PlaylistKind, it.AddOrder, fp, runID, stamp); err != nil {
			return out, err
		}
	}
	return out, tx.Commit()
}

// DeleteAppleLibraryUnseen removes the rows of kind that runID did not see,
// with the track lists of removed playlists, and reports how many rows went.
// The sync calls it only after a complete, consistent read of the kind.
func (s *Store) DeleteAppleLibraryUnseen(ctx context.Context, kind, runID string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if kind == "playlists" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM apple_library_playlist_tracks WHERE playlist_id IN
			(SELECT item_id FROM apple_library_items WHERE kind='playlists' AND seen_run<>?)`, runID); err != nil {
			return 0, err
		}
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM apple_library_items WHERE kind=? AND seen_run<>?`, kind, runID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// AppleLibraryKnownIDs reports which of ids are cached for kind.
func (s *Store) AppleLibraryKnownIDs(ctx context.Context, kind string, ids []string) (map[string]bool, error) {
	return s.appleLibraryLookup(ctx, `SELECT item_id FROM apple_library_items WHERE kind=? AND item_id IN `, kind, ids)
}

// AppleLibraryMembership reports which of the catalog ids are in the cached
// library of kind.
func (s *Store) AppleLibraryMembership(ctx context.Context, kind string, catalogIDs []string) (map[string]bool, error) {
	return s.appleLibraryLookup(ctx, `SELECT catalog_id FROM apple_library_items WHERE kind=? AND catalog_id IN `, kind, catalogIDs)
}

func (s *Store) appleLibraryLookup(ctx context.Context, query, kind string, ids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		args := make([]any, 0, len(chunk)+1)
		args = append(args, kind)
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := s.db.QueryContext(ctx, query+"("+strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")+")", args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = true
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AppleLibraryMaxOrder returns the highest add_order of kind, 0 when empty.
func (s *Store) AppleLibraryMaxOrder(ctx context.Context, kind string) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(add_order) FROM apple_library_items WHERE kind=?`, kind).Scan(&n)
	return n.Int64, err
}

// AppleLibraryCounts returns the number of cached rows per kind.
func (s *Store) AppleLibraryCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, COUNT(*) FROM apple_library_items GROUP BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return nil, err
		}
		out[kind] = n
	}
	return out, rows.Err()
}

// ListAppleLibrary returns one page of a kind and the number of rows matching
// the query.
func (s *Store) ListAppleLibrary(ctx context.Context, q AppleLibraryQuery) ([]domain.AppleLibraryItem, int, error) {
	where := `kind=?`
	args := []any{q.Kind}
	if q.Q != "" {
		pattern := "%" + escapeLike(q.Q) + "%"
		where += ` AND (name LIKE ? ESCAPE '\' OR artist_name LIKE ? ESCAPE '\' OR album_name LIKE ? ESCAPE '\')`
		args = append(args, pattern, pattern, pattern)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM apple_library_items WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	dir := "ASC"
	if q.Desc {
		dir = "DESC"
	}
	var order string
	switch q.Sort {
	case "added":
		order = `add_order ` + dir + `, name COLLATE NOCASE ASC`
	case "artist":
		order = `artist_name COLLATE NOCASE ` + dir + `, name COLLATE NOCASE ` + dir
	case "name":
		order = `name COLLATE NOCASE ` + dir
	default:
		return nil, 0, fmt.Errorf("unknown sort %q", q.Sort)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+appleLibraryColumns+` FROM apple_library_items WHERE `+where+
		` ORDER BY `+order+`, item_id ASC LIMIT ? OFFSET ?`, append(args, q.Limit, q.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	// The API caps a page at 200; the store does not trust that for the capacity.
	items := make([]domain.AppleLibraryItem, 0, min(max(q.Limit, 0), 200))
	for rows.Next() {
		it, err := scanAppleLibraryItem(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, it)
	}
	return items, total, rows.Err()
}

// AppleLibraryPlaylistStates returns every cached playlist's track-list
// bookkeeping.
func (s *Store) AppleLibraryPlaylistStates(ctx context.Context) ([]AppleLibraryPlaylistState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_id, last_modified||'|'||version_hash, tracks_version, tracks_read_at
		FROM apple_library_items WHERE kind='playlists' ORDER BY item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppleLibraryPlaylistState
	for rows.Next() {
		var st AppleLibraryPlaylistState
		if err := rows.Scan(&st.ID, &st.Version, &st.TracksVersion, &st.TracksReadAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// AppleLibraryPlaylistVersion is the value AppleLibraryPlaylistStates reports
// as Version for a playlist item.
func AppleLibraryPlaylistVersion(it domain.AppleLibraryItem) string {
	return it.LastModified + "|" + it.VersionHash
}

// ReplaceAppleLibraryPlaylistTracks swaps a cached playlist's whole track list
// and records the playlist version it was read at. It does nothing when the
// playlist is no longer cached.
func (s *Store) ReplaceAppleLibraryPlaylistTracks(ctx context.Context, playlistID, version string, tracks []domain.AppleLibraryTrack) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE apple_library_items SET tracks_version=?, tracks_read_at=? WHERE kind='playlists' AND item_id=?`,
		version, formatTime(now()), playlistID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM apple_library_playlist_tracks WHERE playlist_id=?`, playlistID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO apple_library_playlist_tracks
		(playlist_id,position,song_id,catalog_id,url,name,artist_name,album_name,duration_ms,artwork_url) VALUES (?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, t := range tracks {
		if _, err := stmt.ExecContext(ctx, playlistID, t.Position, t.SongID, t.CatalogID, t.URL, t.Name, t.ArtistName,
			t.AlbumName, t.DurationMS, t.ArtworkURL); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AppleLibraryPlaylistTracks returns a cached playlist's tracks in order.
// found is false when no such playlist is cached; a cached playlist whose
// tracks were never read returns found and no tracks, with readAt empty.
func (s *Store) AppleLibraryPlaylistTracks(ctx context.Context, playlistID string) (tracks []domain.AppleLibraryTrack, readAt string, found bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT tracks_read_at FROM apple_library_items WHERE kind='playlists' AND item_id=?`, playlistID).Scan(&readAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT position,song_id,catalog_id,url,name,artist_name,album_name,duration_ms,artwork_url
		FROM apple_library_playlist_tracks WHERE playlist_id=? ORDER BY position`, playlistID)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	tracks = []domain.AppleLibraryTrack{}
	for rows.Next() {
		var t domain.AppleLibraryTrack
		if err := rows.Scan(&t.Position, &t.SongID, &t.CatalogID, &t.URL, &t.Name, &t.ArtistName, &t.AlbumName, &t.DurationMS, &t.ArtworkURL); err != nil {
			return nil, "", false, err
		}
		tracks = append(tracks, t)
	}
	return tracks, readAt, true, rows.Err()
}

// AppleLibraryMeta reads one bookkeeping value of the Apple library sync; ""
// when unset.
func (s *Store) AppleLibraryMeta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM apple_library_meta WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetAppleLibraryMeta writes one bookkeeping value of the Apple library sync.
func (s *Store) SetAppleLibraryMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO apple_library_meta(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
