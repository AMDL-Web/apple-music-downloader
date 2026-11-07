package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestAppleLibraryCallsTheSpecRoutes(t *testing.T) {
	var got []string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) // a failed read shows up as a wrong body below
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apple-library/sync":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"account_id":"a1","name":"a@icloud.com","region":"jp","default":false,"enabled":true,"available":true,"state":"running","current":{"id":"als_1","mode":"incremental","trigger":"manual","started_at":"2026-11-02T08:30:00Z","kind":"albums","pages_read":1,"requests":2},"counts":{"songs":0,"albums":1,"playlists":0,"artists":0}}`))
		case "/api/v1/apple-library/status":
			_, _ = w.Write([]byte(`{"sync_interval_minutes":30,"full_sync_hours":24,"accounts":[{"account_id":"a1","name":"a@icloud.com","region":"jp","default":true,"enabled":true,"available":false,"reason":"disabled","state":"idle","counts":{"songs":0,"albums":0,"playlists":0,"artists":0}}]}`))
		case "/api/v1/apple-library/playlists/p.1/tracks":
			_, _ = w.Write([]byte(`{"playlist_id":"p.1","items":[{"position":0,"song_id":"i.1","name":"A","url":"https://music.apple.com/cn/song/1"}],"total":1}`))
		default:
			if r.URL.Query().Get("account") == "nobody" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"code":"account_not_found","message":"no account has this id"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"kind":"albums","items":[{"id":"l.1","catalog_id":"9","url":"https://music.apple.com/cn/album/x/9","name":"X"}],"total":1,"offset":0,"synced_at":"2026-11-02T08:30:00Z"}`))
		}
	}))
	ctx := context.Background()

	page, err := c.AppleLibrary(ctx, "albums", AppleLibraryQuery{Sort: "name", Q: "x y", Limit: 100, Offset: 100, Account: "a1"})
	if err != nil || page.Items[0].URL != "https://music.apple.com/cn/album/x/9" || page.SyncedAt == nil {
		t.Fatalf("page = %+v, %v", page, err)
	}
	tracks, err := c.AppleLibraryPlaylistTracks(ctx, "p.1", "a1")
	if err != nil || tracks.Items[0].SongID != "i.1" || tracks.ReadAt != nil {
		t.Fatalf("tracks = %+v, %v", tracks, err)
	}
	st, err := c.SyncAppleLibrary(ctx, "incremental", "a1")
	if err != nil || st.Current == nil || st.Current.ID != "als_1" || st.AccountID != "a1" || st.Region != "jp" {
		t.Fatalf("sync = %+v, %v", st, err)
	}
	status, err := c.AppleLibraryStatus(ctx)
	if err != nil || status.SyncIntervalMinutes != 30 || len(status.Accounts) != 1 || status.Accounts[0].Reason != "disabled" || !status.Accounts[0].Default {
		t.Fatalf("status = %+v, %v", status, err)
	}
	if _, err = c.SyncAppleLibrary(ctx, "", ""); err != nil {
		t.Fatal(err)
	}
	_, err = c.AppleLibrary(ctx, "songs", AppleLibraryQuery{Account: "nobody"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "account_not_found" {
		t.Fatalf("unknown account err = %v", err)
	}
	want := []string{
		"GET /api/v1/apple-library/albums?account=a1&limit=100&offset=100&q=x+y&sort=name ",
		"GET /api/v1/apple-library/playlists/p.1/tracks?account=a1 ",
		`POST /api/v1/apple-library/sync {"account":"a1","mode":"incremental"}`,
		"GET /api/v1/apple-library/status ",
		"POST /api/v1/apple-library/sync ",
		"GET /api/v1/apple-library/songs?account=nobody ",
	}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("requests = %q, want %q", got, want)
		}
	}
}
