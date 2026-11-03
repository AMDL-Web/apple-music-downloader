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

// accountsServer answers the accounts list and records every other request.
func accountsServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts" {
			_, _ = w.Write([]byte(accountsBody))
			return
		}
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"deleted":"b2","logged_out":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestAccountsSetResolvesARegionAndTakesOnOff(t *testing.T) {
	srv, seen := accountsServer(t)
	r := run(t, srv, "", "accounts", "set", "JP", "--library-sync", "on", "--apple-library", "off")
	if r.code != 0 {
		t.Fatalf("got %+v", r)
	}
	r = run(t, srv, "", "accounts", "set", "b@icloud.com", "--default")
	if r.code != 0 {
		t.Fatalf("got %+v", r)
	}
	want := []string{
		`PATCH /api/v1/accounts/b2 {"apple_library":false,"library_sync":true}`,
		`PATCH /api/v1/accounts/b2 {"position":0}`,
	}
	if got := seen(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("requests = %q, want %q", got, want)
	}
}

func TestAccountsSetNeedsSomethingToChange(t *testing.T) {
	srv, seen := accountsServer(t)
	for _, args := range [][]string{
		{"accounts", "set", "jp"},
		{"accounts", "set", "jp", "--library-sync", "maybe"},
		{"accounts", "set"},
		{"accounts", "set", "jp", "--default", "--position", "2"},
	} {
		if r := run(t, srv, "", args...); r.code != exitInput {
			t.Errorf("%v: got %+v", args, r)
		}
	}
	if got := seen(); len(got) != 0 {
		t.Errorf("a refused command sent %q", got)
	}
}

func TestAccountRegionsThatAreAmbiguousOrUnknownAreRefusedWithTheIds(t *testing.T) {
	srv, seen := accountsServer(t)
	r := run(t, srv, "", "accounts", "delete", "kr", "--yes")
	if r.code != exitInput || !strings.Contains(r.stderr, "c3 (kr), d4 (kr)") {
		t.Errorf("ambiguous: got %+v", r)
	}
	r = run(t, srv, "", "accounts", "delete", "fr", "--yes")
	if r.code != exitNotFound || errorCode(t, r.stderr) != "account_not_found" {
		t.Errorf("unknown: got %+v", r)
	}
	if got := seen(); len(got) != 0 {
		t.Errorf("a refused command sent %q", got)
	}
}

func TestAccountsDeleteAsksAndThenUsesTheId(t *testing.T) {
	srv, seen := accountsServer(t)
	if r := run(t, srv, "", "accounts", "delete", "jp"); r.code != exitConfirm {
		t.Errorf("without --yes: got %+v", r)
	}
	if r := run(t, srv, "", "accounts", "delete", "jp", "--yes"); r.code != 0 || !strings.Contains(r.stdout, `"logged_out"`) {
		t.Errorf("got %+v", r)
	}
	if got := seen(); len(got) != 1 || got[0] != "DELETE /api/v1/accounts/b2 " {
		t.Errorf("requests = %q", got)
	}
}

func TestAccountFlagReachesTheAppleLibraryAndSyncResetRoutes(t *testing.T) {
	srv, seen := accountsServer(t)
	for _, args := range [][]string{
		{"apple-library", "list", "albums", "--account", "jp"},
		{"apple-library", "tracks", "p.1", "--account", "b2"},
		{"apple-library", "sync", "--account", "us", "--mode", "incremental"},
		{"library", "sync", "reset", "--account", "jp", "--yes"},
		{"library", "sync", "reset", "--yes"},
		{"wrapper", "logout", "--account-id", "jp", "--yes"},
	} {
		if r := run(t, srv, "", args...); r.code != 0 {
			t.Fatalf("%v: got %+v", args, r)
		}
	}
	want := []string{
		"GET /api/v1/apple-library/albums?account=b2 ",
		"GET /api/v1/apple-library/playlists/p.1/tracks?account=b2 ",
		`POST /api/v1/apple-library/sync {"account":"a1","mode":"incremental"}`,
		`POST /api/v1/library-sync/reset {"account":"b2"}`,
		`POST /api/v1/library-sync/reset {}`,
		`POST /api/v1/wrapper/logout {"account_id":"b2"}`,
	}
	if got := seen(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("requests = %q, want %q", got, want)
	}
}

func TestWrapperLogoutTakesExactlyOneOfTheTwoNames(t *testing.T) {
	srv, _ := accountsServer(t)
	for _, args := range [][]string{
		{"wrapper", "logout", "--yes"},
		{"wrapper", "logout", "--username", "a@icloud.com", "--account-id", "a1", "--yes"},
	} {
		if r := run(t, srv, "", args...); r.code != exitInput {
			t.Errorf("%v: got %+v", args, r)
		}
	}
}
