package media

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"amdl/internal/limits"
)

func TestReadAllLimitedRejectsOverflow(t *testing.T) {
	if _, err := readAllLimited(strings.NewReader("12345"), 4); err == nil {
		t.Fatal("readAllLimited accepted a response larger than its limit")
	}
}

func TestDownloadBytesRejectsOversizedContentLengthBeforeReading(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt(maxInMemoryMediaBytes+1, 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if _, err := downloadBytes(context.Background(), server.Client(), server.URL, nil); err == nil {
		t.Fatal("downloadBytes accepted an oversized declared response")
	}
}

// AAC-LC leaves retries to its caller: an interrupted body fails the request
// instead of reconnecting with Range.
func TestDownloadBytesDoesNotReconnect(t *testing.T) {
	payload := bytes.Repeat([]byte("aac-lc-memory"), 16*1024)
	cut := 35 * 1024
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Range") != "" {
			t.Errorf("downloadBytes sent Range %q", r.Header.Get("Range"))
		}
		w.Header().Set("ETag", `"strong-but-disabled"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload[:cut])
	}))
	defer server.Close()

	if _, err := downloadBytes(context.Background(), server.Client(), server.URL, nil); err == nil {
		t.Fatal("truncated response succeeded")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want no internal reconnect", got)
	}
}

// A body error after the context is cancelled reports the cancellation, so
// the AAC-LC caller stops instead of retrying a reset connection.
func TestDownloadBytesReportsCancellationOverBodyError(t *testing.T) {
	payload := []byte("partial bytes before cancellation")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var requests atomic.Int32
	var bodyClosed atomic.Bool
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			ContentLength: int64(len(payload) + 10),
			Header:        http.Header{"Content-Length": {strconv.Itoa(len(payload) + 10)}},
			Body:          &cancelOnEOFBody{raw: append([]byte(nil), payload...), cancel: cancel, closed: &bodyClosed},
		}, nil
	})}

	if _, err := downloadBytes(ctx, client, "https://cdn.example.test/cancel", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if requests.Load() != 1 || !bodyClosed.Load() {
		t.Fatalf("requests/closed = %d/%t, want 1/true", requests.Load(), bodyClosed.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// cancelOnEOFBody returns raw, cancels the request's context with the last
// bytes and then fails like a reset connection.
type cancelOnEOFBody struct {
	raw    []byte
	cancel context.CancelFunc
	closed *atomic.Bool
}

func (r *cancelOnEOFBody) Read(p []byte) (int, error) {
	if len(r.raw) == 0 {
		return 0, errors.New("connection reset")
	}
	n := copy(p, r.raw)
	r.raw = r.raw[n:]
	if len(r.raw) == 0 {
		r.cancel()
		return n, errors.New("connection reset")
	}
	return n, nil
}

func (r *cancelOnEOFBody) Close() error {
	r.closed.Store(true)
	return nil
}

func TestManifestRequestRetries429ThroughSharedGate(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "limited", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("#EXTM3U\n"))
	}))
	defer server.Close()

	gate := limits.NewRequestGate(1, 1000, 1000)
	body, err := downloadText(context.Background(), server.Client(), server.URL, gate)
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 || body != "#EXTM3U\n" {
		t.Fatalf("hits/body = %d/%q, want 2/manifest", hits.Load(), body)
	}
}
