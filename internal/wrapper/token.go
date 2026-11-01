package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

var (
	// ErrTokenUnsupported is a wrapper that answers GET /token with 404:
	// upstream wrapper-lite, an older build of amdl's fork, or a
	// wrapper-manager that does not forward the route.
	ErrTokenUnsupported = errors.New("the wrapper has no /token endpoint")
	// ErrNoToken is GET /token's 503: the wrapper has no logged-in account.
	ErrNoToken = errors.New("the wrapper has no music token: no account is logged in")
)

// maxTokenReply bounds a /token reply; the token itself is a few hundred
// bytes.
const maxTokenReply = 64 << 10

// TokenReply is GET /token's data in amdl's wrapper-lite fork. MusicToken is
// the account's media-user-token; Region is the /status regions value;
// RefreshedAt is when the wrapper fetched or last refreshed it and
// NextRefreshAt when its background refresh runs next, both Unix seconds.
type TokenReply struct {
	Region        string `json:"region"`
	MusicToken    string `json:"music_token"`
	RefreshedAt   int64  `json:"refreshed_at"`
	NextRefreshAt int64  `json:"next_refresh_at"`
}

// MusicToken asks GET /token for the wrapper account's media-user-token,
// bounded by the request timeout. A 404 is ErrTokenUnsupported and a 503 with
// code 503 is ErrNoToken; anything else that is not a success is a
// *ReplyError or a transport error. No error carries the reply body, which
// may hold the token.
func (c *Client) MusicToken(ctx context.Context) (TokenReply, error) {
	const endpoint = "/token"
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c.base.JoinPath(endpoint).String(), nil)
	if err != nil {
		return TokenReply{}, fmt.Errorf("build wrapper %s request: %w", endpoint, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return TokenReply{}, requestError(ctx, endpoint, c.timeout, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenReply))
	if err != nil {
		return TokenReply{}, fmt.Errorf("read wrapper %s reply: %w", endpoint, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return TokenReply{}, ErrTokenUnsupported
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return TokenReply{}, fmt.Errorf("%w: wrapper %s: HTTP %d, a %d-byte reply that is not JSON", ErrWrapperResponse, endpoint, resp.StatusCode, len(raw))
	}
	if resp.StatusCode == http.StatusServiceUnavailable && env.Code == http.StatusServiceUnavailable {
		return TokenReply{}, ErrNoToken
	}
	if resp.StatusCode != http.StatusOK || env.Code != 0 {
		return TokenReply{}, &ReplyError{Endpoint: endpoint, HTTPStatus: resp.StatusCode, Code: env.Code, Msg: env.Msg}
	}
	var out TokenReply
	// The decode error is left out: a type error can quote the value.
	if err := json.Unmarshal(env.Data, &out); err != nil {
		return TokenReply{}, fmt.Errorf("%w: wrapper %s data does not have the reply's shape", ErrWrapperResponse, endpoint)
	}
	return out, nil
}
