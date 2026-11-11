package usertoken

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ManualMode says how a token the user gave amdl ranks against the wrapper's.
type ManualMode string

const (
	// ManualFallback uses the given token only while the wrapper has none
	// for the account (logged out, no GET /token, unreachable since start).
	ManualFallback ManualMode = "fallback"
	// ManualOverride uses the given token ahead of the wrapper's.
	ManualOverride ManualMode = "override"
)

// ErrBadMode means a ManualMode is neither fallback nor override.
var ErrBadMode = errors.New("mode must be fallback or override")

// manualFilePrefix names a given token's file in the store directory:
// manual- plus the account's stored-copy file name. The file holds the
// account id, since the file name reduces it.
const manualFilePrefix = "manual-"

// Manual describes a token the user gave for an account, without its value.
// Rejected is set when Apple refused it since it was given; it is not stored,
// so a restart tries it again.
type Manual struct {
	// Account is the account the token was given for: the one asked about,
	// or the implicit account's, which serves every account without its own.
	Account  string
	Mode     ManualMode
	SetAt    time.Time
	Rejected bool
}

type manualEntry struct {
	value string
	Manual
}

// storedManual is a given token's file. SetAt is Unix seconds.
type storedManual struct {
	Account    string     `json:"account"`
	Mode       ManualMode `json:"mode"`
	MusicToken string     `json:"music_token"`
	SetAt      int64      `json:"set_at"`
}

func manualStore(dir, accountID string) tokenStore {
	return tokenStore{dir: dir, name: manualFilePrefix + storeFileName(accountID)}
}

// LoadManual loads the tokens the user gave from dir (StoreDir) and keeps
// dir for SetManual. Call it before the Provider is shared. A file that
// cannot be read is skipped and named in the returned error; the others
// still load.
func (p *Provider) LoadManual(dir string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.manualDir = dir
	p.manual = map[string]manualEntry{}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, manualFilePrefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		st, err := readManual(filepath.Join(dir, name))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p.manual[st.Account] = manualEntry{value: st.MusicToken, Manual: Manual{Account: st.Account, Mode: st.Mode, SetAt: time.Unix(st.SetAt, 0).UTC()}}
	}
	return errors.Join(errs...)
}

// readManual reads and checks one given token's file. Errors never include
// its contents.
func readManual(path string) (storedManual, error) {
	f, err := os.Open(path)
	if err != nil {
		return storedManual{}, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxStoreRead+1))
	if err != nil {
		return storedManual{}, fmt.Errorf("read %s: %w", path, err)
	}
	var st storedManual
	if len(raw) > maxStoreRead || json.Unmarshal(raw, &st) != nil || st.Account == "" {
		return storedManual{}, fmt.Errorf("%s: %w: not a given token's JSON", path, ErrInvalid)
	}
	if st.Mode != ManualFallback && st.Mode != ManualOverride {
		return storedManual{}, fmt.Errorf("%s: %w", path, ErrBadMode)
	}
	v, err := Validate(st.MusicToken)
	if err != nil {
		return storedManual{}, fmt.Errorf("%s: %w", path, err)
	}
	st.MusicToken = v
	return st, nil
}

// SetManual stores raw as the account's given token with mode, replacing
// one given before. It fails with ErrInvalid or ErrBadMode before writing
// anything, and without LoadManual.
func (p *Provider) SetManual(accountID, raw string, mode ManualMode) (Manual, error) {
	if mode == "" {
		mode = ManualFallback
	}
	if mode != ManualFallback && mode != ManualOverride {
		return Manual{}, ErrBadMode
	}
	v, err := Validate(raw)
	if err != nil {
		return Manual{}, err
	}
	if p == nil {
		return Manual{}, errors.New("no token provider")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.manual == nil {
		return Manual{}, errors.New("given tokens are not loaded")
	}
	m := Manual{Account: accountID, Mode: mode, SetAt: time.Now().UTC().Truncate(time.Second)}
	st := storedManual{Account: accountID, Mode: mode, MusicToken: v, SetAt: m.SetAt.Unix()}
	if err := manualStore(p.manualDir, accountID).saveJSON(st); err != nil {
		return Manual{}, err
	}
	p.manual[accountID] = manualEntry{value: v, Manual: m}
	return m, nil
}

// ClearManual drops the account's given token; none is not an error.
func (p *Provider) ClearManual(accountID string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.manual == nil {
		return nil
	}
	// The file first: a failed drop keeps the token in use, as it would be
	// again after a restart.
	if err := manualStore(p.manualDir, accountID).drop(); err != nil {
		return err
	}
	delete(p.manual, accountID)
	return nil
}

// ManualFor describes the given token the account uses (manualToken), false
// when there is none.
func (p *Provider) ManualFor(accountID string) (Manual, bool) {
	e, ok := p.manualToken(accountID)
	return e.Manual, ok
}

// manualToken is the given token the account uses: its own, else the
// implicit account's, which serves every account without one; false when
// there is none.
func (p *Provider) manualToken(accountID string) (manualEntry, bool) {
	if p == nil {
		return manualEntry{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.manual[accountID]
	if !ok && accountID != implicitID {
		e, ok = p.manual[implicitID]
	}
	return e, ok
}

// NoteRejected marks every given token equal to value as refused by Apple,
// so the accounts list can say so. amdl keeps using it: the user decides
// whether to replace or drop it. Tokens are compared by digest.
func (p *Provider) NoteRejected(value string) {
	if p == nil || value == "" {
		return
	}
	want := sha256.Sum256([]byte(value))
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, e := range p.manual {
		got := sha256.Sum256([]byte(e.value))
		if subtle.ConstantTimeCompare(want[:], got[:]) == 1 {
			e.Rejected = true
			p.manual[id] = e
		}
	}
}
