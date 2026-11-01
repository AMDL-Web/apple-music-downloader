package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

// A response recorded once through the proxy is replayed with the origin
// gone, whatever the query string, and Range requests get the right bytes.
func TestRecordThenReplayWithoutTheOrigin(t *testing.T) {
	media := bytes.Repeat([]byte("0123456789"), 4096)
	var originHits atomic.Int64
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits.Add(1)
		w.Header().Set("ETag", `"m1"`)
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(media)
	}))
	p, err := newProxy(t.TempDir(), true, 0)
	if err != nil {
		t.Fatal(err)
	}
	p.origin = origin.Client()
	front := httptest.NewServer(p)
	defer front.Close()
	pool := x509.NewCertPool()
	pool.AddCert(p.caCert)
	proxyURL, _ := url.Parse(front.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: pool}}}

	get := func(path, rng string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, origin.URL+path, nil)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp, body
	}

	if _, body := get("/media.mp4?sig=a", ""); !bytes.Equal(body, media) {
		t.Fatalf("recorded body differs: %d bytes", len(body))
	}
	if originHits.Load() != 1 || p.recorded.Load() != 1 {
		t.Fatalf("origin hits %d, recorded %d; want 1 and 1", originHits.Load(), p.recorded.Load())
	}

	p.record = false
	origin.Close()
	if resp, body := get("/media.mp4?sig=b", ""); resp.StatusCode != http.StatusOK || !bytes.Equal(body, media) || resp.Header.Get("ETag") != `"m1"` {
		t.Fatalf("replay: status %d, %d bytes, ETag %q", resp.StatusCode, len(body), resp.Header.Get("ETag"))
	}
	if resp, body := get("/media.mp4", "bytes=100-199"); resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, media[100:200]) {
		t.Fatalf("range replay: status %d, body %q", resp.StatusCode, body)
	}
	if resp, _ := get("/missing.mp4", ""); resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("a miss with the origin gone answered %d, want 502", resp.StatusCode)
	}

	resp, err := http.Get(front.URL + "/__stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var stats struct {
		Hits   int64            `json:"hits"`
		Misses map[string]int64 `json:"misses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if stats.Hits != 2 || len(stats.Misses) != 1 {
		t.Fatalf("stats %+v, want 2 hits and misses on one host", stats)
	}
}
