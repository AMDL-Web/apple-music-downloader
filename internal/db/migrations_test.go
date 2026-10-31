package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"amdl/internal/domain"
)

func TestOpenMigrates1xDatabasePreservingRows(t *testing.T) {
	path := create1xDatabase(t)
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
		t.Fatalf("open 1.x database: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if got := userVersion(t, store.db); got != 4 {
		t.Fatalf("user_version = %d, want 4", got)
	}
	after := dumpAll(t, store.db)

	// Columns: the three 1.x counters/flags are gone, timing is new.
	jobCols := columnsOf(t, store.db, "jobs")
	for _, gone := range []string{"force", "done_items", "failed_items"} {
		if jobCols[gone] {
			t.Errorf("jobs.%s still exists after migration", gone)
		}
	}
	for _, added := range []string{"started_at", "finished_at", "queue_order"} {
		if !jobCols[added] {
			t.Errorf("jobs.%s missing after migration", added)
		}
	}
	if !columnsOf(t, store.db, "job_items")["download_bytes"] {
		t.Error("job_items.download_bytes missing after migration")
	}
	for _, index := range []string{"idx_jobs_active_key", "idx_job_items_job_id", "idx_job_events_job_id_id", "idx_job_events_type_created", "idx_jobs_queued_order"} {
		if !indexExists(t, store.db, index) {
			t.Errorf("index %s missing after migration", index)
		}
	}

	// queue_order follows (created_at, rowid): j-queued-early was inserted
	// after j-queued but created before it, so it runs first, and every row
	// has a distinct value.
	queueOrder := map[string]int64{}
	orderRows, err := store.db.Query(`SELECT id, queue_order FROM jobs`)
	if err != nil {
		t.Fatal(err)
	}
	for orderRows.Next() {
		var id string
		var order int64
		if err := orderRows.Scan(&id, &order); err != nil {
			t.Fatal(err)
		}
		queueOrder[id] = order
	}
	if err := orderRows.Close(); err != nil {
		t.Fatal(err)
	}
	seenOrder := map[int64]string{}
	for id, order := range queueOrder {
		if order <= 0 {
			t.Errorf("%s.queue_order = %d, want a positive value", id, order)
		}
		if other, dup := seenOrder[order]; dup {
			t.Errorf("%s and %s share queue_order %d", id, other, order)
		}
		seenOrder[order] = id
	}
	if queueOrder["j-queued-early"] >= queueOrder["j-queued"] || queueOrder["j-running"] >= queueOrder["j-queued"] {
		t.Errorf("queue_order = %v, want j-running and j-queued-early (earlier created_at) before j-queued", queueOrder)
	}
	if queued, err := store.QueuedJobIDs(ctx); err != nil || len(queued) != 2 || queued[0] != "j-queued-early" || queued[1] != "j-queued" {
		t.Errorf("queued run order = %v (%v), want [j-queued-early j-queued]", queued, err)
	}

	// Every row of user data survived with its values. overrides changes only
	// where force was folded into it.
	forceFolded := map[string]bool{"j-done": true, "j-running": true}
	assertRowsSurvived(t, "jobs", before["jobs"], after["jobs"], func(key, col string) bool {
		return col == "overrides" && forceFolded[key]
	})
	assertRowsSurvived(t, "job_items", before["job_items"], after["job_items"], nil)
	assertRowsSurvived(t, "library_sync_anchor", before["library_sync_anchor"], after["library_sync_anchor"], nil)
	if len(after["jobs"]) != len(before["jobs"]) || len(after["job_items"]) != len(before["job_items"]) || len(after["library_sync_anchor"]) != 2 {
		t.Fatalf("row counts changed: jobs %d->%d items %d->%d anchor %d",
			len(before["jobs"]), len(after["jobs"]), len(before["job_items"]), len(after["job_items"]), len(after["library_sync_anchor"]))
	}

	// Events: progress of terminal jobs is gone, everything else stays.
	prunedProgress := map[string]bool{"3": true, "8": true}
	for key := range prunedProgress {
		if _, ok := after["job_events"][key]; ok {
			t.Errorf("item_progress event %s of a terminal job survived", key)
		}
		delete(before["job_events"], key)
	}
	if _, ok := after["job_events"]["12"]; !ok {
		t.Error("item_progress event of the running job was deleted")
	}
	if len(after["job_events"]) != len(before["job_events"]) {
		t.Errorf("job_events has %d rows, want %d", len(after["job_events"]), len(before["job_events"]))
	}
	assertRowsSurvived(t, "job_events", before["job_events"], after["job_events"], nil)

	// started_at / finished_at come from the events, compared as instants so
	// a raw-text MIN/MAX over unnormalized timestamps would fail here.
	wantTiming := map[string]struct{ started, finished string }{
		"j-done":      {"2024-07-10T12:00:00.5Z", "2024-07-10T12:05:00.25Z"},
		"j-failed":    {"2024-07-11T08:00:00Z", "2024-07-11T08:10:00Z"},
		"j-running":   {"2024-07-12T09:00:00Z", ""},
		"j-queued":    {"", ""},
		"j-cancelled": {"", "2024-07-12T11:00:30Z"},
		// A retried job's started_at is its latest run's first job_started.
		"j-retried":      {"2024-07-10T12:00:01.5Z", "2024-07-10T12:00:02Z"},
		"j-refailed":     {"2024-07-14T13:00:05.2Z", "2024-07-14T13:00:05.5Z"},
		"j-corrupt":      {"", "2024-07-13T00:00:00Z"},
		"j-queued-early": {"", ""},
	}
	for id, want := range wantTiming {
		var started, finished sql.NullString
		if err := store.db.QueryRow(`SELECT started_at, finished_at FROM jobs WHERE id=?`, id).Scan(&started, &finished); err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			name string
			got  sql.NullString
			want string
		}{{"started_at", started, want.started}, {"finished_at", finished, want.finished}} {
			switch {
			case c.want == "" && c.got.Valid:
				t.Errorf("%s.%s = %q, want NULL", id, c.name, c.got.String)
			case c.want != "" && (!c.got.Valid || !parseTime(c.got.String).Equal(mustTime(t, c.want))):
				t.Errorf("%s.%s = %v, want %s", id, c.name, c.got, c.want)
			case c.want != "" && c.got.String != formatTime(mustTime(t, c.want)):
				t.Errorf("%s.%s = %q, want fixed-width %q", id, c.name, c.got.String, formatTime(mustTime(t, c.want)))
			}
		}
	}

	// The store reads the migrated rows: counters derived from items, legacy
	// force kept as an override, credentials of the failed job kept for retry.
	done, err := store.GetJob(ctx, "j-done")
	if err != nil {
		t.Fatal(err)
	}
	if done.CompletedItems != 2 || done.SkippedItems != 0 || done.FailedItems != 0 || done.TotalItems != 2 {
		t.Errorf("j-done counters = %+v", done)
	}
	if done.Overrides == nil || done.Overrides.ForceOverwrite == nil || !*done.Overrides.ForceOverwrite {
		t.Errorf("j-done overrides = %+v, want force_overwrite true from the legacy force flag", done.Overrides)
	}
	running, err := store.GetJob(ctx, "j-running")
	if err != nil {
		t.Fatal(err)
	}
	if running.Overrides == nil || running.Overrides.ForceOverwrite == nil || !*running.Overrides.ForceOverwrite {
		t.Errorf("j-running overrides = %+v, want force_overwrite true", running.Overrides)
	}
	if running.StartedAt == nil || running.FinishedAt != nil || running.CompletedItems != 1 {
		t.Errorf("j-running = started %v finished %v completed %d", running.StartedAt, running.FinishedAt, running.CompletedItems)
	}
	failed, err := store.GetJob(ctx, "j-failed")
	if err != nil {
		t.Fatal(err)
	}
	if failed.CompletedItems != 1 || failed.SkippedItems != 1 || failed.FailedItems != 1 || failed.CancelledItems != 0 {
		t.Errorf("j-failed counters = completed %d skipped %d failed %d cancelled %d, want 1/1/1/0",
			failed.CompletedItems, failed.SkippedItems, failed.FailedItems, failed.CancelledItems)
	}
	// Its overrides held only removed keys, so the job reads as having none.
	if failed.Overrides != nil {
		t.Errorf("j-failed overrides = %+v, want none", failed.Overrides)
	}
	if got := after["jobs"]["j-corrupt"]["overrides"]; got != "{broken" {
		t.Errorf("unparseable overrides = %#v, want it left as it was", got)
	}
	var payload map[string]any
	events, err := store.ListEventsAfterLimit(ctx, "j-done", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil || payload["message"] != "job queued" {
		t.Errorf("migrated payload = %s (%v), want the 1.x JSON object", events[0].Payload, err)
	}

	// The active-key index still dedups after the table rewrite.
	dup := domain.Job{ID: "j-dup", Input: "x", Type: "album", CanonicalKey: "album:us:2", Status: domain.JobQueued}
	if err := store.CreateJob(ctx, dup); !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("duplicate of a running job's key: err = %v, want ErrDuplicateActive", err)
	}
}

func TestOpenIsNoOpOnMigratedDatabase(t *testing.T) {
	path := create1xDatabase(t)
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
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
	if got := userVersion(t, store.db); got != latestSchemaVersion() {
		t.Fatalf("user_version after reopen = %d, want %d", got, latestSchemaVersion())
	}
	if second := dumpAll(t, store.db); !reflect.DeepEqual(first, second) {
		t.Fatal("reopening a migrated database changed its rows")
	}
}

func TestMigration1LeavesCurrent1xDatabaseUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amdl.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	// A database the last 1.x release wrote and normalized itself: fixed-width
	// timestamps and completed items with their stage breakdown.
	stamp := "2024-07-10T12:00:00.000000000Z"
	for _, stmt := range []string{
		schema1x,
		`INSERT INTO jobs(id,input,type,storefront,canonical_key,force,overrides,status,total_items,done_items,failed_items,created_at,updated_at)
			VALUES('j1','u','song','us','song:us:1',1,'{"embed_cover":false}','completed',1,1,0,'` + stamp + `','` + stamp + `')`,
		`INSERT INTO job_items(id,job_id,adam_id,kind,idx,status,download_progress,decrypt_progress,progress_resolved,progress_remuxed,progress_tagged,progress_saved,created_at,updated_at)
			VALUES('i1','j1','1','song',1,'completed',1,1,1,1,1,1,'` + stamp + `','` + stamp + `')`,
		`INSERT INTO job_items(id,job_id,adam_id,kind,idx,status,created_at,updated_at)
			VALUES('i2','j1','2','song',2,'skipped_existing','` + stamp + `','` + stamp + `')`,
		`INSERT INTO job_events(job_id,type,payload,created_at) VALUES('j1','job_finished','{"id":"j1"}','` + stamp + `')`,
		`INSERT INTO library_sync_anchor(position,song_id) VALUES(0,'i.1')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	raw.SetMaxOpenConns(1)
	before := dumpAll(t, raw)
	if err := migrate(context.Background(), raw, 1); err != nil {
		t.Fatal(err)
	}
	if got := userVersion(t, raw); got != 1 {
		t.Fatalf("user_version = %d, want 1", got)
	}
	if after := dumpAll(t, raw); !reflect.DeepEqual(before, after) {
		t.Fatalf("migration 1 changed a current 1.x database:\nbefore %v\nafter  %v", before, after)
	}
}

func TestOpenCreatesLatestSchemaInFreshDatabase(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := userVersion(t, store.db); got != 4 || got != latestSchemaVersion() {
		t.Fatalf("fresh user_version = %d, want 4 (latest %d)", got, latestSchemaVersion())
	}
	jobCols := columnsOf(t, store.db, "jobs")
	if jobCols["force"] || jobCols["done_items"] || jobCols["failed_items"] || !jobCols["started_at"] || !jobCols["finished_at"] || !jobCols["queue_order"] {
		t.Fatalf("fresh jobs columns = %v", jobCols)
	}
	for _, index := range []string{"idx_jobs_active_key", "idx_job_items_job_id", "idx_job_events_job_id_id", "idx_job_events_type_created", "idx_jobs_queued_order"} {
		if !indexExists(t, store.db, index) {
			t.Errorf("fresh database lacks index %s", index)
		}
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amdl.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, latestSchemaVersion()+1)); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err == nil {
		store.Close()
		t.Fatal("Open accepted a database written by a newer schema")
	}
	if !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Open error = %v, want it to say the schema is newer", err)
	}
}
