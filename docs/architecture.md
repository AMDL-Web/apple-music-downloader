# Architecture

> Type: reference

How amdl-backend is put together, and the rules each layer keeps. When this page and
the code disagree, the code wins and this page gets fixed in the same commit. Breaking
changes against 1.x are listed in [Upgrading to 2.0](upgrading-to-2.0.md).

## 1. Process and lifecycle

`cmd/amdl-api/main.go` only parses the environment and calls `run(ctx) error`; every
deferred close runs on every exit path, and a startup failure is returned, logged and
turned into exit status 1 by `main`. `run` builds the components, starts them, and on
SIGINT/SIGTERM (or a failing HTTP server) stops them in this order
(`cmd/amdl-api/shutdown.go`):

1. HTTP: stop accepting connections and end every SSE stream at once
   (`internal/api#Server.EndStreams`, so a connected client never holds the drain;
   EventSource reconnects and resumes from `Last-Event-ID`), give ordinary requests a
   short drain, then cancel the base context of whatever is still running.
2. Library watcher and catalog token refresher: stop and wait.
3. Job manager: cancel workers with the cause `jobs.ErrShutdown`. A job interrupted by
   shutdown is **not** cancelled: it stays `running` in the database, keeps its items
   and resume checkpoints, emits no terminal event and fires no hook.
   The next start requeues it through recovery (`job_recovered`).
4. Downloader background work (motion-artwork backfill): wait.
5. Hook dispatcher: stop retries at once, drain running attempts, then cancel what is
   still running (each records `hook_failed`).
6. Wrapper client, then store: close.

Each step waits at most its own named timeout and then logs and moves on, so a stuck
component cannot keep the process alive. The timeouts add up to about 105 s;
`docker-compose.yml` sets `stop_grace_period: 2m` above that, and a test in
`cmd/amdl-api` fails when the sum outgrows it.

SIGHUP reloads the config file and applies its logging level (same as
`POST /api/v1/config/reload`); an invalid file is logged and the previous snapshot stays.

Every long-lived component has the same shape: a constructor that does no I/O, a
`Start` that owns its goroutines or a blocking `Run(ctx)` that the caller runs on its
own goroutine, and a `Shutdown(ctx)` or `Wait(ctx)` (or `Run` itself) that returns only
after they exit or its deadline passes. Nothing outlives `Store.Close` unless its shutdown
step timed out, which is logged as "gave up waiting during shutdown"; a job such a worker
was running stays `running` and resumes on the next start.

## 2. Packages

| Package | Owns | Must not |
| --- | --- | --- |
| `internal/api` | HTTP: route table, handlers per resource, JSON and error envelope, SSE | hold business state, talk to Apple or the wrapper directly |
| `internal/console` | the embedded web console (static files + handler) | call anything but the public `/api/v1` |
| `internal/jobs` | admission, queue, workers, terminal-state rules, hook dispatch trigger | know about HTTP |
| `internal/media` | the per-track pipeline: select, fetch, decrypt, remux, tag, finalize; simulate backend | write job rows except through `jobs.Reporter` |
| `internal/applemusic` | both Apple hosts, tokens, catalog and library reads | fail a job because an amp-api-only field is missing |
| `internal/wrapper` | the wrapper-manager gRPC client (login, status, decrypt sessions) | |
| `internal/db` | SQLite schema, versioned migrations, queries | contain business rules |
| `internal/domain` | shared types, enums and event type constants | import other internal packages |
| `internal/events` | in-process wake signals for streams | carry events (subscribers re-read the store) |
| `internal/config` | config file, env overrides, validation, runtime store | |
| `internal/hooks` | webhook/exec hook execution | |
| `internal/librarysync` | the library watcher | |
| `internal/logging` | slog setup, in-memory ring buffer, log subscriptions | |
| `internal/limits` | semaphores and the Apple request gate | |
| `internal/ids` | random IDs (`job_<32 hex>`, `item_<32 hex>`) | |
| `internal/buildinfo` | version, commit, build date (ldflags), process start time | |

## 3. HTTP API conventions

All JSON endpoints live under `/api/v1`; 2.0 keeps that prefix and sets the spec's
`info.version` to the release version. The contract is `internal/api/openapi.yaml`;
the route table in `internal/api` and the spec are checked against each other in both
directions by a test. The server does not serve the spec, and it sends no CORS headers:
the console, served from the same origin, is the one browser client.

### 3.1 Errors

Every non-2xx JSON response is

```json
{"error": {"code": "job_not_found", "message": "job job_… not found", "details": {}}}
```

`code` is a stable snake_case identifier from the `ErrorCode` enum in the spec (Go:
constants in `internal/api`). `message` is for humans and may change. `details` is
optional and code-specific: the batch body for a submit that accepted nothing, `{"keys":
[…]}` for a rejected config write. Unknown routes under `/api/` answer
`route_not_found` with the same envelope; a path that exists with other methods answers
`405 method_not_allowed` with an `Allow` header (computed from the route table, since
`http.ServeMux` writes its own plain-text 404/405 before any handler runs). The one
non-envelope error is the `503` of `GET /health`, which keeps its `HealthResponse` body
(`{"status": "degraded", "database_error": "…"}`) because monitors read it.

| Status | Codes |
| --- | --- |
| 400 | `invalid_request` (query/body validation, a malformed stream cursor), `invalid_json` |
| 401 | `authentication_failed` (wrapper login) |
| 404 | `route_not_found`, `job_not_found`, `login_not_found`, `account_not_found` |
| 405 | `method_not_allowed` |
| 409 | `job_active`, `job_not_retryable`, `job_finalizing`, `duplicate_active`, `already_logged_in`, `login_pending` |
| 413 | `request_too_large` |
| 422 | `invalid_overrides`, `config_key_not_mutable`, `config_key_pinned`, `invalid_config`, `no_urls_accepted` |
| 500 | `internal_error`, `store_error` |
| 502 | `upstream_error` |
| 503 | `queue_full`, `wrapper_unavailable`, `feature_unavailable` |
| 504 | `upstream_timeout` (wrapper login) |

`feature_unavailable` comes only from `GET /logs` and `GET /logs/stream`: `logging.buffer_size`
is `0`, so no log history is kept. Every other dependency in `internal/api#Deps` is
required, and the process does not start without it.

Per-URL submit results keep the coarse `status` enum (`accepted`, `invalid`,
`duplicate_in_request`, `duplicate_active`, `queue_full`, plus `error` for a server-side
failure) and carry the detail in `results[].error` (`{code, message, details?}`). The
codes are the spec's `SubmitErrorCode` enum: with `invalid`, `invalid_url`,
`unsupported_input`, `unsupported_storefront` (`details` carries the storefront and the
supported ones), `decryptor_unavailable` and `invalid_configuration`; with the status of
the same name, `duplicate_in_request`, `duplicate_active` and `queue_full`; with
`error`, `store_error` and `internal_error` (the validator failed unexpectedly).
`POST /downloads` answers `202` with the batch body when at least one URL was accepted.
When none was, it answers `500 store_error` if any rejection was a store failure,
`500 internal_error` if the only server-side failures came from the validator (the
client should retry either), and `422 no_urls_accepted` otherwise; the batch body is in
`error.details` in every case.

### 3.2 Jobs

- Counters are disjoint and always present: `total_items`, `completed_items` (written by
  this job), `skipped_items` (already on disk), `failed_items`, `cancelled_items`.
  `total_items` is stored when the input resolves; the other four are always derived
  from `job_items` by one store query, including when the manager decides the terminal
  status at finalize. There are no stored per-status counter columns. `done_items` and
  the legacy `force` flag are gone; forcing is `overrides.force_overwrite`.
- `started_at` is set when a worker first starts the current run and kept across
  recovery after a restart; a retry starts a new run and clears it, with `finished_at`.
  `finished_at` is set on the terminal transition. So `finished_at - started_at` is the
  wall time of the last run, including any restart in the middle of it. Both are
  RFC 3339 or absent.
- A user cancel marks every non-terminal item `cancelled`, persisted with a
  non-cancelled context; for a queued job (a recovered one can carry rows from the
  interrupted run) that happens in one write before the terminal one, so the terminal
  event and hook count them. A shutdown leaves items as they are so recovery can resume
  them.
- A panic while processing a job fails that job with `internal error: panic: <value>`
  (retryable); the stack is logged and the worker keeps running.
- `GET /downloads` and `GET /downloads/{id}` return `event_cursor`: pass it as
  `?cursor=` (or `Last-Event-ID`) to the matching stream. It is read before the
  snapshot, so resuming from it never skips an event.

### 3.3 Events and streams

- An event is `{id, job_id, item_id?, type, phase?, message?, payload?, created_at}`.
  `payload` is a JSON **object**, not a string. Event types are the constants in
  `internal/domain` and the `EventType` enum in the spec.
- Streams are Server-Sent Events only. There are no WebSocket twins.
- Every stream resumes from `Last-Event-ID` or `?cursor=<id>`; the header wins when both
  are present. A malformed cursor is `400 invalid_request`.
- Every stream sends `X-Accel-Buffering: no` and a `: keepalive` comment every 15 s.
  That comment interval is separate from the store re-drain ticker that bounds latency
  when a wake signal is dropped; keep both.
- `GET /downloads/events`: named events `download_upserted` / `download_deleted`, `id`
  = event id, `data` = `{type, job?, job_id?, event_id, status_counts}`.
- `GET /downloads/{id}/events`: named event = event type, `id` = event id. When the job
  is terminal and the client has everything, the server answers **204 No Content**,
  which makes `EventSource` close without reconnecting. A browser reports any non-200
  as a generic `error`, so a client that sees its stream close refetches the job to tell
  "finished" from "broken".
- `GET /logs` and `GET /logs/stream` page with `cursor` (log sequence) as well; the list
  returns `next_cursor`. With `logging.buffer_size: 0` both answer
  `503 feature_unavailable`; `buffer_size` is a startup key, so the API decides this
  once, in `internal/api#New`.
- Only milestone events reach the overview feed. `item_progress` events are kept while a
  job runs and pruned when it finishes.

### 3.4 Config

- `GET /config` returns the in-memory snapshot and has no side effects.
- `POST /config/reload` (and SIGHUP) re-reads the file; the response is the same shape as
  `GET`, with `reload_error` set if the file is invalid (the previous snapshot stays).
- `PUT /config` re-reads the file under the same lock before applying the patch, so a
  hand edit made since the last reload is not overwritten.

### 3.5 System and stats

- `GET /system`: build info, uptime, simulate flag, signing mode, job/queue capacity,
  pool sizes, tool availability, free space of `downloads_dir` and `temp_dir`.

  ```json
  {"version": "v2.0.0", "commit": "a6a6b46", "build_date": "2026-10-28T12:00:00Z",
   "go_version": "go1.26.5", "os": "linux", "arch": "amd64",
   "started_at": "2026-10-28T12:00:05Z", "uptime_seconds": 3600,
   "simulate": false, "signing_mode": "legacy", "motion_artwork_enabled": true,
   "jobs": {"workers": 3, "active": 1, "queue_depth": 0, "queue_capacity": 256, "recovery_backlog": 0},
   "pools": {"downloads": 16, "decrypts": 32, "wrapper_requests": 24},
   "tools": [{"name": "ffmpeg", "available": true, "path": "/usr/bin/ffmpeg"}],
   "storage": [{"name": "downloads_dir", "path": "data/downloads", "free_bytes": 1, "total_bytes": 2},
               {"name": "temp_dir", "path": "data/tmp", "error": "statfs data/tmp: no such file or directory"}]}
  ```

  `version`, `commit` and `build_date` are stamped by `-ldflags -X` into
  `internal/buildinfo` (the Makefile from git, the Dockerfile from the `VERSION`,
  `COMMIT` and `BUILD_DATE` build args that the image workflows pass). Without the
  stamp `version` is `dev`, and `commit`/`build_date` come from the VCS settings that
  `go build` records in a git checkout (`build_date` is then the commit time), or stay
  empty. `signing_mode` is `local` when the developer-token signing keys are set
  (`internal/config#CatalogConfig.DeveloperTokenSigningEnabled`), else `legacy`; no key
  material is returned. `jobs` comes from `internal/jobs#Manager.QueueStats`; `workers`
  and `pools` are startup values. `tools` is the `internal/media#ToolChecker` result.
  `storage` comes from `statfs`; when a directory cannot be inspected, its entry carries
  `error` instead of the byte counts. The backend builds only for Linux, macOS and
  FreeBSD (`statfs` here, process groups for exec hooks); release images are Linux.
- `GET /stats?days=N` (1-90, default 14; anything else is `400 invalid_request`).
  `totals` and `codecs` cover every retained job; `daily` and `hooks` cover the last
  `days` UTC calendar days, today included. `daily` has exactly `days` entries, oldest
  first, with zeros for a day without activity: a track counts on the day of its last
  `item_completed` event (its `file_size` is the day's `bytes_written`), a job once per
  `job_finished` or `job_failed` event. `hooks` groups `hook_succeeded`/`hook_failed`
  events by hook name. The numbers cover jobs that still exist, since deleting a job
  deletes its items and events. The window queries use the `job_events(type,
  created_at)` index of migration 2 (`internal/db#Store.Stats`). A result is cached for
  30 s per `days` value (`statsCacheTTL` in `internal/api`); `generated_at` says when it
  was computed.

  ```json
  {"generated_at": "2026-10-28T13:00:00Z", "days": 14,
   "totals": {"jobs": 12, "tracks_completed": 210, "tracks_skipped": 4, "tracks_failed": 1,
              "bytes_written": 9876543210, "duration_ms": 52000000},
   "daily": [{"date": "2026-10-28", "tracks_completed": 21, "bytes_written": 987654321,
              "jobs_completed": 1, "jobs_failed": 0}],
   "codecs": [{"codec": "alac", "bit_depth": 24, "sample_rate": 96000, "tracks": 120}],
   "hooks": [{"name": "notify", "succeeded": 10, "failed": 1, "last_failed_at": "2026-10-28T09:15:00Z"}]}
  ```

## 4. Storage

- Schema changes are ordered migrations gated by `PRAGMA user_version`. Migration 1
  brings any 1.x database to the final 1.x schema: it runs, once, everything 1.x ran on
  every open (`CREATE TABLE IF NOT EXISTS`, the column backfills, progress backfill,
  timestamp normalization). After that, `Open` does nothing schema-shaped.
- Migration 2 is the 2.0 schema: `jobs.started_at`/`finished_at` (backfilled from
  events), drops `jobs.force`, `jobs.done_items` and `jobs.failed_items`, indexes
  `job_events(type, created_at)` and deletes `item_progress` events of terminal jobs.
- Migrations preserve every row of user data (jobs, items, milestone events,
  library-sync anchor). A test builds a database from the frozen 1.x schema SQL, inserts
  rows, migrates, and asserts every row survived and migration 1 changed nothing on a
  database that was already current.
- Timestamps are fixed-width UTC text so lexical order is time order.

## 5. Output paths

Path templates, `{Quality}` labels and lyrics sidecar names are unchanged from 1.x,
because the "already downloaded" check compares paths and any change re-downloads whole
libraries. Path segments are cleaned by `safeName`: reserved characters replaced,
leading and trailing dots and spaces removed, each segment capped at 240 bytes on a
rune boundary.

## 6. Hooks

The hook payload's job carries the API counters of 3.2 (`completed_items`,
`skipped_items`, `failed_items`, `cancelled_items`) and `started_at`/`finished_at`; its
items' `output_path` and `AMDL_OUTPUT_PATHS` are absolute (resolved in
`internal/hooks`, so REST and the database keep the stored form). Exec hooks get the
backend's environment without any `AMDL_*` variable (each is a config key override or a
config file path, some of them secret) plus the documented hook variables, and a failed
one records at most the last 4 KiB of its stderr. Webhook URLs must be absolute
`http`/`https` URLs; an invalid one fails startup naming the entry. Attempts run on a
dispatcher-owned context, so shutdown can stop retries and cancel running attempts.

## 7. Web console

`internal/console` serves a no-build Preact+htm app at `/console/`; `/` redirects there.
It is a client of the public API only. See `internal/console/DESIGN.md`.
