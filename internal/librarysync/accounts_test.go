package librarysync

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/usertoken"
)

type fakeAccounts []domain.Account

func (f fakeAccounts) List(context.Context) ([]domain.Account, error) { return f, nil }

// perAccountStore keeps one fakeStore per account.
type perAccountStore map[string]*fakeStore

func (s perAccountStore) get(id string) *fakeStore {
	if s[id] == nil {
		s[id] = &fakeStore{}
	}
	return s[id]
}

func (s perAccountStore) LoadLibrarySyncAnchor(ctx context.Context, id string) ([]domain.LibraryAnchorEntry, error) {
	return s.get(id).LoadLibrarySyncAnchor(ctx, id)
}

func (s perAccountStore) ReplaceLibrarySyncAnchor(ctx context.Context, id string, entries []domain.LibraryAnchorEntry) error {
	return s.get(id).ReplaceLibrarySyncAnchor(ctx, id, entries)
}

func (s perAccountStore) ClearLibrarySyncAnchor(ctx context.Context, id string) (int64, error) {
	return s.get(id).ClearLibrarySyncAnchor(ctx, id)
}

// Only an active account with its switch on is polled, with its own token
// and anchor, and its albums are pinned to the storefront its library links
// name; the status lists every account.
func TestEachEnabledAccountSyncsWithItsOwnTokenAndPin(t *testing.T) {
	songs, catalog := library(anchorSize+2, nil)
	for album, url := range catalog {
		catalog[album] = strings.Replace(url, "/cn/", "/jp/", 1)
	}
	lib := &fakeLibrary{songs: songs, catalog: catalog}
	store := perAccountStore{"jp": {anchor: toEntries(songs[2 : 2+anchorSize])}}
	jobs := &fakeSubmitter{}
	accounts := fakeAccounts{
		{ID: "us", Region: "us", Position: 0, LibrarySync: false, State: domain.AccountActive},
		{ID: "jp", Region: "jp", Position: 1, LibrarySync: true, State: domain.AccountActive},
		{ID: "kr", Region: "kr", Position: 2, LibrarySync: true, State: domain.AccountMissing},
	}
	tokens := usertoken.NewReading(func(id string) (string, error) { return strings.Repeat(strings.ToUpper(id[:1]), 100), nil })
	tokens.SetAccounts([]usertoken.Account{{ID: "us", Region: "us", Active: true}, {ID: "jp", Region: "jp", Position: 1, Active: true}})
	cfg := config.Default()
	cfg.LibrarySync.Enabled = true
	w := New(config.NewStore(cfg), tokens, accounts, lib, store, jobs, slog.New(slog.NewTextHandler(io.Discard, nil)))

	w.tick(context.Background())
	if lib.lastToken != strings.Repeat("J", 100) {
		t.Fatalf("the pass read with %q…, want the jp account's token", lib.lastToken[:4])
	}
	if len(jobs.batches) != 1 || len(jobs.batches[0]) != 2 || jobs.storefronts[0] != "jp" {
		t.Fatalf("batches = %v storefronts = %v, want two jp albums", jobs.batches, jobs.storefronts)
	}
	if store["us"] != nil || store["kr"] != nil {
		t.Fatal("an account with its switch off, or missing, was read")
	}
	st, err := w.Status(context.Background())
	if err != nil || len(st.Accounts) != 3 {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if jp := st.Accounts[1]; jp.AccountID != "jp" || !jp.Enabled || jp.LastSubmitted != 2 || jp.Region != "jp" {
		t.Fatalf("jp status = %+v", jp)
	}
	if us := st.Accounts[0]; us.Enabled || us.LastPollAt != nil {
		t.Fatalf("us status = %+v", us)
	}
}
