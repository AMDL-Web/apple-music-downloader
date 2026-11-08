// Command mediareplay is a caching HTTPS proxy for benchmarking amdl without
// the Apple CDN in the measurement. In record mode it fetches each request it
// has not seen from the origin and keeps a complete 200 response; in replay
// mode it answers from that cache, with Range and If-Range support, and counts
// every request it could not answer. Set only HTTPS_PROXY: a wrapper on an
// http:// or loopback address then bypasses the proxy, so decryption stays live.
//
// Record, then replay:
//
//	go build -o bin/mediareplay ./tools/mediareplay
//	bin/mediareplay -mode record -dir cache     # the first start writes cache/ca.pem
//	cat /etc/ssl/certs/ca-certificates.crt cache/ca.pem > bundle.pem
//	HTTPS_PROXY=http://127.0.0.1:18600 SSL_CERT_FILE=bundle.pem amdl serve --config ...
//
// Download each album twice while recording: a resumed transfer sends a Range
// request, which is never kept, so one pass can leave a file out. Then restart
// with -mode replay, and add -rate to cap the total bandwidth in MB/s. GET
// /__reset before a run and GET /__stats after it: a run whose misses name an
// aod.* host fetched media from Apple and does not count. docs/benchmarks.md
// has the results measured this way.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18600", "proxy listen address")
	dir := flag.String("dir", "mediareplay-cache", "cache directory; holds the CA (ca.pem) and the responses")
	mode := flag.String("mode", "replay", "record: fetch misses from the origin and keep them; replay: answer from the cache, pass misses through and count them")
	mbps := flag.Float64("rate", 0, "total response bandwidth in MB/s across all connections; 0 is unlimited")
	flag.Parse()
	if *mode != "record" && *mode != "replay" {
		log.Fatalf("-mode must be record or replay, not %q", *mode)
	}
	p, err := newProxy(*dir, *mode == "record", *mbps)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("mediareplay %s on %s, cache %s, CA %s", *mode, *listen, *dir, filepath.Join(*dir, "ca.pem"))
	srv := &http.Server{Addr: *listen, Handler: p, ReadHeaderTimeout: 30 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

type proxy struct {
	dir     string
	record  bool
	limiter *rate.Limiter
	ca      tls.Certificate
	caCert  *x509.Certificate
	origin  *http.Client

	mu     sync.Mutex
	leaves map[string]*tls.Certificate
	misses map[string]int64

	hits, recorded, served atomic.Int64
}

func newProxy(dir string, record bool, mbps float64) (*proxy, error) {
	if err := os.MkdirAll(filepath.Join(dir, "obj"), 0o755); err != nil {
		return nil, err
	}
	ca, caCert, err := loadOrCreateCA(dir)
	if err != nil {
		return nil, err
	}
	p := &proxy{dir: dir, record: record, ca: ca, caCert: caCert,
		leaves: map[string]*tls.Certificate{}, misses: map[string]int64{}}
	// The proxy itself must never go through HTTPS_PROXY, which points at it.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	p.origin = &http.Client{Transport: transport, Timeout: 10 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if mbps > 0 {
		bps := mbps * 1e6
		p.limiter = rate.NewLimiter(rate.Limit(bps), int(bps/10)+64<<10)
	}
	return p, nil
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodConnect:
		p.tunnel(w, r)
	case r.URL.Host == "" && r.URL.Path == "/__stats":
		p.writeStats(w)
	case r.URL.Host == "" && r.URL.Path == "/__reset":
		p.mu.Lock()
		p.misses = map[string]int64{}
		p.mu.Unlock()
		p.hits.Store(0)
		p.recorded.Store(0)
		p.served.Store(0)
		p.writeStats(w)
	default:
		p.handle(w, r)
	}
}

func (p *proxy) writeStats(w http.ResponseWriter) {
	p.mu.Lock()
	misses := make(map[string]int64, len(p.misses))
	for k, v := range p.misses {
		misses[k] = v
	}
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"hits": p.hits.Load(), "recorded": p.recorded.Load(), "served_bytes": p.served.Load(), "misses": misses,
	})
}

// tunnel terminates the client's TLS with a leaf for the CONNECT host signed
// by the proxy CA and serves the requests inside it.
func (p *proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		conn.Close()
		return
	}
	tlsConn := tls.Server(conn, &tls.Config{
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := hello.ServerName
			if name == "" {
				name, _, _ = net.SplitHostPort(host)
			}
			return p.leaf(name)
		},
	})
	inner := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Scheme, r.URL.Host = "https", r.Host
			p.handle(w, r)
		}),
		ReadHeaderTimeout: 30 * time.Second,
	}
	_ = inner.Serve(&oneConnListener{conn: tlsConn})
}

func (p *proxy) leaf(name string) (*tls.Certificate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.leaves[name]; ok {
		return c, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	tmpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(30 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(name); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{name}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.caCert, &key.PublicKey, p.ca.PrivateKey)
	if err != nil {
		return nil, err
	}
	c := &tls.Certificate{Certificate: [][]byte{der, p.ca.Certificate[0]}, PrivateKey: key}
	p.leaves[name] = c
	return c, nil
}

// cacheKey names a response by host and path. Query strings are kept only
// for the catalog API, whose include and extend parameters change the body;
// CDN media and artwork URLs can carry rotating signatures.
func cacheKey(r *http.Request) string {
	key := r.URL.Host + r.URL.Path
	if strings.Contains(r.URL.Host, "api.") && r.URL.RawQuery != "" {
		key += "?" + r.URL.RawQuery
	}
	return key
}

type entry struct {
	Key    string      `json:"key"`
	Status int         `json:"status"`
	Header http.Header `json:"header"`
}

var keptHeaders = []string{"Content-Type", "Content-Encoding", "ETag", "Last-Modified", "Cache-Control"}

func (p *proxy) paths(key string) (string, string) {
	sum := sha256.Sum256([]byte(key))
	base := filepath.Join(p.dir, "obj", hex.EncodeToString(sum[:]))
	return base + ".body", base + ".json"
}

func (p *proxy) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		p.pass(w, r, false)
		return
	}
	key := cacheKey(r)
	body, meta := p.paths(key)
	if raw, err := os.ReadFile(meta); err == nil {
		var e entry
		if json.Unmarshal(raw, &e) == nil {
			if f, err := os.Open(body); err == nil {
				defer f.Close()
				p.hits.Add(1)
				p.serveCached(w, r, f, e)
				return
			}
		}
	}
	if !p.record {
		p.mu.Lock()
		p.misses[r.URL.Host]++
		p.mu.Unlock()
		log.Printf("miss %s %s", r.Method, key)
		p.pass(w, r, false)
		return
	}
	p.pass(w, r, r.Method == http.MethodGet && r.Header.Get("Range") == "")
}

func (p *proxy) serveCached(w http.ResponseWriter, r *http.Request, f *os.File, e entry) {
	for k, vs := range e.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if e.Status != http.StatusOK {
		w.WriteHeader(e.Status)
		n, _ := io.Copy(w, p.throttled(r.Context(), f))
		p.served.Add(n)
		return
	}
	modTime := time.Time{}
	if lm := e.Header.Get("Last-Modified"); lm != "" {
		modTime, _ = http.ParseTime(lm)
	}
	cw := &countingWriter{ResponseWriter: w}
	http.ServeContent(cw, r, "", modTime, &throttledSeeker{File: f, p: p, ctx: r.Context()})
	p.served.Add(cw.n)
}

// pass forwards r to the origin. With keep, a complete 200 body is also
// written to the cache as it streams to the client.
func (p *proxy) pass(w http.ResponseWriter, r *http.Request, keep bool) {
	out, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	for k, vs := range r.Header {
		if hopHeader(k) {
			continue
		}
		out.Header[k] = vs
	}
	resp, err := p.origin.Do(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		if hopHeader(k) {
			continue
		}
		w.Header()[k] = vs
	}
	w.WriteHeader(resp.StatusCode)
	if !keep || resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(w, resp.Body)
		return
	}
	body, meta := p.paths(cacheKey(r))
	tmp, err := os.CreateTemp(filepath.Dir(body), "rec-*")
	if err != nil {
		_, _ = io.Copy(w, resp.Body)
		return
	}
	n, copyErr := io.Copy(io.MultiWriter(w, tmp), resp.Body)
	closeErr := tmp.Close()
	complete := copyErr == nil && closeErr == nil && (resp.ContentLength < 0 || n == resp.ContentLength)
	if !complete {
		_ = os.Remove(tmp.Name())
		return
	}
	e := entry{Key: cacheKey(r), Status: resp.StatusCode, Header: http.Header{}}
	for _, k := range keptHeaders {
		if v := resp.Header.Get(k); v != "" {
			e.Header.Set(k, v)
		}
	}
	raw, _ := json.Marshal(e)
	if os.Rename(tmp.Name(), body) == nil && os.WriteFile(meta, raw, 0o644) == nil {
		p.recorded.Add(1)
	}
}

func hopHeader(k string) bool {
	switch http.CanonicalHeaderKey(k) {
	case "Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}

func (p *proxy) throttled(ctx context.Context, r io.Reader) io.Reader {
	if p.limiter == nil {
		return r
	}
	return &throttledReader{r: r, p: p, ctx: ctx}
}

type throttledReader struct {
	r   io.Reader
	p   *proxy
	ctx context.Context
}

func (t *throttledReader) Read(b []byte) (int, error) {
	if len(b) > 32<<10 {
		b = b[:32<<10]
	}
	n, err := t.r.Read(b)
	if n > 0 {
		if werr := t.p.limiter.WaitN(t.ctx, n); werr != nil {
			return n, werr
		}
	}
	return n, err
}

type throttledSeeker struct {
	*os.File
	p   *proxy
	ctx context.Context
}

func (t *throttledSeeker) Read(b []byte) (int, error) {
	if t.p.limiter == nil {
		return t.File.Read(b)
	}
	return (&throttledReader{r: t.File, p: t.p, ctx: t.ctx}).Read(b)
}

type countingWriter struct {
	http.ResponseWriter
	n int64
}

func (c *countingWriter) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	c.n += int64(n)
	return n, err
}

// oneConnListener hands one connection to http.Server, then reports closed
// once that connection ends.
type oneConnListener struct {
	conn net.Conn
	once sync.Once
	done chan struct{}
	mu   sync.Mutex
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if l.done == nil {
		l.done = make(chan struct{})
	}
	done := l.done
	l.mu.Unlock()
	var c net.Conn
	l.once.Do(func() { c = &closeNotifyConn{Conn: l.conn, done: done} })
	if c != nil {
		return c, nil
	}
	<-done
	return nil, net.ErrClosed
}

func (l *oneConnListener) Close() error   { return nil }
func (l *oneConnListener) Addr() net.Addr { return l.conn.LocalAddr() }

type closeNotifyConn struct {
	net.Conn
	done chan struct{}
	once sync.Once
}

func (c *closeNotifyConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { close(c.done) })
	return err
}

func loadOrCreateCA(dir string) (tls.Certificate, *x509.Certificate, error) {
	certPath, keyPath := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca-key.pem")
	if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		leaf, err := x509.ParseCertificate(c.Certificate[0])
		return c, leaf, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return tls.Certificate{}, nil, fmt.Errorf("load CA: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mediareplay local CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(5 * 365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return tls.Certificate{}, nil, err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return tls.Certificate{}, nil, err
	}
	c, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	leaf, err := x509.ParseCertificate(c.Certificate[0])
	return c, leaf, err
}
