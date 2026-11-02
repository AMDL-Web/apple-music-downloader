package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppleLibraryCallsTheSpecRoutes(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) // a failed read shows up as a wrong body below
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apple-library/sync":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"available":true,"state":"running","current":{"id":"als_1","mode":"incremental"},"counts":{}}`))
		case "/api/v1/apple-library/status":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":"apple_library_unavailable","message":"no token","details":{"reason":"no_token"}}}`))
		case "/api/v1/apple-library/playlists/p.1/tracks":
			_, _ = w.Write([]byte(`{"playlist_id":"p.1","items":[{"position":0,"song_id":"i.1","name":"A","url":"https://music.apple.com/cn/song/1"}],"total":1}`))
		default:
			_, _ = w.Write([]byte(`{"kind":"albums","items":[{"id":"l.1","catalog_id":"9","url":"https://music.apple.com/cn/album/x/9","name":"X"}],"total":1,"offset":0,"synced_at":"2026-11-02T08:30:00Z"}`))
		}
	}))
	defer srv.Close()
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	page, err := c.AppleLibrary(ctx, "albums", AppleLibraryQuery{Sort: "name", Q: "x y", Limit: 100, Offset: 100})
	if err != nil || page.Items[0].URL != "https://music.apple.com/cn/album/x/9" || page.SyncedAt == nil {
		t.Fatalf("page = %+v, %v", page, err)
	}
	tracks, err := c.AppleLibraryPlaylistTracks(ctx, "p.1")
	if err != nil || tracks.Items[0].SongID != "i.1" || tracks.ReadAt != nil {
		t.Fatalf("tracks = %+v, %v", tracks, err)
	}
	st, err := c.SyncAppleLibrary(ctx, "incremental")
	if err != nil || st.Current == nil || st.Current.ID != "als_1" {
		t.Fatalf("sync = %+v, %v", st, err)
	}
	_, err = c.AppleLibraryStatus(ctx)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "apple_library_unavailable" {
		t.Fatalf("status err = %v", err)
	}
	want := []string{
		"GET /api/v1/apple-library/albums?limit=100&offset=100&q=x+y&sort=name ",
		"GET /api/v1/apple-library/playlists/p.1/tracks ",
		`POST /api/v1/apple-library/sync {"mode":"incremental"}`,
		"GET /api/v1/apple-library/status ",
	}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("requests = %q, want %q", got, want)
		}
	}
}
