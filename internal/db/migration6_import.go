package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// stoppedByUpgrade is the error of a 1.x job that was still queued or running
// when the 2.0 migration made it a cancelled run.
const stoppedByUpgrade = "stopped by the 2.0 upgrade"

// The columns copied verbatim from the 1.x tables to the 2.0 ones.
const (
	importRunColumns = `id, input, type, storefront, title, artwork_url, artist_name, artist_url, curator_name, release_date, genre,
		artwork_bg_color, artwork_text_color1, artwork_text_color2, artwork_text_color3, artwork_text_color4,
		motion_artwork_url, motion_artwork_tall_url,
		motion_artwork_bg_color, motion_artwork_text_color1, motion_artwork_text_color2, motion_artwork_text_color3, motion_artwork_text_color4,
		motion_artwork_tall_bg_color, motion_artwork_tall_text_color1, motion_artwork_tall_text_color2, motion_artwork_tall_text_color3, motion_artwork_tall_text_color4,
		canonical_key, status, total_items, error, created_at, updated_at`
	importItemColumns = `id, job_id, adam_id, kind, idx, title, artist, album, duration_ms, artwork_url, has_lyrics, lyrics_status, status,
		download_progress, decrypt_progress, progress_resolved, progress_remuxed, progress_verified, progress_tagged, progress_saved,
		codec, bit_depth, sample_rate, bitrate, file_size, retry_kind, attempt, max_attempts, status_message, output_path, error, created_at, updated_at`
	importEventColumns = `id, job_id, item_id, type, phase, message, payload, created_at`
)

// import1x moves the 1.x jobs tables into the 2.0 ones (design §5.2):
//
//   - Every job becomes a run with its id, type, values and timestamps,
//     trigger import_1x and priority 1. started_at and finished_at come from
//     the events as the rc migration 2 did; queue_order follows created_at.
//     force is dropped and force_overwrite is stripped from overrides.
//   - Album runs and pl. playlist runs name their record by the catalog id of
//     their canonical key, without the storefront. Song, artist, station and
//     library-playlist runs name none.
//   - A job still queued or running ends cancelled with stoppedByUpgrade and
//     its unfinished items cancelled; its intent becomes a paused pending_adds
//     row per (kind, catalog id). Stations keep only the run.
//   - Items become run_items in slot main; events become run_events with
//     their ids, and the AUTOINCREMENT high-water mark carries over, so SSE
//     cursors stay valid. item_progress events of terminal runs are dropped.
//   - Completed and skipped_existing items of pl. playlist jobs with an
//     output path become done files of a paused, unresolved playlist record,
//     latest job first per (playlist, song); a path a later row would share
//     is skipped.
//
// Finished song, album and artist jobs are history only: no record and no
// files row.
func import1x(ctx context.Context, tx *sql.Tx, env migrationEnv) error {
	stamp := formatTime(env.now)
	if err := execAll(ctx, tx, []string{
		`INSERT INTO runs(` + importRunColumns + `, overrides, trigger, priority)
			SELECT ` + importRunColumns + `,
				CASE WHEN json_valid(overrides) AND json_type(overrides) = 'object'
						AND json_type(overrides, '$.force_overwrite') IS NOT NULL
					THEN CASE WHEN json_remove(overrides, '$.force_overwrite') = '{}' THEN ''
						ELSE json_remove(overrides, '$.force_overwrite') END
					ELSE overrides END,
				'import_1x', 1
			FROM jobs ORDER BY rowid`,
		`UPDATE runs SET started_at = s.started_at
			FROM (SELECT e.job_id, MIN(e.created_at) AS started_at FROM job_events e
				WHERE e.type = 'job_started'
				AND e.id > COALESCE((SELECT MAX(r.id) FROM job_events r
					WHERE r.job_id = e.job_id AND r.type = 'job_retried'), 0)
				GROUP BY e.job_id) AS s
			WHERE s.job_id = runs.id`,
		`UPDATE runs SET finished_at = f.finished_at
			FROM (SELECT job_id, MAX(created_at) AS finished_at FROM job_events
				WHERE type IN ('job_finished','job_failed','job_cancelled') GROUP BY job_id) AS f
			WHERE f.job_id = runs.id AND runs.status IN ('completed','failed','cancelled')`,
		`UPDATE runs SET queue_order = o.n * 1048576
			FROM (SELECT rowid AS rid, row_number() OVER (ORDER BY created_at, rowid) AS n FROM runs) AS o
			WHERE o.rid = runs.rowid`,
	}); err != nil {
		return err
	}

	stopped, err := stoppedRuns(ctx, tx)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runs SET status = 'cancelled', error = ?, finished_at = ?
		WHERE status IN ('queued','running')`, stoppedByUpgrade, stamp); err != nil {
		return fmt.Errorf("stop active runs: %w", err)
	}
	if err := setRunResources(ctx, tx); err != nil {
		return err
	}

	if err := execAll(ctx, tx, []string{
		`INSERT INTO run_items(` + importItemColumns + `, slot, owner_kind, owner_id)
			SELECT ` + prefixColumns("i.", importItemColumns) + `, 'main',
				COALESCE(r.resource_kind, ''), COALESCE(r.resource_id, '')
			FROM job_items i LEFT JOIN runs r ON r.id = i.job_id ORDER BY i.rowid`,
	}); err != nil {
		return err
	}
	for _, run := range stopped {
		if _, err := tx.ExecContext(ctx, `UPDATE run_items SET status = 'cancelled'
			WHERE job_id = ? AND status NOT IN ('completed','failed','skipped_existing','cancelled')`, run.id); err != nil {
			return fmt.Errorf("cancel unfinished items of %s: %w", run.id, err)
		}
	}
	if err := execAll(ctx, tx, []string{
		`INSERT INTO run_events(` + importEventColumns + `)
			SELECT ` + importEventColumns + ` FROM job_events
			WHERE NOT (type = 'item_progress'
				AND job_id IN (SELECT id FROM runs WHERE status IN ('completed','failed','cancelled')))
			ORDER BY id`,
		// Event ids are SSE cursors: the next id stays above every id 1.x
		// handed out, including deleted ones.
		`UPDATE sqlite_sequence
			SET seq = MAX(seq, COALESCE((SELECT seq FROM sqlite_sequence WHERE name = 'job_events'), 0))
			WHERE name = 'run_events'`,
		`INSERT INTO sqlite_sequence(name, seq)
			SELECT 'run_events', seq FROM sqlite_sequence WHERE name = 'job_events'
			AND NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'run_events')`,
	}); err != nil {
		return err
	}

	if err := importPlaylistFiles(ctx, tx, env); err != nil {
		return err
	}
	return importStoppedIntents(ctx, tx, stopped)
}

func prefixColumns(prefix, columns string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		parts[i] = prefix + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}

// stoppedRun is a 1.x job that was queued or running at the import.
type stoppedRun struct {
	id, jobType, key, input, overrides, createdAt string
}

func stoppedRuns(ctx context.Context, tx *sql.Tx) ([]stoppedRun, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, type, canonical_key, input, overrides, created_at FROM runs
		WHERE status IN ('queued','running') ORDER BY queue_order, rowid`)
	if err != nil {
		return nil, fmt.Errorf("list active jobs: %w", err)
	}
	defer rows.Close()
	var out []stoppedRun
	for rows.Next() {
		var r stoppedRun
		if err := rows.Scan(&r.id, &r.jobType, &r.key, &r.input, &r.overrides, &r.createdAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// setRunResources names the record of every album run and pl. playlist run.
func setRunResources(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, canonical_key FROM runs WHERE type IN ('album','playlist')`)
	if err != nil {
		return fmt.Errorf("list album and playlist runs: %w", err)
	}
	type ref struct{ id, kind, catalogID string }
	var refs []ref
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			rows.Close()
			return err
		}
		kind, _, catalogID, ok := parseKey1x(key)
		if !ok || (kind != "album" && kind != "playlist") || (kind == "playlist" && !strings.HasPrefix(catalogID, "pl.")) {
			continue
		}
		refs = append(refs, ref{id, kind, catalogID})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range refs {
		if _, err := tx.ExecContext(ctx, `UPDATE runs SET resource_kind = ?, resource_id = ? WHERE id = ?`, r.kind, r.catalogID, r.id); err != nil {
			return fmt.Errorf("name record of run %s: %w", r.id, err)
		}
	}
	return nil
}

// importPlaylistFiles turns the done items of pl. playlist runs into files
// rows of paused, unresolved playlist records. Runs are read latest first and
// INSERT OR IGNORE keeps the first row per slot and per path, so the latest
// download of a song wins and a path another playlist already holds is
// skipped. A playlist gets a record only when it gets a file.
func importPlaylistFiles(ctx context.Context, tx *sql.Tx, env migrationEnv) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, resource_id, title, curator_name, artwork_url,
			artwork_bg_color, artwork_text_color1, artwork_text_color2, artwork_text_color3, artwork_text_color4, created_at
		FROM runs WHERE resource_kind = 'playlist' ORDER BY created_at DESC, rowid DESC`)
	if err != nil {
		return fmt.Errorf("list playlist runs: %w", err)
	}
	type playlistRun struct {
		runID, playlistID, name, curator, createdAt string
		artwork                                     importArtwork
	}
	var runs []playlistRun
	for rows.Next() {
		var r playlistRun
		var a importArtwork
		if err := rows.Scan(&r.runID, &r.playlistID, &r.name, &r.curator, &a.URL,
			&a.Colors.Bg, &a.Colors.Text1, &a.Colors.Text2, &a.Colors.Text3, &a.Colors.Text4, &r.createdAt); err != nil {
			rows.Close()
			return err
		}
		r.artwork = a
		runs = append(runs, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	stamp := formatTime(env.now)
	created := map[string]bool{}
	for _, run := range runs {
		items, err := doneItems1x(ctx, tx, run.runID)
		if err != nil {
			return err
		}
		for _, it := range items {
			res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO files(owner_kind, owner_id, song_id, slot, audio_state, lyrics_state,
					codec, bit_depth, sample_rate, bitrate, file_size, path, last_run_id, completed_at, updated_at)
				VALUES('playlist', ?, ?, 'main', 'done', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				run.playlistID, it.songID, it.lyricsStatus, it.codec, it.bitDepth, it.sampleRate, it.bitrate, it.fileSize,
				storedPath(env.downloadsDir, it.outputPath), run.runID, it.updatedAt, it.updatedAt)
			if err != nil {
				return fmt.Errorf("import file of %s in %s: %w", it.songID, run.playlistID, err)
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO playlist_tracks(playlist_id, song_id, position, name, artist_name, album_name, duration_ms, has_lyrics)
				VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
				run.playlistID, it.songID, it.index, it.title, it.artist, it.album, it.durationMS, it.hasLyrics); err != nil {
				return fmt.Errorf("import track %s of %s: %w", it.songID, run.playlistID, err)
			}
			if created[run.playlistID] {
				continue
			}
			created[run.playlistID] = true
			artwork, err := json.Marshal(run.artwork)
			if err != nil {
				return err
			}
			// The latest run names the record; added_at is the first run's.
			if _, err := tx.ExecContext(ctx, `INSERT INTO playlists(id, name, curator_name, artwork, paused, priority, added_at, updated_at)
				VALUES(?, ?, ?, ?, 1, 1,
					(SELECT MIN(created_at) FROM runs WHERE resource_kind = 'playlist' AND resource_id = ?), ?)`,
				run.playlistID, run.name, run.curator, string(artwork), run.playlistID, stamp); err != nil {
				return fmt.Errorf("import playlist %s: %w", run.playlistID, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO resource_sources(resource_kind, resource_id, source, added_at)
				SELECT 'playlist', id, 'import_1x', added_at FROM playlists WHERE id = ?`, run.playlistID); err != nil {
				return fmt.Errorf("import source of %s: %w", run.playlistID, err)
			}
		}
	}
	for id := range created {
		if err := recomputeRollup(ctx, tx, "playlist", id, env.now); err != nil {
			return err
		}
	}
	return nil
}

// importArtwork has the JSON shape of domain.Artwork, spelled out here so the
// migration does not change when the domain type does.
type importArtwork struct {
	URL    string `json:"url,omitempty"`
	Colors struct {
		Bg    string `json:"bg,omitempty"`
		Text1 string `json:"text1,omitempty"`
		Text2 string `json:"text2,omitempty"`
		Text3 string `json:"text3,omitempty"`
		Text4 string `json:"text4,omitempty"`
	} `json:"colors"`
}

type doneItem1x struct {
	songID, title, artist, album, lyricsStatus, codec, outputPath, updatedAt string
	index, durationMS, bitDepth, sampleRate, bitrate                         int
	hasLyrics                                                                bool
	fileSize                                                                 int64
}

func doneItems1x(ctx context.Context, tx *sql.Tx, runID string) ([]doneItem1x, error) {
	rows, err := tx.QueryContext(ctx, `SELECT adam_id, idx, title, artist, album, duration_ms, has_lyrics, lyrics_status,
			codec, bit_depth, sample_rate, bitrate, file_size, output_path, updated_at
		FROM run_items WHERE job_id = ? AND status IN ('completed','skipped_existing') AND output_path <> ''
		ORDER BY idx, rowid`, runID)
	if err != nil {
		return nil, fmt.Errorf("list done items of %s: %w", runID, err)
	}
	defer rows.Close()
	var out []doneItem1x
	for rows.Next() {
		var it doneItem1x
		if err := rows.Scan(&it.songID, &it.index, &it.title, &it.artist, &it.album, &it.durationMS, &it.hasLyrics, &it.lyricsStatus,
			&it.codec, &it.bitDepth, &it.sampleRate, &it.bitrate, &it.fileSize, &it.outputPath, &it.updatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// importStoppedIntents keeps what a stopped job asked for as a paused
// pending_adds row, the first in run order per (kind, catalog id). The
// resolver turns it into a paused record; nothing downloads before a Resume.
func importStoppedIntents(ctx context.Context, tx *sql.Tx, stopped []stoppedRun) error {
	for _, run := range stopped {
		kind, _, catalogID, ok := parseKey1x(run.key)
		if !ok {
			continue
		}
		include, hint := "", ""
		switch kind {
		case "album", "playlist":
		case "song":
			hint = albumHint1x(run.input, catalogID)
		case "artist":
			include = "all"
		default: // station: the run is all there is
			continue
		}
		var overrides string
		if err := tx.QueryRowContext(ctx, `SELECT CASE WHEN json_valid(overrides) AND json_type(overrides) = 'object' THEN overrides ELSE '' END FROM runs WHERE id = ?`, run.id).Scan(&overrides); err != nil {
			return fmt.Errorf("read overrides of %s: %w", run.id, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO pending_adds(kind, catalog_id, album_hint, source, priority, paused, include, overrides, added_at, state)
			VALUES(?, ?, ?, 'import_1x', 1, 1, ?, ?, ?, 'pending')`,
			kind, catalogID, hint, include, overrides, run.createdAt); err != nil {
			return fmt.Errorf("keep the intent of %s: %w", run.id, err)
		}
	}
	return nil
}

// albumHint1x returns the album id of an album-track link (album/…/ID?i=SONG)
// whose song is songID, else "".
func albumHint1x(input, songID string) string {
	u, err := url.Parse(strings.TrimSpace(input))
	if err != nil || u.Query().Get("i") != songID {
		return ""
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, segment := range segments {
		if segment == "album" && i < len(segments)-1 {
			return segments[len(segments)-1]
		}
	}
	return ""
}

// parseKey1x reads a 1.x canonical key "type:storefront:id", including the
// legacy keys with a 12-hex-digit destination segment. A frozen copy of
// internal/jobs#ParseCanonicalKey at the 2.0 migration: internal/jobs imports
// this package, and a migration must not change with later code.
func parseKey1x(key string) (jobType, storefront, id string, ok bool) {
	parts := strings.Split(key, ":")
	if len(parts) < 3 {
		return "", "", "", false
	}
	idParts := parts[2:]
	if len(idParts) > 1 && isLegacyDestination1x(idParts[len(idParts)-1]) {
		idParts = idParts[:len(idParts)-1]
	}
	id = strings.Join(idParts, ":")
	jobType, storefront = parts[0], parts[1]
	if jobType == "" || storefront == "" || id == "" {
		return "", "", "", false
	}
	return jobType, storefront, id, true
}

func isLegacyDestination1x(s string) bool {
	if len(s) != 12 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
