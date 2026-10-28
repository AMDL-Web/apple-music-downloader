package api

import (
	"context"
	"net/http"
	"strings"

	"amdl/internal/media"
)

// QualityService probes the declared codecs and audio quality of a URL
// without creating a job.
type QualityService interface {
	QueryQuality(context.Context, media.QualityRequest) (media.QualityResult, error)
}

// DeveloperTokenService mints Apple Music developer tokens in local signing
// mode.
type DeveloperTokenService interface {
	MintDeveloperToken() (string, error)
}

func (s *Server) queryQuality(w http.ResponseWriter, r *http.Request) {
	var req media.QualityRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "url is required")
		return
	}
	if s.quality == nil {
		writeFeatureUnavailable(w, "the quality probe")
		return
	}
	result, err := s.quality.QueryQuality(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, CodeUpstreamError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// health answers 503 with the HealthResponse body, not the error envelope:
// monitors and the console read status and database_error from it.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if s.store != nil {
		if err := s.store.Ping(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status":         "degraded",
				"database_error": err.Error(),
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) listHooks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.manager.ListHooks())
}

// developerToken hands out a freshly signed Apple Music developer token. Only
// local signing mode can serve it: the legacy web-discovered token is
// origin-locked to music.apple.com and would be rejected anywhere else.
func (s *Server) developerToken(w http.ResponseWriter, r *http.Request) {
	if s.devToken == nil {
		writeFeatureUnavailable(w, "developer token signing")
		return
	}
	if !s.currentConfig().Catalog.DeveloperTokenSigningEnabled() {
		writeError(w, http.StatusConflict, CodeSigningDisabled, "developer token endpoint requires local signing mode (catalog.apple_music_* keys); the web-discovered token is origin-restricted and cannot be shared")
		return
	}
	token, err := s.devToken.MintDeveloperToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternalError, "mint developer token: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}
