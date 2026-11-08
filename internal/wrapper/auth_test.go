package wrapper

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"amdl/internal/config"
)

// keyGateway answers like a gateway in front of a wrapper: 401 without the
// right key, the wrapper's reply with it.
type keyGateway struct {
	mu   sync.Mutex
	key  string
	seen []string
}

func (g *keyGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	auth := r.Header.Get("Authorization")
	g.seen = append(g.seen, auth)
	if g.key != "" && auth != "Bearer "+g.key {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		return
	}
	_, _ = w.Write([]byte(`{"code":0,"msg":"SUCCESS","data":{"regions":["us"]}}`))
}

func (g *keyGateway) last() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.seen[len(g.seen)-1]
}

// wrapper.token goes out as a bearer key, read for each request; empty sends
// no header; a gateway's 401 is ErrUnauthorized, which is not retried.
func TestWrapperTokenIsSentAndARefusalIsUnauthorized(t *testing.T) {
	g := &keyGateway{key: "right-key"}
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	var mu sync.Mutex
	token := " right-key "
	c, err := NewClient(config.WrapperConfig{Address: srv.URL, TimeoutSeconds: 5}, WithToken(func() string { mu.Lock(); defer mu.Unlock(); return token }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()
	if _, err := c.Status(ctx); err != nil {
		t.Fatalf("status with the right key: %v", err)
	}
	if got := g.last(); got != "Bearer right-key" {
		t.Fatalf("Authorization = %q", got)
	}

	mu.Lock()
	token = "wrong-key"
	mu.Unlock()
	_, err = c.Status(ctx)
	if !errors.Is(err, ErrUnauthorized) || !strings.Contains(err.Error(), "wrapper.token") {
		t.Fatalf("status with a wrong key = %v, want ErrUnauthorized", err)
	}
	if strings.Contains(err.Error(), "wrong-key") {
		t.Fatal("the error quotes the key")
	}
	if retryable(err) {
		t.Fatal("a refused key is retried")
	}

	mu.Lock()
	token = ""
	mu.Unlock()
	g.mu.Lock()
	g.key = ""
	g.mu.Unlock()
	if _, err := c.Status(ctx); err != nil {
		t.Fatalf("a LAN wrapper without a key: %v", err)
	}
	if got := g.last(); got != "" {
		t.Fatalf("Authorization = %q, want none", got)
	}
}

// A wrapper's own 401, with its envelope, stays the wrapper's reply.
func TestWrapperOwn401IsNotTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"msg":"login failed: bad password"}`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(config.WrapperConfig{Address: srv.URL, TimeoutSeconds: 5}, WithToken(func() string { return "k" }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Status(context.Background())
	var reply *ReplyError
	if errors.Is(err, ErrUnauthorized) || !errors.As(err, &reply) || reply.Msg != "login failed: bad password" {
		t.Fatalf("status = %v, want the wrapper's reply", err)
	}
}

// A 401 that asks for a bearer key is the gateway's refusal, whatever its
// body: a gateway may answer in JSON shaped like the wrapper's envelope.
func TestBearerChallengeIsTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"msg":"unauthorized"}`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(config.WrapperConfig{Address: srv.URL, TimeoutSeconds: 5}, WithToken(func() string { return "k" }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, err := c.Status(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("status = %v, want ErrUnauthorized", err)
	}
}

// A wrapper at *.workers.dev that times out or loses its connection says to
// bind a custom domain; elsewhere it does not.
func TestWorkersDevHint(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Cut every connection without an answer.
			_ = conn.(*net.TCPConn).SetLinger(0)
			_ = conn.Close()
		}
	}()
	addr := ln.Addr().String()
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, addr)
	}
	for _, tc := range []struct {
		address string
		hint    bool
	}{
		{"http://amdl-wrapper.example.workers.dev", true},
		{"http://wrapper.example.com", false},
	} {
		c, err := NewClient(config.WrapperConfig{Address: tc.address, TimeoutSeconds: 2})
		if err != nil {
			t.Fatal(err)
		}
		c.http.Transport.(*http.Transport).DialContext = dial
		_, err = c.Status(context.Background())
		_ = c.Close()
		if err == nil {
			t.Fatalf("%s: a cut connection answered", tc.address)
		}
		if got := strings.Contains(err.Error(), "custom domain"); got != tc.hint {
			t.Errorf("%s: %v; hint %v, want %v", tc.address, err, got, tc.hint)
		}
	}
	if !isWorkersDev("x.workers.dev.") || isWorkersDev("workers.dev.example.com") {
		t.Error("isWorkersDev")
	}
}

func TestGenerateKey(t *testing.T) {
	a, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GenerateKey()
	raw, err := base64.RawURLEncoding.DecodeString(a)
	if err != nil || len(raw) != 32 || a == b {
		t.Fatalf("keys %q %q: %v", a, b, err)
	}
}
