package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const accountsBody = `{"accounts":[
 {"id":"a1","name":"a@icloud.com","region":"us","position":0,"default":true,"library_sync":true,"apple_library":true,"state":"active","added_at":"2026-11-02T08:30:00Z"},
 {"id":"b2","name":"b@icloud.com","region":"jp","position":1,"default":false,"library_sync":false,"apple_library":false,"state":"active","added_at":"2026-11-02T08:30:00Z"},
 {"id":"c3","name":"c@icloud.com","region":"kr","position":2,"default":false,"library_sync":false,"apple_library":false,"state":"active","added_at":"2026-11-02T08:30:00Z"},
 {"id":"d4","name":"d@icloud.com","region":"kr","position":3,"default":false,"library_sync":false,"apple_library":false,"state":"active","added_at":"2026-11-02T08:30:00Z"}]}`

// recordingServer answers GET /accounts with accountsBody, which commands
// read to turn a region or an Apple ID into an account id, and every other
// request with answer (default {}), and records those other requests as
// "METHOD /path?query body", the query sorted.
func recordingServer(t *testing.T, answer string) (*httptest.Server, func() []string) {
	t.Helper()
	if answer == "" {
		answer = `{}`
	}
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts" {
			_, _ = w.Write([]byte(accountsBody))
			return
		}
		line := r.Method + " " + r.URL.Path
		if q := r.URL.Query().Encode(); q != "" {
			line += "?" + q
		}
		mu.Lock()
		seen = append(seen, strings.TrimSpace(line+" "+string(body)))
		mu.Unlock()
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// Each command's arguments and flags become one HTTP request (or a few),
// and a refused command sends none. The MCP tools build theirs from the
// same capability table (internal/mcpserver's route tests).
func TestCommandsSendTheirRequests(t *testing.T) {
	const pending = `{"accepted":1,"rejected":0,"results":[{"url":"https://music.apple.com/us/artist/x/1","status":"pending"}]}`
	for _, c := range []struct {
		args   []string
		answer string
		want   []string
	}{
		{args: []string{"search", "igor tyler", "--types", "albums", "--limit", "3"},
			want: []string{"GET /api/v1/search?limit=3&term=igor+tyler&types=albums"}},
		// --storefront reaches the catalog routes; without it the default
		// stays the backend's.
		{args: []string{"search", "yorushika", "--storefront", "jp"},
			want: []string{"GET /api/v1/search?storefront=jp&term=yorushika"}},
		{args: []string{"search", "yorushika"},
			want: []string{"GET /api/v1/search?term=yorushika"}},
		{args: []string{"artists", "albums", "7", "--storefront", "jp"},
			want: []string{"GET /api/v1/artists/7/albums?storefront=jp"}},
		{args: []string{"albums", "get", "1783644603", "--storefront", "cn"},
			want: []string{"GET /api/v1/albums/1783644603?storefront=cn"}},
		{args: []string{"jobs", "list", "--status", "failed"},
			want: []string{"GET /api/v1/downloads?status=failed"}},
		{args: []string{"jobs", "retry", "job_x"},
			want: []string{"POST /api/v1/downloads/job_x/retry"}},
		{args: []string{"jobs", "delete", "job_x", "--yes"},
			want: []string{"DELETE /api/v1/downloads/job_x?record=true"}},
		{args: []string{"download", "https://music.apple.com/us/artist/x/1", "--watch", "--progress-interval", "20s"}, answer: pending,
			want: []string{`POST /api/v1/downloads {"urls":["https://music.apple.com/us/artist/x/1"]}`}},
		{args: []string{"library", "check", "--kind", "albums", "--id", "1"},
			want: []string{`POST /api/v1/library/check {"id":"1","kind":"albums"}`}},
		// An account named by region, Apple ID or id reaches the route as
		// its id.
		{args: []string{"accounts", "set", "JP", "--library-sync", "on", "--apple-library", "off"},
			want: []string{`PATCH /api/v1/accounts/b2 {"apple_library":false,"library_sync":true}`}},
		{args: []string{"accounts", "set", "b@icloud.com", "--default"},
			want: []string{`PATCH /api/v1/accounts/b2 {"position":0}`}},
		{args: []string{"accounts", "delete", "jp", "--yes"},
			want: []string{"DELETE /api/v1/accounts/b2"}},
		{args: []string{"apple-library", "list", "albums", "--account", "jp"},
			want: []string{"GET /api/v1/apple-library/albums?account=b2"}},
		{args: []string{"apple-library", "tracks", "p.1", "--account", "b2"},
			want: []string{"GET /api/v1/apple-library/playlists/p.1/tracks?account=b2"}},
		{args: []string{"apple-library", "sync", "--account", "us", "--mode", "incremental"},
			want: []string{`POST /api/v1/apple-library/sync {"account":"a1","mode":"incremental"}`}},
		{args: []string{"library", "sync", "reset", "--account", "jp", "--yes"},
			want: []string{`POST /api/v1/library-sync/reset {"account":"b2"}`}},
		{args: []string{"library", "sync", "reset", "--yes"},
			want: []string{`POST /api/v1/library-sync/reset {}`}},
		{args: []string{"wrapper", "logout", "--account-id", "jp", "--yes"},
			want: []string{`POST /api/v1/wrapper/logout {"account_id":"b2"}`}},
		// Refused before anything is sent.
		{args: []string{"accounts", "set", "jp"}},
		{args: []string{"accounts", "set", "jp", "--library-sync", "maybe"}},
		{args: []string{"accounts", "set"}},
		{args: []string{"accounts", "set", "jp", "--default", "--position", "2"}},
		{args: []string{"accounts", "delete", "kr", "--yes"}},
		{args: []string{"accounts", "delete", "fr", "--yes"}},
		{args: []string{"accounts", "delete", "jp"}},
		{args: []string{"jobs", "delete", "job_x"}},
		{args: []string{"jobs", "delete", "job_x", "--dry-run"}},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			srv, seen := recordingServer(t, c.answer)
			r := run(t, srv, "", c.args...)
			if refused := len(c.want) == 0 && !strings.Contains(strings.Join(c.args, " "), "--dry-run"); refused == (r.code == 0) {
				t.Errorf("exit %d, stderr %s", r.code, r.stderr)
			}
			if got := seen(); strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("requests\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(c.want, "\n"))
			}
		})
	}
}
