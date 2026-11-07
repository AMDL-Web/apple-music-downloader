package main

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(t *testing.T, srv *httptest.Server, world, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: cookie, Value: world})
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func step(t *testing.T, srv *httptest.Server, world, ms string) {
	t.Helper()
	resp, err := srv.Client().Post(srv.URL+"/e2e/step?world="+world+"&ms="+ms, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("step: status %d", resp.StatusCode)
	}
}

func TestArtworkPointsAtTheServer(t *testing.T) {
	srv := httptest.NewServer(newHandler())
	// A cleanup, not a defer: Close waits for open streams, which the
	// response bodies' cleanups (run first) end.
	t.Cleanup(srv.Close)
	body, err := io.ReadAll(get(t, srv, "a", "/api/v1/downloads").Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"artwork_url":"/art/`) || strings.Contains(string(body), "http://demo/") {
		t.Fatalf("artwork not rewritten: %.300s", body)
	}
	if resp := get(t, srv, "a", "/art/920001/64x64bb.webp"); resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("cover: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

func TestFeedEventsAreNamedAndWorldsAreApart(t *testing.T) {
	srv := httptest.NewServer(newHandler())
	// A cleanup, not a defer: Close waits for open streams, which the
	// response bodies' cleanups (run first) end.
	t.Cleanup(srv.Close)
	feed := bufio.NewReader(get(t, srv, "a", "/api/v1/downloads/events").Body)
	step(t, srv, "b", "600000") // another world: its jobs finish, a's stay
	step(t, srv, "a", "3000")
	var event []string
	for {
		line, err := feed.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\n" {
			break
		}
		event = append(event, strings.TrimSuffix(line, "\n"))
	}
	if len(event) != 3 || !strings.HasPrefix(event[0], "id: ") || event[1] != "event: download_upserted" || !strings.HasPrefix(event[2], "data: {") {
		t.Fatalf("first feed event = %q", event)
	}

	body, err := io.ReadAll(get(t, srv, "a", "/api/v1/downloads?status=running").Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"status":"running"`) {
		t.Fatalf("world a moved with world b: %.300s", body)
	}
}
