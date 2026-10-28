# Upgrading to 2.0

> Type: runbook

2.0 changes the HTTP contract, removes 1.x upgrade shims and migrates the database. The
target design is in [Architecture](architecture.md). Clients that mirror
`internal/api/openapi.yaml` by hand (the iOS app and amdl-web) need the API changes
below before they work against 2.0.

## Before you upgrade

1. Stop the container and back up the database file (`database.path`, default
   `data/db/amdl.db`). The first 2.0 start migrates it in place and 1.x cannot open the
   result.
2. If you still have `configs/runtime.yaml`, start 1.x once so it merges the file, or
   copy its keys into `config.yaml` by hand: 2.0 leaves the file alone and never reads
   it. Unset `AMDL_RUNTIME_CONFIG`: 2.0 fails startup on unknown `AMDL_*` variables.
3. Remove `catalog.media_user_token_priority` from `config.yaml` and the environment if
   present: 2.0 rejects unknown keys at startup.
4. If `config.yaml` omits any key in the defaults table below, decide whether you want
   the new default or should write the old value into the file.
5. Give the container time to stop: `stop_grace_period: 2m` in Compose or
   `--stop-timeout 120` with `docker run`. Interrupted jobs now resume on the next start
   instead of being cancelled.

## HTTP API

| Area | 1.x | 2.0 | Affects |
| --- | --- | --- | --- |
| Error body | `{"error": "text"}` | `{"error": {"code", "message", "details?"}}` | every client error handler |
| Submit result error | `results[].error: "code: message"` | `results[].error: {code, message, details?}` | submit flows |
| Submit, nothing accepted | `422` with the batch body | `422 no_urls_accepted` (or `500 store_error`) envelope; the batch body is in `error.details` | submit flows |
| Stream cursor in snapshots | `last_event_id` on `GET /downloads` and `GET /downloads/{id}` | `event_cursor` | stream clients |
| Job counters | `done_items` (completed + skipped), `failed_items` | `completed_items`, `skipped_items`, `failed_items`, `cancelled_items` | job lists, progress bars |
| Job timing | none | `started_at`, `finished_at` | new |
| Job `force` | legacy boolean | removed; use `overrides.force_overwrite` | submit forms |
| Event `payload` | JSON string (parse twice) | JSON object | per-job event consumers |
| WebSocket streams | `/logs/stream/ws`, `/downloads/events/ws`, `/downloads/{id}/events/ws` | removed; use the SSE endpoints | cross-origin clients |
| Stream resume | header only on SSE; `last_event_id` / `after` query on WS and logs | `Last-Event-ID` header or `?cursor=` on every stream; CORS allows the header | stream clients |
| Logs paging | `after`, `next_cursor` | `cursor`, `next_cursor` | log viewers |
| Per-job stream already complete | `409` | `204 No Content` | stream clients |
| Never-emitted event types | `codec_retry`, `codec_exhausted`, `codec_recovered` documented | removed from the docs; codec retries are `operation_*` with `phase` = codec | event handlers |
| Wrapper status | `accounts_supported`, `accounts` omitted when empty | `accounts_supported` removed; `accounts` always present (possibly empty) | account screens |
| `GET /config` | re-read the file and applied it | pure read; use `POST /config/reload` or SIGHUP | settings screens |
| New | | `GET /system`, `GET /stats`, `POST /config/reload`, web console at `/console/` | |

## Config and environment

- `AMDL_RUNTIME_CONFIG` is gone and fails startup like any unknown `AMDL_*` variable;
  a leftover `runtime.yaml` is ignored.
- `catalog.media_user_token_priority` is rejected in the file, as
  `AMDL_CATALOG_MEDIA_USER_TOKEN_PRIORITY`, and in a `PUT /config` body (`400`).
- Built-in defaults now equal the values in `configs/config.example.yaml`, so a key
  missing from `config.yaml` behaves the way the example documents. The exceptions are
  `download.album_path_format` and `download.artist_path_format`: a config without them
  predates `{SongNumber}` paths, and keeps `{TrackNumber}` so existing files are found.
  Defaults that changed for keys missing from `config.yaml`:

  | Key | 1.x default | 2.0 default |
  | --- | --- | --- |
  | `catalog.default_storefront` | `us` | `cn` |
  | `catalog.language` | `en-US` | `zh-Hans_CN` |
  | `download.quality_priority` | `[alac, aac]` | `[alac]` |
  | `download.max_running_jobs` | `2` | `3` |
  | `download.max_attempts` | `4` | `3` |
  | `download.save_album_cover`, `save_artist_cover`, `save_playlist_cover` | `false` | `true` |
- A hand edit to `config.yaml` is no longer applied by `GET /config`; send SIGHUP or
  `POST /config/reload`. `PUT /config` re-reads the file first, so it keeps hand edits.
- An invalid webhook URL in `hooks.yaml` fails startup instead of failing every run.

## Hooks

- `AMDL_OUTPUT_PATHS` and payload `output_paths` are absolute.
- Exec hooks no longer inherit the backend's `AMDL_*` configuration variables
  (for example `AMDL_CATALOG_MEDIA_USER_TOKEN`); pass what a script needs explicitly.
- Failed exec hooks record at most the last 4 KiB of stderr.

## Output paths

Unchanged, with two exceptions that only affect unusual titles:

- Leading dots are stripped from path segments, so a title such as
  "...Baby One More Time" is no longer written as a hidden file. Files written by 1.x
  under the dotted name are not recognized and are downloaded again.
- Segments longer than 240 bytes are truncated on a character boundary. 1.x could not
  write these files at all.

## Database

The first start applies migrations in one transaction each:

- records the schema version (`PRAGMA user_version`);
- adds `jobs.started_at` / `jobs.finished_at`, backfilled from job events;
- folds a 1.x `jobs.force = 1` into `overrides.force_overwrite = true`, then drops
  `jobs.force`, `jobs.done_items` and `jobs.failed_items` (counters are derived from
  items);
- indexes `job_events(type, created_at)` for `/stats`;
- deletes `item_progress` events of finished jobs (live progress telemetry; item rows
  keep their final state).
