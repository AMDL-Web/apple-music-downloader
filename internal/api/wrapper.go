package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"amdl/internal/wrapper"
)

type wrapperService interface {
	Status(context.Context) (wrapper.Status, error)
	StartLogin(context.Context, string, string) (wrapper.LoginResult, error)
	SubmitTwoStepCode(context.Context, string, string) (wrapper.LoginResult, error)
	Logout(context.Context, string) error
}

func (s *Server) wrapperStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.wrapper.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
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
	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("username and password are required"))
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
		writeError(w, http.StatusBadRequest, fmt.Errorf("two_step_code is required"))
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
		writeError(w, http.StatusBadRequest, fmt.Errorf("username is required"))
		return
	}
	if err := s.wrapper.Logout(r.Context(), req.Username); err != nil {
		writeWrapperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out", "username": req.Username})
}

func writeWrapperError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, wrapper.ErrAuthenticationFailed):
		status = http.StatusUnauthorized
	case errors.Is(err, wrapper.ErrAlreadyLoggedIn), errors.Is(err, wrapper.ErrLoginSessionBusy):
		status = http.StatusConflict
	case errors.Is(err, wrapper.ErrLoginSessionNotFound), errors.Is(err, wrapper.ErrAccountNotFound):
		status = http.StatusNotFound
	case errors.Is(err, wrapper.ErrLoginTimeout):
		status = http.StatusGatewayTimeout
	}
	writeError(w, status, err)
}
