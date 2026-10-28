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
	"testing"
	"time"
)

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
		got, err := downloadToMemory(context.Background(), server.Client(), server.URL, true, stall, nil)
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
		_, err := downloadToMemory(context.Background(), server.Client(), server.URL, false, stall, nil)
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
