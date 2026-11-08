// Command server serves the web console for its browser tests
// (apps/console/e2e): the embedded console, exactly as amdl serve mounts it
// under /console/, over the in-memory demo backend (internal/demo) instead of
// the real one, which reads Apple's catalog even in simulate mode.
//
// Every test gets a backend of its own: the e2e-world cookie names it, and a
// name the server has not seen yet starts a fresh backend, so tests share
// nothing and can run in parallel. A backend's clock starts at Epoch and
// stands still; the jobs move only when a test asks:
//
//	POST /e2e/step?world=NAME&ms=N   moves that backend's jobs and clock on by N ms
//
// The demo writes its artwork addresses under demo.Address, a host the
// browser cannot reach and the console's Content-Security-Policy would block;
// the server rewrites them to its own /art/.
//
// Built with go build -cover and run with GOCOVERDIR set, it writes its
// coverage when SIGINT or SIGTERM stops it.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"amdl/apps/console"
	"amdl/internal/demo"
)

// Epoch is every backend's starting time; the tests fix the browser's clock
// to the same instant, so relative times ("3 min ago") never change.
var Epoch = time.Date(2026, time.March, 14, 15, 0, 0, 0, time.UTC)

// cookie names the backend a request goes to.
const cookie = "e2e-world"

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "address to listen on")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: newHandler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Streams stay open until their clients go; closing them is fine here.
		if err := srv.Shutdown(shutdown); err != nil {
			_ = srv.Close()
		}
	}()
	fmt.Printf("console e2e server on http://%s/console/\n", ln.Addr())
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// world is one test's backend with its stopped clock.
type world struct {
	mu      sync.Mutex
	now     time.Time
	backend *demo.Backend
	proxy   *httputil.ReverseProxy
}

func newWorld() *world {
	w := &world{now: Epoch}
	w.backend = demo.New(w.clock)
	target := &url.URL{Scheme: "http", Host: "demo"}
	w.proxy = &httputil.ReverseProxy{
		Rewrite:        func(r *httputil.ProxyRequest) { r.SetURL(target) },
		Transport:      w.backend.Transport(),
		FlushInterval:  -1,
		ModifyResponse: rewriteArtwork,
		// A test that ends drops its streams; that is no error worth a line.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	return w
}

func (w *world) clock() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.now
}

// step moves the clock and the jobs on by d, one demo.Tick at a time, as a
// running backend would.
func (w *world) step(d time.Duration) {
	for d > 0 {
		dt := min(d, demo.Tick)
		w.mu.Lock()
		w.now = w.now.Add(dt)
		w.mu.Unlock()
		w.backend.Step(dt)
		d -= dt
	}
}

type worlds struct {
	mu sync.Mutex
	m  map[string]*world
}

func (ws *worlds) get(name string) *world {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	w, ok := ws.m[name]
	if !ok {
		w = newWorld()
		ws.m[name] = w
	}
	return w
}

func newHandler() http.Handler {
	ws := &worlds{m: map[string]*world{}}
	byCookie := func(r *http.Request) *world {
		name := ""
		if c, err := r.Cookie(cookie); err == nil {
			name = c.Value
		}
		return ws.get(name)
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		byCookie(r).proxy.ServeHTTP(w, r)
	})
	m := http.NewServeMux()
	m.Handle("GET /{$}", http.RedirectHandler("/console/", http.StatusFound))
	m.Handle("GET /console/", http.StripPrefix("/console", console.Handler()))
	m.Handle("/api/", api)
	m.Handle("GET /art/", api)
	m.HandleFunc("POST /e2e/step", func(w http.ResponseWriter, r *http.Request) {
		ms, err := strconv.Atoi(r.URL.Query().Get("ms"))
		if err != nil || ms < 0 {
			http.Error(w, "ms must be a whole number of milliseconds", http.StatusBadRequest)
			return
		}
		ws.get(r.URL.Query().Get("world")).step(time.Duration(ms) * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	})
	return m
}

// rewriteArtwork points the demo's artwork addresses at this server. A
// body is rewritten line by line as it streams, so event streams still flow.
func rewriteArtwork(resp *http.Response) error {
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "image/") {
		return nil
	}
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	resp.Body = &lineRewriter{src: bufio.NewReader(resp.Body), closer: resp.Body}
	return nil
}

var (
	demoArt = []byte(demo.Address + "/art/")
	ownArt  = []byte("/art/")
)

type lineRewriter struct {
	src    *bufio.Reader
	closer io.Closer
	buf    []byte
}

func (l *lineRewriter) Read(p []byte) (int, error) {
	if len(l.buf) == 0 {
		line, err := l.src.ReadBytes('\n')
		if len(line) == 0 {
			return 0, err
		}
		l.buf = bytes.ReplaceAll(line, demoArt, ownArt)
	}
	n := copy(p, l.buf)
	l.buf = l.buf[n:]
	return n, nil
}

func (l *lineRewriter) Close() error { return l.closer.Close() }
