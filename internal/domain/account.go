package domain

import "time"

// AccountState says whether the wrapper still holds an account.
type AccountState string

const (
	// AccountActive is an account the wrapper reported at the last discovery.
	AccountActive AccountState = "active"
	// AccountMissing is an account the wrapper no longer reports (logged out,
	// subscription ended). Its row and switches stay, so they apply again
	// when it logs in again.
	AccountMissing AccountState = "missing"
)

// ImplicitAccountID is the one account of a wrapper that does not list its
// accounts (a bare wrapper-lite, or a wrapper-manager older than the
// instances list).
const ImplicitAccountID = "wrapper"

// Account is one Apple Music account the wrapper holds, with amdl's
// per-account switches (the accounts table). Position 0 is the default
// account: it reads records without a pinned storefront and links whose
// storefront no account serves. Among accounts of one region, the lower
// position's token is used.
type Account struct {
	ID string `json:"id"`
	// Name is the Apple ID the account signed in with; empty when the
	// wrapper does not say.
	Name string `json:"name"`
	// Region is the account's storefront, lower case; empty while unknown.
	Region       string       `json:"region"`
	Position     int          `json:"position"`
	LibrarySync  bool         `json:"library_sync"`
	AppleLibrary bool         `json:"apple_library"`
	State        AccountState `json:"state"`
	AddedAt      time.Time    `json:"added_at"`
	// SeenAt is the last discovery that reported the account.
	SeenAt *time.Time `json:"seen_at,omitempty"`
	// NeedsLogin is an active account whose Apple session stopped working in
	// the wrapper at the last discovery, which routes nothing to it until it
	// logs in again. It is not stored: discovery sets it
	// (internal/accounts#Service.List).
	NeedsLogin bool `json:"needs_login"`
}
