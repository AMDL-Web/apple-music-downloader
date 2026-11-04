package db

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"amdl/internal/domain"
)

// create7Database is the 1.x fixture migrated to schema 7, plus runs of the
// shapes migration 8 decides on: a record that exists, a record a delete
// removed while keeping its runs (before 0f813a54), a failed pending album
// add, and a pending song add whose album hint is the run's album.
func create7Database(t *testing.T) string {
	t.Helper()
	path := create1xDatabase(t)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	ctx := context.Background()
	if err := migrateWith(ctx, raw, 7, migrationEnv{downloadsDir: "/music", now: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	const stamp = "2026-11-02T00:00:00.000Z"
	run := func(id, kind, resource string) string {
		return `INSERT INTO runs(id, input, type, storefront, canonical_key, status, created_at, updated_at, resource_kind, resource_id)
			VALUES('` + id + `', 'https://music.apple.com/us/` + kind + `/x/` + resource + `', '` + kind + `', 'us', '` + kind + `:us:` + resource + `',
				'completed', '` + stamp + `', '` + stamp + `', '` + kind + `', '` + resource + `')`
	}
	for _, stmt := range []string{
		`INSERT INTO albums(id, name, storefront_read, added_at, updated_at) VALUES('700', 'Kept', 'us', '` + stamp + `', '` + stamp + `')`,
		run("r-attached", "album", "700"),
		`INSERT INTO deleted_resources(resource_kind, resource_id, name, deleted_at) VALUES('album', '701', 'Deleted', '` + stamp + `')`,
		run("r-deleted", "album", "701"),
		`INSERT INTO pending_adds(kind, catalog_id, source, added_at, state, attempts, last_error) VALUES('album', '702', 'manual', '` + stamp + `', 'failed', 3, 'boom')`,
		run("r-failed-add", "album", "702"),
		// song/556 in rows1x carries the album hint 555.
		run("r-hint", "album", "555"),
		run("r-pl-deleted", "playlist", "pl.gone"),
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	return path
}

// Migration 8 detaches every run whose record does not exist and no pending
// add will make, keeps every row and every other value, and is a no-op on
// reopen.
func TestMigration8DetachesRunsOfMissingRecords(t *testing.T) {
	path := create7Database(t)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	before := dumpAll(t, raw)
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("open schema 7: %v", err)
	}
	if got := userVersion(t, store.db); got != 8 {
		t.Fatalf("user_version = %d, want 8", got)
	}
	after := dumpAll(t, store.db)

	detached := map[string]bool{"j-done": true, "j-legacy": true, "j-pl-other": true, "r-deleted": true, "r-pl-deleted": true}
	for table, rows := range before {
		if len(after[table]) != len(rows) {
			t.Errorf("%s has %d rows, want %d", table, len(after[table]), len(rows))
		}
		if !reflect.DeepEqual(rows, after[table]) && table != "runs" {
			t.Errorf("%s changed", table)
		}
	}
	for id, old := range before["runs"] {
		now := after["runs"][id]
		for col, value := range old {
			if detached[id] && (col == "resource_kind" || col == "resource_id") {
				if now[col] != nil {
					t.Errorf("%s.%s = %v, want NULL", id, col, now[col])
				}
				continue
			}
			if !reflect.DeepEqual(now[col], value) {
				t.Errorf("%s.%s = %#v, want %#v", id, col, now[col], value)
			}
		}
		if detached[id] && old["resource_id"] == nil {
			t.Errorf("fixture run %s named no record before the migration", id)
		}
	}
	// Kept: the existing record's run, the stopped imports whose paused adds
	// will make album 2 and album 9, the failed add's, the album hint's, and
	// the imported playlist record's runs.
	for id, want := range map[string]string{
		"r-attached": "album/700", "j-running": "album/2", "j-album-us9": "album/9", "j-album-jp9": "album/9",
		"r-failed-add": "album/702", "r-hint": "album/555", "j-pl-us": "playlist/pl.u1", "j-pl-jp": "playlist/pl.u1",
	} {
		run := after["runs"][id]
		if got, _ := run["resource_kind"].(string); got+"/"+stringOf(run["resource_id"]) != want {
			t.Errorf("%s record = %v/%v, want %s", id, run["resource_kind"], run["resource_id"], want)
		}
	}
	for _, index := range []string{"idx_runs_active_resource", "idx_runs_resource"} {
		if !indexExists(t, store.db, index) {
			t.Errorf("index %s missing", index)
		}
	}
	// A detached run reads as a run of no record.
	done, err := store.GetJob(context.Background(), "j-done")
	if err != nil || done.ResourceKind != "" || done.ResourceID != "" {
		t.Fatalf("j-done = %+v (%v), want no record", done, err)
	}

	first := dumpAll(t, store.db)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store.Close()
	if second := dumpAll(t, store.db); !reflect.DeepEqual(first, second) {
		t.Fatal("reopening the migrated database changed its rows")
	}
}

func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// Dropping a pending add detaches the runs that waited for its record; a
// resolved add, whose record exists, and runs another add still waits for
// keep theirs.
func TestDeletePendingDetachesRunsOfTheRecordItWouldMake(t *testing.T) {
	store, err := Open(create7Database(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	pending, err := store.ListPending(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, p := range pending {
		ids[string(p.Kind)+"/"+p.CatalogID] = p.ID
	}
	resource := func(id string) string {
		t.Helper()
		job, err := store.GetJob(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if job.ResourceID == "" {
			return ""
		}
		return string(job.ResourceKind) + "/" + job.ResourceID
	}

	// album/9 has two stopped runs and one add; dropping the add detaches both.
	if err := store.DeletePending(ctx, ids["album/9"]); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"j-album-us9", "j-album-jp9"} {
		if got := resource(id); got != "" {
			t.Errorf("%s record = %q after its add was dropped, want none", id, got)
		}
	}
	// The song add with album hint 555 is what keeps r-hint.
	if err := store.DeletePending(ctx, ids["song/556"]); err != nil {
		t.Fatal(err)
	}
	if got := resource("r-hint"); got != "" {
		t.Errorf("r-hint record = %q after the hinted song add was dropped, want none", got)
	}
	// A resolved add: the record exists, so its run keeps it.
	if _, err := store.AddPending(ctx, domain.PendingAdd{Kind: domain.PendingAlbum, CatalogID: "700", Source: domain.SourceManual, AddedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	added, err := store.ListPending(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range added {
		if p.Kind == domain.PendingAlbum && p.CatalogID == "700" {
			if err := store.DeletePending(ctx, p.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := resource("r-attached"); got != "album/700" {
		t.Errorf("r-attached record = %q, want album/700", got)
	}
	if got := resource("j-running"); got != "album/2" {
		t.Errorf("j-running record = %q, want album/2 (its add is still there)", got)
	}
	if err := store.DeletePending(ctx, ids["album/9"]); !errors.Is(err, ErrPendingNotFound) {
		t.Errorf("second delete = %v, want ErrPendingNotFound", err)
	}
}
