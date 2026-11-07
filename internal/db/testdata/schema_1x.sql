-- The schema the last 1.x release (a6a6b46) created, copied verbatim from its
-- initSchema. Frozen on purpose: it stands for the databases deployed installs
-- carry, not for anything this package writes. Never edit it.
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
