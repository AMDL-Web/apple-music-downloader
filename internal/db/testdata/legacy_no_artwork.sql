-- A database from a build older than the artwork_url, display-metadata,
-- has_lyrics and per-stage progress columns (single `progress` column).
CREATE TABLE jobs (
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
);
CREATE TABLE job_items (
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
);
INSERT INTO jobs(id,input,type,storefront,canonical_key,force,status,created_at,updated_at)
	VALUES('job-old','https://music.apple.com/cn/song/old/1','song','cn','song:cn:1',0,'completed','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z');
INSERT INTO job_items(id,job_id,adam_id,kind,idx,status,created_at,updated_at)
	VALUES('item-old','job-old','1','song',1,'completed','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z');
