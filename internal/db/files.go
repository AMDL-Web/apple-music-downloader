package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"amdl/internal/domain"
)

// ErrFileNotFound is returned when a slot has no files row.
var ErrFileNotFound = errors.New("file slot not found")

const fileColumns = `owner_kind, owner_id, song_id, slot, audio_state, lyrics_state, quality_priority, codec, quality_label,
	bit_depth, sample_rate, bitrate, file_size, path, failures, next_attempt_at, last_error, last_run_id, completed_at, checked_at, updated_at`

func scanFile(row jobScanner) (domain.File, error) {
	var (
		f                                  domain.File
		ownerKind, audio, lyrics, updated  string
		priority, next, completed, checked sql.NullString
	)
	if err := row.Scan(&ownerKind, &f.OwnerID, &f.SongID, &f.Slot, &audio, &lyrics, &priority, &f.Codec, &f.QualityLabel,
		&f.BitDepth, &f.SampleRate, &f.Bitrate, &f.FileSize, &f.Path, &f.Failures, &next, &f.LastError, &f.LastRunID, &completed, &checked, &updated); err != nil {
		return domain.File{}, err
	}
	f.OwnerKind = domain.ResourceKind(ownerKind)
	f.AudioState = domain.AudioState(audio)
	f.LyricsState = domain.LyricsStatus(lyrics)
	if priority.Valid && priority.String != "" {
		if err := json.Unmarshal([]byte(priority.String), &f.QualityPriority); err != nil {
			return domain.File{}, fmt.Errorf("slot %s/%s/%s/%s: decode quality_priority: %w", ownerKind, f.OwnerID, f.SongID, f.Slot, err)
		}
	}
	f.NextAttemptAt, f.CompletedAt, f.CheckedAt = parseOptionalTime(next), parseOptionalTime(completed), parseOptionalTime(checked)
	f.UpdatedAt = parseTime(updated)
	return f, nil
}

const fileKeyWhere = `owner_kind = ? AND owner_id = ? AND song_id = ? AND slot = ?`

func fileKeyArgs(k domain.FileKey) []any {
	return []any{string(k.OwnerKind), k.OwnerID, k.SongID, k.Slot}
}

// GetFile returns one slot, or ErrFileNotFound.
func (s *Store) GetFile(ctx context.Context, key domain.FileKey) (domain.File, error) {
	f, err := scanFile(s.db.QueryRowContext(ctx, `SELECT `+fileColumns+` FROM files WHERE `+fileKeyWhere, fileKeyArgs(key)...))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.File{}, ErrFileNotFound
	}
	return f, err
}

// ListFiles returns a record's slots by song, main slot first.
func (s *Store) ListFiles(ctx context.Context, kind domain.ResourceKind, ownerID string) ([]domain.File, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+fileColumns+` FROM files WHERE owner_kind = ? AND owner_id = ?
		ORDER BY song_id, slot <> 'main', slot`, string(kind), ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.File{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
