package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"amdl/internal/db"
	"amdl/internal/librarysync"
)

func TestLibrarySyncStatusReportsWatcherState(t *testing.T) {
	watcher := &fakeLibrarySync{status: librarysync.Status{
		Enabled: true, IntervalMinutes: 15,
		Accounts: []librarysync.AccountStatus{{AccountID: "a1", Region: "jp", Enabled: true, AnchorSize: 50, TotalSubmitted: 3}},
	}}
	server := newServer(t, Deps{LibrarySync: watcher})
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/library-sync", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var got librarysync.Status
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Enabled || got.IntervalMinutes != 15 || len(got.Accounts) != 1 || got.Accounts[0].AnchorSize != 50 || got.Accounts[0].TotalSubmitted != 3 {
		t.Fatalf("status = %+v, want the watcher's own values", got)
	}
}

// Reset reports how much it forgot and hands back the post-reset status, so a
// client does not need a second request to refresh its view. It names an
// account, else resets the default's; an unknown account is not found.
func TestLibrarySyncResetClearsAnchor(t *testing.T) {
	watcher := &fakeLibrarySync{
		status:  librarysync.Status{Enabled: true, IntervalMinutes: 15, Accounts: []librarysync.AccountStatus{{AccountID: "jp", AnchorSize: 50}}},
		cleared: 50,
	}
	server := newServer(t, Deps{LibrarySync: watcher})
	if _, err := server.store.SyncAccounts(context.Background(), db.AccountDiscovery{
		Seen: []db.SeenAccount{{ID: "us", Region: "us"}, {ID: "jp", Region: "jp"}}, Complete: true,
	}); err != nil {
		t.Fatal(err)
	}
	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/library-sync/reset", `{"account":"jp"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var got struct {
		Account string             `json:"account"`
		Cleared int64              `json:"cleared"`
		Status  librarysync.Status `json:"status"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Cleared != 50 || got.Account != "jp" {
		t.Fatalf("reset = %+v, want 50 cleared for jp", got)
	}
	if got.Status.Accounts[0].AnchorSize != 0 {
		t.Fatalf("post-reset anchor_size = %d, want 0", got.Status.Accounts[0].AnchorSize)
	}
	if len(watcher.resetCalls) != 1 || watcher.resetCalls[0] != "jp" {
		t.Fatalf("reset calls = %v, want [jp]", watcher.resetCalls)
	}
	assertErrorCode(t, requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/library-sync/reset", `{"account":"kr"}`),
		http.StatusNotFound, CodeAccountNotFound)
	if recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/library-sync/reset", ""); recorder.Code != http.StatusOK {
		t.Fatalf("reset without a body = %d %s", recorder.Code, recorder.Body)
	}
}
