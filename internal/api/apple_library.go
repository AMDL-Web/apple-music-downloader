package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"amdl/internal/applelibrary"
	"amdl/internal/applemusic"
)

// AppleLibraryService is the cache of the user's Apple Music account library
// and its sync (internal/applelibrary). Every read is served from the cache.
type AppleLibraryService interface {
	List(ctx context.Context, q applelibrary.Query) (applelibrary.Page, error)
	PlaylistTracks(ctx context.Context, playlistID string) (applelibrary.Tracks, error)
	Trigger(ctx context.Context, mode string) (applelibrary.Status, error)
	Status(ctx context.Context) applelibrary.Status
	Running() string
	// Membership reports which catalog ids of a search group are in the
	// cached library; nil when the cache cannot tell.
	Membership(ctx context.Context, kind string, catalogIDs []string) map[string]bool
}

const (
	appleLibraryDefaultLimit = 50
	appleLibraryMaxLimit     = 200
	appleLibraryMaxQRunes    = 200
)

// appleLibraryList answers GET /apple-library/{kind}.
func (s *Server) appleLibraryList(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !isAppleLibraryKind(kind) {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "kind must be albums, songs, playlists or artists")
		return
	}
	q := r.URL.Query()
	limit, offset, err := parseBrowsePage(q, appleLibraryDefaultLimit, appleLibraryMaxLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
		return
	}
	sortBy := strings.TrimSpace(q.Get("sort"))
	if sortBy == "" {
		sortBy = "added"
		if kind == string(applemusic.LibraryArtists) {
			sortBy = "name"
		}
	}
	switch {
	case sortBy == "name":
	case sortBy == "added" && kind != string(applemusic.LibraryArtists):
	case sortBy == "artist" && (kind == string(applemusic.LibraryAlbums) || kind == string(applemusic.LibrarySongs)):
	default:
		writeError(w, http.StatusBadRequest, CodeInvalidRequest,
			"sort must be added, name or artist; artists sort by name only, artist applies to albums and songs")
		return
	}
	desc := sortBy == "added"
	switch q.Get("order") {
	case "":
	case "asc":
		desc = false
	case "desc":
		desc = true
	default:
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "order must be asc or desc")
		return
	}
	text := strings.Join(strings.Fields(q.Get("q")), " ")
	if len([]rune(text)) > appleLibraryMaxQRunes {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("q must be at most %d characters", appleLibraryMaxQRunes))
		return
	}
	page, err := s.appleLibrary.List(r.Context(), applelibrary.Query{
		Kind: kind, Sort: sortBy, Desc: desc, Q: text, Limit: limit, Offset: offset,
	})
	if err != nil {
		writeAppleLibraryError(w, err, "list apple library")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// appleLibraryPlaylistTracks answers GET
// /apple-library/playlists/{playlist_id}/tracks.
func (s *Server) appleLibraryPlaylistTracks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("playlist_id")
	if id == "" || len(id) > 64 {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "playlist_id must be a library playlist id (p.…)")
		return
	}
	tracks, err := s.appleLibrary.PlaylistTracks(r.Context(), id)
	if errors.Is(err, applelibrary.ErrPlaylistNotFound) {
		writeError(w, http.StatusNotFound, CodeCatalogNotFound, fmt.Sprintf("playlist %s is not in the cached Apple library", id))
		return
	}
	if err != nil {
		writeAppleLibraryError(w, err, "read apple library playlist tracks")
		return
	}
	writeJSON(w, http.StatusOK, tracks)
}

type appleLibrarySyncRequest struct {
	Mode string `json:"mode"`
}

// appleLibrarySync answers POST /apple-library/sync: it starts a pass in the
// background and answers 202 with the status, whose current.id is the pass.
func (s *Server) appleLibrarySync(w http.ResponseWriter, r *http.Request) {
	var req appleLibrarySyncRequest
	if r.ContentLength != 0 {
		if !decodeJSONBody(w, r, &req, true) {
			return
		}
	}
	switch req.Mode {
	case "":
		req.Mode = applelibrary.ModeFull
	case applelibrary.ModeFull, applelibrary.ModeIncremental:
	default:
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "mode must be full or incremental")
		return
	}
	status, err := s.appleLibrary.Trigger(r.Context(), req.Mode)
	if errors.Is(err, applelibrary.ErrRunning) {
		writeErrorDetails(w, http.StatusConflict, CodeAppleLibrarySyncRunning, err.Error(),
			map[string]string{"sync_id": s.appleLibrary.Running()})
		return
	}
	if err != nil {
		writeAppleLibraryError(w, err, "start apple library sync")
		return
	}
	writeJSON(w, http.StatusAccepted, status)
}

// appleLibraryStatus answers GET /apple-library/status.
func (s *Server) appleLibraryStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.appleLibrary.Status(r.Context()))
}

func writeAppleLibraryError(w http.ResponseWriter, err error, op string) {
	var unavailable applelibrary.UnavailableError
	if errors.As(err, &unavailable) {
		writeErrorDetails(w, http.StatusServiceUnavailable, CodeAppleLibraryUnavailable, unavailable.Error(),
			map[string]string{"reason": unavailable.Reason})
		return
	}
	writeStoreError(w, op, err)
}

func isAppleLibraryKind(kind string) bool {
	for _, k := range applemusic.LibraryKinds {
		if string(k) == kind {
			return true
		}
	}
	return false
}

// markAppleLibrary sets in_apple_library on search hits: Apple's own mark
// when the search carried a usable media-user-token, else the cache's answer
// for kinds the cache has read in full; otherwise the field stays out.
func (s *Server) markAppleLibrary(ctx context.Context, group applemusic.SearchType, hits []*searchHit, live map[string]bool) {
	var missing []string
	for _, h := range hits {
		if in, ok := live[h.ID]; ok {
			h.InAppleLibrary = &in
		} else {
			missing = append(missing, h.ID)
		}
	}
	if len(missing) == 0 {
		return
	}
	cached := s.appleLibrary.Membership(ctx, string(group), missing)
	if cached == nil {
		return
	}
	for _, h := range hits {
		if h.InAppleLibrary != nil {
			continue
		}
		if in, ok := cached[h.ID]; ok {
			h.InAppleLibrary = &in
		}
	}
}
