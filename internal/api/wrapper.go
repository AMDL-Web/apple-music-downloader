package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"amdl/internal/wrapper"
)

// WrapperService is the wrapper client: account status, login with an optional
// two-step code, and logout. The last three need a wrapper-manager; a bare
// wrapper-lite answers them with wrapper.ErrLoginUnsupported.
type WrapperService interface {
	Status(context.Context) (wrapper.Status, error)
	StartLogin(context.Context, string, string) (wrapper.LoginResult, error)
	SubmitTwoStepCode(context.Context, string, string) (wrapper.LoginResult, error)
	Logout(context.Context, string) error
}

func (s *Server) wrapperStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.wrapper.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, CodeWrapperUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) wrapperLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	// The password goes to Apple as typed: leading or trailing spaces can be
	// part of it. Only a password that is nothing but whitespace is refused.
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || strings.TrimSpace(req.Password) == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "username and password are required")
		return
	}
	result, err := s.wrapper.StartLogin(r.Context(), req.Username, req.Password)
	if err != nil {
		writeWrapperError(w, err)
		return
	}
	status := http.StatusOK
	if result.Status == wrapper.LoginStatusNeedsTwoStep {
		status = http.StatusAccepted
	}
	writeJSON(w, status, result)
}

func (s *Server) wrapperTwoStep(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"two_step_code"`
	}
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "two_step_code is required")
		return
	}
	result, err := s.wrapper.SubmitTwoStepCode(r.Context(), r.PathValue("login_id"), req.Code)
	if err != nil {
		writeWrapperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) wrapperLogout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "username is required")
		return
	}
	if err := s.wrapper.Logout(r.Context(), req.Username); err != nil {
		writeWrapperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out", "username": req.Username})
}

// writeWrapperError maps a wrapper login, two-step or logout error. Anything
// the wrapper package does not classify is an upstream failure.
func writeWrapperError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wrapper.ErrCredentialsUnusable), errors.Is(err, wrapper.ErrInvalidTwoStepCode):
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
	case errors.Is(err, wrapper.ErrAuthenticationFailed):
		writeError(w, http.StatusUnauthorized, CodeAuthenticationFailed, err.Error())
	case errors.Is(err, wrapper.ErrAlreadyLoggedIn):
		writeError(w, http.StatusConflict, CodeAlreadyLoggedIn, err.Error())
	case errors.Is(err, wrapper.ErrLoginPending), errors.Is(err, wrapper.ErrLoginSessionBusy):
		writeError(w, http.StatusConflict, CodeLoginPending, err.Error())
	case errors.Is(err, wrapper.ErrLoginSessionNotFound):
		writeError(w, http.StatusNotFound, CodeLoginNotFound, err.Error())
	case errors.Is(err, wrapper.ErrAccountNotFound):
		writeError(w, http.StatusNotFound, CodeAccountNotFound, err.Error())
	case errors.Is(err, wrapper.ErrLoginUnsupported):
		writeError(w, http.StatusNotImplemented, CodeLoginUnsupported, err.Error())
	case errors.Is(err, wrapper.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, CodeWrapperUnavailable, err.Error())
	case errors.Is(err, wrapper.ErrLoginTimeout):
		writeError(w, http.StatusGatewayTimeout, CodeUpstreamTimeout, err.Error())
	default:
		writeError(w, http.StatusBadGateway, CodeUpstreamError, err.Error())
	}
}
