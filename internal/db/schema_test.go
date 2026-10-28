package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"amdl/internal/domain"
)

func TestOpenMigratesExistingSchemaWithoutArtworkColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amdl.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-artwork_url schema as created by older builds.
	stmts := []string{
		`CREATE TABLE jobs (
			id TEXT PRIMARY KEY,
			input TEXT NOT NULL,
			type TEXT NOT NULL,
			storefront TEXT,
			canonical_key TEXT NOT NULL,
			force INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			total_items INTEGER NOT NULL DEFAULT 0,
			done_items INTEGER NOT NULL DEFAULT 0,
			failed_items INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE job_items (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL,
			adam_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			idx INTEGER NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			artist TEXT NOT NULL DEFAULT '',
			album TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			progress REAL NOT NULL DEFAULT 0,
			codec TEXT NOT NULL DEFAULT '',
			retry_kind TEXT NOT NULL DEFAULT '',
			attempt INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 0,
			status_message TEXT NOT NULL DEFAULT '',
			output_path TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`INSERT INTO jobs(id,input,type,storefront,canonical_key,force,status,created_at,updated_at)
			VALUES('job-old','https://music.apple.com/cn/song/old/1','song','cn','song:cn:1',0,'completed','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z');`,
		`INSERT INTO job_items(id,job_id,adam_id,kind,idx,status,created_at,updated_at)
			VALUES('item-old','job-old','1','song',1,'completed','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z');`,
	}
	for _, stmt := range stmts {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	// Open must add the artwork_url columns without touching existing rows,
	// and reopening must be idempotent.
	for i := 0; i < 2; i++ {
		store, err := Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		ctx := context.Background()
		got, err := store.GetJob(ctx, "job-old")
		if err != nil {
			t.Fatal(err)
		}
		if got.ArtworkURL != "" {
			t.Fatalf("legacy job artwork_url = %q, want empty", got.ArtworkURL)
		}
		if got.ArtistURL != "" {
			t.Fatalf("legacy job artist url = %q, want empty", got.ArtistURL)
		}
		if got.ArtistName != "" || got.CuratorName != "" || got.ReleaseDate != "" || got.Genre != "" {
			t.Fatalf("legacy job display metadata = %q/%q/%q/%q, want empty", got.ArtistName, got.CuratorName, got.ReleaseDate, got.Genre)
		}
		if got.ArtworkBgColor != "" || got.ArtworkTextColor1 != "" || got.ArtworkTextColor2 != "" ||
			got.ArtworkTextColor3 != "" || got.ArtworkTextColor4 != "" {
			t.Fatalf("legacy job artwork colors = %q/%q/%q/%q/%q, want empty",
				got.ArtworkBgColor, got.ArtworkTextColor1, got.ArtworkTextColor2, got.ArtworkTextColor3, got.ArtworkTextColor4)
		}
		items, err := store.ListItems(ctx, "job-old")
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].ArtworkURL != "" {
			t.Fatalf("legacy items = %+v, want one item with empty artwork_url", items)
		}
		if items[0].HasLyrics {
			t.Fatalf("legacy item has_lyrics = true, want false for rows created before the column existed")
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// The migrated columns must be writable.
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	job, err := store.GetJob(ctx, "job-old")
	if err != nil {
		t.Fatal(err)
	}
	job.ArtworkURL = "https://is1-ssl.mzstatic.com/image/thumb/Music/old/{w}x{h}bb.jpg"
	job.ArtistName = "Old Artist"
	job.ArtistURL = "https://music.apple.com/cn/artist/old-artist/9"
	job.ReleaseDate = "2020-01-01"
	job.Genre = "Rock"
	job.ArtworkBgColor = "101010"
	job.ArtworkTextColor1 = "fafafa"
	job.ArtworkTextColor2 = "e0e0e0"
	job.ArtworkTextColor3 = "c0c0c0"
	job.ArtworkTextColor4 = "a0a0a0"
	if err := store.UpdateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtworkURL != job.ArtworkURL {
		t.Fatalf("migrated job artwork_url = %q, want %q", got.ArtworkURL, job.ArtworkURL)
	}
	if got.ArtistURL != job.ArtistURL {
		t.Fatalf("migrated job artist url = %q, want %q", got.ArtistURL, job.ArtistURL)
	}
	if got.ArtistName != job.ArtistName || got.ReleaseDate != job.ReleaseDate || got.Genre != job.Genre {
		t.Fatalf("migrated job display metadata = %q/%q/%q, want %q/%q/%q", got.ArtistName, got.ReleaseDate, got.Genre, job.ArtistName, job.ReleaseDate, job.Genre)
	}
	if got.ArtworkBgColor != job.ArtworkBgColor || got.ArtworkTextColor1 != job.ArtworkTextColor1 ||
		got.ArtworkTextColor2 != job.ArtworkTextColor2 || got.ArtworkTextColor3 != job.ArtworkTextColor3 ||
		got.ArtworkTextColor4 != job.ArtworkTextColor4 {
		t.Fatalf("migrated job artwork colors = %q/%q/%q/%q/%q, want %q/%q/%q/%q/%q",
			got.ArtworkBgColor, got.ArtworkTextColor1, got.ArtworkTextColor2, got.ArtworkTextColor3, got.ArtworkTextColor4,
			job.ArtworkBgColor, job.ArtworkTextColor1, job.ArtworkTextColor2, job.ArtworkTextColor3, job.ArtworkTextColor4)
	}
	items, err := store.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	item := items[0]
	item.HasLyrics = true
	if err := store.UpdateItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	items, err = store.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !items[0].HasLyrics {
		t.Fatal("migrated item has_lyrics not persisted by UpdateItem")
	}
}

func TestNormalizeTimestampColumnsOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amdl.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE jobs (
			id TEXT PRIMARY KEY, input TEXT NOT NULL, type TEXT NOT NULL, storefront TEXT,
			title TEXT NOT NULL DEFAULT '', artwork_url TEXT NOT NULL DEFAULT '',
			canonical_key TEXT NOT NULL, force INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL,
			total_items INTEGER NOT NULL DEFAULT 0, done_items INTEGER NOT NULL DEFAULT 0,
			failed_items INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE job_items (
			id TEXT PRIMARY KEY, job_id TEXT NOT NULL, adam_id TEXT NOT NULL, kind TEXT NOT NULL,
			idx INTEGER NOT NULL, title TEXT NOT NULL DEFAULT '', artist TEXT NOT NULL DEFAULT '',
			album TEXT NOT NULL DEFAULT '', artwork_url TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
			progress REAL NOT NULL DEFAULT 0, codec TEXT NOT NULL DEFAULT '',
			bit_depth INTEGER NOT NULL DEFAULT 0, sample_rate INTEGER NOT NULL DEFAULT 0,
			bitrate INTEGER NOT NULL DEFAULT 0, retry_kind TEXT NOT NULL DEFAULT '',
			attempt INTEGER NOT NULL DEFAULT 0, max_attempts INTEGER NOT NULL DEFAULT 0,
			status_message TEXT NOT NULL DEFAULT '', output_path TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE job_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT, job_id TEXT NOT NULL, item_id TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL, phase TEXT NOT NULL DEFAULT '', message TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
		);`,
		// Legacy variable-width RFC3339Nano (whole second + trimmed millis).
		`INSERT INTO jobs(id,input,type,storefront,canonical_key,force,status,created_at,updated_at)
			VALUES('j1','u','song','us','k',0,'completed','2024-07-10T12:00:00Z','2024-07-10T12:00:00.123Z');`,
		`INSERT INTO job_items(id,job_id,adam_id,kind,idx,status,created_at,updated_at)
			VALUES('i1','j1','1','song',1,'completed','2024-07-10T12:00:00.5Z','2024-07-10T12:00:00.5Z');`,
		`INSERT INTO job_events(job_id,type,created_at) VALUES('j1','job_finished','2024-07-10T12:00:01Z');`,
	}
	for _, stmt := range stmts {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	checks := []struct {
		query string
		want  string
	}{
		{`SELECT created_at FROM jobs WHERE id='j1'`, "2024-07-10T12:00:00.000000000Z"},
		{`SELECT updated_at FROM jobs WHERE id='j1'`, "2024-07-10T12:00:00.123000000Z"},
		{`SELECT created_at FROM job_items WHERE id='i1'`, "2024-07-10T12:00:00.500000000Z"},
		{`SELECT created_at FROM job_events WHERE job_id='j1'`, "2024-07-10T12:00:01.000000000Z"},
	}
	for _, c := range checks {
		var got string
		if err := store.db.QueryRow(c.query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Fatalf("%s = %q, want %q", c.query, got, c.want)
		}
	}

	// Idempotent: reopen must not fail or change already-normalized rows.
	store.Close()
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var got string
	if err := store.db.QueryRow(`SELECT created_at FROM jobs WHERE id='j1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "2024-07-10T12:00:00.000000000Z" {
		t.Fatalf("after reopen created_at = %q", got)
	}
}

func TestFormatTimeFixedWidth(t *testing.T) {
	exact := time.Date(2024, 7, 10, 12, 0, 0, 0, time.UTC)
	frac := time.Date(2024, 7, 10, 12, 0, 0, 123000000, time.UTC)
	if got, want := formatTime(exact), "2024-07-10T12:00:00.000000000Z"; got != want {
		t.Fatalf("formatTime(exact) = %q, want %q", got, want)
	}
	if got, want := formatTime(frac), "2024-07-10T12:00:00.123000000Z"; got != want {
		t.Fatalf("formatTime(frac) = %q, want %q", got, want)
	}
	// Lexicographic order must match chronological order across the boundary.
	if !(formatTime(exact) < formatTime(frac) && formatTime(frac) < formatTime(exact.Add(time.Second))) {
		t.Fatalf("fixed-width strings are not chronologically ordered: %q %q %q",
			formatTime(exact), formatTime(frac), formatTime(exact.Add(time.Second)))
	}
}

// legacyJobItemsDDL is the job_items schema as it stood before the per-stage
// progress breakdown replaced the single `progress` aggregate.
const legacyJobItemsDDL = `CREATE TABLE job_items (
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
	progress REAL NOT NULL DEFAULT 0,
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
	updated_at TEXT NOT NULL
)`

func TestOpenMigratesLegacyItemProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amdl.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(legacyJobItemsDDL); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	for _, row := range []struct {
		id, status string
		progress   float64
	}{
		{"item-done", string(domain.ItemCompleted), 1},
		{"item-skipped", string(domain.ItemSkipped), 1},
		{"item-partial", string(domain.ItemFailed), 0.4},
		{"item-queued", string(domain.ItemQueued), 0},
	} {
		if _, err := legacy.Exec(`INSERT INTO job_items(id,job_id,adam_id,kind,idx,status,progress,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?)`, row.id, "job-1", "123", "song", 1, row.status, row.progress, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer store.Close()
	items, err := store.ListItems(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.JobItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	if len(byID) != 4 {
		t.Fatalf("got %d items after migration, want the 4 legacy rows preserved", len(byID))
	}
	// A completed row gets the stages it must have passed. Verified stays false:
	// whether the integrity check ran is exactly what the old row no longer says.
	wantDone := domain.ItemProgress{Download: 1, Decrypt: 1, Resolved: true, Remuxed: true, Tagged: true, Saved: true}
	if got := byID["item-done"].Progress; got != wantDone {
		t.Fatalf("completed row progress = %+v, want %+v", got, wantDone)
	}
	// Everything else keeps a zero breakdown — including the skipped row, which
	// under the new model ran no stage at all despite its old progress of 1.
	for _, id := range []string{"item-skipped", "item-partial", "item-queued"} {
		if got := byID[id].Progress; got != (domain.ItemProgress{}) {
			t.Fatalf("%s progress = %+v, want zero", id, got)
		}
	}

	// Idempotent: reopening must not re-run the backfill over rows the current
	// code has since written.
	reset := byID["item-done"]
	reset.Progress = domain.ItemProgress{Download: 1, Resolved: true}
	reset.UpdatedAt = time.Now().UTC()
	if err := store.UpdateItem(context.Background(), reset); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	items, err = reopened.ListItems(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == "item-done" && item.Progress != reset.Progress {
			t.Fatalf("reopen re-ran the backfill: progress = %+v, want %+v", item.Progress, reset.Progress)
		}
	}
}
