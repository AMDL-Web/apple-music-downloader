// Package usertoken decides which Apple Music media-user-token the backend
// uses. The wrapper keeps its account's token in a file named MUSIC_TOKEN and
// refreshes it on its own; when amdl can read that file, it uses it, and
// catalog.media_user_token is the fallback for a wrapper it cannot read. One
// wrapper account is supported, a wrapper-manager's included.
//
// Where the wrapper's token is read, afresh on every use:
//
//   - amdl runs the wrapper (internal/wrapperhost): the managed reader passed
//     to New, which reads the guest's disk image. wrapper.data_dir is ignored.
//   - wrapper.data_dir is set: <data_dir>/MUSIC_TOKEN, the layout of a
//     wrapper-lite base directory; when that file is missing,
//     <data_dir>/instances/*/MUSIC_TOKEN, a wrapper-manager's layout, whose
//     one account is used, the newest token if there are several.
//   - otherwise there is no wrapper token.
//
// The value is never logged, and never written to the config.
package usertoken

import (
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
)

// Source says where an effective token came from.
type Source string

const (
	// SourceNone means neither the wrapper nor the config has a token.
	SourceNone Source = ""
	// SourceWrapper is the wrapper's MUSIC_TOKEN.
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

	mu      sync.Mutex
	lastErr string
}

// New returns a Provider. managed reads the token of a wrapper amdl runs
// itself and returns the file's raw contents, or "" when there is none; pass
// nil when amdl does not run the wrapper.
func New(managed func() (string, error)) *Provider {
	return &Provider{managed: managed}
}

// Token returns the effective token for cfg: the wrapper's when amdl can read
// one, else catalog.media_user_token trimmed, else none. An unreadable or
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

// Wrapper returns the wrapper's token alone, validated and read afresh: ""
// with a nil error when there is no wrapper token to read, an error when the
// file cannot be read or is not a token. A read is one small file, or a few
// blocks of the managed guest's disk, so nothing is cached and a login or a
// refresh shows up on the next call.
func (p *Provider) Wrapper(cfg config.Config) (string, error) {
	if p == nil {
		return "", nil
	}
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
