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
	// LogoutAccount logs out a wrapper-manager instance by its id.
	LogoutAccount(context.Context, string) error
}

// wrapperStatusResponse is the WrapperStatus schema: the wrapper's own status
// plus the default account's media-user-token amdl reads from it, masked, and
// whether wrapper.token is set.
type wrapperStatusResponse struct {
	wrapper.Status
	MediaUserToken string `json:"media_user_token"`
	TokenSet       bool   `json:"token_set"`
}

// wrapperStatus answers GET /wrapper/status. A wrapper whose gateway refuses
// wrapper.token is 503 wrapper_unauthorized; any other failure is 503
// wrapper_unavailable.
func (s *Server) wrapperStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.wrapper.Status(r.Context())
	switch {
	case errors.Is(err, wrapper.ErrUnauthorized):
		writeError(w, http.StatusServiceUnavailable, CodeWrapperUnauthorized, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, CodeWrapperUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wrapperStatusResponse{Status: status, MediaUserToken: s.wrapperToken(), TokenSet: s.currentConfig().Wrapper.Token != ""})
}

// wrapperToken is the wrapper's media-user-token masked as GET /config masks
// the configured one, or "" when amdl cannot read one.
func (s *Server) wrapperToken() string {
	v, err := s.tokens.Wrapper(s.currentConfig(), s.tokens.DefaultAccount())
	if err != nil {
		return ""
	}
	return redactToken(v)
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
	} else {
		s.tokens.RefreshSoon()
		s.nudgeAccounts()
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
	s.tokens.RefreshSoon()
	s.nudgeAccounts()
	writeJSON(w, http.StatusOK, result)
}

// wrapperLogout logs an account out by its Apple ID (username) or its
// account id (account_id, a wrapper-manager instance id); exactly one is
// given. The account's row and library state stay (DELETE /accounts/{id}
// removes them too).
func (s *Server) wrapperLogout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username  string `json:"username"`
		AccountID string `json:"account_id"`
	}
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.AccountID = strings.TrimSpace(req.AccountID)
	if (req.Username == "") == (req.AccountID == "") {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "give username or account_id, not both")
		return
	}
	var err error
	if req.AccountID != "" {
		err = s.wrapper.LogoutAccount(r.Context(), req.AccountID)
	} else {
		err = s.wrapper.Logout(r.Context(), req.Username)
	}
	if err != nil {
		writeWrapperError(w, err)
		return
	}
	s.tokens.RefreshSoon()
	s.nudgeAccounts()
	out := map[string]string{"status": "logged_out"}
	if req.AccountID != "" {
		out["account_id"] = req.AccountID
	} else {
		out["username"] = req.Username
	}
	writeJSON(w, http.StatusOK, out)
}

// writeWrapperError maps a wrapper login, two-step or logout error. Anything
// the wrapper package does not classify is an upstream failure.
func writeWrapperError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wrapper.ErrUnauthorized):
		writeError(w, http.StatusServiceUnavailable, CodeWrapperUnauthorized, err.Error())
	case errors.Is(err, wrapper.ErrCredentialsUnusable), errors.Is(err, wrapper.ErrInvalidTwoStepCode):
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
	case errors.Is(err, wrapper.ErrAuthenticationFailed):
		writeError(w, http.StatusUnauthorized, CodeAuthenticationFailed, err.Error())
	case errors.Is(err, wrapper.ErrAlreadyLoggedIn):
		writeError(w, http.StatusConflict, CodeAlreadyLoggedIn, err.Error())
	case errors.Is(err, wrapper.ErrAccountLimit):
		writeError(w, http.StatusConflict, CodeAccountLimit, err.Error())
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

// nudgeAccounts asks for an account discovery soon, when there is an
// accounts service.
func (s *Server) nudgeAccounts() {
	if s.accounts != nil {
		s.accounts.Nudge()
	}
}
