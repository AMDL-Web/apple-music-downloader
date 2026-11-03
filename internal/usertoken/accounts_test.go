package usertoken

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"amdl/internal/wrapper"
)

// instanceWrapper answers GET /token per instance: tokens[id], or a 503 when
// it has none. It records the instances asked for.
type instanceWrapper struct {
	mu     sync.Mutex
	tokens map[string]string
	asked  []string
}

func (w *instanceWrapper) fetch(_ context.Context, instance string) (wrapper.TokenReply, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.asked = append(w.asked, instance)
	tok, ok := w.tokens[instance]
	if !ok {
		return wrapper.TokenReply{}, wrapper.ErrNoToken
	}
	return wrapper.TokenReply{MusicToken: tok, NextRefreshAt: time.Now().Add(time.Hour).Unix()}, nil
}

func (w *instanceWrapper) askedFor(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, a := range w.asked {
		if a == id {
			return true
		}
	}
	return false
}

var (
	usTok = strings.Repeat("U", 200)
	jpTok = strings.Repeat("J", 200)
)

func accountsProvider(t *testing.T, w *instanceWrapper, dir string) *Provider {
	t.Helper()
	p := New()
	p.UseEndpoint(Endpoint{Fetch: w.fetch, Dir: dir, Address: testAddress, Logger: slog.New(slog.DiscardHandler)})
	return p
}

// Each active account has its own GET /token loop, asked by instance id, and
// TokenFor picks the account of the storefront, else the default.
func TestTokenPerAccountAndByStorefront(t *testing.T) {
	w := &instanceWrapper{tokens: map[string]string{"us-id": usTok, "jp-id": jpTok}}
	p := accountsProvider(t, w, t.TempDir())
	p.SetAccounts([]Account{
		{ID: "jp-id", Region: "JP", Position: 1, Active: true},
		{ID: "us-id", Region: "us", Position: 0, Active: true},
		{ID: "gone", Region: "kr", Position: 2, Active: false},
	})
	runLoop(t, p)
	cfg := cfgWith(configTok, "")
	waitFor(t, "both tokens", func() bool {
		return p.Account(cfg, "us-id").Value == usTok && p.Account(cfg, "jp-id").Value == jpTok
	})
	if w.askedFor("") || w.askedFor("gone") {
		t.Fatalf("asked for %v; want only the active instances", w.asked)
	}
	if got := p.TokenFor(cfg, "jp"); got.Value != jpTok || got.Account != "jp-id" {
		t.Fatalf("TokenFor(jp) = %s from %q", got.Source, got.Account)
	}
	if got := p.TokenFor(cfg, "kr"); got.Value != usTok || got.Account != "us-id" {
		t.Fatalf("TokenFor(kr), served by no active account, = %s from %q, want the default's", got.Source, got.Account)
	}
	if got := p.Token(cfg); got.Account != "us-id" || got.Value != usTok {
		t.Fatalf("Token = %q", got.Account)
	}
	if !p.Serves("JP") || p.Serves("kr") || p.DefaultRegion() != "us" {
		t.Fatalf("Serves(JP)=%v Serves(kr)=%v DefaultRegion=%q", p.Serves("JP"), p.Serves("kr"), p.DefaultRegion())
	}
	// catalog.media_user_token stands in for the default account only.
	if got := p.Account(cfg, "gone"); got.Value != "" {
		t.Fatalf("a non-default account fell back to %s", got.Source)
	}
}

// Two accounts of one region: the lower position's token is used.
func TestSameRegionLowerPositionWins(t *testing.T) {
	w := &instanceWrapper{tokens: map[string]string{"a": usTok, "b": jpTok}}
	p := accountsProvider(t, w, t.TempDir())
	p.SetAccounts([]Account{{ID: "b", Region: "us", Position: 1, Active: true}, {ID: "a", Region: "us", Position: 0, Active: true}})
	runLoop(t, p)
	cfg := cfgWith("", "")
	waitFor(t, "token of a", func() bool { return p.TokenFor(cfg, "us").Value == usTok })
	p.SetAccounts([]Account{{ID: "b", Region: "us", Position: 0, Active: true}, {ID: "a", Region: "us", Position: 1, Active: true}})
	if got := p.TokenFor(cfg, "us"); got.Account != "b" {
		t.Fatalf("after the move TokenFor(us) = %q, want b", got.Account)
	}
}

// Before any account is known the implicit account is asked for with a bare
// GET /token; once instances are known its loop stops.
func TestImplicitAccountUntilAccountsAreKnown(t *testing.T) {
	w := &instanceWrapper{tokens: map[string]string{"": usTok, "jp-id": jpTok}}
	p := accountsProvider(t, w, t.TempDir())
	runLoop(t, p)
	cfg := cfgWith("", "")
	waitFor(t, "implicit token", func() bool { return p.Token(cfg).Value == usTok })
	if p.DefaultAccount() != implicitID {
		t.Fatalf("default = %q", p.DefaultAccount())
	}
	p.SetAccounts([]Account{{ID: "jp-id", Region: "jp", Active: true}})
	waitFor(t, "jp token", func() bool { return p.Token(cfg).Value == jpTok })
}

// data_dir serves an instance its own file.
func TestDataDirInstanceFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "instances", "jp-id", FileName), jpTok, time.Time{})
	writeFile(t, filepath.Join(dir, "instances", "us-id", FileName), usTok, time.Now().Add(time.Hour))
	p := New()
	p.SetAccounts([]Account{{ID: "us-id", Active: true}, {ID: "jp-id", Position: 1, Active: true}})
	cfg := cfgWith("", dir)
	if v, err := p.Wrapper(cfg, "jp-id"); err != nil || v != jpTok {
		t.Fatalf("jp-id = %d bytes, %v", len(v), err)
	}
	if v, err := p.Wrapper(cfg, "../jp-id"); err != nil || v != "" {
		t.Fatalf("a path in the id read %d bytes, %v", len(v), err)
	}
	if v, _ := p.Wrapper(cfg, "missing"); v != "" {
		t.Fatal("an account without a file has a token")
	}
}

// Forget drops a deleted account's stored copy.
func TestForgetDropsTheStoredCopy(t *testing.T) {
	dir := t.TempDir()
	w := &instanceWrapper{tokens: map[string]string{"jp-id": jpTok}}
	p := accountsProvider(t, w, dir)
	p.SetAccounts([]Account{{ID: "jp-id", Region: "jp", Active: true}})
	runLoop(t, p)
	path := filepath.Join(dir, "account-jp-id.json")
	waitFor(t, "stored copy", func() bool { _, err := os.Stat(path); return err == nil })
	if err := p.Forget("jp-id"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stored copy still there: %v", err)
	}
	if got := p.Account(cfgWith("", ""), "jp-id"); got.Value != "" {
		t.Fatal("a forgotten account still has a token")
	}
}

// MatchStored finds the account whose token the pre-accounts copy holds.
func TestMatchStored(t *testing.T) {
	dir := t.TempDir()
	if err := (tokenStore{dir: dir}).save(storedToken{Address: testAddress, MusicToken: jpTok}); err != nil {
		t.Fatal(err)
	}
	w := &instanceWrapper{tokens: map[string]string{"us-id": usTok, "jp-id": jpTok}}
	p := accountsProvider(t, w, dir)
	if got := p.MatchStored(context.Background(), []string{"us-id", "jp-id"}); got != "jp-id" {
		t.Fatalf("MatchStored = %q, want jp-id", got)
	}
	if got := p.MatchStored(context.Background(), []string{"us-id"}); got != "" {
		t.Fatalf("MatchStored without the owner = %q", got)
	}
}
