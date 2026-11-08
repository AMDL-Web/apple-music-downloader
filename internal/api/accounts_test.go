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
		first["media_user_token"] != nil || second["default"] != false || second["state"] != "active" {
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
