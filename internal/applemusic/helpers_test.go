package applemusic

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/limits"
)

// testWebToken is the scraped web-player JWT newTestClient starts with.
const testWebToken = "cached-token"

// Most catalog tests construct only the fields relevant to the behavior under
// test. Give those fixtures a fast gate explicitly (64 in parallel, 64 per
// second) instead of the production rate; production still treats a
// max_parallel_requests <= 0 as 1.
func newTestCatalogClient(cfg config.CatalogConfig, logger *slog.Logger) *CatalogClient {
	if cfg.MaxParallelRequests <= 0 {
		cfg.MaxParallelRequests = 64
	}
	c := NewCatalogClient(cfg, logger)
	c.gate = limits.NewRequestGate(cfg.MaxParallelRequests, 64, 64)
	return c
}

// newTestClient returns a client whose requests, to whichever Apple host they
// name, reach respond through a real local listener: the client's transport
// sends every request to an httptest server and keeps the original Host. Inside
// respond, req.URL is absolute again (https://<original host>/<path>), so a
// test can assert on the host, path, query and headers Apple would see.
// respond runs on the server's goroutine: report failures with t.Error, not
// t.Fatal. The web token cache starts holding testWebToken for an hour.
func newTestClient(t *testing.T, cfg config.CatalogConfig, respond func(*http.Request) *http.Response) *CatalogClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.URL.Scheme, req.URL.Host = "https", req.Host
		resp := respond(req)
		if resp == nil {
			http.Error(w, "test responder returned no response", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()
		for key, values := range resp.Header {
			w.Header()[key] = values
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body) // the client may hang up early; the test sees that on its side
	}))
	t.Cleanup(srv.Close)

	client := newTestCatalogClient(cfg, slog.Default())
	client.http = &http.Client{Timeout: 30 * time.Second, Transport: toListener(srv.Listener.Addr())}
	client.webToken.value, client.webToken.until = testWebToken, time.Now().Add(time.Hour)
	return client
}

// toListener sends every request to addr over plain HTTP, keeping the
// request's own host in the Host header.
func toListener(addr net.Addr) http.RoundTripper {
	transport := &http.Transport{}
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		out := req.Clone(req.Context())
		out.URL.Scheme, out.URL.Host, out.Host = "http", addr.String(), req.URL.Host
		resp, err := transport.RoundTrip(out)
		if resp != nil {
			resp.Request = req
		}
		return resp, err
	})
}

// routes answers each request path with the body routes maps it to; any other
// path is a 404, so a test fails loudly when the client asks for something
// else.
func routes(bodies map[string]string) func(*http.Request) *http.Response {
	return func(req *http.Request) *http.Response {
		if body, ok := bodies[req.URL.Path]; ok {
			return jsonResponse(http.StatusOK, body)
		}
		return jsonResponse(http.StatusNotFound, "unexpected path: "+req.URL.RequestURI())
	}
}

// fixture returns testdata/name.
func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

// scrapeResponse serves the two music.apple.com requests a web token scrape
// makes, or reports false for any other request.
func scrapeResponse(req *http.Request) (*http.Response, bool) {
	if req.URL.Host != "music.apple.com" {
		return nil, false
	}
	if req.URL.Path == "" || req.URL.Path == "/" {
		return jsonResponse(http.StatusOK, `<script src="/assets/index~abc123.js"></script>`), true
	}
	return jsonResponse(http.StatusOK, `token:"eyJhbGciOiJFUzI1NiJ9.eyJmb28iOiJiYXIifQ.refreshed-sig"`), true
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func trackIDs(tracks []Song) []string {
	out := make([]string, 0, len(tracks))
	for _, track := range tracks {
		out = append(out, track.ID)
	}
	return out
}

func albumIDs(albums []Collection) []string {
	out := make([]string, 0, len(albums))
	for _, album := range albums {
		out = append(out, album.ID)
	}
	return out
}
