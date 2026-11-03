// Package usertoken decides which Apple Music media-user-token the backend
// uses for which account. The wrapper keeps each account's token and
// refreshes it on its own; amdl uses the wrapper's token whenever it can get
// it, and catalog.media_user_token is the default account's fallback.
//
// Accounts are the rows of the accounts table (internal/db), which the
// account discovery (internal/accounts) passes in with SetAccounts: a
// wrapper-manager's instances by their id, or the one implicit account
// (domain.ImplicitAccountID) of a wrapper that does not list accounts. The
// account at position 0 is the default. Before the first SetAccounts, the
// implicit account is the only one.
//
// Where an account's token comes from, first match wins:
//
//  1. The wrapper's GET /token (amdl's wrapper-lite fork; GET
//     /token?instance=<id> on a wrapper-manager), held in memory from this
//     run's last 200. It is the token the running wrapper uses, so it beats
//     a file that may be a leftover of another account or install, and it
//     works for a wrapper on another host or in another container.
//  2. wrapper.data_dir: an instance's <data_dir>/instances/<id>/MUSIC_TOKEN;
//     for the implicit account <data_dir>/MUSIC_TOKEN, the layout of a
//     wrapper-lite base directory, or when that file is missing the newest
//     <data_dir>/instances/*/MUSIC_TOKEN. Read afresh on every use.
//  3. amdl's stored copy of an earlier run's GET /token, until the wrapper
//     answers in this run, so amdl keeps working while the wrapper restarts.
//  4. For the default account only, catalog.media_user_token.
//
// TokenFor picks by storefront: the active account of that region with the
// lowest position, else the default account.
//
// GET /token is asked by RunEndpoint's loops, one per active account, never
// on a token read: once at start, then a few seconds after the wrapper's
// next_refresh_at (clamped to one minute at the least and one hour at the
// most), when RefreshSoon asks (a login, a logout, Apple refusing the token;
// at most every 30 seconds), and after an error with backoff from 5 seconds
// to 5 minutes. Each request is bounded by the wrapper client's request
// timeout. A 200 replaces the in-memory token and the stored copy. A 503 (no
// logged-in account) drops both, so a logged-out account's token is not used
// on. A 404 (a wrapper without /token: upstream wrapper-lite, an older fork
// build, a wrapper-manager that does not forward it) drops both too, since
// the wrapper cannot vouch for them, and /token is not asked again for 30
// minutes, RefreshSoon included. Any other failure keeps what amdl has.
//
// The stored copies live in <database dir>/usertoken (StoreDir), directory
// 0700 and files 0600, written by a rename, and tied to the wrapper.address
// they came from: wrapper-token.json for the implicit account,
// account-<id>.json for another. The value is never logged, never written to
// the config, and never part of an error.
package usertoken

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"amdl/internal/config"
	"amdl/internal/domain"
)

// Source says where an effective token came from.
type Source string

const (
	// SourceNone means neither the wrapper nor the config has a token.
	SourceNone Source = ""
	// SourceWrapper is the wrapper's token: its MUSIC_TOKEN file, its GET
	// /token, or amdl's stored copy of that.
	SourceWrapper Source = "wrapper"
	// SourceConfig is catalog.media_user_token.
	SourceConfig Source = "config"
)

// Token is an effective media-user-token, where it came from and the
// account it belongs to. Value is empty when Source is SourceNone.
type Token struct {
	Value   string
	Source  Source
	Account string
}

const (
	// FileName is the file the wrapper keeps its token in.
	FileName = "MUSIC_TOKEN"
	minLen   = 16
	maxLen   = 4096
	// maxRead bounds a read of the file, surrounding whitespace included; a
	// longer file is not a token.
	maxRead = 2 * maxLen

	implicitID = domain.ImplicitAccountID
)

// ErrInvalid means the wrapper's file does not hold a media-user-token. The
// error never includes the file's contents.
var ErrInvalid = errors.New("not a media-user-token")

// Account is what the provider knows of one account: its id, storefront
// (lower case), position (0 is the default) and whether the wrapper reported
// it at the last discovery.
type Account struct {
	ID       string
	Region   string
	Position int
	Active   bool
}

// Provider reads the wrapper's tokens and picks the effective one. The zero
// value and a nil *Provider both behave as "no wrapper token". It is safe for
// concurrent use.
type Provider struct {
	// read is NewReading's source, nil without one.
	read     func(accountID string) (string, error)
	mu       sync.Mutex
	endpoint *Endpoint
	// accounts are sorted by position; empty until SetAccounts.
	accounts []Account
	remotes  map[string]*remote
	// run is RunEndpoint's context, nil before it starts; loops are the
	// remotes' fetch loops started under it, and stops end one.
	run   context.Context
	stops map[string]context.CancelFunc
	loops sync.WaitGroup

	errMu   sync.Mutex
	lastErr string
}

// New returns a Provider without a GET /token source; UseEndpoint adds one.
func New() *Provider {
	return &Provider{}
}

// NewReading returns a Provider that reads an account's raw token with read
// in place of wrapper.data_dir: "" when it has none. Tests use it to stand
// in for a wrapper.
func NewReading(read func(accountID string) (string, error)) *Provider {
	return &Provider{read: read}
}

// UseEndpoint adds the wrapper's GET /token as a source and loads amdl's
// stored copies from e.Dir; RunEndpoint then keeps them current. Call it
// before the Provider is shared.
func (p *Provider) UseEndpoint(e Endpoint) {
	if p == nil || e.Fetch == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.endpoint = &e
	p.syncRemotesLocked()
}

// SetAccounts replaces the accounts the provider picks among. An active
// account gets a GET /token loop; an account no longer active loses its loop
// and keeps its stored copy, which is used again once the account is back.
func (p *Provider) SetAccounts(accounts []Account) {
	if p == nil {
		return
	}
	sorted := make([]Account, 0, len(accounts))
	for _, a := range accounts {
		if a.ID == "" {
			continue
		}
		a.Region = strings.ToLower(strings.TrimSpace(a.Region))
		sorted = append(sorted, a)
	}
	sortAccounts(sorted)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.accounts = sorted
	p.syncRemotesLocked()
}

func sortAccounts(accounts []Account) {
	for i := 1; i < len(accounts); i++ {
		for j := i; j > 0 && accounts[j].Position < accounts[j-1].Position; j-- {
			accounts[j], accounts[j-1] = accounts[j-1], accounts[j]
		}
	}
}

// Accounts returns the accounts SetAccounts passed, by position.
func (p *Provider) Accounts() []Account {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Account(nil), p.accounts...)
}

// wantedRemotesLocked is the accounts that get a GET /token loop: the
// active ones, or the implicit account before the first SetAccounts. The
// value is the instance asked for.
func (p *Provider) wantedRemotesLocked() map[string]string {
	want := map[string]string{}
	if len(p.accounts) == 0 {
		want[implicitID] = ""
		return want
	}
	for _, a := range p.accounts {
		if !a.Active {
			continue
		}
		if a.ID == implicitID {
			want[a.ID] = ""
		} else {
			want[a.ID] = a.ID
		}
	}
	return want
}

// syncRemotesLocked starts and stops remotes to match the accounts. A
// stopped account's remote stays in remotes, so its stored copy still
// serves; only its loop ends.
func (p *Provider) syncRemotesLocked() {
	if p.endpoint == nil {
		return
	}
	if p.remotes == nil {
		p.remotes = map[string]*remote{}
		p.stops = map[string]context.CancelFunc{}
	}
	want := p.wantedRemotesLocked()
	for id, instance := range want {
		if _, ok := p.remotes[id]; !ok {
			p.remotes[id] = newRemote(*p.endpoint, id, instance)
		}
		p.startLocked(id)
	}
	for id, stop := range p.stops {
		if _, ok := want[id]; !ok {
			stop()
			delete(p.stops, id)
		}
	}
}

// startLocked runs id's fetch loop under RunEndpoint's context, once.
func (p *Provider) startLocked(id string) {
	if p.run == nil || p.stops[id] != nil {
		return
	}
	ctx, stop := context.WithCancel(p.run)
	p.stops[id] = stop
	r := p.remotes[id]
	p.loops.Go(func() { r.run(ctx) })
}

// RunEndpoint asks GET /token for every active account until ctx ends, as
// the package comment says, and returns once every loop has stopped. It
// returns at once without UseEndpoint.
func (p *Provider) RunEndpoint(ctx context.Context) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.endpoint == nil {
		p.mu.Unlock()
		return
	}
	p.run = ctx
	for id := range p.wantedRemotesLocked() {
		p.startLocked(id)
	}
	p.mu.Unlock()
	<-ctx.Done()
	p.loops.Wait()
}

// RefreshSoon asks every GET /token loop to fetch now, without waiting for
// it: after a login or a logout, or when Apple refused a token.
func (p *Provider) RefreshSoon() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for id := range p.stops {
		p.remotes[id].requestRefresh()
	}
}

// Forget drops what amdl keeps of a deleted account: its stored copy and its
// loop.
func (p *Provider) Forget(accountID string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if stop := p.stops[accountID]; stop != nil {
		stop()
		delete(p.stops, accountID)
	}
	r := p.remotes[accountID]
	delete(p.remotes, accountID)
	if r != nil {
		r.mu.Lock()
		r.live, r.stored = "", ""
		r.mu.Unlock()
		return r.store.drop()
	}
	if p.endpoint != nil {
		return tokenStore{dir: p.endpoint.Dir, name: storeFileName(accountID)}.drop()
	}
	return nil
}

// DefaultAccount is the id of the default account: position 0, or the
// implicit account before the first SetAccounts.
func (p *Provider) DefaultAccount() string {
	if p == nil {
		return implicitID
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.defaultLocked().ID
}

func (p *Provider) defaultLocked() Account {
	if len(p.accounts) == 0 {
		return Account{ID: implicitID, Active: true}
	}
	return p.accounts[0]
}

// DefaultRegion is the default account's storefront, "" while unknown.
func (p *Provider) DefaultRegion() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.defaultLocked().Region
}

// Serves reports whether an active account has storefront as its region.
func (p *Provider) Serves(storefront string) bool {
	return p.accountFor(storefront) != ""
}

// accountFor is the active account of storefront with the lowest position,
// "" when none.
func (p *Provider) accountFor(storefront string) string {
	if p == nil {
		return ""
	}
	storefront = strings.ToLower(strings.TrimSpace(storefront))
	if storefront == "" {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, a := range p.accounts {
		if a.Active && a.Region == storefront {
			return a.ID
		}
	}
	return ""
}

// AccountFor is the account TokenFor reads storefront with: the active
// account of that region with the lowest position, else the default.
func (p *Provider) AccountFor(storefront string) string {
	if id := p.accountFor(storefront); id != "" {
		return id
	}
	return p.DefaultAccount()
}

// Token returns the default account's effective token for cfg (Account).
func (p *Provider) Token(cfg config.Config) Token {
	return p.Account(cfg, p.DefaultAccount())
}

// TokenFor returns the effective token of the account that serves
// storefront (AccountFor).
func (p *Provider) TokenFor(cfg config.Config, storefront string) Token {
	return p.Account(cfg, p.AccountFor(storefront))
}

// Account returns an account's effective token for cfg: the wrapper's
// (Wrapper) when amdl has one, else, for the default account,
// catalog.media_user_token trimmed, else none. An unreadable or invalid
// wrapper file is logged once per distinct error.
func (p *Provider) Account(cfg config.Config, accountID string) Token {
	v, err := p.Wrapper(cfg, accountID)
	p.noteErr(err)
	if err == nil && v != "" {
		return Token{Value: v, Source: SourceWrapper, Account: accountID}
	}
	if accountID == p.DefaultAccount() {
		if v = strings.TrimSpace(cfg.Catalog.MediaUserToken); v != "" {
			return Token{Value: v, Source: SourceConfig, Account: accountID}
		}
	}
	return Token{Account: accountID}
}

// Wrapper returns an account's wrapper token alone, in the package comment's
// order without catalog.media_user_token: "" with a nil error when there is
// none, an error when the file cannot be read or is not a token and no stored
// copy stands in. A file read is one small file, so files are not cached and
// a login or a refresh shows up on the next call; GET /token's token is held
// in memory.
func (p *Provider) Wrapper(cfg config.Config, accountID string) (string, error) {
	if p == nil {
		return "", nil
	}
	p.mu.Lock()
	r := p.remotes[accountID]
	p.mu.Unlock()
	if v := r.liveToken(); v != "" {
		return v, nil
	}
	v, err := p.readFile(cfg, accountID)
	if err == nil && v != "" {
		return v, nil
	}
	if s := r.storedToken(); s != "" {
		return s, nil
	}
	return v, err
}

// MatchStored returns the account among candidates whose current GET /token
// answer is the token in the implicit account's stored copy, the one amdl
// used before it knew accounts; "" when none matches or there is no copy.
// Each fetch is bounded by the wrapper client's request timeout. Tokens are
// compared by digest and never logged.
func (p *Provider) MatchStored(ctx context.Context, candidates []string) string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	e := p.endpoint
	p.mu.Unlock()
	if e == nil || e.Fetch == nil {
		return ""
	}
	stored, err := tokenStore{dir: e.Dir, name: StoreFileName}.load(e.Address)
	if err != nil || stored == "" {
		return ""
	}
	want := sha256.Sum256([]byte(stored))
	for _, id := range candidates {
		reply, err := e.Fetch(ctx, id)
		if err != nil {
			continue
		}
		v, err := Validate(reply.MusicToken)
		if err != nil {
			continue
		}
		got := sha256.Sum256([]byte(v))
		if subtle.ConstantTimeCompare(want[:], got[:]) == 1 {
			return id
		}
	}
	return ""
}

// readFile reads and validates an account's MUSIC_TOKEN under
// wrapper.data_dir, or NewReading's source.
func (p *Provider) readFile(cfg config.Config, accountID string) (string, error) {
	if p.read != nil {
		raw, err := p.read(accountID)
		if err != nil || raw == "" {
			return "", err
		}
		return Validate(raw)
	}
	dir := strings.TrimSpace(cfg.Wrapper.DataDir)
	if dir == "" {
		return "", nil
	}
	raw, err := readDataDir(dir, accountID)
	if err != nil {
		return "", err
	}
	if raw == "" {
		return "", nil
	}
	v, err := Validate(raw)
	if err != nil {
		return "", fmt.Errorf("wrapper.data_dir %s: %w", dir, err)
	}
	return v, nil
}

// noteErr logs a wrapper token error when it differs from the last one, so a
// bad file does not repeat on every download; nil forgets the last one.
func (p *Provider) noteErr(err error) {
	if p == nil {
		return
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	p.errMu.Lock()
	repeat := msg == p.lastErr
	p.lastErr = msg
	p.errMu.Unlock()
	if msg != "" && !repeat {
		slog.Default().Warn("the wrapper's media-user-token is unusable; catalog.media_user_token applies", "component", "usertoken", "error", msg)
	}
}

// Validate trims surrounding whitespace and checks what is left looks like a
// media-user-token: 16 to 4096 bytes of base64 or base64url (Apple's are
// base64). A torn read of a file being rewritten, or another file's bytes,
// fails here too.
func Validate(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if len(v) < minLen || len(v) > maxLen {
		return "", fmt.Errorf("%w: %d bytes, want %d to %d", ErrInvalid, len(v), minLen, maxLen)
	}
	for i := 0; i < len(v); i++ {
		if !tokenByte(v[i]) {
			return "", fmt.Errorf("%w: byte %d is not a base64 character", ErrInvalid, i)
		}
	}
	return v, nil
}

func tokenByte(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || strings.IndexByte("+/=-_", c) >= 0
}

// readDataDir reads an account's MUSIC_TOKEN under dir: a wrapper-manager
// instance's <dir>/instances/<id>/MUSIC_TOKEN, or for the implicit account
// <dir>/MUSIC_TOKEN, and when that is missing the newest
// <dir>/instances/*/MUSIC_TOKEN. A missing directory or file is no token.
func readDataDir(dir, accountID string) (string, error) {
	if accountID != implicitID {
		if strings.ContainsAny(accountID, `/\`) || accountID == "." || accountID == ".." {
			return "", nil
		}
		raw, err := readLimited(filepath.Join(dir, "instances", accountID, FileName))
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return raw, err
	}
	raw, err := readLimited(filepath.Join(dir, FileName))
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return raw, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, "instances"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	var newest string
	var newestMod time.Time
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, "instances", e.Name(), FileName)
		st, err := os.Stat(path)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		if newest == "" || st.ModTime().After(newestMod) {
			newest, newestMod = path, st.ModTime()
		}
	}
	if newest == "" {
		return "", nil
	}
	return readLimited(newest)
}

// readLimited reads path, refusing a file longer than maxRead.
func readLimited(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxRead+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if len(b) > maxRead {
		return "", fmt.Errorf("%s: %w: longer than %d bytes", path, ErrInvalid, maxRead)
	}
	return string(b), nil
}
