package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/jobs"
)

const (
	libraryDefaultLimit = 50
	libraryMaxLimit     = 500
)

// albumResource is the AlbumResource schema: an album record with its
// active run.
type albumResource struct {
	domain.Album
	ActiveJobID string    `json:"active_job_id,omitempty"`
	ActivityAt  time.Time `json:"activity_at"`
}

// playlistResource is the PlaylistResource schema.
type playlistResource struct {
	domain.Playlist
	ActiveJobID string    `json:"active_job_id,omitempty"`
	ActivityAt  time.Time `json:"activity_at"`
}

// fileSlot is the FileSlot schema: one quality of one track copy. Path is
// absolute.
type fileSlot struct {
	Slot          string              `json:"slot"`
	AudioState    domain.AudioState   `json:"audio_state"`
	LyricsState   domain.LyricsStatus `json:"lyrics_state,omitempty"`
	Codec         string              `json:"codec,omitempty"`
	QualityLabel  string              `json:"quality_label,omitempty"`
	BitDepth      int                 `json:"bit_depth,omitempty"`
	SampleRate    int                 `json:"sample_rate,omitempty"`
	Bitrate       int                 `json:"bitrate,omitempty"`
	FileSize      int64               `json:"file_size,omitempty"`
	Path          string              `json:"path,omitempty"`
	Failures      int                 `json:"failures"`
	NextAttemptAt *time.Time          `json:"next_attempt_at,omitempty"`
	LastError     string              `json:"last_error,omitempty"`
	LastJobID     string              `json:"last_job_id,omitempty"`
	CompletedAt   *time.Time          `json:"completed_at,omitempty"`
	CheckedAt     *time.Time          `json:"checked_at,omitempty"`
}

// recordTrack is the RecordTrack schema: a track of an album record or a
// song of a playlist record, with its slots.
type recordTrack struct {
	SongID       string              `json:"song_id"`
	DiscNumber   int                 `json:"disc_number,omitempty"`
	TrackNumber  int                 `json:"track_number,omitempty"`
	SongNumber   int                 `json:"song_number,omitempty"`
	Position     int                 `json:"position,omitempty"`
	Name         string              `json:"name"`
	ArtistName   string              `json:"artist_name,omitempty"`
	AlbumName    string              `json:"album_name,omitempty"`
	DurationMS   int                 `json:"duration_ms,omitempty"`
	HasLyrics    bool                `json:"has_lyrics"`
	Wanted       bool                `json:"wanted"`
	Availability domain.Availability `json:"availability"`
	RemovedAt    *time.Time          `json:"removed_at,omitempty"`
	Files        []fileSlot          `json:"files"`
}

// recordDetail is the extra of GET /library/{kind}/{id}.
type recordDetail struct {
	Sources        []domain.RecordSource `json:"sources"`
	InAppleLibrary *bool                 `json:"in_apple_library,omitempty"`
	LastCheckedAt  *time.Time            `json:"last_checked_at,omitempty"`
	Tracks         []recordTrack         `json:"tracks"`
}

// resourceKind reads the {kind} path segment: albums or playlists.
func resourceKind(r *http.Request) (domain.ResourceKind, bool) {
	switch r.PathValue("kind") {
	case "albums":
		return domain.ResourceAlbum, true
	case "playlists":
		return domain.ResourcePlaylist, true
	}
	return "", false
}

// recordRef reads {kind} and {id}, answering 404 resource_not_found for an
// unknown kind.
func recordRef(w http.ResponseWriter, r *http.Request) (domain.ResourceKind, string, bool) {
	kind, ok := resourceKind(r)
	if !ok {
		writeError(w, http.StatusNotFound, CodeResourceNotFound, fmt.Sprintf("no record kind %q: albums or playlists", r.PathValue("kind")))
		return "", "", false
	}
	return kind, r.PathValue("id"), true
}

// writeRecordError answers a failed record action.
func writeRecordError(w http.ResponseWriter, kind domain.ResourceKind, id string, err error) {
	switch {
	case errors.Is(err, db.ErrRecordNotFound):
		writeError(w, http.StatusNotFound, CodeResourceNotFound, fmt.Sprintf("no %s record %s", kind, id))
	case errors.Is(err, db.ErrRunActive):
		writeError(w, http.StatusConflict, CodeRunActive, fmt.Sprintf("%s %s has a queued or running run; pause it first", kind, id))
	case errors.Is(err, db.ErrMainNotDone):
		writeError(w, http.StatusConflict, CodeMainNotDone, "another quality needs the track's first quality downloaded; reset with codecs to choose the first quality")
	case errors.Is(err, db.ErrQualityPresent):
		writeError(w, http.StatusConflict, CodeQualityPresent, "the track's first quality already is that codec")
	case errors.Is(err, jobs.ErrQueueFull):
		writeError(w, http.StatusServiceUnavailable, CodeQueueFull, err.Error())
	default:
		writeStoreError(w, fmt.Sprintf("%s %s", kind, id), err)
	}
}

// parseRecordFilter reads the record list filters of a query.
func parseRecordFilter(q url.Values) (db.RecordFilter, error) {
	f := db.RecordFilter{Query: strings.TrimSpace(q.Get("q")), Sort: db.RecordSort(q.Get("sort")), Now: time.Now().UTC()}
	for _, v := range splitCSV(q["state"]) {
		st := domain.RecordState(v)
		if st != domain.RecordNone && st != domain.RecordPartial && st != domain.RecordComplete {
			return f, fmt.Errorf("state must be none, partial or complete, not %q", v)
		}
		f.States = append(f.States, st)
	}
	for _, v := range splitCSV(q["flag"]) {
		flag := db.RecordFlag(v)
		if !slices.Contains(db.RecordFlags, flag) {
			return f, fmt.Errorf("flag must be one of failed, given_up, missing, waiting, paused, unresolved, not %q", v)
		}
		f.Flags = append(f.Flags, flag)
	}
	if v := q.Get("source"); v != "" {
		src := domain.RecordSource(v)
		if !slices.Contains([]domain.RecordSource{domain.SourceManual, domain.SourceLibrarySync, domain.SourceArtist, domain.SourceImport1x}, src) {
			return f, fmt.Errorf("source must be manual, library_sync, artist or import_1x, not %q", v)
		}
		f.Source = src
	}
	switch f.Sort {
	case "", db.SortAdded, db.SortName, db.SortArtist, db.SortRelease, db.SortActivity:
	default:
		return f, fmt.Errorf("sort must be added, name, artist, release or activity, not %q", f.Sort)
	}
	return f, nil
}

func splitCSV(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// listLibrary answers GET /library/albums and GET /library/playlists.
func (s *Server) listLibrary(kind domain.ResourceKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		filter, err := parseRecordFilter(q)
		if err != nil {
			writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
			return
		}
		if filter.Limit, filter.Offset, err = parseBrowsePage(q, libraryDefaultLimit, libraryMaxLimit); err != nil {
			writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
			return
		}
		var items any
		var total int
		if kind == domain.ResourceAlbum {
			list, n, err := s.store.ListAlbums(r.Context(), filter)
			if err != nil {
				writeStoreError(w, "list album records", err)
				return
			}
			out := make([]albumResource, 0, len(list))
			for _, it := range list {
				out = append(out, albumResource{Album: it.Record, ActiveJobID: it.ActiveJobID, ActivityAt: it.ActivityAt})
			}
			items, total = out, n
		} else {
			list, n, err := s.store.ListPlaylists(r.Context(), filter)
			if err != nil {
				writeStoreError(w, "list playlist records", err)
				return
			}
			out := make([]playlistResource, 0, len(list))
			for _, it := range list {
				out = append(out, playlistResource{Playlist: it.Record, ActiveJobID: it.ActiveJobID, ActivityAt: it.ActivityAt})
			}
			items, total = out, n
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": filter.Limit, "offset": filter.Offset})
	}
}

// getLibraryRecord answers GET /library/albums/{id} and
// /library/playlists/{id}: the record, its tracks and each one's slots.
func (s *Server) getLibraryRecord(kind domain.ResourceKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		body, err := s.recordBody(r, kind, id)
		if err != nil {
			writeRecordError(w, kind, id, err)
			return
		}
		writeJSON(w, http.StatusOK, body)
	}
}

// recordBody reads a record as GET /library/{kind}/{id} answers it.
func (s *Server) recordBody(r *http.Request, kind domain.ResourceKind, id string) (any, error) {
	ctx := r.Context()
	files, err := s.store.ListFiles(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	slots := map[string][]fileSlot{}
	dir := s.store.DownloadsDir()
	for _, f := range files {
		slots[f.SongID] = append(slots[f.SongID], fileSlot{
			Slot: f.Slot, AudioState: f.AudioState, LyricsState: f.LyricsState, Codec: f.Codec, QualityLabel: f.QualityLabel,
			BitDepth: f.BitDepth, SampleRate: f.SampleRate, Bitrate: f.Bitrate, FileSize: f.FileSize,
			Path: domain.ResolveStoredPath(dir, f.Path), Failures: f.Failures, NextAttemptAt: f.NextAttemptAt,
			LastError: f.LastError, LastJobID: f.LastRunID, CompletedAt: f.CompletedAt, CheckedAt: f.CheckedAt,
		})
	}
	var detail recordDetail
	if detail.Sources, err = s.store.RecordSources(ctx, kind, id); err != nil {
		return nil, err
	}
	if detail.Sources == nil {
		detail.Sources = []domain.RecordSource{}
	}
	if detail.LastCheckedAt, err = s.store.LastCheckedAt(ctx, kind, id); err != nil {
		return nil, err
	}
	pin, err := s.manager.RecordPin(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	if in, ok := s.appleLibrary.Membership(ctx, pin, string(kind)+"s", []string{id})[id]; ok {
		detail.InAppleLibrary = &in
	}
	active, _, err := s.store.ActiveRunID(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	activity, err := s.store.RecordActivity(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	detail.Tracks = []recordTrack{}
	if kind == domain.ResourceAlbum {
		album, err := s.store.GetAlbum(ctx, id)
		if err != nil {
			return nil, err
		}
		tracks, err := s.store.ListAlbumTracks(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, t := range tracks {
			detail.Tracks = append(detail.Tracks, recordTrack{
				SongID: t.SongID, DiscNumber: t.DiscNumber, TrackNumber: t.TrackNumber, SongNumber: t.SongNumber,
				Name: t.Name, ArtistName: t.ArtistName, DurationMS: t.DurationMS, HasLyrics: t.HasLyrics,
				Wanted: t.Wanted, Availability: t.Availability, Files: orEmpty(slots[t.SongID]),
			})
		}
		return struct {
			albumResource
			recordDetail
		}{albumResource{Album: album, ActiveJobID: active, ActivityAt: activity}, detail}, nil
	}
	playlist, err := s.store.GetPlaylist(ctx, id)
	if err != nil {
		return nil, err
	}
	tracks, err := s.store.ListPlaylistTracks(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, t := range tracks {
		detail.Tracks = append(detail.Tracks, recordTrack{
			SongID: t.SongID, Position: t.Position, Name: t.Name, ArtistName: t.ArtistName, AlbumName: t.AlbumName,
			DurationMS: t.DurationMS, HasLyrics: t.HasLyrics, Wanted: t.RemovedAt == nil, Availability: t.Availability,
			RemovedAt: t.RemovedAt, Files: orEmpty(slots[t.SongID]),
		})
	}
	return struct {
		playlistResource
		recordDetail
	}{playlistResource{Playlist: playlist, ActiveJobID: active, ActivityAt: activity}, detail}, nil
}

func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// librarySummary answers GET /library/summary.
func (s *Server) librarySummary(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	albums, err := s.store.CountRecords(r.Context(), domain.ResourceAlbum, now)
	if err != nil {
		writeStoreError(w, "count album records", err)
		return
	}
	playlists, err := s.store.CountRecords(r.Context(), domain.ResourcePlaylist, now)
	if err != nil {
		writeStoreError(w, "count playlist records", err)
		return
	}
	pending, err := s.store.CountPending(r.Context())
	if err != nil {
		writeStoreError(w, "count pending adds", err)
		return
	}
	check, err := s.lastCheck(r)
	if err != nil {
		writeStoreError(w, "read the last disk check", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"albums": albums, "playlists": playlists, "pending": pending, "check": check})
}

// libraryAddRequest is the LibraryAddRequest schema.
type libraryAddRequest struct {
	URLs []string `json:"urls"`
	// Codecs is the codec order of the main slots the add creates
	// (overrides.quality_priority).
	Codecs    []string                  `json:"codecs,omitempty"`
	Overrides *config.DownloadOverrides `json:"overrides,omitempty"`
	// Now moves each run the add queues to the front.
	Now bool `json:"now,omitempty"`
	// Include picks an artist link's albums: all or albums.
	Include string `json:"include,omitempty"`
}

// addToLibrary answers POST /library/add: POST /downloads with codecs, now
// and include.
func (s *Server) addToLibrary(w http.ResponseWriter, r *http.Request) {
	var req libraryAddRequest
	if !decodeJSONBody(w, r, &req, true) {
		return
	}
	if req.Include != "" && req.Include != "all" && req.Include != "albums" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "include must be all or albums")
		return
	}
	if len(req.Codecs) > 0 {
		for _, c := range req.Codecs {
			if !slices.Contains(config.Codecs, c) {
				writeError(w, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("codec %q is not one of %s", c, strings.Join(config.Codecs, ", ")))
				return
			}
		}
		if req.Overrides == nil {
			req.Overrides = &config.DownloadOverrides{}
		}
		codecs := req.Codecs
		req.Overrides.QualityPriority = &codecs
	}
	s.submit(w, r, req.URLs, req.Overrides, jobs.SubmitOptions{ArtistInclude: req.Include}, req.Now)
}

// listPending answers GET /library/pending.
func (s *Server) listPending(w http.ResponseWriter, r *http.Request) {
	state := domain.PendingAddState(r.URL.Query().Get("state"))
	if state != "" && state != domain.PendingAddPending && state != domain.PendingAddFailed {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "state must be pending or failed")
		return
	}
	rows, err := s.store.ListPending(r.Context(), state)
	if err != nil {
		writeStoreError(w, "list pending adds", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func pendingID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("pending_id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, CodeResourceNotFound, "no pending add "+r.PathValue("pending_id"))
		return 0, false
	}
	return id, true
}

func writePendingError(w http.ResponseWriter, id int64, err error) {
	if errors.Is(err, db.ErrPendingNotFound) {
		writeError(w, http.StatusNotFound, CodeResourceNotFound, fmt.Sprintf("no pending add %d", id))
		return
	}
	writeStoreError(w, fmt.Sprintf("pending add %d", id), err)
}

// deletePending answers DELETE /library/pending/{pending_id}.
func (s *Server) deletePending(w http.ResponseWriter, r *http.Request) {
	id, ok := pendingID(w, r)
	if !ok {
		return
	}
	if err := s.manager.DeletePending(r.Context(), id); err != nil {
		writePendingError(w, id, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// retryPending answers POST /library/pending/{pending_id}/retry.
func (s *Server) retryPending(w http.ResponseWriter, r *http.Request) {
	id, ok := pendingID(w, r)
	if !ok {
		return
	}
	if err := s.manager.RetryPending(r.Context(), id); err != nil {
		writePendingError(w, id, err)
		return
	}
	row, err := s.store.GetPending(r.Context(), id)
	if err != nil {
		writePendingError(w, id, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// recordDownloadRequest is the RecordDownloadRequest schema.
type recordDownloadRequest struct {
	Codec string   `json:"codec,omitempty"`
	Songs []string `json:"songs,omitempty"`
	Now   bool     `json:"now,omitempty"`
}

// downloadRecord answers POST /library/{kind}/{id}/download.
func (s *Server) downloadRecord(w http.ResponseWriter, r *http.Request) {
	kind, id, ok := recordRef(w, r)
	if !ok {
		return
	}
	var req recordDownloadRequest
	if r.ContentLength != 0 && !decodeJSONBody(w, r, &req, true) {
		return
	}
	if req.Codec != "" && !slices.Contains(config.Codecs, req.Codec) {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("codec %q is not one of %s", req.Codec, strings.Join(config.Codecs, ", ")))
		return
	}
	res, err := s.manager.DownloadRecord(r.Context(), jobs.RecordDownload{Kind: kind, ID: id, Codec: req.Codec, SongIDs: req.Songs, Now: req.Now})
	if err != nil {
		writeRecordError(w, kind, id, err)
		return
	}
	switch {
	case res.Job != nil:
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "job": res.Job})
	case res.AlreadyDownloaded:
		writeJSON(w, http.StatusOK, map[string]any{"status": "already_downloaded"})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"status": "waiting"})
	}
}

// resetRequest is the RecordResetRequest schema.
type resetRequest struct {
	What   string              `json:"what"`
	Slot   string              `json:"slot,omitempty"`
	Songs  []string            `json:"songs,omitempty"`
	Codecs []string            `json:"codecs,omitempty"`
	States []domain.AudioState `json:"states,omitempty"`
}

// validate checks a reset's fields, answering 400 invalid_scope.
func (q resetRequest) validate(w http.ResponseWriter) (jobs.ResetWhat, bool) {
	what := jobs.ResetWhat(q.What)
	switch what {
	case jobs.ResetAll, jobs.ResetAudio, jobs.ResetLyrics, jobs.ResetCovers:
	default:
		writeError(w, http.StatusBadRequest, CodeInvalidScope, "what must be all, audio, lyrics or covers")
		return "", false
	}
	if what != jobs.ResetAudio && (q.Slot != "" || len(q.Codecs) > 0 || len(q.States) > 0) {
		writeError(w, http.StatusBadRequest, CodeInvalidScope, "slot, codecs and states go with what: audio")
		return "", false
	}
	if q.Slot != "" && q.Slot != domain.SlotMain && !slices.Contains(config.Codecs, q.Slot) {
		writeError(w, http.StatusBadRequest, CodeInvalidScope, "slot must be main or a codec")
		return "", false
	}
	if len(q.Codecs) > 0 && q.Slot != domain.SlotMain {
		writeError(w, http.StatusBadRequest, CodeInvalidScope, "codecs set the order of the main slot: slot must be main")
		return "", false
	}
	for _, c := range q.Codecs {
		if !slices.Contains(config.Codecs, c) {
			writeError(w, http.StatusBadRequest, CodeInvalidScope, fmt.Sprintf("codec %q is not one of %s", c, strings.Join(config.Codecs, ", ")))
			return "", false
		}
	}
	for _, st := range q.States {
		switch st {
		case domain.AudioPending, domain.AudioDone, domain.AudioFailed, domain.AudioGivenUp, domain.AudioUnavailable, domain.AudioMissing:
		default:
			writeError(w, http.StatusBadRequest, CodeInvalidScope, fmt.Sprintf("states holds %q, not a slot state a reset re-arms", st))
			return "", false
		}
	}
	return what, true
}

// resetRecord answers POST /library/{kind}/{id}/reset.
func (s *Server) resetRecord(w http.ResponseWriter, r *http.Request) {
	kind, id, ok := recordRef(w, r)
	if !ok {
		return
	}
	var req resetRequest
	if !decodeJSONBody(w, r, &req, true) {
		return
	}
	what, ok := req.validate(w)
	if !ok {
		return
	}
	res, err := s.manager.Reset(r.Context(), jobs.ResetRequest{Kind: kind, ID: id, What: what, SongIDs: req.Songs, Slot: req.Slot, Codecs: req.Codecs, States: req.States})
	if err != nil {
		writeRecordError(w, kind, id, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slots": res.Slots, "job": res.Run})
}

// libraryResetRequest is the LibraryResetRequest schema.
type libraryResetRequest struct {
	resetRequest
	// Kind limits the reset to albums or playlists; both when empty.
	Kind   string `json:"kind,omitempty"`
	Filter struct {
		State  []domain.RecordState `json:"state,omitempty"`
		Flag   []db.RecordFlag      `json:"flag,omitempty"`
		Source domain.RecordSource  `json:"source,omitempty"`
		Q      string               `json:"q,omitempty"`
	} `json:"filter"`
}

// resetLibrary answers POST /library/reset: a reset of every record the
// filter selects. An audio reset by the failed, given_up or missing flag
// re-arms only the slots in that state, so "download missing" is
// {what: audio, filter: {flag: [missing]}}.
func (s *Server) resetLibrary(w http.ResponseWriter, r *http.Request) {
	var req libraryResetRequest
	if !decodeJSONBody(w, r, &req, true) {
		return
	}
	what, ok := req.validate(w)
	if !ok {
		return
	}
	if req.Songs != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidScope, "songs name tracks of one record: use POST /library/{kind}/{id}/reset")
		return
	}
	q := url.Values{}
	for _, st := range req.Filter.State {
		q.Add("state", string(st))
	}
	for _, f := range req.Filter.Flag {
		q.Add("flag", string(f))
	}
	if req.Filter.Source != "" {
		q.Set("source", string(req.Filter.Source))
	}
	q.Set("q", req.Filter.Q)
	filter, err := parseRecordFilter(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidScope, err.Error())
		return
	}
	if len(filter.States) == 0 && len(filter.Flags) == 0 && filter.Source == "" && filter.Query == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidScope, "filter selects every record; name a state, flag, source or q")
		return
	}
	var kinds []domain.ResourceKind
	switch req.Kind {
	case "":
		kinds = []domain.ResourceKind{domain.ResourceAlbum, domain.ResourcePlaylist}
	case "albums":
		kinds = []domain.ResourceKind{domain.ResourceAlbum}
	case "playlists":
		kinds = []domain.ResourceKind{domain.ResourcePlaylist}
	default:
		writeError(w, http.StatusBadRequest, CodeInvalidScope, "kind must be albums or playlists")
		return
	}
	states := req.States
	if what == jobs.ResetAudio && len(states) == 0 {
		states = jobs.AudioStatesForFlags(filter.Flags)
	}
	res, err := s.manager.ResetMany(r.Context(), jobs.ResetSelection{Kinds: kinds, Filter: filter, What: what, Slot: req.Slot, Codecs: req.Codecs, States: states})
	if err != nil {
		writeStoreError(w, "reset records", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// pauseRecord answers POST /library/{kind}/{id}/pause and resumeRecord
// /resume; both answer the record.
func (s *Server) pauseRecord(w http.ResponseWriter, r *http.Request) {
	s.setPaused(w, r, true)
}

func (s *Server) resumeRecord(w http.ResponseWriter, r *http.Request) {
	s.setPaused(w, r, false)
}

func (s *Server) setPaused(w http.ResponseWriter, r *http.Request, paused bool) {
	kind, id, ok := recordRef(w, r)
	if !ok {
		return
	}
	var err error
	if paused {
		err = s.manager.Pause(r.Context(), kind, id)
	} else {
		err = s.manager.Resume(r.Context(), kind, id)
	}
	if err != nil {
		writeRecordError(w, kind, id, err)
		return
	}
	body, err := s.recordBody(r, kind, id)
	if err != nil {
		writeRecordError(w, kind, id, err)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// deleteRecord answers DELETE /library/{kind}/{id}: the record goes, its
// files and runs stay.
func (s *Server) deleteRecord(w http.ResponseWriter, r *http.Request) {
	kind, id, ok := recordRef(w, r)
	if !ok {
		return
	}
	if err := s.manager.DeleteRecord(r.Context(), kind, id); err != nil {
		writeRecordError(w, kind, id, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkRequest is the CheckRequest schema.
type checkRequest struct {
	Kind  string `json:"kind,omitempty"`
	ID    string `json:"id,omitempty"`
	Force bool   `json:"force,omitempty"`
}

// startCheck answers POST /library/check.
func (s *Server) startCheck(w http.ResponseWriter, r *http.Request) {
	var req checkRequest
	if r.ContentLength != 0 && !decodeJSONBody(w, r, &req, true) {
		return
	}
	creq := jobs.CheckRequest{Force: req.Force, Trigger: domain.CheckManual}
	switch {
	case req.Kind == "" && req.ID == "":
	case (req.Kind == "albums" || req.Kind == "playlists") && req.ID != "":
		kind := domain.ResourceAlbum
		if req.Kind == "playlists" {
			kind = domain.ResourcePlaylist
		}
		creq.Resource = &domain.ResourceRef{Kind: kind, ID: req.ID}
	default:
		writeError(w, http.StatusBadRequest, CodeInvalidScope, "kind (albums or playlists) and id go together; neither checks the whole library")
		return
	}
	run, err := s.manager.StartCheck(r.Context(), creq)
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, map[string]any{"check": run})
	case errors.Is(err, jobs.ErrCheckRunning):
		writeError(w, http.StatusConflict, CodeCheckRunning, err.Error())
	case errors.Is(err, jobs.ErrDownloadsDirUnavailable):
		writeError(w, http.StatusServiceUnavailable, CodeDownloadsDirUnavailable, err.Error())
	case errors.Is(err, db.ErrRecordNotFound):
		writeError(w, http.StatusNotFound, CodeResourceNotFound, fmt.Sprintf("no %s record %s", creq.Resource.Kind, creq.Resource.ID))
	default:
		writeStoreError(w, "start a disk check", err)
	}
}

// getCheck answers GET /library/check: the running or last disk check, null
// before the first.
func (s *Server) getCheck(w http.ResponseWriter, r *http.Request) {
	check, err := s.lastCheck(r)
	if err != nil {
		writeStoreError(w, "read the last disk check", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"check": check})
}

// lastCheck is the check this process runs, else the newest stored one
// (Store.LastCheckRun skips a finished check of a removed record); nil
// before any.
func (s *Server) lastCheck(r *http.Request) (*domain.CheckRun, error) {
	if run, _, ok := s.manager.CheckStatus(); ok && run.FinishedAt == nil {
		return &run, nil
	}
	run, found, err := s.store.LastCheckRun(r.Context())
	if err != nil || !found {
		return nil, err
	}
	if run.FinishedAt == nil {
		// Left by a server that stopped during it.
		run.AbortedReason = domain.CheckAbortedInterrupted
	}
	return &run, nil
}
