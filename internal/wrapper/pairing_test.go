package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"amdl/internal/config"
)

// gateway is a fake Cloudflare Worker: device-code pairing, rotating
// tokens, and wrapper routes that need the current access token.
type gateway struct {
	mu        sync.Mutex
	polls     int
	approveAt int
	deny      bool
	access    string
	refresh   string
	rotations int
	// expireAccess makes the next wrapper request answer 401 once.
	expireAccess  bool
	refuseRefresh bool
	seen          []string
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	write := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	var body map[string]string
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch r.URL.Path {
	case "/pair/start":
		if !strings.HasPrefix(body["name"], "amdl") {
			write(400, map[string]string{"error": "invalid_request"})
			return
		}
		write(200, map[string]any{"user_code": "ABCD-EFGH", "verification_url": "https://gw.example/pair",
			"verification_url_complete": "https://gw.example/pair?code=ABCD-EFGH", "device_code": "dev-1", "interval": 0.01, "expires_in": 600})
	case "/pair/poll":
		g.polls++
		switch {
		case body["device_code"] != "dev-1":
			write(400, map[string]string{"error": "invalid_request"})
		case g.deny:
			write(400, map[string]string{"error": "access_denied"})
		case g.polls < g.approveAt:
			write(400, map[string]string{"error": "authorization_pending"})
		default:
			g.access, g.refresh = "access-1", "refresh-1"
			write(200, map[string]any{"access_token": g.access, "refresh_token": g.refresh, "expires_in": 86400})
		}
	case "/pair/refresh":
		if g.refuseRefresh || body["refresh_token"] != g.refresh {
			write(400, map[string]string{"error": "invalid_grant"})
			return
		}
		g.rotations++
		g.access, g.refresh = "access-"+string(rune('1'+g.rotations)), "refresh-"+string(rune('1'+g.rotations))
		write(200, map[string]any{"access_token": g.access, "refresh_token": g.refresh, "expires_in": 86400})
	default:
		auth := r.Header.Get("Authorization")
		g.seen = append(g.seen, auth)
		if g.expireAccess || auth != "Bearer "+g.access || g.access == "" {
			g.expireAccess = false
			write(401, map[string]string{"error": "unauthorized"})
			return
		}
		write(200, map[string]any{"code": 0, "msg": "SUCCESS", "data": map[string]any{"regions": []string{"us"}}})
	}
}

func pairedClient(t *testing.T, g *gateway) (*Client, *Pairing, string) {
	t.Helper()
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	dir := filepath.Join(t.TempDir(), "pairing")
	p, err := NewPairing(srv.URL, 5*time.Second, dir, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	p.minInterval = 0
	c, err := NewClient(config.WrapperConfig{Address: srv.URL, TimeoutSeconds: 5}, WithPairing(p))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, p, dir
}

func waitState(t *testing.T, p *Pairing, want string) PairingStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := p.Status()
		if st.State == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("pairing state = %+v, want %s", st, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Pairing goes start → pending (code and page) → paired once the owner
// approves; the credential is stored privately and every wrapper request
// carries the access token.
func TestPairingFlowAndBearer(t *testing.T) {
	g := &gateway{approveAt: 3}
	c, p, dir := pairedClient(t, g)
	ctx := context.Background()
	if _, err := c.Status(ctx); err == nil {
		t.Fatal("an unpaired request to the gateway succeeded")
	}
	if st := p.Status(); st.State != PairingNeeded {
		t.Fatalf("after a 401 without a token: %+v", st)
	}
	st, err := p.Start(ctx)
	if err != nil || st.State != PairingPending || st.UserCode != "ABCD-EFGH" || st.VerificationURLComplete != "https://gw.example/pair?code=ABCD-EFGH" || st.ExpiresAt == nil {
		t.Fatalf("start = %+v, %v", st, err)
	}
	waitState(t, p, PairingPaired)
	info, err := os.Stat(filepath.Join(dir, pairingFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stored credential: %v %v", info, err)
	}
	if _, err := c.Status(ctx); err != nil {
		t.Fatalf("paired status: %v", err)
	}
	if got := g.seen[len(g.seen)-1]; got != "Bearer access-1" {
		t.Fatalf("Authorization = %q", got)
	}
	// A restart reads the stored credential.
	again, err := NewPairing(p.address, time.Second, dir, slog.New(slog.DiscardHandler))
	if err != nil || again.Status().State != PairingPaired {
		t.Fatalf("reloaded = %+v, %v", again.Status(), err)
	}
	// Another wrapper address does not use it.
	other, _ := NewPairing("http://other.example", time.Second, dir, slog.New(slog.DiscardHandler))
	if other.Status().State != PairingUnpaired {
		t.Fatal("a credential of another wrapper was used")
	}
}

// A 401 renews the tokens once and retries; a refused refresh means pairing
// again.
func TestPairingRefreshAndRepair(t *testing.T) {
	g := &gateway{approveAt: 1}
	c, p, dir := pairedClient(t, g)
	ctx := context.Background()
	if _, err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitState(t, p, PairingPaired)
	g.mu.Lock()
	g.expireAccess = true
	g.mu.Unlock()
	if _, err := c.Status(ctx); err != nil {
		t.Fatalf("status after a 401: %v", err)
	}
	if g.rotations != 1 || g.seen[len(g.seen)-1] != "Bearer access-2" {
		t.Fatalf("rotations %d, last %q", g.rotations, g.seen[len(g.seen)-1])
	}
	g.mu.Lock()
	g.expireAccess, g.refuseRefresh = true, true
	g.mu.Unlock()
	if _, err := c.Status(ctx); err == nil {
		t.Fatal("a refused refresh still answered")
	}
	if st := p.Status(); st.State != PairingNeeded || !strings.Contains(st.LastError, "pair again") {
		t.Fatalf("after a refused refresh: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(dir, pairingFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the refused credential is still stored: %v", err)
	}
}

// An owner who refuses ends the pairing with an error; a wrapper without
// pairing says so.
func TestPairingRefusedAndUnsupported(t *testing.T) {
	g := &gateway{deny: true}
	_, p, _ := pairedClient(t, g)
	if _, err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, p, PairingUnpaired)
	if st.LastError == "" {
		t.Fatal("a refused pairing left no error")
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	bare, _ := NewPairing(srv.URL, time.Second, t.TempDir(), slog.New(slog.DiscardHandler))
	if _, err := bare.Start(context.Background()); !errors.Is(err, ErrPairingUnsupported) {
		t.Fatalf("start on a bare wrapper = %v", err)
	}
}
