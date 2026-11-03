package wrapper

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// ErrUnauthorized is a wrapper that answered 401 without a wrapper reply: the
// gateway or reverse proxy in front of it refused wrapper.token, or wants one
// that is not set. Asking again cannot pass.
var ErrUnauthorized error = unauthorizedError{}

type unauthorizedError struct{}

func (unauthorizedError) Error() string {
	return "the wrapper refused the key (HTTP 401); check wrapper.token"
}

// NonRetryable marks it for internal/jobs#IsNonRetryable.
func (unauthorizedError) NonRetryable() bool { return true }

// WithToken sends token(), read afresh for each request, as
// "Authorization: Bearer <token>" on every wrapper request; an empty token
// sends no header, as a wrapper on the LAN expects.
func WithToken(token func() string) ClientOption {
	return func(c *Client) { c.token = token }
}

// tokenTransport sets the Authorization header from wrapper.token and turns a
// gateway's 401 into ErrUnauthorized.
type tokenTransport struct {
	token func() string
	next  http.RoundTripper
}

func (t tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if token := strings.TrimSpace(t.token()); token != "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	// A wrapper's own 401 carries its {code, msg} envelope; a gateway's
	// does not.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	var env envelope
	if json.Unmarshal(raw, &env) == nil && env.Msg != "" {
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		return resp, nil
	}
	return nil, ErrUnauthorized
}

// GenerateKey returns a strong random key for wrapper.token: 32 random bytes,
// base64url without padding.
func GenerateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// workersDevHint ends an error of a wrapper at *.workers.dev that timed out or
// whose connection was cut: mainland China blocks that domain.
const workersDevHint = "workers.dev is blocked in mainland China: bind a custom domain to the Worker and set wrapper.address to it"

// hintError is a request error with a hint for the person after it.
type hintError struct {
	err  error
	hint string
}

func (e *hintError) Error() string { return e.err.Error() + "; " + e.hint }
func (e *hintError) Unwrap() error { return e.err }

// requestError is the package's requestError, with workersDevHint added when
// the wrapper is at *.workers.dev and the request timed out or its
// connection was reset.
func (c *Client) requestError(parent context.Context, endpoint string, timeout time.Duration, err error) error {
	out := requestError(parent, endpoint, timeout, err)
	if c.workersDev && cutOff(err) {
		return &hintError{err: out, hint: workersDevHint}
	}
	return out
}

// cutOff reports a request that timed out or lost its connection.
func cutOff(err error) bool {
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		(errors.As(err, &netErr) && netErr.Timeout())
}

// isWorkersDev reports a host under workers.dev.
func isWorkersDev(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "workers.dev" || strings.HasSuffix(host, ".workers.dev")
}
