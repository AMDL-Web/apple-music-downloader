package artwork

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"
)

// photo is a JPEG of side pixels that decodes as slowly as a real cover:
// noise, which compresses as badly as a photograph.
func photo(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	r := rand.New(rand.NewPCG(uint64(side), 7))
	for i := range img.Pix {
		img.Pix[i] = byte(r.IntN(256))
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// window draws frames over c on a clock it moves with time's, as a view
// does: each frame asks for the covers it shows, then the UI thread runs
// what was posted to it. In a synctest bubble, time is the bubble's.
type window struct {
	t      *testing.T
	c      *Cache
	posted chan func()
	clock  time.Time
	mu     sync.Mutex
}

func newWindow(t *testing.T, rt http.RoundTripper) *window {
	w := &window{t: t, posted: make(chan func(), 4096), clock: time.Date(2026, 11, 4, 21, 51, 0, 0, time.UTC)}
	w.c = NewWith(func(fn func()) { w.posted <- fn }, rt)
	w.c.now = func() time.Time { return w.clock }
	return w
}

// frame shows the covers of tpls at px pixels, 16 ms after the frame before,
// and reports how many of them show.
func (w *window) frame(tpls []string, px int) int {
	time.Sleep(16 * time.Millisecond)
	w.clock = w.clock.Add(16 * time.Millisecond)
	shown := 0
	for _, tpl := range tpls {
		if w.c.Get(tpl, px) != nil {
			shown++
		}
	}
	for {
		select {
		case fn := <-w.posted:
			fn()
		default:
			return shown
		}
	}
}

// cdnURL is where the tests' covers are, served by memLink.
const cdnURL = "https://cdn.test"

// memLink serves h to a client in memory, streaming the body as h writes
// it, so that a test can run inside a synctest bubble: its waits, its
// handlers' waits and the cache's stall timer are on the bubble's clock,
// where a real listener's reads would not let that clock move.
type memLink struct{ h http.Handler }

func (l memLink) RoundTrip(r *http.Request) (*http.Response, error) {
	pr, pw := io.Pipe()
	w := &pipeResponse{header: http.Header{}, body: pw, ready: make(chan struct{})}
	go func() {
		l.h.ServeHTTP(w, r)
		w.WriteHeader(http.StatusOK)
		// As a client sees a connection: a request it gave up fails, and so
		// does a body shorter than its Content-Length.
		n, err := strconv.ParseInt(w.sent.Get("Content-Length"), 10, 64)
		switch {
		case r.Context().Err() != nil:
			pw.CloseWithError(context.Cause(r.Context()))
		case err == nil && w.written < n:
			pw.CloseWithError(io.ErrUnexpectedEOF)
		default:
			pw.Close()
		}
	}()
	ctx := r.Context()
	select {
	case <-w.ready:
	case <-ctx.Done():
		pr.CloseWithError(context.Cause(ctx))
		return nil, context.Cause(ctx)
	}
	// A request given up ends its body's read, as a connection closed.
	stop := context.AfterFunc(ctx, func() { pr.CloseWithError(context.Cause(ctx)) })
	n, err := strconv.ParseInt(w.sent.Get("Content-Length"), 10, 64)
	if err != nil {
		n = -1
	}
	return &http.Response{Status: strconv.Itoa(w.status) + " " + http.StatusText(w.status), StatusCode: w.status, Header: w.sent, ContentLength: n,
		Body: bodyCloser{pr, stop}, Request: r}, nil
}

type bodyCloser struct {
	*io.PipeReader
	stop func() bool
}

func (b bodyCloser) Close() error {
	b.stop()
	return b.PipeReader.Close()
}

// pipeResponse is the ResponseWriter of memLink: its header goes out at
// the first write, as a server's.
type pipeResponse struct {
	header http.Header
	body   *io.PipeWriter
	once   sync.Once
	// status and sent are the status and header that went out.
	status int
	sent   http.Header
	ready  chan struct{}
	// written counts the body's bytes the client took.
	written int64
}

func (w *pipeResponse) Header() http.Header { return w.header }

func (w *pipeResponse) WriteHeader(status int) {
	w.once.Do(func() {
		w.status, w.sent = status, w.header.Clone()
		close(w.ready)
	})
}

func (w *pipeResponse) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	n, err := w.body.Write(p)
	w.written += int64(n)
	return n, err
}

func (w *pipeResponse) Flush() {}
