package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"amdl/internal/db"
	"amdl/internal/usertoken"
	"amdl/internal/wrapper"
)

func TestAccountRoutes(t *testing.T) {
	fake := &fakeWrapperService{statusResult: wrapper.Status{Ready: true, Listed: true, LoginSupported: true,
		Instances: []wrapper.Instance{{ID: "us-id", Region: "us", Name: "u@icloud.com"}, {ID: "jp-id", Region: "jp", Name: "j@icloud.com"}}}}
	tokens := usertoken.NewReading(func(id string) (string, error) { return strings.Repeat("A", 96) + id[:2] + "zz", nil })
	sync := &fakeLibrarySync{}
	server := newServer(t, Deps{Wrapper: fake, Tokens: tokens, LibrarySync: sync})
	if _, err := server.store.SyncAccounts(context.Background(), db.AccountDiscovery{
		Seen: []db.SeenAccount{{ID: "us-id", Name: "u@icloud.com", Region: "us"}, {ID: "jp-id", Name: "j@icloud.com", Region: "jp"}}, Complete: true,
	}); err != nil {
		t.Fatal(err)
	}
	handler := server.Routes()

	got := decodeBody(t, requestJSON(t, handler, http.MethodGet, "/api/v1/accounts", "").Body.Bytes())
	list := got["accounts"].([]any)
	first, second := list[0].(map[string]any), list[1].(map[string]any)
	if len(list) != 2 || first["id"] != "us-id" || first["default"] != true || first["name"] != "u@icloud.com" ||
		first["media_user_token"].(map[string]any)["source"] != "wrapper" || second["default"] != false || second["state"] != "active" {
		t.Fatalf("accounts = %v", got)
	}
	if strings.Contains(requestJSON(t, handler, http.MethodGet, "/api/v1/accounts", "").Body.String(), strings.Repeat("A", 16)) {
		t.Fatal("the account list carries a token")
	}

	recorder := requestJSON(t, handler, http.MethodPatch, "/api/v1/accounts/jp-id", `{"library_sync":true,"apple_library":true,"position":0}`)
	got = decodeBody(t, recorder.Body.Bytes())
	if recorder.Code != http.StatusOK || got["default"] != true || got["library_sync"] != true || got["apple_library"] != true {
		t.Fatalf("patch = %d %v", recorder.Code, got)
	}
	assertErrorCode(t, requestJSON(t, handler, http.MethodPatch, "/api/v1/accounts/jp-id", `{}`), http.StatusBadRequest, CodeInvalidRequest)
	assertErrorCode(t, requestJSON(t, handler, http.MethodPatch, "/api/v1/accounts/jp-id", `{"position":-1}`), http.StatusBadRequest, CodeInvalidRequest)
	assertErrorCode(t, requestJSON(t, handler, http.MethodPatch, "/api/v1/accounts/nope", `{"library_sync":true}`), http.StatusNotFound, CodeAccountNotFound)

	recorder = requestJSON(t, handler, http.MethodDelete, "/api/v1/accounts/us-id", "")
	got = decodeBody(t, recorder.Body.Bytes())
	if recorder.Code != http.StatusOK || got["deleted"] != "us-id" || got["logged_out"] != true {
		t.Fatalf("delete = %d %v", recorder.Code, got)
	}
	if len(fake.loggedOutIDs) != 1 || fake.loggedOutIDs[0] != "us-id" || len(sync.forgotten) != 1 {
		t.Fatalf("logged out %v, forgotten %v", fake.loggedOutIDs, sync.forgotten)
	}
	assertErrorCode(t, requestJSON(t, handler, http.MethodDelete, "/api/v1/accounts/us-id", ""), http.StatusNotFound, CodeAccountNotFound)

	// Logout by account id.
	recorder = requestJSON(t, handler, http.MethodPost, "/api/v1/wrapper/logout", `{"account_id":"jp-id"}`)
	if got := decodeBody(t, recorder.Body.Bytes()); recorder.Code != http.StatusOK || got["account_id"] != "jp-id" {
		t.Fatalf("logout = %d %v", recorder.Code, got)
	}
	assertErrorCode(t, requestJSON(t, handler, http.MethodPost, "/api/v1/wrapper/logout", `{"account_id":"a","username":"b"}`), http.StatusBadRequest, CodeInvalidRequest)

	fake.startErr = wrapper.ErrAccountLimit
	assertErrorCode(t, requestJSON(t, handler, http.MethodPost, "/api/v1/wrapper/login", `{"username":"x","password":"y"}`), http.StatusConflict, CodeAccountLimit)
}

func TestAccountTokenRoutes(t *testing.T) {
	wrapperTok, given := strings.Repeat("W", 100), strings.Repeat("G", 100)
	// The wrapper has a token for us-id and none for jp-id.
	tokens := usertoken.NewReading(func(id string) (string, error) {
		if id == "us-id" {
			return wrapperTok, nil
		}
		return "", nil
	})
	dir := t.TempDir()
	if err := tokens.LoadManual(dir); err != nil {
		t.Fatal(err)
	}
	server := newServer(t, Deps{Wrapper: &fakeWrapperService{}, Tokens: tokens, LibrarySync: &fakeLibrarySync{}})
	if _, err := server.store.SyncAccounts(context.Background(), db.AccountDiscovery{
		Seen: []db.SeenAccount{{ID: "us-id", Region: "us"}, {ID: "jp-id", Region: "jp"}}, Complete: true,
	}); err != nil {
		t.Fatal(err)
	}
	handler := server.Routes()
	source := func(rec map[string]any) string { return rec["media_user_token"].(map[string]any)["source"].(string) }

	// Fallback fills in for the wrapper and loses to it.
	for _, id := range []string{"us-id", "jp-id"} {
		rec := requestJSON(t, handler, http.MethodPut, "/api/v1/accounts/"+id+"/media-user-token", `{"token":" `+given+` "}`)
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), given) {
			t.Fatalf("put %s = %d %s", id, rec.Code, rec.Body)
		}
	}
	cfg := server.currentConfig()
	if got := tokens.Account(cfg, "us-id"); got.Value != wrapperTok || got.Source != usertoken.SourceWrapper {
		t.Fatalf("us-id fallback = %q", got.Source)
	}
	if got := tokens.Account(cfg, "jp-id"); got.Value != given || got.Source != usertoken.SourceManual {
		t.Fatalf("jp-id fallback = %q", got.Source)
	}

	// Override wins over the wrapper.
	got := decodeBody(t, requestJSON(t, handler, http.MethodPut, "/api/v1/accounts/us-id/media-user-token", `{"token":"`+given+`","mode":"override"}`).Body.Bytes())
	if source(got) != "manual" || got["media_user_token"].(map[string]any)["manual"].(map[string]any)["mode"] != "override" {
		t.Fatalf("override = %v", got)
	}
	tokens.NoteRejected(given)
	if m, _ := tokens.ManualFor("us-id"); !m.Rejected {
		t.Fatal("a refused given token is not marked")
	}

	// A restart loads what was given.
	reloaded := usertoken.New()
	if err := reloaded.LoadManual(dir); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Account(cfg, "us-id"); got.Value != given || got.Source != usertoken.SourceManual {
		t.Fatalf("reloaded = %q", got.Source)
	}

	got = decodeBody(t, requestJSON(t, handler, http.MethodDelete, "/api/v1/accounts/us-id/media-user-token", "").Body.Bytes())
	if source(got) != "wrapper" || got["media_user_token"].(map[string]any)["manual"] != nil {
		t.Fatalf("delete = %v", got)
	}
	got = decodeBody(t, requestJSON(t, handler, http.MethodDelete, "/api/v1/accounts/jp-id/media-user-token", "").Body.Bytes())
	if source(got) != "none" {
		t.Fatalf("delete jp = %v", got)
	}

	assertErrorCode(t, requestJSON(t, handler, http.MethodPut, "/api/v1/accounts/jp-id/media-user-token", `{"token":"short"}`), http.StatusBadRequest, CodeInvalidRequest)
	assertErrorCode(t, requestJSON(t, handler, http.MethodPut, "/api/v1/accounts/jp-id/media-user-token", `{"token":"`+given+`","mode":"x"}`), http.StatusBadRequest, CodeInvalidRequest)
	assertErrorCode(t, requestJSON(t, handler, http.MethodPut, "/api/v1/accounts/nope/media-user-token", `{"token":"`+given+`"}`), http.StatusNotFound, CodeAccountNotFound)
	// The implicit account takes a token before the wrapper lists it.
	if rec := requestJSON(t, handler, http.MethodPut, "/api/v1/accounts/wrapper/media-user-token", `{"token":"`+given+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("put wrapper = %d %s", rec.Code, rec.Body)
	}
	got = decodeBody(t, requestJSON(t, handler, http.MethodGet, "/api/v1/accounts/wrapper/media-user-token", "").Body.Bytes())
	if got["source"] != "manual" || got["manual"].(map[string]any)["mode"] != "fallback" {
		t.Fatalf("get wrapper = %v", got)
	}
	assertErrorCode(t, requestJSON(t, handler, http.MethodGet, "/api/v1/accounts/nope/media-user-token", ""), http.StatusNotFound, CodeAccountNotFound)
}
