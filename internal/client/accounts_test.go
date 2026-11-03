package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testAccounts = []Account{
	{ID: "a1", Name: "a@icloud.com", Region: "us", Position: 0, Default: true, State: AccountActive},
	{ID: "b2", Name: "b@icloud.com", Region: "jp", Position: 1, State: AccountActive},
	{ID: "c3", Name: "c@icloud.com", Region: "jp", Position: 2, State: AccountActive},
	{ID: "d4", Name: "d@icloud.com", Region: "cn", Position: 3, State: AccountMissing},
	{ID: "e5", Name: "e@icloud.com", Region: "de", Position: 4, State: AccountActive},
}

func TestFindAccount(t *testing.T) {
	for _, c := range []struct{ ref, want, code string }{
		{"", "a1", ""},
		{"c3", "c3", ""},
		{"E@iCloud.com", "e5", ""},
		{"DE", "e5", ""},
		{"us", "a1", ""},
		{"jp", "", "invalid_request"},
		{"cn", "", "account_not_found"},
		{"fr", "", "account_not_found"},
	} {
		a, err := FindAccount(testAccounts, c.ref)
		var api *APIError
		switch {
		case c.code == "" && (err != nil || a.ID != c.want):
			t.Errorf("FindAccount(%q) = %q, %v; want %s", c.ref, a.ID, err, c.want)
		case c.code != "" && (!errors.As(err, &api) || api.Code != c.code):
			t.Errorf("FindAccount(%q) = %v; want %s", c.ref, err, c.code)
		}
	}
	_, err := FindAccount(testAccounts, "jp")
	if msg := err.Error(); !strings.Contains(msg, "b2 (jp)") || !strings.Contains(msg, "c3 (jp)") || strings.Contains(msg, "a1") {
		t.Errorf("an ambiguous region should list the ids of that region: %q", msg)
	}
	if _, err := FindAccount(nil, ""); err == nil {
		t.Error("no account has no default")
	}
}

func TestAccountCallsTheSpecRoutes(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts":
			_, _ = w.Write([]byte(`{"accounts":[{"id":"a1","name":"a@icloud.com","region":"us","position":0,"default":true,"library_sync":true,"apple_library":false,"state":"active","added_at":"2026-11-02T08:30:00Z","media_user_token":"...a1b2"}]}`))
		case r.Method == http.MethodPatch:
			_, _ = w.Write([]byte(`{"id":"a1","name":"","region":"us","position":0,"default":true,"library_sync":false,"apple_library":true,"state":"active","added_at":"2026-11-02T08:30:00Z","media_user_token":""}`))
		case r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`{"deleted":"a/1","logged_out":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"account_not_found","message":"no account has this id"}}`))
		}
	}))
	defer srv.Close()
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	list, err := c.ListAccounts(ctx)
	if err != nil || len(list.Accounts) != 1 || !list.Accounts[0].LibrarySync || list.Accounts[0].MediaUserToken != "...a1b2" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	off, on, first := false, true, 0
	a, err := c.UpdateAccount(ctx, "a1", AccountPatch{LibrarySync: &off, AppleLibrary: &on, Position: &first})
	if err != nil || !a.AppleLibrary || a.LibrarySync {
		t.Fatalf("update = %+v, %v", a, err)
	}
	if _, err := c.UpdateAccount(ctx, "a1", AccountPatch{Position: &first}); err != nil {
		t.Fatal(err)
	}
	d, err := c.DeleteAccount(ctx, "a/1")
	if err != nil || !d.LoggedOut || d.Deleted != "a/1" {
		t.Fatalf("delete = %+v, %v", d, err)
	}
	reset, err := c.ResetLibrarySync(ctx, "")
	var api *APIError
	if !errors.As(err, &api) || api.Code != "account_not_found" {
		t.Fatalf("reset = %+v, %v", reset, err)
	}
	want := []string{
		"GET /api/v1/accounts ",
		`PATCH /api/v1/accounts/a1 {"library_sync":false,"apple_library":true,"position":0}`,
		`PATCH /api/v1/accounts/a1 {"position":0}`,
		"DELETE /api/v1/accounts/a%2F1 ",
		"POST /api/v1/library-sync/reset ",
	}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("requests = %q, want %q", got, want)
		}
	}
	if _, err := c.ResetLibrarySync(ctx, "b2"); err == nil || got[len(got)-1] != `POST /api/v1/library-sync/reset {"account":"b2"}` {
		t.Errorf("reset with an account: %v, last request %q", err, got[len(got)-1])
	}
}

func TestResolveAccountsRewritesOnlyWhatIsThere(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"accounts":[{"id":"a1","name":"a@icloud.com","region":"us","state":"active"},{"id":"b2","name":"b@icloud.com","region":"jp","state":"active"}]}`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL)
	ctx := context.Background()
	v := map[string]any{"kind": "albums", "limit": 5}
	if err := c.ResolveAccounts(ctx, v); err != nil || calls != 0 {
		t.Fatalf("no reference: %v, %d calls", err, calls)
	}
	v = map[string]any{"account": "JP"}
	if err := c.ResolveAccounts(ctx, v); err != nil || v["account"] != "b2" || calls != 1 {
		t.Fatalf("region: %v, %v, %d calls", err, v, calls)
	}
	v = map[string]any{"account_id": "nowhere"}
	if err := c.ResolveAccounts(ctx, v); err == nil {
		t.Fatal("an unknown reference should fail")
	}
}
