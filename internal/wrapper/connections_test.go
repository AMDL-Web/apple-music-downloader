package wrapper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/config"
)

// wrapperLiteThreads is the HTTP thread pool of wrapper-lite
// (CPPHTTPLIB_THREAD_POOL_COUNT in its lite_main.cpp).
const wrapperLiteThreads = 8

// threadListener serves connections the way wrapper-lite does: a fixed number
// of threads, each holding one connection until the client closes it or it
// idles out, so a connection beyond them is not accepted, and its request not
// read, until a thread is free.
type threadListener struct {
	net.Listener
	threads chan struct{}
	done    chan struct{}
	close   sync.Once
	open    atomic.Int32
	peak    atomic.Int32
}

func newThreadListener(t *testing.T, threads int) *threadListener {
	t.Helper()
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return &threadListener{Listener: inner, threads: make(chan struct{}, threads), done: make(chan struct{})}
}

func (l *threadListener) Accept() (net.Conn, error) {
	select {
	case l.threads <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.threads
		return nil, err
	}
	n := l.open.Add(1)
	for {
		p := l.peak.Load()
		if n <= p || l.peak.CompareAndSwap(p, n) {
			break
		}
	}
	return &threadConn{Conn: conn, release: sync.OnceFunc(func() {
		l.open.Add(-1)
		<-l.threads
	})}, nil
}

func (l *threadListener) Close() error {
	l.close.Do(func() { close(l.done) })
	return l.Listener.Close()
}

type threadConn struct {
	net.Conn
	release func()
}

func (c *threadConn) Close() error {
	c.release()
	return c.Conn.Close()
}

func newThreadedClient(t *testing.T, threads int, handler http.Handler) (*Client, *threadListener) {
	t.Helper()
	listener := newThreadListener(t, threads)
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = listener
	srv.Config.IdleTimeout = 30 * time.Second
	srv.Start()
	t.Cleanup(srv.Close)
	c, err := NewClient(config.WrapperConfig{Address: srv.URL, TimeoutSeconds: 3}, WithTemplateParser(parseXOR))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, listener
}

// An album's tracks ask for their lyrics at once. Every connection the client
// opens holds one of wrapper-lite's threads until it idles out, so a client
// that opened one per request would leave the requests past the eighth unread
// until their timeout.
func TestLyricsBurstFitsWrapperLiteThreads(t *testing.T) {
	c, listener := newThreadedClient(t, wrapperLiteThreads, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		writeReply(w, 0, "SUCCESS", map[string]any{"lyrics": "doc " + r.URL.Query().Get("adamId")})
	}))

	start := time.Now()
	var tracks sync.WaitGroup
	for i := range 13 {
		tracks.Go(func() {
			id := fmt.Sprint(i)
			got, err := c.Lyrics(context.Background(), id, LyricsRequestOptions{Syllable: true})
			if err != nil {
				t.Errorf("track %s: %v", id, err)
			} else if got != "doc "+id {
				t.Errorf("track %s lyrics = %q", id, got)
			}
		})
	}
	tracks.Wait()
	if peak := listener.peak.Load(); peak >= wrapperLiteThreads {
		t.Errorf("client held %d connections at once; wrapper-lite reads only %d, so none is left for another client", peak, wrapperLiteThreads)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("13 lyrics requests took %s", elapsed)
	}
}

// A track cancelled while it waits for a data permit gives the wait up at
// once, and holds no permit afterwards.
func TestLyricsWaitEndsOnCancelWithoutKeepingAPermit(t *testing.T) {
	var gate atomic.Pointer[chan struct{}]
	first := make(chan struct{})
	gate.Store(&first)
	var inFlight atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wait := *gate.Load()
		inFlight.Add(1)
		<-wait
		inFlight.Add(-1)
		writeReply(w, 0, "SUCCESS", map[string]any{"lyrics": "doc"})
	}))

	var holders sync.WaitGroup
	for i := range config.MaxParallelWrapperRequests {
		holders.Go(func() {
			if _, err := c.Lyrics(context.Background(), fmt.Sprint(i), LyricsRequestOptions{}); err != nil {
				t.Error(err)
			}
		})
	}
	waitFor(t, func() bool { return inFlight.Load() == config.MaxParallelWrapperRequests })

	ctx, cancel := context.WithCancel(context.Background())
	waited := make(chan error, 1)
	go func() {
		_, err := c.Lyrics(ctx, "cancelled", LyricsRequestOptions{})
		waited <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-waited:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled wait = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled Lyrics call kept waiting for a permit")
	}
	second := make(chan struct{})
	gate.Store(&second)
	close(first)
	holders.Wait()

	// All permits are back: a full burst is in flight at once again.
	var again sync.WaitGroup
	for i := range config.MaxParallelWrapperRequests {
		again.Go(func() {
			if _, err := c.Lyrics(context.Background(), fmt.Sprint(i), LyricsRequestOptions{}); err != nil {
				t.Error(err)
			}
		})
	}
	waitFor(t, func() bool { return inFlight.Load() == config.MaxParallelWrapperRequests })
	close(second)
	again.Wait()
}

// A request the wrapper does not answer in time says so, without the URL and
// query http.Client puts into its error.
func TestRequestTimeoutIsWordedWithoutTheURL(t *testing.T) {
	stop := make(chan struct{})
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-stop:
		case <-r.Context().Done():
		}
	}))
	defer close(stop)
	c.timeout = 50 * time.Millisecond

	_, err := c.Lyrics(context.Background(), "1783644606", LyricsRequestOptions{Language: "zh-Hans_CN", Syllable: true})
	if err == nil {
		t.Fatal("Lyrics returned no error for a wrapper that never answers")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it to wrap context.DeadlineExceeded", err)
	}
	if got, want := err.Error(), "wrapper /lyrics: no reply within 50ms"; got != want {
		t.Errorf("err = %q, want %q", got, want)
	}
	for _, leak := range []string{"http://", "adamId", "1783644606", "Get "} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("err = %q, contains %q", err.Error(), leak)
		}
	}
}

// A caller that gives up first is a cancellation, not the wrapper's timeout.
func TestRequestCancelledByCallerIsNotATimeout(t *testing.T) {
	stop := make(chan struct{})
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-stop:
		case <-r.Context().Done():
		}
	}))
	defer close(stop)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Lyrics(ctx, "1", LyricsRequestOptions{})
	var timeout *timeoutError
	if errors.As(err, &timeout) {
		t.Fatalf("err = %v, reported as the wrapper's timeout", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the caller's deadline", err)
	}
}
