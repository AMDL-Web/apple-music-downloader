package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"amdl/internal/media"
)

type qualityService interface {
	QueryQuality(context.Context, media.QualityRequest) (media.QualityResult, error)
}

type developerTokenService interface {
	MintDeveloperToken() (string, error)
}

func (s *Server) queryQuality(w http.ResponseWriter, r *http.Request) {
	var req media.QualityRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("url is required"))
		return
	}
	if s.quality == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("quality service is not configured"))
		return
	}
	result, err := s.quality.QueryQuality(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

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
	if s.devToken == nil || !s.currentConfig().Catalog.DeveloperTokenSigningEnabled() {
		writeError(w, http.StatusConflict, fmt.Errorf("developer token endpoint requires local signing mode (catalog.apple_music_* keys); the web-discovered token is origin-restricted and cannot be shared"))
		return
	}
	token, err := s.devToken.MintDeveloperToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}
