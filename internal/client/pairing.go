package client

import (
	"context"
	"net/http"
	"time"
)

// WrapperPairing is the WrapperPairing schema: amdl's pairing with a wrapper
// behind a gateway that asks for a bearer token.
type WrapperPairing struct {
	// State is one of the Pairing* constants.
	State           string `json:"state"`
	UserCode        string `json:"user_code,omitempty"`
	VerificationURL string `json:"verification_url,omitempty"`
	// VerificationURLComplete is VerificationURL with the code filled in:
	// the link to show.
	VerificationURLComplete string     `json:"verification_url_complete,omitempty"`
	ExpiresAt               *time.Time `json:"expires_at,omitempty"`
	PairedAt                *time.Time `json:"paired_at,omitempty"`
	AccessExpiresAt         *time.Time `json:"access_expires_at,omitempty"`
	LastError               string     `json:"last_error,omitempty"`
}

// Pairing states.
const (
	PairingUnpaired = "unpaired"
	PairingPending  = "pending"
	PairingPaired   = "paired"
	PairingNeeded   = "needs_pairing"
)

// WrapperPairing reads GET /api/v1/wrapper/pairing.
func (c *Client) WrapperPairing(ctx context.Context) (WrapperPairing, error) {
	var p WrapperPairing
	err := c.do(ctx, http.MethodGet, "/api/v1/wrapper/pairing", nil, nil, &p)
	return p, err
}

// StartWrapperPairing starts a pairing, POST /api/v1/wrapper/pairing: the
// answer carries the user code and the page the wrapper's owner approves it
// on; poll WrapperPairing until the state leaves pending.
func (c *Client) StartWrapperPairing(ctx context.Context) (WrapperPairing, error) {
	var p WrapperPairing
	err := c.do(ctx, http.MethodPost, "/api/v1/wrapper/pairing", nil, nil, &p)
	return p, err
}

// UnpairWrapper forgets the pairing, DELETE /api/v1/wrapper/pairing.
func (c *Client) UnpairWrapper(ctx context.Context) (WrapperPairing, error) {
	var p WrapperPairing
	err := c.do(ctx, http.MethodDelete, "/api/v1/wrapper/pairing", nil, nil, &p)
	return p, err
}
