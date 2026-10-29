# Upgrading to 2.0

> Type: runbook

2.0 changes the HTTP contract, removes 1.x upgrade shims and migrates the database. The
target design is in [Architecture](architecture.md). The embedded console ships with
the new API; any other client of `internal/api/openapi.yaml`, such as your scripts,
needs the API changes below before it works against 2.0.

## Before you upgrade

1. Stop the container and back up the database file (`database.path`, default
   `data/db/amdl.db`). The first 2.0 start migrates it in place and 1.x cannot open the
   result.
2. If you still have `configs/runtime.yaml`, start 1.x once so it merges the file, or
   copy its keys into `config.yaml` by hand: 2.0 leaves the file alone and never reads
   it.
3. Move every `AMDL_*` variable except `AMDL_CONFIG`, `AMDL_HOOKS_CONFIG`,
   `AMDL_SERVER_LISTEN`, `AMDL_WRAPPER_ADDRESS` and `AMDL_DATABASE_PATH` into
   `config.yaml` (for example `AMDL_SIMULATE_ENABLED=true` becomes `enabled: true` under
   `simulate:`), then unset it: 2.0 ignores the rest and logs a warning naming each one.
4. Remove the keys listed as removed under [Config and environment](#config-and-environment)
   from `config.yaml` if present, and the removed keys under [Hooks](#hooks) from
   `hooks.yaml` (`timeout_seconds` and `max_concurrent` at the top level,
   `send_payload` and `timeout_seconds` in entries): 2.0 rejects them at startup.
5. If `config.yaml` omits any key in the defaults table below, decide whether you want
   the new default or should write the old value into the file.
6. Give the container time to stop: `stop_grace_period: 2m` in Compose or
   `--stop-timeout 120` with `docker run`. Interrupted jobs now resume on the next start
   instead of being cancelled.
7. Replace wrapper-manager with
   [wrapper-lite](https://github.com/WorldObservationLog/wrapper/tree/lite), logged in
   with its `--login`, and set `wrapper.address` (or `AMDL_WRAPPER_ADDRESS`) to its URL,
   such as `http://127.0.0.1:12340`. 2.0 no longer speaks wrapper-manager's gRPC and
   decrypts media itself; see [Wrapper](#wrapper).

## HTTP API

| Area | 1.x | 2.0 | Affects |
| --- | --- | --- | --- |
| Error body | `{"error": "text"}` | `{"error": {"code", "message", "details?"}}`; codes are the spec's `ErrorCode` enum (`GET /health` keeps its `503` body) | every client error handler |
| Unknown route, wrong method | `http.ServeMux` plain text `404` / `405` | JSON `404 route_not_found`; `405 method_not_allowed` with an `Allow` header | generic error handlers |
| Job lookup on a store error | `404` (`GET /downloads/{id}`, its stream) | `500 store_error`; `404 job_not_found` only for a missing job | job detail and stream clients |
| Submit result error | `results[].error: "code: message"`, only on some rejections | `results[].error: {code, message, details?}` on every result that is not `accepted`; codes are the spec's `SubmitErrorCode` enum | submit flows |
| Submit store failure | `status: "invalid"` with the raw error text | `status: "error"`, `error.code: "store_error"` (retry the submit) | submit flows, status enums |
| Submit, nothing accepted | `422` with the batch body | `422 no_urls_accepted` (or `500 store_error`) envelope; the batch body is in `error.details` | submit flows |
| Wrapper login already pending | `401` (wrapper-manager's `login already pending` reply) | `409 login_pending`; a two-step code already being verified is also `login_pending` | account screens |
| Wrapper login timeout | `504` | `504 upstream_timeout` | account screens |
| Wrapper login password | trimmed | sent as typed; only an all-whitespace password is `400` | account screens |
| Config `PUT` with an invalid file on disk | `500` | `422 invalid_config` | settings screens |
| Logs with `logging.buffer_size: 0` | `GET /logs` an empty `200`; `GET /logs/stream` live records only | `503 feature_unavailable` on both | log viewers |
| Stream cursor in snapshots | `last_event_id` on `GET /downloads` and `GET /downloads/{id}` | `event_cursor` | stream clients |
| Job counters | `done_items` (completed + skipped), `failed_items` | `completed_items`, `skipped_items`, `failed_items`, `cancelled_items` | job lists, progress bars |
| Job timing | none | `started_at`, `finished_at` | new |
| Job `force` | legacy boolean | removed; use `overrides.force_overwrite` | submit forms |
| Event `payload` | JSON string (parse twice) | JSON object | per-job event consumers |
| WebSocket streams | `/logs/stream/ws`, `/downloads/events/ws`, `/downloads/{id}/events/ws` | removed; use the SSE endpoints | cross-origin clients |
| Stream resume | header only on SSE; `last_event_id` / `after` query on WS and logs | `Last-Event-ID` header or `?cursor=` on every stream (the header wins); a malformed cursor is `400 invalid_request` instead of being read as `0` | stream clients |
| Logs paging | `after`, `next_cursor` | `cursor`, `next_cursor` | log viewers |
| Per-job stream already complete | `409` | `204 No Content` | stream clients |
| Never-emitted event types | `codec_retry`, `codec_exhausted`, `codec_recovered` documented | removed from the docs; codec retries are `operation_*` with `phase` = codec | event handlers |
| Wrapper status | `accounts_supported`, `accounts` omitted when empty, `regions` `null` when empty | `accounts_supported` removed; `accounts` and `regions` always arrays (possibly empty) | account screens |
| Wrapper client count | `client_count` on `GET /wrapper/status` (wrapper-manager's count of connected clients) | removed; the response is `ready`, `status`, `regions`, `accounts` | account screens that showed the count |
| `GET /config` | re-read the file and applied it | pure read; use `POST /config/reload` | settings screens |
| Log records | `source` (file and line) when `logging.include_source` was on | no `source` field | log viewers |
| `PUT /config` with a removed key | `logging.format`, the log file keys, `catalog.requests_per_second`, `catalog.request_burst`, `download.max_parallel_decrypts`, `download.max_parallel_wrapper_requests` were startup keys (`422 config_key_not_mutable`) | `400 invalid_request` naming the unknown field, like any removed key | settings screens |
| Env-pinned keys | `GET /config` listed the runtime keys an `AMDL_*` variable set in `pinned`; `PUT /config` refused them with `422 config_key_pinned` | `pinned`, the `PinnedKey` schema and the `config_key_pinned` code are gone: no environment variable can set a runtime key any more | settings screens |
| Developer token | `GET /developer-token` signed a token for other clients in local signing mode (`409 signing_disabled` in legacy mode) | removed (`404 route_not_found`), and the `signing_disabled` code with it; the backend signs only its own catalog token | browser players that fetched a token |
| API reference | Swagger UI at `/docs` (from unpkg.com) and the spec at `/api/openapi.yaml` | neither is served (`404`); read `internal/api/openapi.yaml` and `docs/api.md` in the repository | API explorers, client generators that fetched the spec |
| CORS | `Access-Control-Allow-Origin: *` on every response; `OPTIONS` answered `204` | no CORS headers; `OPTIONS` under `/api/` is `405 method_not_allowed`. Only same-origin pages (the console) and non-browser clients can call the API | web clients on another origin, such as amdl-web |
| Request id | a well-formed incoming `X-Request-ID` was adopted and echoed | always generated by the server; read it from the response to filter `GET /logs?request_id=` | scripts that set their own id |
| `GET /downloads` filters | `storefront`, `created_after`, `created_before`, `updated_after`, `updated_before` | removed and ignored like any unknown parameter: the response is no longer narrowed by them (`status`, `type` and `q` still filter) | scripts that filtered by storefront or date |
| Submit `overrides` | overlaid the whole job-mutable `download` section (directories, path templates, lyrics, covers, `memory_mode`, `max_attempts`, `check_integrity`) plus `media_user_token` and a `hooks` allow-list | only `quality_priority`, `codec_alternative`, `alac_max_sample_rate`, `alac_max_bit_depth`, `force_overwrite`; any other field is `400 invalid_request` naming it. Set the rest in the runtime config; every enabled matching hook runs | scripts that sent per-batch folders, lyrics or cover settings, a token or a hook list |
| Media-user-token per request | `overrides.media_user_token` on `POST /downloads` and `media_user_token` on `POST /lookup` overlaid `catalog.media_user_token` (`""` cleared it) | removed (`400 invalid_request` naming the field); downloads and lookups always use `catalog.media_user_token` | scripts that passed a token per request |
| Job `overrides` in responses and events | could hold every field above; the token was redacted | holds only the five fields above | job detail clients |
| Spec version | `info.version: 1.0.0` | `2.0.0`; the URL prefix stays `/api/v1` | |
| New | | `GET /system` (with `default_storefront`), `GET /stats`, `POST /config/reload`, `POST /lookup` (a link preview that creates no job), web console at `/console/` (`/` redirects there) | |

## Config and environment

- Environment overrides: 1.x read `AMDL_<SECTION>_<KEY>` for every key. 2.0 reads five
  variables: `AMDL_CONFIG`, `AMDL_HOOKS_CONFIG` and the overrides `AMDL_SERVER_LISTEN`,
  `AMDL_WRAPPER_ADDRESS`, `AMDL_DATABASE_PATH`. Every other `AMDL_*` variable, such as
  `AMDL_SIMULATE_ENABLED`, `AMDL_DOWNLOAD_DOWNLOADS_DIR` or `AMDL_LOGGING_LEVEL`, is
  ignored, and startup logs one warning per variable; set the key in `config.yaml`
  instead. That covers the removed `AMDL_RUNTIME_CONFIG` (a leftover `runtime.yaml` is
  ignored), `AMDL_LISTEN` and `AMDL_WRAPPER_ADDR`.
- SIGHUP no longer reloads the config file; the backend ignores it. Use
  `POST /config/reload` (the console's "Reload from file") after a hand edit;
  `docker compose kill -s SIGHUP amdl-backend` no longer does anything.
- `catalog.media_user_token_priority` is rejected in the file and in a `PUT /config` body
  (`400`).
- Logging writes only to stdout and the in-memory buffer. `logging.format` (always text
  now), `logging.include_source`, and the log file keys `logging.file_enabled`,
  `logging.file_path`, `logging.max_size_mb`, `logging.max_backups`,
  `logging.max_age_days` and `logging.compress` are rejected in the file; delete them and
  collect stdout (`docker logs`) to keep logs. The `./data/logs` mount is no longer used.
- `catalog.requests_per_second` (10), `catalog.request_burst` (16) and
  `download.max_parallel_wrapper_requests` (24) are fixed at those values, and
  `download.max_parallel_decrypts` at the number of CPUs the backend uses (`GOMAXPROCS`,
  which follows a container CPU limit). All four are rejected in the file; delete them.
  `catalog.max_parallel_requests`, `download.max_parallel_downloads` and
  `download.max_running_jobs` remain keys.
- `catalog.developer_token_ttl_hours` and `catalog.allowed_origins` configured the removed
  `GET /developer-token`. Both are rejected in the file; delete them. The backend's own
  signed token is unaffected.
- Built-in defaults now equal the values in `configs/config.example.yaml`, so a key
  missing from `config.yaml` behaves the way the example documents. The exceptions are
  `download.album_path_format` and `download.artist_path_format`: a config without them
  predates `{SongNumber}` paths, and keeps `{TrackNumber}` so existing files are found.
  Defaults that changed for keys missing from `config.yaml`:

  | Key | 1.x default | 2.0 default |
  | --- | --- | --- |
  | `wrapper.address` | `127.0.0.1:8080` | `http://127.0.0.1:12340` |
  | `catalog.default_storefront` | `us` | `cn` |
  | `catalog.language` | `en-US` | `zh-Hans_CN` |
  | `download.quality_priority` | `[alac, aac]` | `[alac]` |
  | `download.max_running_jobs` | `2` | `3` |
  | `download.max_attempts` | `4` | `3` |
  | `download.save_album_cover`, `save_artist_cover`, `save_playlist_cover` | `false` | `true` |
- A hand edit to `config.yaml` is no longer applied by `GET /config`; send
  `POST /config/reload`. `PUT /config` re-reads the file first, so it keeps hand edits.
- An invalid webhook URL in `hooks.yaml` fails startup instead of failing every run.
- `download.cover_size` must be `{width}x{height}` in pixels. 1.x accepted any string,
  empty included, and silently fell back to the standard sizes when Apple could not
  serve it; 2.0 fails startup and `PUT /config` (`422 invalid_config`). Set it in
  `config.yaml` to a size such as `5000x5000`.
- `download.*_path_format` templates must stay relative to `download.downloads_dir`
  and may carry only the `:02d` format spec. 1.x stored absolute templates, `..`
  segments and other `{Variable:spec}` forms verbatim; expansion sanitized them into
  `_` folders or literal braces rather than escaping the downloads folder, so nothing
  was ever written outside it, but the result never matched the intent. 2.0 fails
  startup and `PUT /config` (`422 invalid_config`) on them. Fix the template to a
  relative path.

## Wrapper

| Area | 1.x | 2.0 | Affects |
| --- | --- | --- | --- |
| Wrapper service | wrapper-manager over gRPC; it decrypted samples | wrapper-lite over HTTP; it returns a key template per key URI and the backend decrypts | deployments |
| `wrapper.address` | gRPC `host:port` | wrapper-lite URL, `http://host:port` or `https://host:port`; anything else, in the file or in `AMDL_WRAPPER_ADDRESS`, fails startup | `config.yaml`, Compose files |
| `wrapper.insecure` | `true` for plaintext gRPC, `false` for TLS | rejected in the file; the URL scheme picks plaintext (`http`) or TLS 1.2+ (`https`) | `config.yaml` |
| `wrapper.timeout_seconds` | bound on a unary RPC and on each decrypt-fragment wait | bound on one wrapper HTTP request; decryption has no wrapper timeout | `config.yaml` |

## Hooks

| Area | 1.x | 2.0 | Affects |
| --- | --- | --- | --- |
| Job counters in the payload | `job.done_items` (completed + skipped), `job.failed_items` | `job.completed_items`, `job.skipped_items`, `job.failed_items`, `job.cancelled_items`, the API's counters | webhook receivers |
| Job counters in the exec environment | `AMDL_DONE_ITEMS`, `AMDL_FAILED_ITEMS` | `AMDL_COMPLETED_ITEMS`, `AMDL_SKIPPED_ITEMS`, `AMDL_FAILED_ITEMS`, `AMDL_CANCELLED_ITEMS` | exec scripts |
| Job timing in the payload | none | `job.started_at`, `job.finished_at` (RFC 3339, absent until set) | new |
| Output paths | as stored, relative when `download.downloads_dir` is (the default `data/downloads`) | absolute, in `items[].output_path` and `AMDL_OUTPUT_PATHS` | scripts and media servers that resolved them |
| Exec environment | the whole backend environment, `AMDL_*` overrides included (for example `AMDL_CATALOG_MEDIA_USER_TOKEN`) | the backend environment without any `AMDL_*` variable, plus the hook variables; pass what a script needs explicitly | exec scripts |
| Failed exec hook message | all of stderr | the last 4 KiB of stderr | hook status views |
| Restart while a job runs | the job ended `cancelled` and fired `job_cancelled` | no hook: the job resumes on the next start and fires its terminal hook when it finishes | `job_cancelled` receivers |
| Hook selection per submission | `overrides.hooks` limited a batch to named entries (`[]` ran none) | removed; every enabled entry whose `events` and `job_types` match runs for every job. `GET /api/v1/hooks` still lists the entries | scripts that turned hooks off for a batch |
| Hook limits | top-level `timeout_seconds` (default 30) and `max_concurrent` (default 2) in `hooks.yaml` | removed, fixed at 30 seconds per attempt and 2 executions at once; a file that still has either fails startup naming it | `hooks.yaml` files that set either |
| Per-entry `timeout_seconds` | overrode the top-level timeout for one hook | removed; every attempt is cut off after 30 seconds; a file that still has it fails startup naming the entry | `hooks.yaml` entries that set it |
| Webhook `send_payload` | `send_payload: false` sent an empty body | removed; the body is always the JSON payload, and a receiver that ignores the body ignores it; a file that still has it fails startup naming the entry | `hooks.yaml` webhook entries that set it |
| Shutdown while a hook retries | slept out every remaining retry | retries stop; an attempt still running after the drain is cancelled and recorded as `hook_failed` | |

## Output paths

Unchanged, with three exceptions that only affect unusual titles:

- Leading dots (and spaces between them) are stripped from path segments, so a title
  such as "...Baby One More Time" is no longer written as a hidden file. Files written
  by 1.x under the dotted name are not recognized and are downloaded again.
- Dots and spaces are trimmed from the end of a segment together: 1.x left a trailing
  space when a name ended in a dot, a space and more dots ("Title . ." became
  "Title . "); 2.0 writes "Title", so such a track downloads again.
- Segments longer than 240 bytes are truncated on a character boundary. 1.x failed
  longer names only after the whole download (file names over 251 bytes, folder names
  over 255); a name between 241 bytes and those limits is now shorter, so that track
  downloads again.

## Downloads

- Cancelling a job ends each of its unfinished items `cancelled`; 1.x left them in
  their last state, such as `downloading`. Items of a running job each get an
  `item_progress` event whose `phase` is `cancelled`; a queued job's leftover items
  (from a run a restart interrupted) are ended in one write before its `job_cancelled`
  event. Job detail views and stream clients see the new terminal item status.
  `POST /api/v1/downloads/{id}/retry` re-queues the unfinished tracks of a cancelled job
  as it does for a failed one.
- Stopping the backend no longer cancels running jobs: they stay `running` and resume
  on the next start (`job_recovered`), keeping finished tracks and, while the temp
  directory survives, partial transfers.
- Media transfers no longer have a 90 s whole-request timeout: a transfer fails only
  when no data arrives for 60 s, so a large Hi-Res track on a slow link finishes.
- A song without an Enhanced HLS manifest falls back to the next codec at once instead
  of after `download.max_attempts` retries, and a failed integrity check reports
  ffmpeg's own message.
- Jobs queued, running or retryable at the upgrade keep the overrides they were
  submitted with, read without the removed fields: they run under the runtime config's
  folders, path templates, lyrics, covers and retry settings, the runtime
  `catalog.media_user_token`, and every enabled hook. A station job submitted with its
  own token needs `catalog.media_user_token` set to run again. Resume scratch that a
  job left under its own former `overrides.temp_dir` is not cleaned up: that job starts
  its tracks again, and the old folder can be deleted by hand.
- Duplicate detection ignores the destination folder now that a submission cannot pick
  one: the same album, song or playlist is one download. A job from before the upgrade
  keeps its old key, so while it is queued or running (also after a retry of a failed
  or cancelled one) it neither blocks a new submission of the same link nor is refused
  because of one. Before retrying such a job, check that the same link is not already
  queued or running.

## Database

The first start applies migrations in one transaction each:

- records the schema version (`PRAGMA user_version`);
- adds `jobs.started_at` / `jobs.finished_at`, backfilled from job events (`started_at`
  from the latest run, after the last retry);
- folds a 1.x `jobs.force = 1` into `overrides.force_overwrite = true`, then drops
  `jobs.force`, `jobs.done_items` and `jobs.failed_items` (counters are derived from
  items);
- indexes `job_events(type, created_at)` for `/stats`;
- deletes `item_progress` events of finished jobs (live progress telemetry; item rows
  keep their final state).

No migration rewrites `jobs.overrides`: a row that still holds a `media_user_token`,
`hooks` or another removed field is read with it ignored, and the job's next terminal
write stores only the fields that remain.
