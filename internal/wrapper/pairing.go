package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Pairing gives amdl the right to call a wrapper behind a gateway that asks
// for a bearer token (the Cloudflare deploy of wrapper-manager), device-code
// style:
//
//  1. Start asks POST /pair/start for a user code, the page the owner
//     approves it on (with a passkey) and a device code.
//  2. A background poll asks POST /pair/poll {device_code} every interval
//     until the owner approved (an access token of about a day and a refresh
//     token), refused, or the code expired.
//  3. Every wrapper request carries Authorization: Bearer <access token>. The
//     access token is renewed with POST /pair/refresh before it expires, and
//     once after a 401; the refresh rotates both tokens, and the new pair is
//     stored before it is used, since the gateway revokes a device whose old
//     refresh token comes back later than a minute. A refresh the
//     gateway refuses (the refresh token expired after 30 idle days, or was
//     revoked) drops the credential: the wrapper needs pairing again.
//
// A wrapper that answers /pair/start with 404 has no pairing
// (ErrPairingUnsupported) and is called without a header, as before.
//
// The credential is stored in <dir>/wrapper.json (directory 0700, file 0600,
// written by a rename), tied to the wrapper.address it was paired with; it is
// never written to config.yaml, logged or put in an error.
type Pairing struct {
	base    *url.URL
	address string
	http    *http.Client
	timeout time.Duration
	store   pairingStore
	logger  *slog.Logger
	now     func() time.Time
	// minInterval floors the poll interval the gateway asks for.
	minInterval time.Duration

	// refreshMu makes one refresh at a time; a request that waited for it
	// uses its result.
	refreshMu sync.Mutex

	mu      sync.Mutex
	cred    *pairingCredential
	pending *pairingSession
	// needed is set when a refresh was refused: the stored credential is
	// gone and the wrapper needs pairing again.
	needed  bool
	lastErr string
	// life ends at Close and stops a poll in flight.
	life     context.Context
	shutdown context.CancelFunc
}

// Pairing states, PairingStatus.State.
const (
	PairingUnpaired = "unpaired"
	PairingPending  = "pending"
	PairingPaired   = "paired"
	// PairingNeeded is a credential the gateway refused: pair again.
	PairingNeeded = "needs_pairing"
)

var (
	// ErrPairingUnsupported is a wrapper without /pair/start.
	ErrPairingUnsupported = errors.New("the wrapper has no pairing: it takes requests without a token")
	// ErrPairingRateLimited is a /pair/start the gateway answered 429.
	ErrPairingRateLimited = errors.New("the wrapper asks to wait before the next pairing; try again in a minute")
	// ErrPairingRefused is a refresh or poll the gateway refused.
	ErrPairingRefused = errors.New("the wrapper refused the pairing")
)

// PairingStatus is GET /api/v1/wrapper/pairing.
type PairingStatus struct {
	State string `json:"state"`
	// UserCode and VerificationURL are what the owner enters and opens while
	// a pairing is pending; ExpiresAt is when that code expires.
	UserCode        string `json:"user_code,omitempty"`
	VerificationURL string `json:"verification_url,omitempty"`
	// VerificationURLComplete is VerificationURL with the code filled in,
	// the link a client shows.
	VerificationURLComplete string     `json:"verification_url_complete,omitempty"`
	ExpiresAt               *time.Time `json:"expires_at,omitempty"`
	// PairedAt is when the stored credential was first issued, and
	// AccessExpiresAt when its access token runs out (it is renewed before).
	PairedAt        *time.Time `json:"paired_at,omitempty"`
	AccessExpiresAt *time.Time `json:"access_expires_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

type pairingCredential struct {
	Address      string    `json:"address"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	PairedAt     time.Time `json:"paired_at"`
	RefreshedAt  time.Time `json:"refreshed_at"`
}

type pairingSession struct {
	userCode                string
	verificationURL         string
	verificationURLComplete string
	deviceCode              string
	interval                time.Duration
	expiresAt               time.Time
	cancel                  context.CancelFunc
}

// Timing.
const (
	// refreshBefore renews an access token this long before it expires.
	refreshBefore = 10 * time.Minute
	// keepAlive is how often the background loop looks whether a renewal
	// is due, so an idle amdl keeps its refresh token in use.
	keepAlive         = 15 * time.Minute
	minPollInterval   = 2 * time.Second
	defaultPollPeriod = 5 * time.Second
	slowDownStep      = 5 * time.Second
	maxPairingReply   = 64 << 10
)

// NewPairing builds the pairing for the wrapper at cfg's address, its
// credential in dir. It reads the stored credential; it does not contact
// the wrapper.
func NewPairing(address string, timeout time.Duration, dir string, logger *slog.Logger) (*Pairing, error) {
	base, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("parse wrapper address %q: %w", address, err)
	}
	if logger == nil {
		logger = slog.Default().With("component", "wrapper")
	}
	life, shutdown := context.WithCancel(context.Background())
	p := &Pairing{
		base: base, address: address, http: &http.Client{Timeout: timeout}, timeout: timeout,
		store: pairingStore{dir: dir}, logger: logger, now: time.Now, minInterval: minPollInterval, life: life, shutdown: shutdown,
	}
	cred, err := p.store.load(address)
	if err != nil {
		logger.Warn("the stored wrapper pairing is unusable; ignoring it", "error", err.Error())
	}
	p.cred = cred
	return p, nil
}

// PairingDir is the directory the pairing credential is kept in, for a
// database at dbPath.
func PairingDir(dbPath string) string { return filepath.Join(filepath.Dir(dbPath), "pairing") }

// Close stops a pairing poll in flight.
func (p *Pairing) Close() {
	if p != nil {
		p.shutdown()
	}
}

// Status reports the pairing.
func (p *Pairing) Status() PairingStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := PairingStatus{State: PairingUnpaired, LastError: p.lastErr}
	switch {
	case p.pending != nil:
		exp := p.pending.expiresAt
		st.State, st.UserCode, st.VerificationURL, st.ExpiresAt = PairingPending, p.pending.userCode, p.pending.verificationURL, &exp
		st.VerificationURLComplete = p.pending.verificationURLComplete
	case p.cred != nil:
		paired, access := p.cred.PairedAt, p.cred.ExpiresAt
		st.State, st.PairedAt, st.AccessExpiresAt = PairingPaired, &paired, &access
	case p.needed:
		st.State = PairingNeeded
	}
	return st
}

// Start begins a pairing and returns its status with the user code and the
// page to approve it on; a background poll finishes it. A pending pairing is
// replaced. ErrPairingUnsupported when the wrapper has no pairing.
func (p *Pairing) Start(ctx context.Context) (PairingStatus, error) {
	var reply struct {
		UserCode                string  `json:"user_code"`
		VerificationURL         string  `json:"verification_url"`
		VerificationURLComplete string  `json:"verification_url_complete"`
		DeviceCode              string  `json:"device_code"`
		Interval                float64 `json:"interval"`
		ExpiresIn               float64 `json:"expires_in"`
	}
	status, err := p.post(ctx, "/pair/start", map[string]string{"name": deviceName()}, &reply)
	switch {
	case status == http.StatusNotFound:
		return p.Status(), ErrPairingUnsupported
	case status == http.StatusTooManyRequests:
		return p.Status(), ErrPairingRateLimited
	case err != nil:
		return p.Status(), err
	}
	if reply.UserCode == "" || reply.DeviceCode == "" || reply.VerificationURL == "" || reply.ExpiresIn <= 0 {
		return p.Status(), fmt.Errorf("%w: /pair/start answered without a user code, device code, page or lifetime", ErrWrapperResponse)
	}
	interval := max(time.Duration(reply.Interval*float64(time.Second)), p.minInterval)
	if reply.Interval <= 0 {
		interval = defaultPollPeriod
	}
	pollCtx, cancel := context.WithCancel(p.life)
	session := &pairingSession{
		userCode: reply.UserCode, verificationURL: reply.VerificationURL, verificationURLComplete: reply.VerificationURLComplete,
		deviceCode: reply.DeviceCode,
		interval:   interval, expiresAt: p.now().Add(time.Duration(reply.ExpiresIn * float64(time.Second))), cancel: cancel,
	}
	p.mu.Lock()
	if p.pending != nil {
		p.pending.cancel()
	}
	p.pending, p.lastErr = session, ""
	p.mu.Unlock()
	go p.poll(pollCtx, session)
	return p.Status(), nil
}

// poll asks /pair/poll until the session ends.
func (p *Pairing) poll(ctx context.Context, s *pairingSession) {
	defer s.cancel()
	interval := s.interval
	for {
		wait := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			wait.Stop()
			return
		case <-wait.C:
		}
		if p.now().After(s.expiresAt) {
			p.endSession(s, nil, "the pairing code expired before it was approved; start again")
			return
		}
		var reply tokenReply
		status, err := p.post(ctx, "/pair/poll", map[string]string{"device_code": s.deviceCode}, &reply)
		if ctx.Err() != nil {
			return
		}
		switch code := deviceError(err); {
		case err == nil && reply.AccessToken != "" && reply.RefreshToken != "":
			p.endSession(s, p.credential(reply, p.now()), "")
			p.logger.Info("paired with the wrapper")
			return
		case code == "authorization_pending":
		case code == "slow_down":
			interval += slowDownStep
		case code == "access_denied", code == "expired_token", status == http.StatusNotFound, status == http.StatusGone:
			p.endSession(s, nil, "the wrapper's owner did not approve the pairing, or its code expired; start again")
			return
		case err != nil:
			// A network error or a 5xx: try again at the next interval.
			p.logger.Debug("pairing poll failed; asking again", "error", err.Error())
		default:
			p.endSession(s, nil, "the wrapper answered the pairing poll without tokens")
			return
		}
	}
}

// endSession ends a pending pairing, storing cred when it succeeded.
func (p *Pairing) endSession(s *pairingSession, cred *pairingCredential, failure string) {
	if cred != nil {
		if err := p.store.save(*cred); err != nil {
			p.logger.Warn("could not store the wrapper pairing; it is kept in memory until a restart", "error", err.Error())
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending != s {
		return
	}
	p.pending, p.lastErr = nil, failure
	if cred != nil {
		p.cred, p.needed = cred, false
	}
}

// Unpair forgets the stored credential and any pending pairing.
func (p *Pairing) Unpair() error {
	p.mu.Lock()
	if p.pending != nil {
		p.pending.cancel()
		p.pending = nil
	}
	p.cred, p.needed, p.lastErr = nil, false, ""
	p.mu.Unlock()
	return p.store.drop()
}

type tokenReply struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresIn    float64 `json:"expires_in"`
}

func (p *Pairing) credential(r tokenReply, now time.Time) *pairingCredential {
	return &pairingCredential{Address: p.address, AccessToken: r.AccessToken, RefreshToken: r.RefreshToken,
		ExpiresAt: now.Add(time.Duration(r.ExpiresIn * float64(time.Second))), PairedAt: now, RefreshedAt: now}
}

// accessToken returns the token to send, renewing it first when it expires
// soon; "" when amdl is not paired.
func (p *Pairing) accessToken(ctx context.Context) string {
	p.mu.Lock()
	cred := p.cred
	p.mu.Unlock()
	if cred == nil {
		return ""
	}
	if p.now().Add(refreshBefore).After(cred.ExpiresAt) {
		if renewed, err := p.refresh(ctx, cred.AccessToken); err == nil {
			return renewed
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.cred == nil {
			return ""
		}
		return p.cred.AccessToken
	}
	return cred.AccessToken
}

// refresh renews the credential unless another refresh already replaced
// stale, and returns the access token to use. A refresh the gateway refuses
// drops the credential (needs pairing).
func (p *Pairing) refresh(ctx context.Context, stale string) (string, error) {
	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()
	p.mu.Lock()
	cred := p.cred
	p.mu.Unlock()
	if cred == nil {
		return "", ErrPairingRefused
	}
	if cred.AccessToken != stale {
		return cred.AccessToken, nil
	}
	var reply tokenReply
	status, err := p.post(ctx, "/pair/refresh", map[string]string{"refresh_token": cred.RefreshToken}, &reply)
	if err == nil && (reply.AccessToken == "" || reply.RefreshToken == "") {
		err = fmt.Errorf("%w: /pair/refresh answered without tokens", ErrWrapperResponse)
	}
	if err != nil {
		if deviceError(err) == "invalid_grant" || status == http.StatusUnauthorized || status == http.StatusForbidden {
			p.drop("the wrapper refused the pairing's refresh token; pair again")
			return "", ErrPairingRefused
		}
		p.noteErr("could not renew the wrapper pairing: " + err.Error())
		return "", err
	}
	next := p.credential(reply, p.now())
	next.PairedAt = cred.PairedAt
	if err := p.store.save(*next); err != nil {
		p.logger.Warn("could not store the renewed wrapper pairing; it is kept in memory", "error", err.Error())
	}
	p.mu.Lock()
	p.cred, p.lastErr = next, ""
	p.mu.Unlock()
	return next.AccessToken, nil
}

// unauthorized handles a 401 to a request sent with token: one refresh,
// and the token to retry with, or "" when there is none.
func (p *Pairing) unauthorized(ctx context.Context, token string) string {
	renewed, err := p.refresh(ctx, token)
	if err != nil {
		return ""
	}
	return renewed
}

// askedForToken notes a 401 to a request sent without a token: the wrapper
// wants pairing.
func (p *Pairing) askedForToken() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cred == nil && p.pending == nil {
		p.needed = true
	}
}

func (p *Pairing) drop(reason string) {
	p.mu.Lock()
	had := p.cred != nil
	p.cred, p.needed, p.lastErr = nil, true, reason
	p.mu.Unlock()
	if err := p.store.drop(); err != nil {
		p.logger.Warn("could not remove the refused wrapper pairing", "error", err.Error())
	}
	if had {
		p.logger.Warn("the wrapper needs pairing again", "reason", reason)
	}
}

func (p *Pairing) noteErr(msg string) {
	p.mu.Lock()
	repeat := p.lastErr == msg
	p.lastErr = msg
	p.mu.Unlock()
	if !repeat {
		p.logger.Warn(msg)
	}
}

// Run renews the access token before it expires until ctx ends, so an idle
// amdl keeps its refresh token in use.
func (p *Pairing) Run(ctx context.Context) {
	if p == nil {
		return
	}
	ticker := time.NewTicker(keepAlive)
	defer ticker.Stop()
	for {
		p.accessToken(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// deviceError is the device-flow error code of a pairing reply
// ("authorization_pending", "slow_down", …), "" for none.
func deviceError(err error) string {
	var reply *pairingError
	if errors.As(err, &reply) {
		return reply.code
	}
	return ""
}

type pairingError struct {
	endpoint string
	status   int
	code     string
}

func (e *pairingError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("wrapper %s: HTTP %d: %s", e.endpoint, e.status, e.code)
	}
	return fmt.Sprintf("wrapper %s: HTTP %d", e.endpoint, e.status)
}

// post sends one pairing request, bounded by the request timeout, and
// decodes a 200's JSON into out. Any other status is a *pairingError with the
// reply's {"error"} code; an error names only those, never the body, which
// may hold a token.
func (p *Pairing) post(ctx context.Context, endpoint string, body any, out any) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	raw := []byte("{}")
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base.JoinPath(endpoint).String(), bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return 0, fmt.Errorf("wrapper %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPairingReply))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("read wrapper %s reply: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		var reply struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &reply)
		return resp.StatusCode, &pairingError{endpoint: endpoint, status: resp.StatusCode, code: reply.Error}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return resp.StatusCode, fmt.Errorf("%w: wrapper %s reply does not have the expected shape", ErrWrapperResponse, endpoint)
	}
	return resp.StatusCode, nil
}

// pairingStore is the stored credential.
type pairingStore struct{ dir string }

const pairingFile = "wrapper.json"

func (s pairingStore) path() string { return filepath.Join(s.dir, pairingFile) }

func (s pairingStore) load(address string) (*pairingCredential, error) {
	if s.dir == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cred pairingCredential
	if json.Unmarshal(raw, &cred) != nil || cred.AccessToken == "" || cred.RefreshToken == "" {
		return nil, fmt.Errorf("%s is not a stored pairing", s.path())
	}
	if cred.Address != address {
		return nil, nil
	}
	return &cred, nil
}

func (s pairingStore) save(cred pairingCredential) error {
	if s.dir == "" {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, pairingFile+".*.tmp")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path()); err != nil {
		return err
	}
	done = true
	return nil
}

func (s pairingStore) drop() error {
	if s.dir == "" {
		return nil
	}
	err := os.Remove(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// pairedTransport sends the pairing's access token on every request and,
// after a 401, renews it once and sends the request again.
type pairedTransport struct {
	pairing *Pairing
	next    http.RoundTripper
}

func (t pairedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token := t.pairing.accessToken(req.Context())
	if token == "" {
		resp, err := t.next.RoundTrip(req)
		if err == nil && resp.StatusCode == http.StatusUnauthorized {
			t.pairing.askedForToken()
		}
		return resp, err
	}
	resp, err := t.next.RoundTrip(withBearer(req, token))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	retry := t.pairing.unauthorized(req.Context(), token)
	if retry == "" || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		return resp, nil
	}
	again := withBearer(req, retry)
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return resp, nil
		}
		again.Body = body
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	resp, err = t.next.RoundTrip(again)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		// A renewed token refused too: the pairing itself is gone.
		t.pairing.drop("the wrapper refused a renewed access token; pair again")
	}
	return resp, err
}

func withBearer(req *http.Request, token string) *http.Request {
	out := req.Clone(req.Context())
	out.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	return out
}

// deviceName is the name the owner sees for this pairing: "amdl on <host>".
func deviceName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "amdl"
	}
	return "amdl on " + host
}
