package api

import (
	"context"
	"net/http"
	"testing"

	"amdl/internal/wrapper"
)

type fakePairing struct {
	status   wrapper.PairingStatus
	startErr error
	unpaired bool
}

func (f *fakePairing) Status() wrapper.PairingStatus { return f.status }

func (f *fakePairing) Start(context.Context) (wrapper.PairingStatus, error) {
	if f.startErr != nil {
		return f.status, f.startErr
	}
	f.status = wrapper.PairingStatus{State: wrapper.PairingPending, UserCode: "ABCD", VerificationURL: "https://gw.example/pair"}
	return f.status, nil
}

func (f *fakePairing) Unpair() error {
	f.unpaired = true
	f.status = wrapper.PairingStatus{State: wrapper.PairingUnpaired}
	return nil
}

func TestWrapperPairingRoutes(t *testing.T) {
	pairing := &fakePairing{status: wrapper.PairingStatus{State: wrapper.PairingUnpaired}}
	handler := newServer(t, Deps{Pairing: pairing}).Routes()
	if got := decodeBody(t, requestJSON(t, handler, http.MethodGet, "/api/v1/wrapper/pairing", "").Body.Bytes()); got["state"] != "unpaired" {
		t.Fatalf("status = %v", got)
	}
	recorder := requestJSON(t, handler, http.MethodPost, "/api/v1/wrapper/pairing", "")
	got := decodeBody(t, recorder.Body.Bytes())
	if recorder.Code != http.StatusAccepted || got["state"] != "pending" || got["user_code"] != "ABCD" || got["verification_url"] == nil {
		t.Fatalf("start = %d %v", recorder.Code, got)
	}
	if got := decodeBody(t, requestJSON(t, handler, http.MethodDelete, "/api/v1/wrapper/pairing", "").Body.Bytes()); got["state"] != "unpaired" || !pairing.unpaired {
		t.Fatalf("unpair = %v", got)
	}
	pairing.startErr = wrapper.ErrPairingUnsupported
	assertErrorCode(t, requestJSON(t, handler, http.MethodPost, "/api/v1/wrapper/pairing", ""), http.StatusNotImplemented, CodePairingUnsupported)
	assertErrorCode(t, requestJSON(t, newServer(t, Deps{}).Routes(), http.MethodGet, "/api/v1/wrapper/pairing", ""), http.StatusServiceUnavailable, CodeFeatureUnavailable)
}
