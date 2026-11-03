package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"amdl/internal/domain"
)

// RecordFlag selects records by what their slots or flags say.
type RecordFlag string

const (
	// FlagFailed: a wanted track has a failed slot.
	FlagFailed RecordFlag = "failed"
	// FlagGivenUp: a wanted track has a given-up slot.
	FlagGivenUp RecordFlag = "given_up"
	// FlagMissing: a check found a done slot's file gone.
	FlagMissing RecordFlag = "missing"
	// FlagWaiting: a failed slot waits for its automatic retry.
	FlagWaiting RecordFlag = "waiting"
	// FlagPaused: the record is paused.
	FlagPaused RecordFlag = "paused"
	// FlagUnresolved: no read has listed the record's tracks yet.
	FlagUnresolved RecordFlag = "unresolved"
)

// RecordFlags is every RecordFlag.
var RecordFlags = []RecordFlag{FlagFailed, FlagGivenUp, FlagMissing, FlagWaiting, FlagPaused, FlagUnresolved}

// RecordSort orders a record list.
type RecordSort string

const (
	SortAdded   RecordSort = "added"
	SortName    RecordSort = "name"
	SortArtist  RecordSort = "artist"
	SortRelease RecordSort = "release"
	// SortActivity is the newest activity first (RecordListItem.ActivityAt).
	SortActivity RecordSort = "activity"
)

// RecordFilter selects records of one kind. Empty fields select everything;
// several states or flags match any of them. Limit 0 means no limit.
type RecordFilter struct {
	States []domain.RecordState
	Flags  []RecordFlag
	Source domain.RecordSource
	Query  string
	Sort   RecordSort
	Limit  int
	Offset int
	// Now is the clock of FlagWaiting.
	Now time.Time
}

// RecordListItem is a listed record with the id of its queued or running
// run, empty when it has none, and its last activity.
type RecordListItem[T any] struct {
	Record      T
	ActiveJobID string
	// ActivityAt is when one of the record's runs last changed, or when the
	// record was added while it has none.
	ActivityAt time.Time
}

// activitySQL is a record r's ActivityAt, of kind.
func activitySQL(kind domain.ResourceKind) string {
	return `COALESCE((SELECT MAX(COALESCE(a.finished_at, a.updated_at)) FROM runs a WHERE a.resource_kind = '` + string(kind) +
		`' AND a.resource_id = r.id), r.added_at)`
}

// RecordActivity is the ActivityAt of record kind id.
func (s *Store) RecordActivity(ctx context.Context, kind domain.ResourceKind, id string) (time.Time, error) {
	table, err := recordTable(kind)
	if err != nil {
		return time.Time{}, err
	}
	var at string
	err = s.db.QueryRowContext(ctx, `SELECT `+activitySQL(kind)+` FROM `+table+` r WHERE r.id = ?`, id).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, ErrRecordNotFound
	}
	return parseTime(at), err
}

// ListAlbums returns the album records filter selects, with the total that
// matches before paging.
func (s *Store) ListAlbums(ctx context.Context, f RecordFilter) ([]RecordListItem[domain.Album], int, error) {
	return listRecords(ctx, s.db, domain.ResourceAlbum, albumColumns, f, scanAlbum)
}

// ListPlaylists is ListAlbums for playlist records.
func (s *Store) ListPlaylists(ctx context.Context, f RecordFilter) ([]RecordListItem[domain.Playlist], int, error) {
	return listRecords(ctx, s.db, domain.ResourcePlaylist, playlistColumns, f, scanPlaylist)
}

func listRecords[T any](ctx context.Context, q *sql.DB, kind domain.ResourceKind, columns string, f RecordFilter,
	scan func(jobScanner) (T, error)) ([]RecordListItem[T], int, error) {
	table, err := recordTable(kind)
	if err != nil {
		return nil, 0, err
	}
	where, args, err := recordWhere(kind, f)
	if err != nil {
		return nil, 0, err
	}
	var total int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` r`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order, err := recordOrder(kind, f.Sort)
	if err != nil {
		return nil, 0, err
	}
	query := `SELECT ` + qualifyColumns("r.", columns) + `,
		COALESCE((SELECT j.id FROM runs j WHERE j.resource_kind = '` + string(kind) + `' AND j.resource_id = r.id AND j.status IN ('queued','running') LIMIT 1), ''),
		` + activitySQL(kind) + ` AS activity_at
		FROM ` + table + ` r` + where + ` ORDER BY ` + order
	if f.Limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, f.Limit, f.Offset)
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []RecordListItem[T]{}
	for rows.Next() {
		var item RecordListItem[T]
		var activity string
		item.Record, err = scan(scannerFunc(func(dest ...any) error {
			return rows.Scan(append(dest, &item.ActiveJobID, &activity)...)
		}))
		if err != nil {
			return nil, 0, err
		}
		item.ActivityAt = parseTime(activity)
		out = append(out, item)
	}
	return out, total, rows.Err()
}

// qualifyColumns qualifies each column of a comma-separated list.
func qualifyColumns(prefix, columns string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

func recordWhere(kind domain.ResourceKind, f RecordFilter) (string, []any, error) {
	var conds []string
	var args []any
	if len(f.States) > 0 {
		conds = append(conds, `r.state IN (`+placeholders(len(f.States))+`)`)
		for _, st := range f.States {
			args = append(args, string(st))
		}
	}
	var flagConds []string
	for _, flag := range f.Flags {
		switch flag {
		case FlagFailed:
			flagConds = append(flagConds, `r.tracks_failed > 0`)
		case FlagGivenUp:
			flagConds = append(flagConds, `r.tracks_given_up > 0`)
		case FlagMissing:
			flagConds = append(flagConds, `r.tracks_missing > 0`)
		case FlagPaused:
			flagConds = append(flagConds, `r.paused = 1`)
		case FlagUnresolved:
			flagConds = append(flagConds, `r.resolved_at IS NULL`)
		case FlagWaiting:
			flagConds = append(flagConds, `EXISTS (SELECT 1 FROM files w WHERE w.owner_kind = '`+string(kind)+`' AND w.owner_id = r.id
				AND w.audio_state = 'failed' AND w.next_attempt_at > ?)`)
			args = append(args, formatTime(f.Now))
		default:
			return "", nil, fmt.Errorf("unknown record flag %q", flag)
		}
	}
	if len(flagConds) > 0 {
		conds = append(conds, `(`+strings.Join(flagConds, ` OR `)+`)`)
	}
	if f.Source != "" {
		conds = append(conds, `EXISTS (SELECT 1 FROM resource_sources s WHERE s.resource_kind = '`+string(kind)+`' AND s.resource_id = r.id AND s.source = ?)`)
		args = append(args, string(f.Source))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + escapeLike(q) + "%"
		if kind == domain.ResourceAlbum {
			conds = append(conds, `(r.name LIKE ? ESCAPE '\' OR r.artist_name LIKE ? ESCAPE '\' OR r.id = ?)`)
		} else {
			conds = append(conds, `(r.name LIKE ? ESCAPE '\' OR r.curator_name LIKE ? ESCAPE '\' OR r.id = ?)`)
		}
		args = append(args, like, like, q)
	}
	if len(conds) == 0 {
		return "", args, nil
	}
	return ` WHERE ` + strings.Join(conds, ` AND `), args, nil
}

func recordOrder(kind domain.ResourceKind, sort RecordSort) (string, error) {
	switch sort {
	case "", SortAdded:
		return `r.added_at DESC, r.id`, nil
	case SortName:
		return `r.name COLLATE NOCASE, r.id`, nil
	case SortArtist:
		if kind == domain.ResourcePlaylist {
			return `r.curator_name COLLATE NOCASE, r.name COLLATE NOCASE, r.id`, nil
		}
		return `r.artist_name COLLATE NOCASE, r.name COLLATE NOCASE, r.id`, nil
	case SortRelease:
		if kind == domain.ResourcePlaylist {
			return `r.last_modified DESC, r.id`, nil
		}
		return `r.release_date DESC, r.id`, nil
	case SortActivity:
		return `activity_at DESC, r.id`, nil
	}
	return "", fmt.Errorf("unknown record sort %q", sort)
}

// RecordCounts is how many records of one kind are in each state and carry
// each flag.
type RecordCounts struct {
	Total  int                        `json:"total"`
	States map[domain.RecordState]int `json:"states"`
	Flags  map[RecordFlag]int         `json:"flags"`
}

// CountRecords counts the records of kind per state and per flag.
func (s *Store) CountRecords(ctx context.Context, kind domain.ResourceKind, now time.Time) (RecordCounts, error) {
	table, err := recordTable(kind)
	if err != nil {
		return RecordCounts{}, err
	}
	out := RecordCounts{
		States: map[domain.RecordState]int{domain.RecordNone: 0, domain.RecordPartial: 0, domain.RecordComplete: 0},
		Flags:  map[RecordFlag]int{},
	}
	var none, partial, complete, failed, givenUp, missing, paused, unresolved, waiting int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(state = 'none'), 0), COALESCE(SUM(state = 'partial'), 0), COALESCE(SUM(state = 'complete'), 0),
		COALESCE(SUM(tracks_failed > 0), 0), COALESCE(SUM(tracks_given_up > 0), 0), COALESCE(SUM(tracks_missing > 0), 0),
		COALESCE(SUM(paused = 1), 0), COALESCE(SUM(resolved_at IS NULL), 0),
		(SELECT COUNT(DISTINCT owner_id) FROM files WHERE owner_kind = ? AND audio_state = 'failed' AND next_attempt_at > ?)
		FROM `+table, string(kind), formatTime(now)).Scan(
		&out.Total, &none, &partial, &complete, &failed, &givenUp, &missing, &paused, &unresolved, &waiting)
	if err != nil {
		return RecordCounts{}, err
	}
	out.States[domain.RecordNone], out.States[domain.RecordPartial], out.States[domain.RecordComplete] = none, partial, complete
	out.Flags[FlagFailed], out.Flags[FlagGivenUp], out.Flags[FlagMissing] = failed, givenUp, missing
	out.Flags[FlagPaused], out.Flags[FlagUnresolved], out.Flags[FlagWaiting] = paused, unresolved, waiting
	return out, nil
}

// CountPending counts pending adds per state.
func (s *Store) CountPending(ctx context.Context) (map[domain.PendingAddState]int, error) {
	out := map[domain.PendingAddState]int{domain.PendingAddPending: 0, domain.PendingAddFailed: 0}
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM pending_adds GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[domain.PendingAddState(state)] = n
	}
	return out, rows.Err()
}

// GetPending returns one pending add, or ErrPendingNotFound.
func (s *Store) GetPending(ctx context.Context, id int64) (domain.PendingAdd, error) {
	p, err := scanPending(s.db.QueryRowContext(ctx, `SELECT `+pendingColumns+` FROM pending_adds WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return domain.PendingAdd{}, ErrPendingNotFound
	}
	return p, err
}

// RecordMarks returns the marks of the records of kind among ids, keyed by
// id; an id without a record is absent.
func (s *Store) RecordMarks(ctx context.Context, kind domain.ResourceKind, ids []string) (map[string]domain.ResourceMark, error) {
	out := map[string]domain.ResourceMark{}
	if len(ids) == 0 {
		return out, nil
	}
	table, err := recordTable(kind)
	if err != nil {
		return nil, err
	}
	withheld := `0`
	if kind == domain.ResourceAlbum {
		withheld = `r.withheld_count`
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.state, r.tracks_done, r.tracks_wanted, `+withheld+`, r.paused,
		COALESCE((SELECT j.id FROM runs j WHERE j.resource_kind = '`+string(kind)+`' AND j.resource_id = r.id AND j.status IN ('queued','running') LIMIT 1), '')
		FROM `+table+` r WHERE r.id IN (`+placeholders(len(ids))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m := domain.ResourceMark{Kind: kind}
		var state string
		if err := rows.Scan(&m.ID, &state, &m.TracksDone, &m.TracksWanted, &m.WithheldCount, &m.Paused, &m.ActiveJobID); err != nil {
			return nil, err
		}
		m.State = domain.RecordState(state)
		out[m.ID] = m
	}
	return out, rows.Err()
}

// SongMarks returns, for each of songIDs an album record holds, that record's
// mark with the song's main slot state. A song two album records hold is
// marked with the one that has it done, else the first by id.
func (s *Store) SongMarks(ctx context.Context, songIDs []string) (map[string]domain.ResourceMark, error) {
	out := map[string]domain.ResourceMark{}
	if len(songIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(songIDs))
	for _, id := range songIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.song_id, t.album_id, COALESCE(f.audio_state, '') FROM tracks t
		LEFT JOIN files f ON f.owner_kind = 'album' AND f.owner_id = t.album_id AND f.song_id = t.song_id AND f.slot = 'main'
		WHERE t.song_id IN (`+placeholders(len(songIDs))+`) ORDER BY t.song_id, COALESCE(f.audio_state, '') <> 'done', t.album_id`, args...)
	if err != nil {
		return nil, err
	}
	type hold struct{ album, state string }
	first := map[string]hold{}
	var albums []string
	for rows.Next() {
		var song string
		var h hold
		if err := rows.Scan(&song, &h.album, &h.state); err != nil {
			rows.Close()
			return nil, err
		}
		if _, seen := first[song]; !seen {
			first[song] = h
			albums = append(albums, h.album)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	marks, err := s.RecordMarks(ctx, domain.ResourceAlbum, albums)
	if err != nil {
		return nil, err
	}
	for song, h := range first {
		if m, ok := marks[h.album]; ok {
			m.SongState = domain.AudioState(h.state)
			out[song] = m
		}
	}
	return out, nil
}

// AlbumTrackStates returns the main slot state of each track of an album
// record, keyed by song id.
func (s *Store) AlbumTrackStates(ctx context.Context, albumID string) (map[string]domain.AudioState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT song_id, audio_state FROM files WHERE owner_kind = 'album' AND owner_id = ? AND slot = 'main'`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.AudioState{}
	for rows.Next() {
		var song, state string
		if err := rows.Scan(&song, &state); err != nil {
			return nil, err
		}
		out[song] = domain.AudioState(state)
	}
	return out, rows.Err()
}
