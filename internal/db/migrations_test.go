package db

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"amdl/internal/domain"
)

// stoppedJobs are the rows1x jobs that were queued or running.
var stoppedJobs = map[string]bool{
	"j-running": true, "j-queued": true, "j-queued-early": true, "j-album-us9": true, "j-album-jp9": true,
	"j-queued-song-album": true, "j-legacy-queued": true, "j-station-queued": true, "j-pl-lib-queued": true,
}

func TestOpenMigrates1xDatabasePreservingRows(t *testing.T) {
	path := create1xDatabase(t)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	before := dump1x(t, raw)
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenWith(path, Options{DownloadsDir: "/music"})
	if err != nil {
		t.Fatalf("open 1.x database: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if got := userVersion(t, store.db); got != 7 {
		t.Fatalf("user_version = %d, want 7", got)
	}
	after := dumpAll(t, store.db)

	// The 1.x tables are gone; the 2.0 ones are in place.
	for _, table := range []string{"jobs", "job_items", "job_events"} {
		if len(columnsOf(t, store.db, table)) != 0 {
			t.Errorf("1.x table %s survived the migration", table)
		}
	}
	runCols := columnsOf(t, store.db, "runs")
	for _, gone := range []string{"force", "done_items", "failed_items"} {
		if runCols[gone] {
			t.Errorf("runs.%s exists", gone)
		}
	}
	for _, added := range []string{"started_at", "finished_at", "queue_order", "resource_kind", "resource_id", "trigger", "priority", "plan"} {
		if !runCols[added] {
			t.Errorf("runs.%s missing", added)
		}
	}
	itemCols := columnsOf(t, store.db, "run_items")
	for _, added := range []string{"download_bytes", "slot", "owner_kind", "owner_id"} {
		if !itemCols[added] {
			t.Errorf("run_items.%s missing", added)
		}
	}
	for _, index := range v6Indexes {
		if !indexExists(t, store.db, index) {
			t.Errorf("index %s missing after migration", index)
		}
	}

	// Counts per table.
	wantCounts := map[string]int{
		"runs": len(before["jobs"]), "run_items": len(before["job_items"]),
		// item_progress events of terminal runs are pruned: 3 and 8 as before,
		// and 12, whose run the upgrade stopped.
		"run_events": len(before["job_events"]) - 3,
		"albums":     0, "tracks": 0,
		"playlists": 1, "playlist_tracks": 3, "files": 3, "resource_sources": 1,
		"pending_adds": 6, "deleted_resources": 0, "library_sync_anchor": 2,
	}
	for table, want := range wantCounts {
		if got := len(after[table]); got != want {
			t.Errorf("%s has %d rows, want %d", table, got, want)
		}
	}

	// Every run keeps its values; status, error and overrides change only
	// where the import says so.
	assertRowsSurvived(t, "runs", before["jobs"], after["runs"], func(key, col string) bool {
		if stoppedJobs[key] && (col == "status" || col == "error") {
			return true
		}
		return col == "overrides" && key == "j-running"
	})
	assertRowsSurvived(t, "run_items", before["job_items"], after["run_items"], func(key, col string) bool {
		return key == "i-running-2" && col == "status"
	})
	assertRowsSurvived(t, "library_sync_anchor", before["library_sync_anchor"], after["library_sync_anchor"], nil)
	for _, key := range []string{"3", "8", "12"} {
		if _, ok := after["run_events"][key]; ok {
			t.Errorf("item_progress event %s of a terminal run survived", key)
		}
		delete(before["job_events"], key)
	}
	assertRowsSurvived(t, "run_events", before["job_events"], after["run_events"], nil)

	for id, run := range after["runs"] {
		if run["trigger"] != "import_1x" || run["priority"] != int64(1) {
			t.Errorf("%s trigger/priority = %v/%v, want import_1x/1", id, run["trigger"], run["priority"])
		}
		if stoppedJobs[id] {
			if run["status"] != "cancelled" || run["error"] != stoppedByUpgrade || run["finished_at"] == nil {
				t.Errorf("stopped %s = status %v error %v finished %v", id, run["status"], run["error"], run["finished_at"])
			}
		}
		if overrides, _ := run["overrides"].(string); strings.Contains(overrides, "force_overwrite") {
			t.Errorf("%s overrides still name force_overwrite: %s", id, overrides)
		}
	}
	if got := after["runs"]["j-running"]["overrides"]; got != `{"embed_cover":true}` {
		t.Errorf("j-running overrides = %#v, want force_overwrite stripped", got)
	}
	if got := after["runs"]["j-corrupt"]["overrides"]; got != "{broken" {
		t.Errorf("unparseable overrides = %#v, want it left as it was", got)
	}
	if got := after["run_items"]["i-running-2"]["status"]; got != "cancelled" {
		t.Errorf("unfinished item of a stopped run = %v, want cancelled", got)
	}
	if got := after["run_items"]["i-running-1"]["status"]; got != "completed" {
		t.Errorf("completed item of a stopped run = %v, want completed", got)
	}

	// Records named by runs, storefront dropped, legacy segment ignored.
	wantResource := map[string]string{
		"j-done": "album/1", "j-running": "album/2", "j-legacy": "album/10", "j-album-us9": "album/9", "j-album-jp9": "album/9",
		"j-pl-us": "playlist/pl.u1", "j-pl-jp": "playlist/pl.u1", "j-pl-other": "playlist/pl.u2",
		"j-pl-lib": "", "j-pl-lib-queued": "", "j-failed": "", "j-retried": "", "j-queued": "", "j-legacy-queued": "",
	}
	for id, want := range wantResource {
		run := after["runs"][id]
		got := ""
		if run["resource_id"] != nil {
			got = fmt.Sprintf("%v/%v", run["resource_kind"], run["resource_id"])
		}
		if got != want {
			t.Errorf("%s record = %q, want %q", id, got, want)
		}
	}
	if item := after["run_items"]["i-pl-jp-1"]; item["slot"] != "main" || item["owner_kind"] != "playlist" || item["owner_id"] != "pl.u1" {
		t.Errorf("i-pl-jp-1 slot/owner = %v/%v/%v", item["slot"], item["owner_kind"], item["owner_id"])
	}
	if item := after["run_items"]["i-failed-1"]; item["slot"] != "main" || item["owner_kind"] != "" {
		t.Errorf("station item slot/owner = %v/%v", item["slot"], item["owner_kind"])
	}

	// started_at / finished_at come from the events, compared as instants so
	// a raw-text MIN/MAX over unnormalized timestamps would fail here.
	wantTiming := map[string]struct{ started, finished string }{
		"j-done":      {"2024-07-10T12:00:00.5Z", "2024-07-10T12:05:00.25Z"},
		"j-failed":    {"2024-07-11T08:00:00Z", "2024-07-11T08:10:00Z"},
		"j-cancelled": {"", "2024-07-12T11:00:30Z"},
		// A retried job's started_at is its latest run's first job_started.
		"j-retried":  {"2024-07-10T12:00:01.5Z", "2024-07-10T12:00:02Z"},
		"j-refailed": {"2024-07-14T13:00:05.2Z", "2024-07-14T13:00:05.5Z"},
		"j-corrupt":  {"", "2024-07-13T00:00:00Z"},
	}
	for id, want := range wantTiming {
		var started, finished sql.NullString
		if err := store.db.QueryRow(`SELECT started_at, finished_at FROM runs WHERE id=?`, id).Scan(&started, &finished); err != nil {
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

	// queue_order follows (created_at, rowid): j-queued-early was inserted
	// after j-queued but created before it. No run is queued any more.
	order := func(id string) int64 { return after["runs"][id]["queue_order"].(int64) }
	if order("j-queued-early") >= order("j-queued") || order("j-running") >= order("j-queued") {
		t.Errorf("queue_order j-running %d j-queued-early %d j-queued %d", order("j-running"), order("j-queued-early"), order("j-queued"))
	}
	if queued, err := store.QueuedJobIDs(ctx); err != nil || len(queued) != 0 {
		t.Errorf("queued runs = %v (%v), want none", queued, err)
	}

	// The intent of every stopped job but the station, once per (kind, id).
	pending, err := store.ListPending(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	gotPending := map[string]domain.PendingAdd{}
	for _, p := range pending {
		gotPending[string(p.Kind)+"/"+p.CatalogID] = p
		if !p.Paused || p.Source != domain.SourceImport1x || p.Priority != 1 || p.State != domain.PendingAddPending {
			t.Errorf("pending %s/%s = %+v, want paused import_1x priority 1 pending", p.Kind, p.CatalogID, p)
		}
	}
	for _, key := range []string{"album/2", "song/3", "song/8", "album/9", "song/556", "artist/20"} {
		if _, ok := gotPending[key]; !ok {
			t.Errorf("no pending add for %s; have %v", key, gotPending)
		}
	}
	for _, key := range []string{"station/ra.2", "playlist/p.lib2"} {
		if _, ok := gotPending[key]; ok {
			t.Errorf("pending add for %s, want none", key)
		}
	}
	if p := gotPending["song/556"]; p.AlbumHint != "555" {
		t.Errorf("album-track link hint = %q, want 555", p.AlbumHint)
	}
	if p := gotPending["artist/20"]; p.Include != "all" {
		t.Errorf("artist include = %q, want all", p.Include)
	}
	if p := gotPending["album/9"]; !p.AddedAt.Equal(mustTime(t, "2024-07-17T01:00:00Z")) || p.Overrides == nil || p.Overrides.CodecAlternative == nil || *p.Overrides.CodecAlternative {
		t.Errorf("album 9 = added %v overrides %+v, want the us job's (first in run order)", p.AddedAt, p.Overrides)
	}
	if got := after["pending_adds"]["album/2"]["overrides"]; got != `{"embed_cover":true}` {
		t.Errorf("album 2 overrides = %#v, want the run's, force_overwrite stripped", got)
	}

	// The catalog playlist's done items became files of a paused, unresolved
	// record: jp (newer) wins song 61, us gives 62, jp gives 63. pl.u2's only
	// file shares a path with pl.u1 and is dropped, so pl.u2 has no record;
	// the library playlist has none either.
	files, err := store.ListFiles(ctx, domain.ResourcePlaylist, "pl.u1")
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := map[string]struct{ path, run, codec string }{
		"61": {"Playlists/Mix JP/01 A.m4a", "j-pl-jp", "aac-lc"},
		"62": {"Playlists/Mix/02 B.m4a", "j-pl-us", ""},
		"63": {"Playlists/Mix JP/03 C.m4a", "j-pl-jp", "alac"},
	}
	if len(files) != len(wantFiles) {
		t.Fatalf("pl.u1 files = %+v", files)
	}
	for _, f := range files {
		want := wantFiles[f.SongID]
		if f.Slot != domain.SlotMain || f.AudioState != domain.AudioDone || f.Path != want.path || f.LastRunID != want.run || f.Codec != want.codec {
			t.Errorf("pl.u1 file %s = %+v, want %+v", f.SongID, f, want)
		}
	}
	if f := files[0]; f.LyricsState != domain.LyricsFetched || f.SampleRate != 44100 || f.Bitrate != 256000 || f.FileSize != 50 || f.CompletedAt == nil {
		t.Errorf("pl.u1 file 61 = %+v, want the jp item's values", f)
	}
	playlist, err := store.GetPlaylist(ctx, "pl.u1")
	if err != nil {
		t.Fatal(err)
	}
	if !playlist.Paused || playlist.ResolvedAt != nil || playlist.State != domain.RecordPartial || playlist.TracksWanted != 3 || playlist.TracksDone != 3 ||
		playlist.Name != "Mix JP" || playlist.Artwork.URL != "https://art/mixjp.jpg" || playlist.Artwork.Colors.Bg != "445566" ||
		!playlist.AddedAt.Equal(mustTime(t, "2024-07-15T10:00:00Z")) {
		t.Errorf("pl.u1 record = %+v", playlist)
	}
	if sources, err := store.RecordSources(ctx, domain.ResourcePlaylist, "pl.u1"); err != nil || !reflect.DeepEqual(sources, []domain.RecordSource{domain.SourceImport1x}) {
		t.Errorf("pl.u1 sources = %v (%v)", sources, err)
	}
	for _, id := range []string{"pl.u2", "p.lib1"} {
		if _, err := store.GetPlaylist(ctx, id); !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("playlist %s record: err = %v, want ErrRecordNotFound", id, err)
		}
	}
	assertRollupsConsistent(t, store)

	// Event ids are SSE cursors: the next one is above 30, the highest id
	// 1.x handed out (and deleted).
	ev, err := store.AddEvent(ctx, domain.Event{JobID: "j-done", Type: domain.EventJobQueued})
	if err != nil {
		t.Fatal(err)
	}
	if ev.ID != 31 {
		t.Errorf("first event id after the import = %d, want 31", ev.ID)
	}

	// The store reads the migrated runs: counters derived from items, the
	// legacy force flag gone with force_overwrite.
	done, err := store.GetJob(ctx, "j-done")
	if err != nil {
		t.Fatal(err)
	}
	if done.CompletedItems != 2 || done.TotalItems != 2 || done.Overrides != nil || done.ResourceKind != domain.ResourceAlbum || done.ResourceID != "1" || done.Trigger != domain.TriggerImport1x {
		t.Errorf("j-done = %+v", done)
	}
	failed, err := store.GetJob(ctx, "j-failed")
	if err != nil {
		t.Fatal(err)
	}
	if failed.CompletedItems != 1 || failed.SkippedItems != 1 || failed.FailedItems != 1 || failed.CancelledItems != 0 || failed.Overrides != nil {
		t.Errorf("j-failed = %+v", failed)
	}
	running, err := store.GetJob(ctx, "j-running")
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != domain.JobCancelled || running.StartedAt == nil || running.FinishedAt == nil || running.CompletedItems != 1 || running.CancelledItems != 1 {
		t.Errorf("j-running = %+v", running)
	}
}

// v6Indexes are the indexes the 2.0 schema creates.
var v6Indexes = []string{
	"idx_runs_active_key", "idx_runs_active_resource", "idx_runs_queued_order", "idx_runs_resource",
	"idx_run_items_job_id", "idx_run_events_job_id_id", "idx_run_events_type_created",
	"idx_albums_state", "idx_albums_pick", "idx_tracks_song", "idx_playlists_state", "idx_playlists_pick", "idx_playlist_tracks_song",
	"idx_files_path", "idx_files_due", "idx_pending_adds_catalog",
	"idx_apple_library_items_catalog", "idx_apple_library_items_order",
}

func TestOpenIsNoOpOnMigratedDatabase(t *testing.T) {
	path := create1xDatabase(t)
	store, err := OpenWith(path, Options{DownloadsDir: "/music"})
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

func TestMigrationImportsPathsVerbatimWithoutDownloadsDir(t *testing.T) {
	store, err := Open(create1xDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	f, err := store.GetFile(context.Background(), domain.FileKey{OwnerKind: domain.ResourcePlaylist, OwnerID: "pl.u1", SongID: "62", Slot: domain.SlotMain})
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != "/music/Playlists/Mix/02 B.m4a" {
		t.Fatalf("path = %q, want the 1.x output path", f.Path)
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
	before := dump1x(t, raw)
	if err := migrate(context.Background(), raw, 1); err != nil {
		t.Fatal(err)
	}
	if got := userVersion(t, raw); got != 1 {
		t.Fatalf("user_version = %d, want 1", got)
	}
	if after := dump1x(t, raw); !reflect.DeepEqual(before, after) {
		t.Fatalf("migration 1 changed a current 1.x database:\nbefore %v\nafter  %v", before, after)
	}
}

func TestOpenCreatesLatestSchemaInFreshDatabase(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := userVersion(t, store.db); got != 7 || got != latestSchemaVersion() {
		t.Fatalf("fresh user_version = %d, want 7 (latest %d)", got, latestSchemaVersion())
	}
	for _, table := range []string{"runs", "run_items", "run_events", "albums", "tracks", "playlists", "playlist_tracks", "files",
		"pending_adds", "resource_sources", "deleted_resources", "check_runs", "library_sync_anchor",
		"apple_library_items", "apple_library_playlist_tracks", "apple_library_meta", "accounts"} {
		if len(columnsOf(t, store.db, table)) == 0 {
			t.Errorf("fresh database lacks table %s", table)
		}
	}
	for _, table := range []string{"jobs", "job_items", "job_events"} {
		if len(columnsOf(t, store.db, table)) != 0 {
			t.Errorf("fresh database has the 1.x table %s", table)
		}
	}
	for _, index := range v6Indexes {
		if !indexExists(t, store.db, index) {
			t.Errorf("fresh database lacks index %s", index)
		}
	}
	ev, err := store.AddEvent(context.Background(), domain.Event{JobID: "j", Type: domain.EventJobQueued})
	if err != nil || ev.ID != 1 {
		t.Fatalf("first event = %d (%v), want id 1", ev.ID, err)
	}
}

func TestCreateJobDedupsActiveRunsAfterImport(t *testing.T) {
	store, err := Open(create1xDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	// The canonical key still dedups, and so does the record: two
	// storefronts of one album are one active run.
	first := domain.Job{ID: "a", Input: "x", Type: "album", CanonicalKey: "album:us:9", Status: domain.JobQueued,
		ResourceKind: domain.ResourceAlbum, ResourceID: "9"}
	if err := store.CreateJob(ctx, first); err != nil {
		t.Fatal(err)
	}
	sameKey := domain.Job{ID: "b", Input: "x", Type: "album", CanonicalKey: "album:us:9", Status: domain.JobQueued}
	if err := store.CreateJob(ctx, sameKey); !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("same key: err = %v, want ErrDuplicateActive", err)
	}
	sameRecord := domain.Job{ID: "c", Input: "x", Type: "album", CanonicalKey: "album:jp:9", Status: domain.JobQueued,
		ResourceKind: domain.ResourceAlbum, ResourceID: "9"}
	if err := store.CreateJob(ctx, sameRecord); !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("same record: err = %v, want ErrDuplicateActive", err)
	}
	station := domain.Job{ID: "d", Input: "x", Type: "station", CanonicalKey: "station:us:ra.9", Status: domain.JobQueued}
	if err := store.CreateJob(ctx, station); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetJob(ctx, "d")
	if err != nil {
		t.Fatal(err)
	}
	if got.ResourceKind != "" || got.ResourceID != "" || got.Trigger != domain.TriggerManual || got.Priority != 0 {
		t.Fatalf("station run = %+v, want no record, trigger manual", got)
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

func TestOpenRefusesReleaseCandidateDatabaseUnchanged(t *testing.T) {
	for version := 2; version <= 5; version++ {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "amdl.db")
			raw, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			for _, stmt := range []string{
				`PRAGMA journal_mode=WAL`,
				`CREATE TABLE jobs (id TEXT PRIMARY KEY)`,
				`INSERT INTO jobs VALUES ('rc-job')`,
				fmt.Sprintf(`PRAGMA user_version = %d`, version),
			} {
				if _, err := raw.Exec(stmt); err != nil {
					t.Fatal(err)
				}
			}
			if err := raw.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			store, err := Open(path)
			if err == nil {
				store.Close()
				t.Fatal("Open accepted a release-candidate database")
			}
			want := fmt.Sprintf("%s was written by a 2.0 release candidate (schema %d), which this version does not read: move the file aside (it is not changed) and start again", path, version)
			if err.Error() != want {
				t.Fatalf("Open error = %q, want %q", err, want)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("refusing the database changed the file")
			}
		})
	}
}
