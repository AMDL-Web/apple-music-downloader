package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"amdl/internal/domain"
)

// ErrPathTaken matches every *PathTakenError.
var ErrPathTaken = errors.New("path_taken")

// ErrMainNotDone is returned by RequestSlot while the track's main slot is
// not done: another quality is always a second copy of a downloaded track.
var ErrMainNotDone = errors.New("main_not_done")

// ErrQualityPresent is returned by RequestSlot when the main slot already
// holds that codec, or holds a file whose codec is unknown.
var ErrQualityPresent = errors.New("quality_present")

// PathTakenError reports that another slot already holds the output path.
type PathTakenError struct {
	Path  string
	Owner domain.FileKey
}

func (e *PathTakenError) Error() string {
	return fmt.Sprintf("path_taken: %s is held by %s %s song %s slot %s", e.Path, e.Owner.OwnerKind, e.Owner.OwnerID, e.Owner.SongID, e.Owner.Slot)
}

func (e *PathTakenError) Is(target error) bool { return target == ErrPathTaken }

// NonRetryable reports true: the other slot keeps the path until a template
// change or a reset, so a later run would only meet it again.
func (e *PathTakenError) NonRetryable() bool { return true }

// Retry backoff across runs: a slot that failed n times is due again
// retryBackoffBase × 4^(n−1) after the failure (30 min, then 2 h).
const retryBackoffBase = 30 * time.Minute

func retryBackoff(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	return retryBackoffBase * time.Duration(math.Pow(4, float64(failures-1)))
}

// ClaimPath claims path for a slot before its download: in one transaction it
// fails with a *PathTakenError when another slot holds the path, else stores
// the path (relative to the downloads dir when under it), sets the slot
// running and names the run. The caller holds the pipeline's output lock.
func (s *Store) ClaimPath(ctx context.Context, key domain.FileKey, path, runID string, now time.Time) error {
	if path == "" {
		return fmt.Errorf("claim path for %s/%s: empty path", key.OwnerID, key.SongID)
	}
	stored := s.storedPath(path)
	_, err := withTx(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		if err := checkPathFree(ctx, tx, key, stored); err != nil {
			return struct{}{}, err
		}
		res, err := tx.ExecContext(ctx, `UPDATE files SET path = ?, audio_state = 'running', last_run_id = ?, updated_at = ? WHERE `+fileKeyWhere,
			append([]any{stored, runID, formatTime(now)}, fileKeyArgs(key)...)...)
		if err := requireFileRow(res, err); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, recomputeRollup(ctx, tx, key.OwnerKind, key.OwnerID, now)
	})
	return err
}

func checkPathFree(ctx context.Context, tx *sql.Tx, key domain.FileKey, stored string) error {
	var holder domain.FileKey
	var kind string
	err := tx.QueryRowContext(ctx, `SELECT owner_kind, owner_id, song_id, slot FROM files WHERE path = ?
		AND NOT (`+fileKeyWhere+`)`, append([]any{stored}, fileKeyArgs(key)...)...).Scan(&kind, &holder.OwnerID, &holder.SongID, &holder.Slot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	holder.OwnerKind = domain.ResourceKind(kind)
	return &PathTakenError{Path: stored, Owner: holder}
}

// FileOutcome is what landed for a slot.
type FileOutcome struct {
	// Path is where the file is. Empty keeps the claimed path; a new path is
	// claimed as ClaimPath does, so a file already at the target can be
	// adopted as done.
	Path         string
	Codec        string
	QualityLabel string
	BitDepth     int
	SampleRate   int
	Bitrate      int
	FileSize     int64
	LyricsState  domain.LyricsStatus
	RunID        string
	Now          time.Time
}

// MarkFileDone records a slot's file as done, clears its failures and
// recomputes the record's rollup, in one transaction.
func (s *Store) MarkFileDone(ctx context.Context, key domain.FileKey, out FileOutcome) error {
	stored := s.storedPath(out.Path)
	_, err := withTx(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		if stored != "" {
			if err := checkPathFree(ctx, tx, key, stored); err != nil {
				return struct{}{}, err
			}
		}
		stamp := formatTime(out.Now)
		res, err := tx.ExecContext(ctx, `UPDATE files SET audio_state = 'done', path = CASE WHEN ? <> '' THEN ? ELSE path END,
				codec = ?, quality_label = ?, bit_depth = ?, sample_rate = ?, bitrate = ?, file_size = ?, lyrics_state = ?,
				failures = 0, next_attempt_at = NULL, last_error = '', last_run_id = ?, completed_at = ?, updated_at = ?
			WHERE `+fileKeyWhere,
			append([]any{stored, stored, out.Codec, out.QualityLabel, out.BitDepth, out.SampleRate, out.Bitrate, out.FileSize, string(out.LyricsState),
				out.RunID, stamp, stamp}, fileKeyArgs(key)...)...)
		if err := requireFileRow(res, err); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, recomputeRollup(ctx, tx, key.OwnerKind, key.OwnerID, out.Now)
	})
	return err
}

// FileFailure is how a run ended a slot without a file.
type FileFailure struct {
	Error string
	RunID string
	Now   time.Time
	// GiveUpAfter is download.give_up_after_runs: the failure count at which
	// the slot is given up; 0 never gives up.
	GiveUpAfter int
	// NonRetryable gives up at once.
	NonRetryable bool
}

// MarkFileFailed counts a run's failure of a slot: failures + 1 and the next
// attempt after the backoff, or given_up when the count reaches GiveUpAfter
// or the error is not retryable. It returns the slot's new state.
func (s *Store) MarkFileFailed(ctx context.Context, key domain.FileKey, f FileFailure) (domain.AudioState, error) {
	return withTx(ctx, s.db, func(tx *sql.Tx) (domain.AudioState, error) {
		var failures int
		err := tx.QueryRowContext(ctx, `SELECT failures FROM files WHERE `+fileKeyWhere, fileKeyArgs(key)...).Scan(&failures)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrFileNotFound
		}
		if err != nil {
			return "", err
		}
		failures++
		state := domain.AudioFailed
		var next any = formatTime(f.Now.Add(retryBackoff(failures)))
		if f.NonRetryable || (f.GiveUpAfter > 0 && failures >= f.GiveUpAfter) {
			state, next = domain.AudioGivenUp, nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE files SET audio_state = ?, failures = ?, next_attempt_at = ?, last_error = ?, last_run_id = ?, updated_at = ?
			WHERE `+fileKeyWhere,
			append([]any{string(state), failures, next, f.Error, f.RunID, formatTime(f.Now)}, fileKeyArgs(key)...)...); err != nil {
			return "", err
		}
		return state, recomputeRollup(ctx, tx, key.OwnerKind, key.OwnerID, f.Now)
	})
}

// MarkFileUnavailable records that the track does not offer the slot's codec.
// It is not retried and counts no failure.
func (s *Store) MarkFileUnavailable(ctx context.Context, key domain.FileKey, runID string, now time.Time) error {
	return s.setFileState(ctx, key, `audio_state = 'unavailable', next_attempt_at = NULL, last_run_id = ?`, []any{runID}, now)
}

// SetFileLyricsState records a lyrics refresh of a done slot. The audio
// state and the file are left as they are.
func (s *Store) SetFileLyricsState(ctx context.Context, key domain.FileKey, state domain.LyricsStatus, now time.Time) error {
	return s.setFileState(ctx, key, `lyrics_state = ?`, []any{string(state)}, now)
}

// MarkFileMissing records that a done slot's file is no longer at its path.
// It reports whether the slot was done.
func (s *Store) MarkFileMissing(ctx context.Context, key domain.FileKey, now time.Time) (bool, error) {
	return withTx(ctx, s.db, func(tx *sql.Tx) (bool, error) {
		stamp := formatTime(now)
		res, err := tx.ExecContext(ctx, `UPDATE files SET audio_state = 'missing', checked_at = ?, updated_at = ? WHERE `+fileKeyWhere+` AND audio_state = 'done'`,
			append([]any{stamp, stamp}, fileKeyArgs(key)...)...)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		if err != nil || n == 0 {
			return false, err
		}
		return true, recomputeRollup(ctx, tx, key.OwnerKind, key.OwnerID, now)
	})
}

func (s *Store) setFileState(ctx context.Context, key domain.FileKey, set string, args []any, now time.Time) error {
	_, err := withTx(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		all := append(append(args, formatTime(now)), fileKeyArgs(key)...)
		res, err := tx.ExecContext(ctx, `UPDATE files SET `+set+`, updated_at = ? WHERE `+fileKeyWhere, all...)
		if err := requireFileRow(res, err); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, recomputeRollup(ctx, tx, key.OwnerKind, key.OwnerID, now)
	})
	return err
}

// RequestSlot adds another quality of a track, pending. key.Slot is a codec
// id. The main slot must be done (ErrMainNotDone) and hold another codec
// (ErrQualityPresent); a main slot whose codec is unknown counts as holding
// it. An existing slot is left as it is.
func (s *Store) RequestSlot(ctx context.Context, key domain.FileKey, now time.Time) error {
	if key.Slot == "" || key.Slot == domain.SlotMain {
		return fmt.Errorf("request slot: %q is not a codec", key.Slot)
	}
	_, err := withTx(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		main := key
		main.Slot = domain.SlotMain
		var state, codec string
		err := tx.QueryRowContext(ctx, `SELECT audio_state, codec FROM files WHERE `+fileKeyWhere, fileKeyArgs(main)...).Scan(&state, &codec)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && state != string(domain.AudioDone)) {
			return struct{}{}, ErrMainNotDone
		}
		if err != nil {
			return struct{}{}, err
		}
		// A done main slot with no codec holds a file whose quality amdl
		// does not know: one adopted at the track's path whose boxes could
		// not be read or name only the AAC kind
		// (internal/media#adoptedCodec), or a 1.x item that stored none. It
		// may be exactly the asked-for quality, and a second
		// slot would then download a byte-identical copy under a
		// quality-suffixed name, so it counts as the track's copy of every
		// quality until its codec is known.
		if codec == key.Slot || codec == "" {
			return struct{}{}, ErrQualityPresent
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO files(owner_kind, owner_id, song_id, slot, audio_state, updated_at)
			VALUES(?, ?, ?, ?, 'pending', ?)`, string(key.OwnerKind), key.OwnerID, key.SongID, key.Slot, formatTime(now)); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, recomputeRollup(ctx, tx, key.OwnerKind, key.OwnerID, now)
	})
	return err
}

// FileReset selects the slots ResetFiles re-arms: every slot of the record,
// or only SongIDs, only Slot when set, and only slots in States when set.
type FileReset struct {
	Kind    domain.ResourceKind
	ID      string
	SongIDs []string
	Slot    string
	States  []domain.AudioState
	// QualityPriority, when set, becomes the main slots' codec order.
	QualityPriority []string
	Now             time.Time
}

// ResetFiles sets the selected slots pending with no failures, keeping their
// stored paths so a re-download replaces the file where it is. A record with
// a queued or running run is refused with ErrRunActive. It returns the number
// of slots reset.
func (s *Store) ResetFiles(ctx context.Context, r FileReset) (int64, error) {
	if _, err := recordTable(r.Kind); err != nil {
		return 0, err
	}
	return withTx(ctx, s.db, func(tx *sql.Tx) (int64, error) {
		if err := refuseActiveRun(ctx, tx, r.Kind, r.ID); err != nil {
			return 0, err
		}
		set := `audio_state = 'pending', failures = 0, next_attempt_at = NULL, last_error = '', updated_at = ?`
		args := []any{formatTime(r.Now)}
		if len(r.QualityPriority) > 0 {
			raw, err := json.Marshal(r.QualityPriority)
			if err != nil {
				return 0, err
			}
			set += `, quality_priority = CASE WHEN slot = 'main' THEN ? ELSE quality_priority END`
			args = append(args, string(raw))
		}
		where := `owner_kind = ? AND owner_id = ?`
		args = append(args, string(r.Kind), r.ID)
		if len(r.SongIDs) > 0 {
			where += ` AND song_id IN (` + placeholders(len(r.SongIDs)) + `)`
			for _, id := range r.SongIDs {
				args = append(args, id)
			}
		}
		if r.Slot != "" {
			where += ` AND slot = ?`
			args = append(args, r.Slot)
		}
		if len(r.States) > 0 {
			where += ` AND audio_state IN (` + placeholders(len(r.States)) + `)`
			for _, state := range r.States {
				args = append(args, string(state))
			}
		}
		res, err := tx.ExecContext(ctx, `UPDATE files SET `+set+` WHERE `+where, args...)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		return n, recomputeRollup(ctx, tx, r.Kind, r.ID, r.Now)
	})
}

// RecoverRunningFiles sets every running slot back to pending, without
// counting a failure, and recomputes the affected rollups. Startup runs it
// before recovering runs: a slot left running would never be picked again.
func (s *Store) RecoverRunningFiles(ctx context.Context, now time.Time) (int, error) {
	return withTx(ctx, s.db, func(tx *sql.Tx) (int, error) {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT owner_kind, owner_id FROM files WHERE audio_state = 'running'`)
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
		res, err := tx.ExecContext(ctx, `UPDATE files SET audio_state = 'pending', updated_at = ? WHERE audio_state = 'running'`, formatTime(now))
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

func requireFileRow(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrFileNotFound
	}
	return nil
}
