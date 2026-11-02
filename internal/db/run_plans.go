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

// The joins that put a record's slots next to its track list, each with one
// placeholder for the owner id. A slot counts only while its track is wanted
// (an album's) or still listed (a playlist's), and available.
const (
	albumSlotJoin = `files f JOIN tracks t ON t.album_id = f.owner_id AND t.song_id = f.song_id
		WHERE f.owner_kind = 'album' AND f.owner_id = ? AND t.wanted = 1 AND t.availability = 'available'`
	playlistSlotJoin = `files f JOIN playlist_tracks t ON t.playlist_id = f.owner_id AND t.song_id = f.song_id
		WHERE f.owner_kind = 'playlist' AND f.owner_id = ? AND t.removed_at IS NULL AND t.availability = 'available'`
	// slotDue is a slot a run should download, with one placeholder for now:
	// pending, or failed with its backoff over.
	slotDue = `(f.audio_state = 'pending' OR (f.audio_state = 'failed' AND (f.next_attempt_at IS NULL OR f.next_attempt_at <= ?)))`
)

// prefixedFileColumns is fileColumns over "files f", for joins with a track
// table that shares column names.
var prefixedFileColumns = func() string {
	cols := strings.Fields(strings.ReplaceAll(fileColumns, ",", " "))
	return "f." + strings.Join(cols, ", f.")
}()

func slotJoin(kind domain.ResourceKind) (string, error) {
	switch kind {
	case domain.ResourceAlbum:
		return albumSlotJoin, nil
	case domain.ResourcePlaylist:
		return playlistSlotJoin, nil
	}
	return "", fmt.Errorf("unknown record kind %q", kind)
}

// PlanSlot is one slot a run plans, with what the run needs of its track.
type PlanSlot struct {
	File domain.File
	// MainPath is the stored path of the track's main slot; an extra slot's
	// file is named after it. Empty for a main slot and while main has none.
	MainPath string
	// Position is the track's {SongNumber}: the album's song number, or the
	// playlist position.
	Position   int
	Name       string
	ArtistName string
	DurationMS int
}

// DueSlots returns the slots of a record a run should download at now, in
// track order: pending, or failed with the backoff over, of wanted (album) or
// listed (playlist), available tracks.
func (s *Store) DueSlots(ctx context.Context, kind domain.ResourceKind, id string, now time.Time) ([]PlanSlot, error) {
	return s.planSlots(ctx, kind, id, slotDue, formatTime(now))
}

// DoneSlots returns the done slots of a record's wanted (album) or listed
// (playlist), available tracks, in track order: what a lyrics run refreshes.
// A done file of a track Apple withheld or a playlist dropped is left out, as
// the run's read no longer lists it.
func (s *Store) DoneSlots(ctx context.Context, kind domain.ResourceKind, id string) ([]PlanSlot, error) {
	return s.planSlots(ctx, kind, id, `f.audio_state = 'done'`)
}

// planSlots returns the slots of a record that match cond (over files f,
// with args after the owner id) on the record's slot join, in track order.
func (s *Store) planSlots(ctx context.Context, kind domain.ResourceKind, id, cond string, args ...any) ([]PlanSlot, error) {
	join, err := slotJoin(kind)
	if err != nil {
		return nil, err
	}
	position, order := "t.song_number", "t.list_position, t.disc_number, t.track_number, f.song_id, f.slot"
	if kind == domain.ResourcePlaylist {
		position, order = "t.position", "t.position, f.song_id, f.slot"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+prefixedFileColumns+`, `+position+`, t.name, t.artist_name, t.duration_ms,
			COALESCE((SELECT m.path FROM files m WHERE m.owner_kind = f.owner_kind AND m.owner_id = f.owner_id
				AND m.song_id = f.song_id AND m.slot = 'main' AND f.slot <> 'main'), '')
		FROM `+join+` AND `+cond+` ORDER BY `+order, append([]any{id}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanSlot
	for rows.Next() {
		var slot PlanSlot
		scan := func(dest ...any) error {
			return rows.Scan(append(dest, &slot.Position, &slot.Name, &slot.ArtistName, &slot.DurationMS, &slot.MainPath)...)
		}
		if slot.File, err = scanFile(scannerFunc(scan)); err != nil {
			return nil, err
		}
		out = append(out, slot)
	}
	return out, rows.Err()
}

type scannerFunc func(dest ...any) error

func (f scannerFunc) Scan(dest ...any) error { return f(dest...) }

// RecordNeedsRun reports whether a record has work for a run at now: it is
// unresolved, or one of its slots is due (DueSlots). Paused, catalog state and
// active runs are not considered; ListEligibleRecords adds those.
func (s *Store) RecordNeedsRun(ctx context.Context, kind domain.ResourceKind, id string, now time.Time) (bool, error) {
	table, err := recordTable(kind)
	if err != nil {
		return false, err
	}
	join, err := slotJoin(kind)
	if err != nil {
		return false, err
	}
	var resolved sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT resolved_at FROM `+table+` WHERE id = ?`, id).Scan(&resolved); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrRecordNotFound
		}
		return false, err
	}
	if !resolved.Valid {
		return true, nil
	}
	var due bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+join+` AND `+slotDue+`)`, id, formatTime(now)).Scan(&due)
	return due, err
}

// ActiveRunID returns the record's queued or running run, if any.
func (s *Store) ActiveRunID(ctx context.Context, kind domain.ResourceKind, id string) (string, bool, error) {
	var runID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM runs WHERE resource_kind = ? AND resource_id = ? AND status IN ('queued','running')`,
		string(kind), id).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return runID, err == nil, err
}

// SetRunPlan stores the plan a run made (runs.plan).
func (s *Store) SetRunPlan(ctx context.Context, runID, plan string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE runs SET plan = ? WHERE id = ?`, plan, runID)
	return requireJobRow(res, err)
}

// SetRunPriority sets a run's claim class (domain.PriorityNow and so on).
func (s *Store) SetRunPriority(ctx context.Context, runID string, priority int) error {
	res, err := s.db.ExecContext(ctx, `UPDATE runs SET priority = ? WHERE id = ?`, priority, runID)
	return requireJobRow(res, err)
}

// ReleaseRunSlots sets the slots runID left running back to pending, without
// counting a failure, and recomputes the affected rollups: the run was paused
// or cancelled. It returns how many slots it released.
func (s *Store) ReleaseRunSlots(ctx context.Context, runID string, now time.Time) (int, error) {
	return withTx(ctx, s.db, func(tx *sql.Tx) (int, error) {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT owner_kind, owner_id FROM files WHERE audio_state = 'running' AND last_run_id = ?`, runID)
		if err != nil {
			return 0, err
		}
		var owners []RecordRef
		for rows.Next() {
			var ref RecordRef
			var kind string
			if err := rows.Scan(&kind, &ref.ID); err != nil {
				rows.Close()
				return 0, err
			}
			ref.Kind = domain.ResourceKind(kind)
			owners = append(owners, ref)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx, `UPDATE files SET audio_state = 'pending', updated_at = ? WHERE audio_state = 'running' AND last_run_id = ?`,
			formatTime(now), runID)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		for _, owner := range owners {
			if err := recomputeRollup(ctx, tx, owner.Kind, owner.ID, now); err != nil {
				return 0, err
			}
		}
		return int(n), nil
	})
}

func requireJobRow(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrJobNotFound
	}
	return nil
}
