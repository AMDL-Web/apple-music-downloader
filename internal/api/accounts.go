package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"amdl/internal/accounts"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/usertoken"
)

// AccountsService is the accounts table and its discovery
// (internal/accounts.Service).
type AccountsService interface {
	List(ctx context.Context) ([]domain.Account, error)
	Update(ctx context.Context, id string, p db.AccountPatch) (domain.Account, error)
	Delete(ctx context.Context, id string) (loggedOut bool, err error)
	// Nudge asks for a discovery soon: after a login or a logout.
	Nudge()
}

// accountResponse is the Account schema: the stored account, whether it is
// the default and where its media-user-token comes from.
type accountResponse struct {
	domain.Account
	Default        bool                 `json:"default"`
	MediaUserToken accountTokenResponse `json:"media_user_token"`
}

// accountTokenResponse is the AccountToken schema. It never carries a token.
type accountTokenResponse struct {
	// Source is wrapper, manual or none.
	Source string               `json:"source"`
	Manual *manualTokenResponse `json:"manual,omitempty"`
}

type manualTokenResponse struct {
	Mode     usertoken.ManualMode `json:"mode"`
	SetAt    time.Time            `json:"set_at"`
	Rejected bool                 `json:"rejected"`
}

func (s *Server) accountResponse(a domain.Account) accountResponse {
	return accountResponse{Account: a, Default: a.Position == 0, MediaUserToken: s.accountToken(a.ID)}
}

func (s *Server) accountToken(id string) accountTokenResponse {
	out := accountTokenResponse{Source: string(s.tokens.Account(s.currentConfig(), id).Source)}
	if out.Source == "" {
		out.Source = "none"
	}
	if m, ok := s.tokens.ManualFor(id); ok {
		out.Manual = &manualTokenResponse{Mode: m.Mode, SetAt: m.SetAt, Rejected: m.Rejected}
	}
	return out
}

// findAccount is the account with id; the implicit account of a wrapper that
// does not list accounts is always there, so a token can be given before the
// wrapper has one.
func (s *Server) findAccount(ctx context.Context, id string) (domain.Account, bool, error) {
	list, err := s.accounts.List(ctx)
	if err != nil {
		return domain.Account{}, false, err
	}
	for _, a := range list {
		if a.ID == id {
			return a, true, nil
		}
	}
	if id == domain.ImplicitAccountID {
		return domain.Account{ID: id, Position: 0, State: domain.AccountActive}, true, nil
	}
	return domain.Account{}, false, nil
}

// setAccountToken answers PUT /accounts/{account_id}/media-user-token: it
// stores a media-user-token the user gave, used ahead of the wrapper's
// (override) or only while the wrapper has none (fallback, the default).
func (s *Server) setAccountToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string               `json:"token"`
		Mode  usertoken.ManualMode `json:"mode"`
	}
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	id := r.PathValue("account_id")
	a, ok, err := s.findAccount(r.Context(), id)
	if err != nil {
		writeStoreError(w, "list accounts", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, CodeAccountNotFound, "no account has id "+id)
		return
	}
	if _, err := s.tokens.SetManual(id, req.Token, req.Mode); err != nil {
		if errors.Is(err, usertoken.ErrInvalid) || errors.Is(err, usertoken.ErrBadMode) {
			writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, CodeStoreError, "save the media-user-token: "+err.Error())
		return
	}
	s.accounts.Nudge()
	writeJSON(w, http.StatusOK, s.accountResponse(a))
}

// clearAccountToken answers DELETE /accounts/{account_id}/media-user-token:
// the account goes back to the wrapper's token alone.
func (s *Server) clearAccountToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("account_id")
	a, ok, err := s.findAccount(r.Context(), id)
	if err != nil {
		writeStoreError(w, "list accounts", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, CodeAccountNotFound, "no account has id "+id)
		return
	}
	if err := s.tokens.ClearManual(id); err != nil {
		writeError(w, http.StatusInternalServerError, CodeStoreError, "drop the media-user-token: "+err.Error())
		return
	}
	s.accounts.Nudge()
	writeJSON(w, http.StatusOK, s.accountResponse(a))
}

// listAccounts answers GET /accounts: every account, the default first.
func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	list, err := s.accounts.List(r.Context())
	if err != nil {
		writeStoreError(w, "list accounts", err)
		return
	}
	out := make([]accountResponse, 0, len(list))
	for _, a := range list {
		out = append(out, s.accountResponse(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

// updateAccount answers PATCH /accounts/{account_id}: the account's library
// sync and Apple library switches, and its position (0 makes it the
// default).
func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LibrarySync  *bool `json:"library_sync"`
		AppleLibrary *bool `json:"apple_library"`
		Position     *int  `json:"position"`
	}
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	if req.LibrarySync == nil && req.AppleLibrary == nil && req.Position == nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "give library_sync, apple_library or position")
		return
	}
	if req.Position != nil && *req.Position < 0 {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "position must be 0 or more")
		return
	}
	a, err := s.accounts.Update(r.Context(), r.PathValue("account_id"),
		db.AccountPatch{LibrarySync: req.LibrarySync, AppleLibrary: req.AppleLibrary, Position: req.Position})
	if errors.Is(err, db.ErrAccountNotFound) {
		writeError(w, http.StatusNotFound, CodeAccountNotFound, "no account has id "+r.PathValue("account_id"))
		return
	}
	if err != nil {
		writeStoreError(w, "update account", err)
		return
	}
	writeJSON(w, http.StatusOK, s.accountResponse(a))
}

// deleteAccount answers DELETE /accounts/{account_id}: it logs the account
// out of the wrapper and drops its library-sync anchor, Apple library cache
// and stored token. Downloaded files and records stay.
func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("account_id")
	loggedOut, err := s.accounts.Delete(r.Context(), id)
	switch {
	case errors.Is(err, db.ErrAccountNotFound):
		writeError(w, http.StatusNotFound, CodeAccountNotFound, "no account has id "+id)
		return
	case errors.Is(err, accounts.ErrLogoutUnsupported):
		writeError(w, http.StatusNotImplemented, CodeLoginUnsupported, err.Error())
		return
	case err != nil:
		writeWrapperError(w, err)
		return
	}
	s.librarySync.Forget(id)
	s.tokens.RefreshSoon()
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id, "logged_out": loggedOut})
}
