// Package usertoken decides which Apple Music media-user-token the backend
// uses. The wrapper keeps its account's token and refreshes it on its own;
// amdl uses the wrapper's token whenever it can get it, and
// catalog.media_user_token is the fallback. One wrapper account is supported,
// a wrapper-manager's included.
//
// Where the token comes from, first match wins:
//
//  1. amdl runs the wrapper (internal/wrapperhost): the managed reader passed
//     to New, which reads the guest's disk image afresh on every use. GET
//     /token is not asked and wrapper.data_dir is ignored.
//  2. The wrapper's GET /token (amdl's wrapper-lite fork), held in memory from
//     this run's last 200. It is the token the running wrapper uses, so it
//     beats a file that may be a leftover of another account or install, and
//     it works for a wrapper on another host or in another container.
//  3. wrapper.data_dir: <data_dir>/MUSIC_TOKEN, the layout of a wrapper-lite
//     base directory; when that file is missing,
//     <data_dir>/instances/*/MUSIC_TOKEN, a wrapper-manager's layout, whose
//     one account is used, the newest token if there are several. Read afresh
//     on every use.
//  4. amdl's stored copy of an earlier run's GET /token, until the wrapper
//     answers in this run, so amdl keeps working while the wrapper restarts.
//  5. catalog.media_user_token.
//
// GET /token is asked by RunEndpoint's loop, never on a token read: once at
// start, then a few seconds after the wrapper's next_refresh_at (clamped to
// one minute at the least and one hour at the most), when RefreshSoon asks (a
// login, a logout, Apple refusing the token; at most every 30 seconds), and
// after an error with backoff from 5 seconds to 5 minutes. Each request is
// bounded by the wrapper client's request timeout. A 200 replaces the
// in-memory token and the stored copy. A 503 (no logged-in account) drops
// both, so a logged-out account's token is not used on. A 404 (a wrapper
// without /token: upstream wrapper-lite, an older fork build, a
// wrapper-manager that does not forward it) drops both too, since the wrapper
// cannot vouch for them, and /token is not asked again for 30 minutes,
// RefreshSoon included. Any other failure keeps what amdl has.
//
// The stored copy is <database dir>/usertoken/wrapper-token.json (StoreDir),
// directory 0700 and file 0600, written by a rename, and tied to the
// wrapper.address it came from. The value is never logged, never written to
// the config, and never part of an error.
package usertoken

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"amdl/internal/config"
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

// Token is the effective media-user-token and where it came from. Value is
// empty when Source is SourceNone.
type Token struct {
	Value  string
	Source Source
}

const (
	// FileName is the file the wrapper keeps its token in.
	FileName = "MUSIC_TOKEN"
	minLen   = 16
	maxLen   = 4096
	// maxRead bounds a read of the file, surrounding whitespace included; a
	// longer file is not a token.
	maxRead = 2 * maxLen
)

// ErrInvalid means the wrapper's file does not hold a media-user-token. The
// error never includes the file's contents.
var ErrInvalid = errors.New("not a media-user-token")

// Provider reads the wrapper's token and picks the effective one. The zero
// value and a nil *Provider both behave as "no wrapper token". It is safe for
// concurrent use.
type Provider struct {
	managed func() (string, error)
	remote  atomic.Pointer[remote]

	mu      sync.Mutex
	lastErr string
}

// New returns a Provider. managed reads the token of a wrapper amdl runs
// itself and returns the file's raw contents, or "" when there is none; pass
// nil when amdl does not run the wrapper.
func New(managed func() (string, error)) *Provider {
	return &Provider{managed: managed}
}

// UseEndpoint adds the wrapper's GET /token as a source and loads amdl's
// stored copy from e.Dir; RunEndpoint then keeps it current. It does nothing
// on a Provider that reads a managed wrapper, whose disk is read directly.
// Call it before the Provider is shared.
func (p *Provider) UseEndpoint(e Endpoint) {
	if p == nil || p.managed != nil || e.Fetch == nil {
		return
	}
	p.remote.Store(newRemote(e))
}

// RunEndpoint asks GET /token until ctx ends, as the package comment says. It
// returns at once without UseEndpoint.
func (p *Provider) RunEndpoint(ctx context.Context) {
	if p == nil {
		return
	}
	if r := p.remote.Load(); r != nil {
		r.run(ctx)
	}
}

// RefreshSoon asks RunEndpoint's loop to fetch GET /token now, without
// waiting for it: after a login or a logout, or when Apple refused the token.
func (p *Provider) RefreshSoon() {
	if p == nil {
		return
	}
	p.remote.Load().requestRefresh()
}

// Token returns the effective token for cfg: the wrapper's (Wrapper) when amdl
// has one, else catalog.media_user_token trimmed, else none. An unreadable or
// invalid wrapper file falls back to the config token and is logged once per
// distinct error.
func (p *Provider) Token(cfg config.Config) Token {
	v, err := p.Wrapper(cfg)
	p.noteErr(err)
	if err == nil && v != "" {
		return Token{Value: v, Source: SourceWrapper}
	}
	if v = strings.TrimSpace(cfg.Catalog.MediaUserToken); v != "" {
		return Token{Value: v, Source: SourceConfig}
	}
	return Token{}
}

// Wrapper returns the wrapper's token alone, in the package comment's order
// without catalog.media_user_token: "" with a nil error when there is none,
// an error when the file cannot be read or is not a token and no stored copy
// stands in. A file read is one small file, or a few blocks of the managed
// guest's disk, so files are not cached and a login or a refresh shows up on
// the next call; GET /token's token is held in memory.
func (p *Provider) Wrapper(cfg config.Config) (string, error) {
	if p == nil {
		return "", nil
	}
	r := p.remote.Load()
	if v := r.liveToken(); v != "" {
		return v, nil
	}
	v, err := p.readFile(cfg)
	if err == nil && v != "" {
		return v, nil
	}
	if s := r.storedToken(); s != "" {
		return s, nil
	}
	return v, err
}

// readFile reads and validates the managed wrapper's or wrapper.data_dir's
// MUSIC_TOKEN.
func (p *Provider) readFile(cfg config.Config) (string, error) {
	key, read := p.source(cfg)
	if read == nil {
		return "", nil
	}
	raw, err := read()
	if err != nil {
		return "", err
	}
	if raw == "" {
		return "", nil
	}
	v, err := Validate(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", key, err)
	}
	return v, nil
}

// source names where cfg's wrapper token lives and how to read it; read is
// nil when there is no wrapper token to read.
func (p *Provider) source(cfg config.Config) (key string, read func() (string, error)) {
	if p.managed != nil {
		return "the managed wrapper's " + FileName, p.managed
	}
	if dir := strings.TrimSpace(cfg.Wrapper.DataDir); dir != "" {
		return "wrapper.data_dir " + dir, func() (string, error) { return readDataDir(dir) }
	}
	return "", nil
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
	p.mu.Lock()
	repeat := msg == p.lastErr
	p.lastErr = msg
	p.mu.Unlock()
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

// readDataDir reads <dir>/MUSIC_TOKEN, or when that is missing the
// <dir>/instances/*/MUSIC_TOKEN of a wrapper-manager's one account, the newest
// if there are several. A missing directory or file is no token.
func readDataDir(dir string) (string, error) {
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
