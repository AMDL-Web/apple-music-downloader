package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Account is the Account schema: one Apple Music account the wrapper holds,
// with amdl's switches for it.
type Account struct {
	ID string `json:"id"`
	// Name is the Apple ID, "" when the wrapper does not say.
	Name string `json:"name"`
	// Region is the lower-case storefront, "" while unknown.
	Region string `json:"region"`
	// Position 0 is the default account.
	Position     int  `json:"position"`
	Default      bool `json:"default"`
	LibrarySync  bool `json:"library_sync"`
	AppleLibrary bool `json:"apple_library"`
	// State is AccountActive or AccountMissing.
	State          string     `json:"state"`
	AddedAt        time.Time  `json:"added_at"`
	SeenAt         *time.Time `json:"seen_at,omitempty"`
	MediaUserToken string     `json:"media_user_token"`
}

// Account states.
const (
	AccountActive  = "active"
	AccountMissing = "missing"
)

// AccountList is the AccountList schema, the default account first.
type AccountList struct {
	Accounts []Account `json:"accounts"`
}

// AccountPatch is the AccountPatch schema: a nil field is left alone, and at
// least one must be set. Position 0 makes the account the default.
type AccountPatch struct {
	LibrarySync  *bool `json:"library_sync,omitempty"`
	AppleLibrary *bool `json:"apple_library,omitempty"`
	Position     *int  `json:"position,omitempty"`
}

// AccountDeleted is the AccountDeleted schema.
type AccountDeleted struct {
	Deleted   string `json:"deleted"`
	LoggedOut bool   `json:"logged_out"`
}

// ListAccounts reads GET /api/v1/accounts.
func (c *Client) ListAccounts(ctx context.Context) (AccountList, error) {
	var l AccountList
	err := c.do(ctx, http.MethodGet, "/api/v1/accounts", nil, nil, &l)
	return l, err
}

// UpdateAccount changes an account's switches or position: PATCH
// /api/v1/accounts/{account_id}.
func (c *Client) UpdateAccount(ctx context.Context, id string, p AccountPatch) (Account, error) {
	var a Account
	err := c.do(ctx, http.MethodPatch, "/api/v1/accounts/"+url.PathEscape(id), nil, p, &a)
	return a, err
}

// DeleteAccount logs the account out of the wrapper and drops amdl's row and
// library state for it: DELETE /api/v1/accounts/{account_id}.
func (c *Client) DeleteAccount(ctx context.Context, id string) (AccountDeleted, error) {
	var d AccountDeleted
	err := c.do(ctx, http.MethodDelete, "/api/v1/accounts/"+url.PathEscape(id), nil, nil, &d)
	return d, err
}

// FindAccount picks the account a person means by ref: an account id, else
// the Apple ID, else a region (the storefront code, any case). A region
// names the single active account serving it. Nothing matching is
// account_not_found, and several accounts of one region is invalid_request
// listing their ids, both as *APIError. An empty ref is the default account,
// the first of the list.
func FindAccount(list []Account, ref string) (Account, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if len(list) == 0 {
			return Account{}, &APIError{Status: http.StatusNotFound, Code: "account_not_found", Message: "no account is signed in"}
		}
		return list[0], nil
	}
	for _, a := range list {
		if a.ID == ref {
			return a, nil
		}
	}
	for _, a := range list {
		if a.Name != "" && strings.EqualFold(a.Name, ref) {
			return a, nil
		}
	}
	var same []Account
	for _, a := range list {
		if a.State == AccountActive && a.Region != "" && strings.EqualFold(a.Region, ref) {
			same = append(same, a)
		}
	}
	switch len(same) {
	case 1:
		return same[0], nil
	case 0:
		return Account{}, &APIError{Status: http.StatusNotFound, Code: "account_not_found",
			Message: fmt.Sprintf("no account has the id, Apple ID or active region %q; known: %s", ref, describeAccounts(list))}
	}
	return Account{}, &APIError{Status: http.StatusBadRequest, Code: "invalid_request",
		Message: fmt.Sprintf("%d active accounts serve %q, so name one by id: %s", len(same), ref, describeAccounts(same))}
}

func describeAccounts(list []Account) string {
	if len(list) == 0 {
		return "none"
	}
	parts := make([]string, len(list))
	for i, a := range list {
		parts[i] = a.ID
		if a.Region != "" {
			parts[i] += " (" + a.Region + ")"
		}
	}
	return strings.Join(parts, ", ")
}

// accountKeys are the request fields that name an account.
var accountKeys = []string{"account", "account_id"}

// ResolveAccounts rewrites the account references in values, the arguments of
// an operation by their parameter and body field names, to account ids, so
// that a person can give an id, an Apple ID or a region (FindAccount). It
// asks the server for the list only when a reference is there.
func (c *Client) ResolveAccounts(ctx context.Context, values map[string]any) error {
	var keys []string
	for _, k := range accountKeys {
		if s, ok := values[k].(string); ok && s != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	list, err := c.ListAccounts(ctx)
	if err != nil {
		return err
	}
	sort.Strings(keys)
	for _, k := range keys {
		a, err := FindAccount(list.Accounts, values[k].(string))
		if err != nil {
			return err
		}
		values[k] = a.ID
	}
	return nil
}

// LibrarySyncReset is the LibrarySyncResetResponse schema.
type LibrarySyncReset struct {
	Account string      `json:"account"`
	Cleared bool        `json:"cleared"`
	Status  LibrarySync `json:"status"`
}

// ResetLibrarySync clears the library monitor's anchor of an account (the
// default account's when account is ""): POST /api/v1/library-sync/reset.
func (c *Client) ResetLibrarySync(ctx context.Context, account string) (LibrarySyncReset, error) {
	var body any
	if account != "" {
		body = map[string]string{"account": account}
	}
	var r LibrarySyncReset
	err := c.do(ctx, http.MethodPost, "/api/v1/library-sync/reset", nil, body, &r)
	return r, err
}
