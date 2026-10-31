package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// schema1x is the schema the last 1.x release (a6a6b46) created, copied
// verbatim from its initSchema. It is frozen on purpose: it stands for the
// databases deployed installs carry, not for anything this package writes.
const schema1x = `
CREATE TABLE IF NOT EXISTS jobs (
	id TEXT PRIMARY KEY,
	input TEXT NOT NULL,
	type TEXT NOT NULL,
	storefront TEXT,
	title TEXT NOT NULL DEFAULT '',
	artwork_url TEXT NOT NULL DEFAULT '',
	artist_name TEXT NOT NULL DEFAULT '',
	artist_url TEXT NOT NULL DEFAULT '',
	curator_name TEXT NOT NULL DEFAULT '',
	release_date TEXT NOT NULL DEFAULT '',
	genre TEXT NOT NULL DEFAULT '',
	artwork_bg_color TEXT NOT NULL DEFAULT '',
	artwork_text_color1 TEXT NOT NULL DEFAULT '',
	artwork_text_color2 TEXT NOT NULL DEFAULT '',
	artwork_text_color3 TEXT NOT NULL DEFAULT '',
	artwork_text_color4 TEXT NOT NULL DEFAULT '',
	motion_artwork_url TEXT NOT NULL DEFAULT '',
	motion_artwork_tall_url TEXT NOT NULL DEFAULT '',
	motion_artwork_bg_color TEXT NOT NULL DEFAULT '',
	motion_artwork_text_color1 TEXT NOT NULL DEFAULT '',
	motion_artwork_text_color2 TEXT NOT NULL DEFAULT '',
	motion_artwork_text_color3 TEXT NOT NULL DEFAULT '',
	motion_artwork_text_color4 TEXT NOT NULL DEFAULT '',
	motion_artwork_tall_bg_color TEXT NOT NULL DEFAULT '',
	motion_artwork_tall_text_color1 TEXT NOT NULL DEFAULT '',
	motion_artwork_tall_text_color2 TEXT NOT NULL DEFAULT '',
	motion_artwork_tall_text_color3 TEXT NOT NULL DEFAULT '',
	motion_artwork_tall_text_color4 TEXT NOT NULL DEFAULT '',
	canonical_key TEXT NOT NULL,
	force INTEGER NOT NULL DEFAULT 0,
	overrides TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	total_items INTEGER NOT NULL DEFAULT 0,
	done_items INTEGER NOT NULL DEFAULT 0,
	failed_items INTEGER NOT NULL DEFAULT 0,
	error TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_active_key
	ON jobs(canonical_key)
	WHERE status IN ('queued','running');
CREATE TABLE IF NOT EXISTS job_items (
	id TEXT PRIMARY KEY,
	job_id TEXT NOT NULL,
	adam_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	idx INTEGER NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	artist TEXT NOT NULL DEFAULT '',
	album TEXT NOT NULL DEFAULT '',
	duration_ms INTEGER NOT NULL DEFAULT 0,
	artwork_url TEXT NOT NULL DEFAULT '',
	has_lyrics INTEGER NOT NULL DEFAULT 0,
	lyrics_status TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	download_progress REAL NOT NULL DEFAULT 0,
	decrypt_progress REAL NOT NULL DEFAULT 0,
	progress_resolved INTEGER NOT NULL DEFAULT 0,
	progress_remuxed INTEGER NOT NULL DEFAULT 0,
	progress_verified INTEGER NOT NULL DEFAULT 0,
	progress_tagged INTEGER NOT NULL DEFAULT 0,
	progress_saved INTEGER NOT NULL DEFAULT 0,
	codec TEXT NOT NULL DEFAULT '',
	bit_depth INTEGER NOT NULL DEFAULT 0,
	sample_rate INTEGER NOT NULL DEFAULT 0,
	bitrate INTEGER NOT NULL DEFAULT 0,
	file_size INTEGER NOT NULL DEFAULT 0,
	retry_kind TEXT NOT NULL DEFAULT '',
	attempt INTEGER NOT NULL DEFAULT 0,
	max_attempts INTEGER NOT NULL DEFAULT 0,
	status_message TEXT NOT NULL DEFAULT '',
	output_path TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	FOREIGN KEY(job_id) REFERENCES jobs(id)
);
CREATE INDEX IF NOT EXISTS idx_job_items_job_id ON job_items(job_id, idx);
CREATE TABLE IF NOT EXISTS job_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	job_id TEXT NOT NULL,
	item_id TEXT NOT NULL DEFAULT '',
	type TEXT NOT NULL,
	phase TEXT NOT NULL DEFAULT '',
	message TEXT NOT NULL DEFAULT '',
	payload TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_job_events_job_id_id ON job_events(job_id, id);
CREATE TABLE IF NOT EXISTS library_sync_anchor (
	position INTEGER PRIMARY KEY,
	song_id TEXT NOT NULL,
	song_name TEXT NOT NULL DEFAULT '',
	album_name TEXT NOT NULL DEFAULT ''
);
`

// rows1x fills a 1.x database with one of each kind of row an install
// carries. Some timestamps use the variable-width form 1.x releases wrote
// before normalization existed, chosen so that ordering them as raw text gives
// the wrong earliest/latest event (".5Z" sorts before "Z").
const rows1x = `
INSERT INTO jobs(id,input,type,storefront,title,artwork_url,artist_name,canonical_key,force,overrides,status,total_items,done_items,failed_items,error,created_at,updated_at) VALUES
	('j-done','https://music.apple.com/us/album/a/1','album','us','Album A','https://art/a.jpg','Artist A','album:us:1',1,'','completed',2,2,0,'','2024-07-10T12:00:00Z','2024-07-10T12:05:00.25Z'),
	('j-failed','https://music.apple.com/us/station/ra.1','station','us','Station','','','station:us:ra.1',0,'{"media_user_token":"tok","embed_cover":false}','failed',3,2,1,'1 of 3 tracks failed','2024-07-11T08:00:00.000000000Z','2024-07-11T08:10:00.000000000Z'),
	('j-running','https://music.apple.com/us/album/b/2','album','us','Album B','','','album:us:2',1,'{"force_overwrite":false,"embed_cover":true}','running',2,1,0,'','2024-07-12T09:00:00.000000000Z','2024-07-12T09:01:00.000000000Z'),
	('j-queued','https://music.apple.com/us/song/c/3','song','us','','','','song:us:3',0,'','queued',0,0,0,'','2024-07-12T10:00:00.000000000Z','2024-07-12T10:00:00.000000000Z'),
	('j-cancelled','https://music.apple.com/us/song/d/4','song','us','','','','song:us:4',0,'','cancelled',0,0,0,'cancelled','2024-07-12T11:00:00.000000000Z','2024-07-12T11:00:30.000000000Z'),
	('j-retried','https://music.apple.com/us/song/e/5','song','us','Song E','','','song:us:5',0,'','completed',1,1,0,'','2024-07-10T12:00:00.000000000Z','2024-07-10T12:00:02Z'),
	('j-refailed','https://music.apple.com/us/song/g/7','song','us','','','','song:us:7',0,'','failed',0,0,0,'failed again','2024-07-14T13:00:00.000000000Z','2024-07-14T13:00:05.5Z'),
	('j-corrupt','https://music.apple.com/us/song/f/6','song','us','','','','song:us:6',1,'{broken','completed',0,0,0,'','2024-07-13T00:00:00.000000000Z','2024-07-13T00:00:00.000000000Z');

INSERT INTO job_items(id,job_id,adam_id,kind,idx,title,artist,album,duration_ms,has_lyrics,lyrics_status,status,download_progress,decrypt_progress,progress_resolved,progress_remuxed,progress_verified,progress_tagged,progress_saved,codec,bit_depth,sample_rate,bitrate,file_size,output_path,error,created_at,updated_at) VALUES
	('i-done-1','j-done','11','song',1,'One','Artist A','Album A',180000,1,'fetched','completed',1,1,1,1,1,1,1,'alac',24,96000,0,40000000,'/music/Artist A/Album A/01 One.m4a','','2024-07-10T12:00:01Z','2024-07-10T12:02:00.5Z'),
	('i-done-2','j-done','12','song',2,'Two','Artist A','Album A',200000,0,'none','completed',1,1,1,1,0,1,1,'aac-lc',0,44100,256000,6000000,'/music/Artist A/Album A/02 Two.m4a','','2024-07-10T12:00:01.000000000Z','2024-07-10T12:04:00.000000000Z'),
	('i-failed-1','j-failed','21','song',1,'X','','',0,0,'','completed',1,1,1,1,1,1,1,'alac',16,44100,0,30000000,'/music/x.m4a','','2024-07-11T08:00:01.000000000Z','2024-07-11T08:05:00.000000000Z'),
	('i-failed-2','j-failed','22','song',2,'Y','','',0,0,'','failed',0.4,0,1,0,0,0,0,'alac',16,44100,0,0,'','decrypt failed','2024-07-11T08:00:01.000000000Z','2024-07-11T08:06:00.000000000Z'),
	('i-failed-3','j-failed','23','song',3,'Z','','',0,0,'','skipped_existing',0,0,1,0,0,0,0,'',0,0,0,1234,'/music/z.m4a','','2024-07-11T08:00:01.000000000Z','2024-07-11T08:07:00.000000000Z'),
	('i-running-1','j-running','31','song',1,'P','','',0,0,'','completed',1,1,1,1,1,1,1,'alac',24,48000,0,1000,'/music/p.m4a','','2024-07-12T09:00:01.000000000Z','2024-07-12T09:00:30.000000000Z'),
	('i-running-2','j-running','32','song',2,'Q','','',0,0,'','downloading',0.5,0,1,0,0,0,0,'alac',24,48000,0,0,'','','2024-07-12T09:00:01.000000000Z','2024-07-12T09:00:40.000000000Z'),
	('i-retried-1','j-retried','51','song',1,'E','','',0,0,'','completed',1,1,1,1,1,1,1,'alac',16,44100,0,2000,'/music/e.m4a','','2024-07-10T12:00:00.000000000Z','2024-07-10T12:00:03.000000000Z');

INSERT INTO job_events(id,job_id,item_id,type,phase,message,payload,created_at) VALUES
	(1,'j-done','','job_queued','','job queued','{"id":"j-done","message":"job queued"}','2024-07-10T12:00:00Z'),
	(2,'j-done','','job_started','','job started','{"id":"j-done","message":"job started"}','2024-07-10T12:00:00.5Z'),
	(3,'j-done','i-done-1','item_progress','downloading','','{"id":"i-done-1","status":"downloading"}','2024-07-10T12:01:00Z'),
	(4,'j-done','i-done-1','item_completed','','done','{"id":"i-done-1","status":"completed"}','2024-07-10T12:02:00.5Z'),
	(5,'j-done','','job_finished','','completed','{"id":"j-done","status":"completed"}','2024-07-10T12:05:00.25Z'),
	(6,'j-done','','hook_succeeded','notify','','{"hook":"notify","attempt":1}','2024-07-10T12:05:01Z'),
	(7,'j-failed','','job_started','','job started','','2024-07-11T08:00:00.000000000Z'),
	(8,'j-failed','i-failed-2','item_progress','decrypting','','{"id":"i-failed-2"}','2024-07-11T08:05:30.000000000Z'),
	(9,'j-failed','i-failed-2','item_failed','','decrypt failed','{"id":"i-failed-2","status":"failed"}','2024-07-11T08:06:00.000000000Z'),
	(10,'j-failed','','job_failed','','1 of 3 tracks failed','{"id":"j-failed","status":"failed"}','2024-07-11T08:10:00.000000000Z'),
	(11,'j-running','','job_started','','job started','','2024-07-12T09:00:00.000000000Z'),
	(12,'j-running','i-running-2','item_progress','downloading','','{"id":"i-running-2","progress":{"download":0.5}}','2024-07-12T09:00:40.000000000Z'),
	(13,'j-queued','','job_queued','','job queued','','2024-07-12T10:00:00.000000000Z'),
	(14,'j-cancelled','','job_queued','','job queued','','2024-07-12T11:00:00.000000000Z'),
	(15,'j-cancelled','','job_cancelled','','job cancelled','','2024-07-12T11:00:30.000000000Z'),
	(16,'j-retried','','job_started','','job started','','2024-07-10T12:00:01Z'),
	(17,'j-retried','','job_failed','','failed','','2024-07-10T12:00:01.1Z'),
	(18,'j-retried','','job_retried','','retry','','2024-07-10T12:00:01.2Z'),
	(19,'j-retried','','job_started','','job started','','2024-07-10T12:00:01.5Z'),
	(20,'j-retried','','job_finished','','completed','','2024-07-10T12:00:02Z'),
	(21,'j-corrupt','','job_finished','','completed','','2024-07-13T00:00:00.000000000Z'),
	(22,'j-gone','','job_deleted','','','','2024-07-13T01:00:00.000000000Z'),
	(23,'j-refailed','','job_started','','job started','','2024-07-14T13:00:00Z'),
	(24,'j-refailed','','job_failed','','failed','','2024-07-14T13:00:05Z'),
	(25,'j-refailed','','job_retried','','retry','','2024-07-14T13:00:05.1Z'),
	(26,'j-refailed','','job_started','','job started','','2024-07-14T13:00:05.2Z'),
	(27,'j-refailed','','job_failed','','failed again','','2024-07-14T13:00:05.5Z');

INSERT INTO library_sync_anchor(position,song_id,song_name,album_name) VALUES
	(0,'i.song1','Newest','Album N'),
	(1,'i.song2','Older','Album O');
`

func create1xDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "amdl.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	for _, script := range []string{schema1x, rows1x} {
		if _, err := raw.Exec(script); err != nil {
			t.Fatalf("build 1.x database: %v", err)
		}
	}
	return path
}

type tableDump map[string]map[string]any // primary key -> column -> value

func dumpTable(t *testing.T, database *sql.DB, table, key string) tableDump {
	t.Helper()
	rows, err := database.Query(`SELECT * FROM ` + table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	out := tableDump{}
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		row := map[string]any{}
		for i, col := range cols {
			row[col] = values[i]
		}
		out[fmt.Sprint(row[key])] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func dumpAll(t *testing.T, database *sql.DB) map[string]tableDump {
	t.Helper()
	return map[string]tableDump{
		"jobs":                dumpTable(t, database, "jobs", "id"),
		"job_items":           dumpTable(t, database, "job_items", "id"),
		"job_events":          dumpTable(t, database, "job_events", "id"),
		"library_sync_anchor": dumpTable(t, database, "library_sync_anchor", "position"),
	}
}

func isTimestampColumn(col string) bool {
	return strings.HasSuffix(col, "_at")
}

// assertRowsSurvived checks that every row of before is still in after with
// the same value in every column after still has. Timestamps may have been
// rewritten to the fixed-width form, so they compare as instants.
func assertRowsSurvived(t *testing.T, table string, before, after tableDump, skip func(key, col string) bool) {
	t.Helper()
	for key, oldRow := range before {
		newRow, ok := after[key]
		if !ok {
			t.Errorf("%s row %s was lost", table, key)
			continue
		}
		for col, oldValue := range oldRow {
			newValue, kept := newRow[col]
			if !kept || (skip != nil && skip(key, col)) {
				continue
			}
			if isTimestampColumn(col) {
				oldTime, newTime := parseTime(fmt.Sprint(oldValue)), parseTime(fmt.Sprint(newValue))
				if oldTime.IsZero() || !oldTime.Equal(newTime) {
					t.Errorf("%s %s.%s = %v, want the instant %v", table, key, col, newValue, oldValue)
				}
				continue
			}
			if !reflect.DeepEqual(oldValue, newValue) {
				t.Errorf("%s %s.%s = %#v, want %#v", table, key, col, newValue, oldValue)
			}
		}
	}
}

func columnsOf(t *testing.T, database *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := database.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out[name] = true
	}
	return out
}

func indexExists(t *testing.T, database *sql.DB, name string) bool {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}

func userVersion(t *testing.T, database *sql.DB) int {
	t.Helper()
	version, err := schemaVersion(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
