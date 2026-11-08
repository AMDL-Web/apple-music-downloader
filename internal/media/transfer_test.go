package media

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

// trickleServer sends payload in small pieces with a pause between them, so the
// whole transfer takes far longer than one pause.
func trickleServer(t *testing.T, payload []byte, piece int, pause time.Duration) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Header().Set("ETag", `"trickle"`)
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for start := 0; start < len(payload); start += piece {
			end := min(start+piece, len(payload))
			if _, err := w.Write(payload[start:end]); err != nil {
				return
			}
			flusher.Flush()
			select {
			case <-time.After(pause):
			case <-r.Context().Done():
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// stallServer sends the first half of payload and then nothing until the
// client gives up.
func stallServer(t *testing.T, payload []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload[:len(payload)/2])
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// A slow transfer that keeps delivering bytes must finish however long it
// takes in total: only a gap longer than the stall timeout ends it.
func TestMediaTransferSurvivesSlowSteadyLink(t *testing.T) {
	payload := bytes.Repeat([]byte("slow-hi-res-track"), 400) // 6800 bytes
	const stall = 150 * time.Millisecond
	// 17 pieces 40 ms apart: about 0.7 s in total, several stall windows.
	server := trickleServer(t, payload, 400, 40*time.Millisecond)

	t.Run("memory", func(t *testing.T) {
		start := time.Now()
		got, err := downloadToMemory(context.Background(), server.Client(), server.URL, stall, nil)
		if err != nil {
			t.Fatalf("slow transfer failed: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("downloaded %d bytes, want %d", len(got), len(payload))
		}
		if elapsed := time.Since(start); elapsed < 3*stall {
			t.Fatalf("transfer took %v, want it to outlast several stall windows", elapsed)
		}
	})
	t.Run("checkpoint", func(t *testing.T) {
		path, err := downloadToCheckpoint(context.Background(), server.Client(), server.URL, t.TempDir(), "job-slow", "out.m4a", stall, nil)
		if err != nil {
			t.Fatalf("slow transfer failed: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("checkpoint holds %d bytes, want %d", len(got), len(payload))
		}
	})
}

func TestMediaTransferFailsClearlyWhenTheBodyStalls(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 64*1024)
	const stall = 100 * time.Millisecond
	server := stallServer(t, payload)

	check := func(t *testing.T, err error, elapsed time.Duration) {
		t.Helper()
		if !errors.Is(err, errTransferStalled) {
			t.Fatalf("error = %v, want errTransferStalled", err)
		}
		if !strings.Contains(err.Error(), "stalled: no data for 100ms") {
			t.Fatalf("error = %q, want it to name the stall and its duration", err)
		}
		if elapsed > 3*time.Second {
			t.Fatalf("stall detected after %v, want about %v", elapsed, stall)
		}
	}
	t.Run("memory", func(t *testing.T) {
		start := time.Now()
		_, err := downloadToMemory(context.Background(), server.Client(), server.URL, stall, nil)
		check(t, err, time.Since(start))
	})
	t.Run("checkpoint keeps the partial bytes", func(t *testing.T) {
		dir := t.TempDir()
		start := time.Now()
		_, err := downloadToCheckpoint(context.Background(), server.Client(), server.URL, dir, "job-stall", "out.m4a", stall, nil)
		check(t, err, time.Since(start))
		path, _ := resumableDownloadPaths(dir, "job-stall", "out.m4a")
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() != int64(len(payload)/2) {
			t.Fatalf("checkpoint after stall: size %v, err %v; want the %d bytes received", info, statErr, len(payload)/2)
		}
	})
}

// The media client must not bound the whole request: http.Client.Timeout also
// covers reading the body, which a large track on a slow link outlasts.
func TestMediaHTTPClientBoundsConnectionNotTransfer(t *testing.T) {
	client := newHTTPClient()
	if client.Timeout != 0 {
		t.Fatalf("media client Timeout = %v, want none", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	if transport.TLSHandshakeTimeout <= 0 || transport.ResponseHeaderTimeout <= 0 || transport.DialContext == nil {
		t.Fatalf("transport timeouts: tls=%v header=%v dial set=%v, want all bounded",
			transport.TLSHandshakeTimeout, transport.ResponseHeaderTimeout, transport.DialContext != nil)
	}
}

// Playlists keep a whole-request bound, applied on a copy so the shared media
// client is not changed.
func TestDownloadTextBoundsTheRequestWithoutChangingTheClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n"))
	}))
	defer server.Close()
	client := server.Client()
	if _, err := downloadText(context.Background(), client, server.URL); err != nil {
		t.Fatal(err)
	}
	if client.Timeout != 0 {
		t.Fatalf("downloadText changed the caller's client Timeout to %v", client.Timeout)
	}
}
