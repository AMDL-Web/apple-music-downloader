package api

import (
	"context"
	"errors"
	"net/http"

	"amdl/internal/wrapper"
)

// PairingService is the wrapper pairing (internal/wrapper.Pairing): the
// device-code pairing that gives amdl a bearer token for a wrapper behind a
// gateway.
type PairingService interface {
	Status() wrapper.PairingStatus
	Start(ctx context.Context) (wrapper.PairingStatus, error)
	Unpair() error
}

// wrapperPairing answers GET /wrapper/pairing.
func (s *Server) wrapperPairing(w http.ResponseWriter, r *http.Request) {
	if s.pairing == nil {
		writeError(w, http.StatusServiceUnavailable, CodeFeatureUnavailable, "this process has no wrapper pairing")
		return
	}
	writeJSON(w, http.StatusOK, s.pairing.Status())
}

// startWrapperPairing answers POST /wrapper/pairing: it asks the wrapper for
// a user code and the page to approve it on, and finishes the pairing in the
// background once the owner approves.
func (s *Server) startWrapperPairing(w http.ResponseWriter, r *http.Request) {
	if s.pairing == nil {
		writeError(w, http.StatusServiceUnavailable, CodeFeatureUnavailable, "this process has no wrapper pairing")
		return
	}
	st, err := s.pairing.Start(r.Context())
	switch {
	case errors.Is(err, wrapper.ErrPairingUnsupported):
		writeError(w, http.StatusNotImplemented, CodePairingUnsupported, err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, CodeUpstreamError, err.Error())
	default:
		writeJSON(w, http.StatusAccepted, st)
	}
}

// deleteWrapperPairing answers DELETE /wrapper/pairing: amdl forgets its
// credential and any pairing under way.
func (s *Server) deleteWrapperPairing(w http.ResponseWriter, r *http.Request) {
	if s.pairing == nil {
		writeError(w, http.StatusServiceUnavailable, CodeFeatureUnavailable, "this process has no wrapper pairing")
		return
	}
	if err := s.pairing.Unpair(); err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternalError, "remove the stored pairing: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.pairing.Status())
}
