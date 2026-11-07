-- An older 1.x database whose timestamps use the legacy variable-width
-- RFC3339Nano form (whole second, trimmed fraction).
CREATE TABLE jobs (
	id TEXT PRIMARY KEY, input TEXT NOT NULL, type TEXT NOT NULL, storefront TEXT,
	title TEXT NOT NULL DEFAULT '', artwork_url TEXT NOT NULL DEFAULT '',
	canonical_key TEXT NOT NULL, force INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL,
	total_items INTEGER NOT NULL DEFAULT 0, done_items INTEGER NOT NULL DEFAULT 0,
	failed_items INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE job_items (
	id TEXT PRIMARY KEY, job_id TEXT NOT NULL, adam_id TEXT NOT NULL, kind TEXT NOT NULL,
	idx INTEGER NOT NULL, title TEXT NOT NULL DEFAULT '', artist TEXT NOT NULL DEFAULT '',
	album TEXT NOT NULL DEFAULT '', artwork_url TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
	progress REAL NOT NULL DEFAULT 0, codec TEXT NOT NULL DEFAULT '',
	bit_depth INTEGER NOT NULL DEFAULT 0, sample_rate INTEGER NOT NULL DEFAULT 0,
	bitrate INTEGER NOT NULL DEFAULT 0, retry_kind TEXT NOT NULL DEFAULT '',
	attempt INTEGER NOT NULL DEFAULT 0, max_attempts INTEGER NOT NULL DEFAULT 0,
	status_message TEXT NOT NULL DEFAULT '', output_path TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE job_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT, job_id TEXT NOT NULL, item_id TEXT NOT NULL DEFAULT '',
	type TEXT NOT NULL, phase TEXT NOT NULL DEFAULT '', message TEXT NOT NULL DEFAULT '',
	payload TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
INSERT INTO jobs(id,input,type,storefront,canonical_key,force,status,created_at,updated_at)
	VALUES('j1','u','song','us','k',0,'completed','2024-07-10T12:00:00Z','2024-07-10T12:00:00.123Z');
INSERT INTO job_items(id,job_id,adam_id,kind,idx,status,created_at,updated_at)
	VALUES('i1','j1','1','song',1,'completed','2024-07-10T12:00:00.5Z','2024-07-10T12:00:00.5Z');
INSERT INTO job_events(job_id,type,created_at) VALUES('j1','job_finished','2024-07-10T12:00:01Z');

