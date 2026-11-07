package db

import (
	"context"
	"reflect"
	"testing"

	"amdl/internal/domain"
)

// The first discovery hands the rows from before accounts to one account,
// which becomes the default with the single account's switches; later
// accounts start with both off.
func TestSyncAccountsAdoptsUnownedRows(t *testing.T) {
	store, err := Open(create6Database(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	res, err := store.SyncAccounts(ctx, AccountDiscovery{
		Seen:     []SeenAccount{{ID: "jp", Name: "jp@example.com", Region: "JP"}, {ID: "us", Name: "us@example.com", Region: "us"}},
		Adopt:    "us",
		Complete: true, AdoptLibrarySync: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Adopted != "us" || len(res.Added) != 2 {
		t.Fatalf("sync = %+v", res)
	}
	accounts, err := store.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[0].ID != "us" || accounts[0].Position != 0 || !accounts[0].LibrarySync || !accounts[0].AppleLibrary {
		t.Fatalf("default = %+v", accounts)
	}
	if jp := accounts[1]; jp.ID != "jp" || jp.Position != 1 || jp.LibrarySync || jp.AppleLibrary || jp.Region != "jp" || jp.Name != "jp@example.com" {
		t.Fatalf("second = %+v", jp)
	}
	anchor, err := store.LoadLibrarySyncAnchor(ctx, "us")
	if err != nil || len(anchor) != 2 {
		t.Fatalf("us anchor = %v, %v", anchor, err)
	}
	if counts, err := store.AppleLibraryCounts(ctx, "us"); err != nil || counts["albums"] != 1 || counts["playlists"] != 1 {
		t.Fatalf("us counts = %v, %v", counts, err)
	}
	if tracks, _, found, err := store.AppleLibraryPlaylistTracks(ctx, "us", "p.x"); err != nil || !found || len(tracks) != 1 {
		t.Fatalf("us p.x tracks = %v, %v, %v", tracks, found, err)
	}
	if v, err := store.AppleLibraryMeta(ctx, "us", "kind:albums"); err != nil || v == "" {
		t.Fatalf("us meta = %q, %v", v, err)
	}
	for _, table := range accountTables {
		var n int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE account_id = ''`).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s keeps %d unowned rows (%v)", table, n, err)
		}
	}

	// A later discovery that misses one marks it missing and keeps its row;
	// adoption does not run again.
	res, err = store.SyncAccounts(ctx, AccountDiscovery{Seen: []SeenAccount{{ID: "jp", Region: "jp"}, {ID: "kr", Region: "kr"}}, Complete: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Adopted != "" || len(res.Added) != 1 || res.Added[0] != "kr" {
		t.Fatalf("second sync = %+v", res)
	}
	accounts, _ = store.ListAccounts(ctx)
	want := map[string]domain.AccountState{"us": domain.AccountMissing, "jp": domain.AccountActive, "kr": domain.AccountActive}
	for _, a := range accounts {
		if a.State != want[a.ID] {
			t.Errorf("%s state = %s, want %s", a.ID, a.State, want[a.ID])
		}
	}
	if accounts[1].Name != "jp@example.com" {
		t.Errorf("an empty name replaced the stored one: %+v", accounts[1])
	}
	if accounts[2].ID != "kr" || accounts[2].Position != 2 {
		t.Errorf("new account placed at %+v, want position 2", accounts[2])
	}
}

func TestSyncAccountsAdoptsPastAnEmptyID(t *testing.T) {
	store, err := Open(create6Database(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	res, err := store.SyncAccounts(ctx, AccountDiscovery{Seen: []SeenAccount{{ID: ""}, {ID: "acc1"}}, Complete: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Adopted != "acc1" {
		t.Fatalf("sync = %+v, want acc1 to adopt", res)
	}
	accounts, err := store.ListAccounts(ctx)
	if err != nil || len(accounts) != 1 || accounts[0].ID != "acc1" || accounts[0].Position != 0 || !accounts[0].AppleLibrary {
		t.Fatalf("accounts = %+v, %v", accounts, err)
	}
	unowned := func() int {
		total := 0
		for _, table := range accountTables {
			var n int
			if err := store.db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE account_id = ''`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			total += n
		}
		return total
	}
	if n := unowned(); n != 0 {
		t.Fatalf("%d rows left without an account", n)
	}

	// Rows without an account that show up while the table has accounts go
	// to the default account on the next discovery.
	if _, err := store.db.Exec(`INSERT INTO library_sync_anchor(account_id, position, song_id) VALUES('', 99, 'late')`); err != nil {
		t.Fatal(err)
	}
	res, err = store.SyncAccounts(ctx, AccountDiscovery{Seen: []SeenAccount{{ID: "acc1"}}, Complete: true})
	if err != nil || res.Adopted != "acc1" {
		t.Fatalf("second sync = %+v, %v", res, err)
	}
	if n := unowned(); n != 0 {
		t.Fatalf("%d rows left without an account after the second sync", n)
	}
	var owner string
	if err := store.db.QueryRow(`SELECT account_id FROM library_sync_anchor WHERE song_id = 'late'`).Scan(&owner); err != nil || owner != "acc1" {
		t.Fatalf("late row owner = %q, %v", owner, err)
	}
}

func TestUpdateAndDeleteAccount(t *testing.T) {
	store, err := Open(create6Database(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.SyncAccounts(ctx, AccountDiscovery{Seen: []SeenAccount{{ID: "a"}, {ID: "b"}, {ID: "c"}}, Complete: true}); err != nil {
		t.Fatal(err)
	}
	on, top := true, 0
	got, err := store.UpdateAccount(ctx, "c", AccountPatch{LibrarySync: &on, Position: &top})
	if err != nil || !got.LibrarySync || got.Position != 0 {
		t.Fatalf("update c = %+v, %v", got, err)
	}
	order := func() []string {
		accounts, err := store.ListAccounts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for i, a := range accounts {
			if a.Position != i {
				t.Errorf("%s at position %d, want %d", a.ID, a.Position, i)
			}
			ids = append(ids, a.ID)
		}
		return ids
	}
	if ids := order(); !reflect.DeepEqual(ids, []string{"c", "a", "b"}) {
		t.Fatalf("order = %v", ids)
	}
	if _, err := store.UpdateAccount(ctx, "nobody", AccountPatch{LibrarySync: &on}); err != ErrAccountNotFound {
		t.Fatalf("update unknown = %v", err)
	}

	// "a" adopted the unowned rows; deleting it drops them and moves "b" up.
	if err := store.DeleteAccount(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if ids := order(); !reflect.DeepEqual(ids, []string{"c", "b"}) {
		t.Fatalf("order after delete = %v", ids)
	}
	for _, table := range accountTables {
		var n int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s keeps %d rows after the delete (%v)", table, n, err)
		}
	}
	var albums int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM albums`).Scan(&albums); err != nil || albums != 1 {
		t.Errorf("albums = %d (%v), want the record kept", albums, err)
	}
	if err := store.DeleteAccount(ctx, "a"); err != ErrAccountNotFound {
		t.Fatalf("second delete = %v", err)
	}
}
