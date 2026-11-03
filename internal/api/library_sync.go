package api

import (
	"context"
	"net/http"
	"strings"

	"amdl/internal/librarysync"
)

// LibrarySyncService is the library watcher.
type LibrarySyncService interface {
	Status(ctx context.Context) (librarysync.Status, error)
	ResetAnchor(ctx context.Context, accountID string) (int64, error)
	Forget(accountID string)
}

// librarySyncStatus reports what the watcher is doing: the live config values
// plus, per account, its switch and what its last pass observed. It answers
// even while the feature is disabled, so a client can render the toggles'
// current state without guessing.
func (s *Server) librarySyncStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.librarySync.Status(r.Context())
	if err != nil {
		writeStoreError(w, "read library sync status", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// librarySyncReset forgets the watcher's watermark.
//
// This deliberately does not re-download anything. The next pass finds no
// anchor, re-anchors to the library as it stands then, and submits nothing — so
// the effect is "stop treating my current library as new", not "fetch it all
// again". Re-downloading one album is a normal POST /api/v1/downloads.
//
// The body's account names the account; without a body or account it is the
// default account.
func (s *Server) librarySyncReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account string `json:"account"`
	}
	if r.ContentLength != 0 {
		if !decodeJSONBody(w, r, &req, true) {
			return
		}
	}
	account, ok := s.resolveAccount(w, r, strings.TrimSpace(req.Account))
	if !ok {
		return
	}
	cleared, err := s.librarySync.ResetAnchor(r.Context(), account)
	if err != nil {
		writeStoreError(w, "reset library sync anchor", err)
		return
	}
	st, err := s.librarySync.Status(r.Context())
	if err != nil {
		writeStoreError(w, "read library sync status", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"account": account,
		"cleared": cleared,
		"status":  st,
	})
}

// resolveAccount turns a request's account ("" for the default) into an
// account id the accounts table holds, answering 404 account_not_found
// otherwise. Before the first account is known, the default is the implicit
// one and is accepted.
func (s *Server) resolveAccount(w http.ResponseWriter, r *http.Request, account string) (string, bool) {
	if account == "" {
		return s.tokens.DefaultAccount(), true
	}
	accounts, err := s.accounts.List(r.Context())
	if err != nil {
		writeStoreError(w, "list accounts", err)
		return "", false
	}
	for _, a := range accounts {
		if a.ID == account {
			return account, true
		}
	}
	writeError(w, http.StatusNotFound, CodeAccountNotFound, "no account has id "+account)
	return "", false
}
