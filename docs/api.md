# API

> Type: reference

Examples assume the service listens on `http://localhost:18080`.

- OpenAPI 3.1 spec: [`internal/api/openapi.yaml`](../internal/api/openapi.yaml) in this
  repository; the server does not serve it
- Web console: <http://localhost:18080/console/>; `/` redirects there

The spec is the source of truth for every request and response shape; its `info.version`
is the contract version, while the URL prefix stays `/api/v1`. The web console is the
maintained client and changes together with the API; other clients, the iOS app and
amdl-web included, are not kept in sync. The API sends no CORS headers, so the console,
served from the same origin, is its only browser client; scripts and `curl` call it
directly.

## Errors

Every non-2xx JSON response has one shape:

```json
{"error": {"code": "job_not_found", "message": "job job_… not found"}}
```

Switch on `code`, a stable value of the `ErrorCode` enum in the spec (Go: the constants in
`internal/api/errors.go`). `message` is for humans and may change. `details` is present
only for some codes: on `POST /downloads` it holds the batch body, on a rejected config
write it lists the offending keys, on a failed `POST /lookup` it holds the parsed link.

| Status | Codes |
| --- | --- |
| 400 | `invalid_request` (query or body validation, a malformed stream cursor), `invalid_json` (the body is not well-formed JSON), `invalid_scope` (a reset or check names no valid scope) |
| 401 | `authentication_failed` |
| 404 | `route_not_found`, `job_not_found`, `login_not_found`, `account_not_found`, `catalog_not_found`, `hook_not_found`, `resource_not_found` (no record or pending add) |
| 405 | `method_not_allowed`, with an `Allow` header |
| 409 | `job_active`, `job_not_retryable`, `job_not_cancellable`, `job_not_queued`, `duplicate_active`, `already_logged_in`, `login_pending`, `account_limit_reached`, `hook_read_only`, `apple_library_sync_running`, `run_active`, `main_not_done`, `quality_present`, `check_running` |
| 413 | `request_too_large` (JSON bodies are capped at 1 MiB) |
| 422 | `invalid_overrides`, `config_key_not_mutable`, `invalid_config`, `invalid_hook`, `invalid_hooks_file`, `no_urls_accepted` |
| 500 | `internal_error`, `store_error` (the job store failed; retrying may succeed) |
| 501 | `login_unsupported` |
| 502 | `upstream_error` |
| 503 | `queue_full`, `wrapper_unavailable`, `wrapper_unauthorized` (the wrapper's gateway refused `wrapper.token`), `feature_unavailable`, `apple_library_unavailable`, `downloads_dir_unavailable` |
| 504 | `upstream_timeout` |

An unknown path under `/api/` answers `404 route_not_found`; a path that exists with other
methods answers `405 method_not_allowed`. `feature_unavailable` comes from
`GET /logs` and `GET /logs/stream`, when `logging.buffer_size` is `0` and no log history
is kept, and from the hook routes that write `hooks.yaml` in a process started without
its path. A job lookup answers `404 job_not_found` only
when the job does not exist; a store failure is `500 store_error`, so a client never drops
a job because of a transient error.

The one exception is `GET /api/v1/health`, whose `503` body is
`{"status":"degraded","database_error":"…"}`.

## Streams

Every stream is Server-Sent Events: the per-job events, the overview feed and the log
tail. They share these rules:

- **Resume cursor.** Send the `Last-Event-ID` header or `?cursor=<id>`; when both are set
  the header wins, because `EventSource` sends it on every automatic reconnect while the
  URL still carries the first cursor. A cursor that is not a non-negative integer is
  `400 invalid_request`. The stream replays everything after the cursor, then goes live.
- **Seeding.** `GET /downloads` and `GET /downloads/{id}` return `event_cursor`, read
  before the snapshot, so resuming from it never skips an event. Never open the overview
  feed without a cursor: that replays the whole retained milestone history. Event ids
  only grow in one database, and a cursor past the newest event is accepted and stays
  silent until new ids pass it; a backend whose `event_cursor` is below a client's
  cursor runs on another database, so a reconnecting client compares the two and
  starts over from a snapshot (`internal/client#Client.FollowFeed`; the console does the
  same through `verifyCursor` in `apps/console/static/js/lib/stream.js`).
- **Keepalive.** Every stream sends a `: keepalive` comment every 15 seconds and the
  `X-Accel-Buffering: no` header, so proxies neither buffer nor time out an idle stream.

Events are named (`event: <type>`), so a browser client uses `addEventListener` with the
name; `onmessage` does not fire for them.

## System

```bash
curl http://localhost:18080/api/v1/health
```

Returns `{"status":"ok"}`, or `503` with `{"status":"degraded","database_error":"…"}`
when the SQLite store cannot be reached. This is what the container health check uses.

```bash
curl http://localhost:18080/api/v1/system
```

Returns what is running and what it has to work with:

- `version`, `commit`, `build_date`: stamped at build time (`make build`, the Docker
  image). A plain `go build` reports `version` `dev` and takes the commit and its time
  from git, if it was built inside a checkout; `go run` leaves both empty.
- `go_version`, `os`, `arch`, `started_at`, `uptime_seconds`.
- `simulate` (`simulate.enabled`), `motion_artwork_enabled`,
  `default_storefront`, the effective value: the default account's region, else the
  `catalog.default_storefront` key (used while no account is known). The API refuses a
  link without a storefront in its path (`invalid_url`); a client that accepts such links, like the
  web console, completes them with this value. Search, artist albums and the album page default to it.
- `jobs`: `workers` (`download.max_running_jobs` at startup), `active`, `queue_depth` of
  `queue_capacity` (a full queue rejects submissions with `queue_full`), and
  `recovery_backlog`, the jobs resumed at startup that still wait for a worker (they do
  not take queue slots and run in queue order like any other job).
- `pools`: the startup sizes of the download, decrypt and wrapper-request pools.
- `tools`: each external command (`ffmpeg`) with `available` and its resolved `path`, or
  the lookup `error`.
- `storage`: the absolute `path`, `free_bytes` and `total_bytes` of the file systems
  holding `download.downloads_dir` and `download.temp_dir`, or an `error` when a directory
  cannot be inspected (it does not exist yet, or the platform is not Linux, macOS or
  FreeBSD).
- `startup`: the configuration this process read when it started, as it runs (a restart
  key saved since then shows here after the next start):
  `config_path` (the absolute path of the config file the file-only keys are edited in, `""`
  when the config is kept in memory only), `wrapper_address` (`wrapper.address`, without
  user info, query or fragment), `listen` (`server.listen`), `database_path`
  (`database.path`, absolute), `log_buffer_size`
  (`logging.buffer_size`), `catalog_language` (`catalog.language`) and
  `max_parallel_requests` (`catalog.max_parallel_requests`, or 1 when it is 0 or less).
- `pending_restart`: the config keys saved with a value the next start applies, as in
  the config answers below; `[]` when nothing waits.

```bash
curl -X POST http://localhost:18080/api/v1/system/restart
```

Answers `202` with `{"restarting": true}`, then stops every component the way a shutdown
does and starts again in the same process, on the same address, from `config.yaml`: the
keys in `pending_restart` apply. Running jobs are interrupted and resume after the
start, as after any restart. The process keeps its start time, so `started_at` and
`uptime_seconds` of `GET /api/v1/system` do not reset. The API does not answer for the few seconds this takes;
poll `GET /api/v1/health`. `amdl serve`, the container and the desktop app restart in
place; a program that embeds the backend without starting it again answers `503
feature_unavailable`.

No secret is returned.

```bash
curl 'http://localhost:18080/api/v1/stats?days=7'
```

Download activity. `days` is 1-90 (default 14); anything else is `400 invalid_request`.

- `totals`: every retained job (`jobs`), its tracks by final status (`tracks_completed`,
  `tracks_skipped` for files already on disk, `tracks_failed`), the bytes written by
  completed tracks and their total playback time (`duration_ms`).
- `daily`: exactly `days` entries, one per UTC date, oldest first, ending today. Each has
  `tracks_completed`, `bytes_written`, `jobs_completed` and `jobs_failed`; a quiet day has
  zeros. A track counts on the day it completed, a job on each day it finished or failed
  (a failed job that is retried and completes counts on both).
- `codecs`: completed tracks of every retained job by `codec`, `bit_depth` and
  `sample_rate`, most first.
- `hooks`: per hook name, `succeeded` and `failed` runs inside the window, and
  `last_failed_at` when one failed.

Deleting a job removes it from every figure. Results are cached for 30 seconds per `days`
value; `generated_at` says when a result was computed.

### Logs

```bash
curl 'http://localhost:18080/api/v1/logs?level=error&limit=100'
curl 'http://localhost:18080/api/v1/logs?level=error&cursor=1234'
```

The first query returns the newest `limit` entries; pass the response's `next_cursor` as
`?cursor=` to page forward. `truncated` says that entries after the cursor were already
evicted from the buffer.

```bash
curl -N 'http://localhost:18080/api/v1/logs/stream?job_id=job_01JZ0000000000000000000000'
```

Each entry is an event named `log` whose `id` is the log sequence. With a cursor the
stream replays every matching entry still buffered; without one, the newest `limit`
(default 500). A client that reads too slowly is disconnected and reconnects from its last
id.

Every HTTP response carries an `X-Request-ID` the server generates; an incoming header of
that name is ignored. `GET /api/v1/logs?request_id=<id>` aggregates that request's access log with the
synchronous job operations it triggered.

In-memory retention is `logging.buffer_size`; at `0` no history is kept and both
`GET /logs` and `GET /logs/stream` answer `503 feature_unavailable`; read stdout
(`logging.console`) instead. The backend writes no log file. `logging.level` and
`logging.access_log` are runtime-mutable; `logging.console` and the buffer size need a
restart.

## Wrapper

```bash
curl http://localhost:18080/api/v1/wrapper/status
```

Returns `ready`, `logged_in`, `regions`, `login_supported` and
`token_set`. `logged_in` is true
when an account is logged in and serves a storefront. `regions` lists each storefront the
wrapper's accounts serve once; the accounts themselves are under
[Accounts](#accounts). `login_supported` is true when the wrapper is a wrapper-manager, or a
wrapper amdl runs on this machine; the three routes below need one of them. The backend
does not contact the wrapper at startup, so it starts fine without a reachable wrapper,
and this endpoint is how you verify connectivity; an unreachable wrapper is
`503 wrapper_unavailable`.

`token_set` says the backend sends `wrapper.token` as a bearer key, for a wrapper behind a
gateway or proxy that checks one ([configuration.md](configuration.md#wrapper)); the key
itself is never in a response. A gateway that answers `401` to it is
`503 wrapper_unauthorized`, here and on every route that asks the wrapper.

No response carries a media-user-token. The backend reads each account's from the wrapper's
`GET /token` ([configuration.md](configuration.md#the-media-user-token)); stations, private
playlist covers, library sync and the Apple library need it.

When amdl runs the wrapper itself (`amdl wrapper install`, [install.md](install.md#the-wrapper)),
the response also carries `managed`: `mode` (`starting`, `serving`, `login`, `stopped` or
`failed`), the QEMU `accel` in use and whether it fell back to TCG (`accel_fallback`), the
`qemu` binary, the installed `guest`, `last_error`, and `login_residue`. While mode is
`starting` and the backend downloads the guest's wrapper-lite payload before the boot,
`download` carries its `done_bytes` and `total_bytes`. A booting or stopped managed
wrapper answers `200` with `ready` false rather than `503`.

```bash
curl -X POST http://localhost:18080/api/v1/wrapper/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"apple-id@example.com","password":"password"}'
```

The username is trimmed; the password is forwarded exactly as sent, because spaces can be
part of it. If the response contains `"status":"needs_2fa"`, submit the code within 60
seconds using the `login_id` from that response. The password is held in memory for that
wait (at most 75 seconds) and, once a code is submitted, until its verification ends: at most
75 seconds plus `wrapper.login_timeout_seconds` (195 seconds by default) after the
`needs_2fa` response. With a wrapper-manager it is never written to disk. A wrong code ends
the login; start again from the password.

With a wrapper amdl runs, login and two-step verification use the guest manager's
HTTP API on loopback. They do not restart the guest or put credentials in boot
arguments; `managed.login_residue` is false for manager guests. A login or code
request is bounded by `internal/wrapperhost#LoginLimit`; the manager's own wait ends
first and answers `504 upstream_timeout`. A client that closes the login or code request
cancels the login: the manager stops it and frees the account, so a retry starts at once.
A new login of an account replaces one that waits for its code. A code that is not six
digits is `400 invalid_request` and leaves the pending login available. While
`amdl wrapper stop` has turned the wrapper off, login is `503 wrapper_unavailable`.
The local manager holds at most ten accounts. Local logout resolves the supplied
account name to its manager instance id and removes only that account. Updating a bare-lite guest preserves its disk and
requires login again ([Install](install.md#a-wrapper-amdl-runs)).

```bash
curl -X POST http://localhost:18080/api/v1/wrapper/login/{login_id}/2fa \
  -H 'Content-Type: application/json' \
  -d '{"two_step_code":"123456"}'
```

```bash
curl -X POST http://localhost:18080/api/v1/wrapper/logout \
  -H 'Content-Type: application/json' \
  -d '{"username":"apple-id@example.com"}'
```

The body names the account by `username` or by `account_id` (an [account](#accounts)'s id),
exactly one of them. The answer is `status` `logged_out` with the field you sent. amdl keeps
the account's row, which becomes `missing`; `DELETE /accounts/{account_id}` removes it.

Login errors: `401 authentication_failed`, `409 already_logged_in` (the account already
has a wrapper), `409 account_limit_reached` (a wrapper-manager holds at most 10 accounts,
logins in flight included), `409 login_pending` (a login for the account, or this session's two-step
code, is already in progress), `404 login_not_found` (the two-step session expired),
`404 account_not_found` (logout of an account with no wrapper), `501 login_unsupported`
(the wrapper is a bare wrapper-lite: log in with `wrapper-lite-rootless --login` on its
host), `502 upstream_error`, `504 upstream_timeout`.

## Accounts

amdl serves several Apple Music accounts held by one wrapper-manager (at most 10). Its
`/status` lists them as instances, by instance id, and `GET /token?instance=<id>` hands
over each one's media-user-token. A wrapper that lists none (a bare wrapper-lite) counts as one implicit account with the id `wrapper`.

```bash
curl http://localhost:18080/api/v1/accounts
```

```json
{"accounts":[{"id":"6f1c…","name":"a@icloud.com","region":"us","position":0,"default":true,
  "library_sync":true,"apple_library":true,"state":"active",
  "added_at":"2026-11-02T08:30:00Z","seen_at":"2026-11-03T01:10:00Z"}]}
```

The list is sorted by `position`; position 0 is the default account, which reads records
with no pinned storefront and links whose storefront no account serves. `name` is the
Apple ID, `""` when the wrapper does not say; `region` is the storefront, `""` while
unknown. `state` is `active` or `missing` (the wrapper no longer reports it; its row
and switches stay). The route makes no wrapper request: amdl reads the wrapper's accounts
at start, every 10 minutes and after a login, logout or delete, so refetch a second or two
after a login. A new account starts with `library_sync` and `apple_library` off; the one
that adopts the library state of a database from before accounts takes `library_sync` from
`library_sync.enabled` and `apple_library` on.

```bash
curl -X PATCH http://localhost:18080/api/v1/accounts/6f1c… \
  -d '{"library_sync":true,"apple_library":true,"position":0}'
```

Any of `library_sync`, `apple_library` and `position` (an integer from 0), at least one.
`position: 0` makes the account the default. The answer is the updated account;
`404 account_not_found`.

```bash
curl -X DELETE http://localhost:18080/api/v1/accounts/6f1c…
```

Logs the account out of the wrapper and drops its row, its library-sync anchor, its Apple
library cache and its stored token; files, records and runs stay. The answer is
`{"deleted":"<id>","logged_out":true|false}`. `404 account_not_found`; `501
login_unsupported` for the active implicit account of a bare wrapper-lite; wrapper
errors `502` and `503`. Records pinned to the deleted account's storefront read in it
while another account serves it, else they fail with `decryptor_unavailable`.

A link is pinned to its storefront when an account serves that storefront, and read in the
default account's storefront when none does. A record carries that as `pinned_storefront`
(absent: the default account's), a pending add as `storefront`, and a record added by
library sync is pinned to the syncing account's storefront. Add an account with
`POST /wrapper/login` ([Wrapper](#wrapper)).

## Library

amdl keeps a record of each album and catalog playlist it downloads: its tracks, and per
track one row per quality slot. Records, never the disk, say what is downloaded: search,
lookup and the album page read them, a run downloads only the slots its record does not
have, and a file deleted outside amdl stays downloaded until a [disk check](#disk-check)
marks it missing. Each download of a record is a run, which the API calls a job: the
`/downloads` routes list runs, and a job names its record in `resource {kind, id}`.

| Route | What it does |
| --- | --- |
| `GET /library/albums`, `GET /library/playlists` | List records: `state` (`none`, `partial`, `complete`), `flag` (`failed`, `given_up`, `missing`, `waiting`, `paused`, `unresolved`), `source`, `q`, `sort` (`added`, `name`, `artist`, `release`, `activity`), `limit` (max 500), `offset` |
| `GET /library/albums/{id}`, `GET /library/playlists/{id}` | One record with `sources`, `in_apple_library`, `last_checked_at` and `tracks`, each with its `files` |
| `GET /library/summary` | Records per state and flag, pending adds, the last check |
| `POST /library/add` | `POST /downloads` plus `codecs`, `now` and `include` |
| `POST /library/{kind}/{id}/download` | Queue the record's run now; `{codec}` adds another quality, `{now: true}` puts the run first |
| `POST /library/{kind}/{id}/reset` | Download files, lyrics or covers again |
| `POST /library/reset` | Reset every record a filter selects |
| `POST /library/{kind}/{id}/pause`, `/resume` | Stop and continue a record |
| `DELETE /library/{kind}/{id}` | Remove a record and its runs; its files stay |
| `GET /library/pending`, `DELETE /library/pending/{id}`, `POST /library/pending/{id}/retry` | Adds waiting for a catalog read |
| `POST /library/check`, `GET /library/check` | Check the stored files on disk |

`{kind}` is `albums` or `playlists`; another value is `404 resource_not_found`.

```bash
curl 'http://localhost:18080/api/v1/library/albums?flag=given_up,missing&sort=name'
curl http://localhost:18080/api/v1/library/albums/1463409338
```

A record's rollup is stored with it: `tracks_wanted` (wanted tracks its storefront
lists), `tracks_done` (of those, the ones whose main slot is done), `tracks_failed`,
`tracks_given_up`, `tracks_missing`, and `state`, `complete` when every wanted track is
done. `withheld_count` counts tracks of the release its storefront does not list;
they are not counted and not retried. `activity_at` is when one of the record's runs
last changed (its finish, else its last update), or `added_at` while it has none;
`sort=activity` lists the newest first. A track's `files` are its slots: `main`, the first
quality, owns the plain file name, and another quality is a codec slot whose file name
carries a suffix such as ` [EC3 768Kbps]`. Each slot has `audio_state` (`pending`,
`running`, `done`, `failed`, `given_up`, `unavailable`, `missing`), `lyrics_state`, what
landed (`codec`, `quality_label`, `bit_depth`, `sample_rate`, `bitrate`, `file_size`), the
absolute `path`, `failures`, `next_attempt_at`, `last_error` and `last_job_id`.

**Retries.** Inside a run, `download.max_attempts` bounds the tries of each operation. A
run that ends a slot failed counts a failure and the scheduler queues a new run (trigger
`retry`) 30 minutes later, then each wait four times the last; after
`download.give_up_after_runs` (default 3) the slot is `given_up` until a reset. An error
that cannot pass gives up at once. A track its storefront stops listing, and a
codec the track does not offer, is `unavailable` and never retried.

**Download, pause, delete.** `POST /library/{kind}/{id}/download` resumes the record,
sets its failed, given-up and missing slots pending as a retry does, and answers `202
{status: "queued", job}`, or `200 {status: "already_downloaded"}`, or `200 {status:
"waiting"}` when no run takes the record now (a full queue, a record gone from the catalog). With `codec`, each track whose
main slot is done gets that quality as another slot (`409 main_not_done` when none has its
first quality, `409 quality_present` when the first quality is that codec or a file of
unknown quality, such as one amdl adopted at the track's path). Pause cancels
the record's run, keeps its resume checkpoints and stops the scheduler from queuing it;
resume queues its run again. Both answer the record, which names a queued or running run
in `active_job_id`. Delete answers `204`; a record with a queued
or running run is `409 run_active`. The library monitor does not add a deleted album back;
adding it by hand does, and its first run takes a file still at a track's path as that
track's download instead of downloading it again (with `download.overwrite_existing`, it
downloads the track and replaces the file).

**Reset** downloads a record again, as 1.x's `force_overwrite` did for a submit;
`download.overwrite_existing` only decides what a download does with a file it finds at
its path:

```bash
curl -X POST http://localhost:18080/api/v1/library/albums/1463409338/reset -d '{"what":"audio","states":["missing"]}'
curl -X POST http://localhost:18080/api/v1/library/reset -d '{"what":"audio","filter":{"flag":["missing"]}}'
```

`what` is `all` (every slot, failures, the paused flag and the covers), `audio` (the
selected slots, by `slot`, `songs` and `states`; `codecs` with `slot: "main"` sets the main
slots' codec order), `lyrics` (the lyrics of done slots, written into the file and its
sidecars) or `covers`. A done file is downloaded again to its stored path and replaced only
once the new copy is complete. The answer is `{slots, job}`; a record with an active run is
`409 run_active` (pause it first). `POST /library/reset` resets every record its `filter`
(`state`, `flag`, `source`, `q`; at least one) selects, skips those with an active run, and
answers `{reset, skipped_active}`; an audio reset by the `failed`, `given_up` or `missing`
flag re-arms only the slots in that state, so the second line above downloads every
missing file again.

**Pending adds.** A song link without its album, an artist link and a library playlist's
songs need a catalog read before they become records. `POST /downloads` answers them
`pending`; the resolver reads them in the background, and one it gives up on is `failed`
with `last_error` in `GET /library/pending` until a retry or a delete.

### Disk check

```bash
curl -X POST http://localhost:18080/api/v1/library/check
curl -X POST http://localhost:18080/api/v1/library/check -d '{"kind":"albums","id":"1463409338"}'
curl http://localhost:18080/api/v1/library/check
```

Stats the stored path of every done slot (audio files, not sidecars or covers) of the
library or of one record, in the background, and marks the missing ones `missing`. It
never marks a found file, moves a file or queues a download; reset with `states:
["missing"]` to download them again. Two guards: `download.downloads_dir` must exist (`503
downloads_dir_unavailable`), and when more than half the checked files are missing the
check marks nothing and ends with `aborted_reason: "too_many_missing"` unless the body sets
`force: true`, so an unmounted share does not mark a whole library missing. One check runs
at a time (`409 check_running`). `POST` answers `202 {check}`; `GET` answers the running or
last `{check}` (`{id, trigger, scope, started_at, finished_at?, checked, missing,
aborted_reason?}`, `null` before the first; a finished check of a record removed since is
skipped), and the overview feed sends `check_updated` as it moves on.

## Downloads

### Create

```bash
curl -X POST http://localhost:18080/api/v1/downloads \
  -H 'Content-Type: application/json' \
  -d '{"urls":["https://music.apple.com/us/album/example/123456789?i=987654321"],
       "overrides":{"quality_priority":["alac","aac"]}}'
```

`urls` is a list; each entry may itself hold several URLs separated by whitespace, commas
or semicolons, up to 100 in all. Each URL is reported back in `results[]` with a `status`
and, unless it was accepted, an `error` object `{code, message, details?}`:

| `status` | `error.code` |
| --- | --- |
| `accepted` | — (`resource` names the record; `job` is its queued run, absent when the record waits for a retry or the queue is full) |
| `pending` | — (the link is read into records first; no `job` yet) |
| `already_downloaded` | — (`resource` names the record; every wanted track is done, nothing was queued) |
| `invalid` | `invalid_url`, `unsupported_input`, `decryptor_unavailable` (`details`: `storefront`, `supported_storefronts` when the wrapper does not serve the storefront the run reads in), `invalid_configuration` |
| `duplicate_in_request` | `duplicate_in_request` |
| `duplicate_active` | `duplicate_active` (`existing_job_id` names the running job) |
| `queue_full` | `queue_full` |
| `error` | `store_error`, `internal_error` — the server failed; the same submit may succeed when retried |

The response is `202` with `{accepted, rejected, results}` when at least one URL was
taken (`accepted`, `pending` and `already_downloaded` all count). When none was, it is an
error whose `details` is that same body: `500 store_error` if any URL failed in the job
store (`500 internal_error` if the only server failure was an unexpected validator error),
else `422 no_urls_accepted`.

An album link, an album-track link and a catalog playlist (`pl.`) add or extend a
[record](#library), set the failed, given-up and missing slots of what the link names
pending, and queue its run when the record needs one; a record with an active run
answers `duplicate_active` with that run's id. Every download is a new job. A record is
read in its pinned storefront (`pinned_storefront`): the link's when an account serves it,
else the default account's, so a `jp` link downloads the `jp` track list with a `jp`
account and the default account's list without one. The wrapper must serve the pinned
storefront, else the link is `decryptor_unavailable` naming both. A station link is a one-off run read in
its own storefront. An artist link adds every album the artist read returns; `POST
/library/add` with `include: "albums"` leaves singles out.

For album links carrying `?i=<song_id>`, `catalog.album_track_url_mode` decides the task
type: `song` (default) treats it as a single-song job using the `i` parameter, `album`
ignores `i` and downloads the whole album.

### Job overrides

`overrides` overlays the quality keys of the runtime `download` config for the records and
runs the submission adds. Omitted fields keep the global value; a value that fails validation
is `422 invalid_overrides`.

| Field | Notes |
| --- | --- |
| `quality_priority`, `codec_alternative` | Codec order and whether to fall back to the next codec |
| `alac_max_sample_rate`, `alac_max_bit_depth` | ALAC limits |

Everything else comes from the runtime config for every job: directories, path templates,
lyrics, covers, `max_attempts`, `check_integrity`, `integrity_mode`, and the media-user-token
(the wrapper's). Every enabled hook in `hooks.yaml` whose `events` and
`job_types` match runs for every job. Any other field, at the top level (`force`,
`media_user_token`) or inside `overrides` (`force_overwrite`, `hooks`, `downloads_dir`,
`media_user_token`, ...), is `400 invalid_request` naming the unknown field. To download
again, [reset](#library) the record.

The overrides are stored on the record, the latest add's winning until one of its tracks
is done, and copied into each of its runs, so they apply on a retry and after a restart.
An add of a record with done tracks keeps its overrides: another quality of those tracks
is `POST /library/{kind}/{id}/download` with `codec`.
A job row written before the other fields were removed is read with them ignored, and its
terminal write drops them.

### Radio stations

Station links (`https://music.apple.com/us/station/.../ra.xxxx`) are supported when they
resolve to a track list — personalised and curated stations. They need an Apple Music
subscription token, which the backend reads from the wrapper; a wrapper without `GET /token`
gives none, so station links do not download.

```bash
curl -X POST http://localhost:18080/api/v1/downloads \
  -H 'Content-Type: application/json' \
  -d '{"urls":["https://music.apple.com/us/station/example/ra.978194965"]}'
```

Live stations such as Apple Music 1 have no static track list and end with an explicit
error. Station tracks come from Apple's rolling "next tracks" list, so one download
captures whatever is offered at that moment rather than a fixed catalogue. Output goes to
its own directory per `download.station_path_format`, outside the library that songs,
albums and artists share (`download.library_path_format`).

A token also unlocks private-playlist artwork: the public catalog has no cover for a
private playlist, so with a token the backend reads it from the library copy as the user.
Without one the playlist still downloads, just without its cover.

### Query, cancel, retry, move, delete

```bash
curl http://localhost:18080/api/v1/downloads/{job_id}
```

Returns `{job, items, hooks, event_cursor}`. A record's run has one item per planned slot,
with its `slot`. A finished item (`completed` or `skipped_existing`) carries `output_path`, the absolute path of its file on the machine
that runs the backend, fixed when the track was written; the console's Files section
shows the folder it sits in. An item
that is not finished omits it, and the item events on the stream follow the same rule.

```bash
curl 'http://localhost:18080/api/v1/downloads?limit=20&offset=0&status=failed,cancelled&type=album&q=beta&sort=updated_at&order=desc'
```

Parameters: `limit` (default 50, max 200), `offset`, `status`, `type`, `resource_kind`
(`album`, `playlist`) and `resource_id` (one record's runs), `trigger`, `current=true`
(leaves out a run once its record, or a station's link, has a newer run, and a run of a
complete record), `q` (substring of
the title, artist name or input), `sort` (`created_at` | `updated_at` | `queue_order`), `order`
(`asc` | `desc`); other parameters are ignored. `sort=queue_order` lists the queued jobs in
run order (with `order=asc`, the next to start first) ahead of every other job; combine it
with `status=queued` to page through the queue. Jobs that tie on the sort field, such as a batch submitted in one request,
list in submission order whichever `order` is asked for, so a batch reads in paste order.
Multi-value parameters accept repeated keys or comma-separated values. The response adds `status_counts` (for the same
filter), `total`, `limit`, `offset` and the global `event_cursor`.

Every job carries `total_items` (tracks the input resolved to) and four disjoint item
counters, always present: `completed_items` (written by this job), `skipped_items`
(already at the slot's path, or `unavailable`: not offered), `failed_items` and
`cancelled_items`. `resource` names the record a run downloads for (absent for a station),
`trigger` what started it (`manual`, `library_sync`, `artist`, `retry`, `reset`,
`import_1x`) and `priority` its class (`0` download now, `1` manual, `2` the library
monitor's): queued runs start by class, then `queue_order`. They are derived from the job's
items on every read, so a running job's counters match its `items`. `progress` (0 to 1, to
three decimals) is the job's overall progress, the mean of its tracks' stage-weighted
progress as the console's album bar shows it (`internal/progress#Overall`): below 1 while
a track has not finished, 1 once the job completed. It is derived on every read too, and
event payloads leave it out. `started_at` is set
when a worker first starts the current run and kept across restarts; a retry starts a
new run and clears it. `finished_at` is set when the job reaches a terminal status and
cleared by a retry. Both are RFC 3339 and absent until set.
A queued job carries `queue_order`, its place in the run order: when a worker frees up it
starts the queued job with the lowest value, so a client sorts the queue ascending by it.
The values are sparse and only their order means anything; running and finished jobs omit
the field.

```bash
curl -X POST http://localhost:18080/api/v1/downloads/{job_id}/cancel
```

```bash
curl -X POST http://localhost:18080/api/v1/downloads/{job_id}/retry
```

Only `failed` and `cancelled` jobs can be retried: otherwise `409 job_not_retryable`.
Only a `queued` or `running` job can be cancelled: a `completed` or `failed` one is
`409 job_not_cancellable`, and a `cancelled` one answers `200` again. Cancel on a record's
run pauses the record (clients label it Pause); a station's run is cancelled.
A retry is a new job: for a record's run it resumes the record, re-arms its failed,
given-up and missing slots and answers `202 {status: "queued", job}`, `200 {status:
"already_downloaded"}` when every wanted track is done, or `200 {status: "waiting"}` when
slots are left that no run takes now (a full queue, which the scheduler fills later, or a
record gone from the catalog).
For a station it queues the same link again, and a full queue is `503 queue_full`. A record
or link that already has a queued or running job is `409 duplicate_active`.

```bash
curl -X POST http://localhost:18080/api/v1/downloads/{job_id}/move -d '{"before":"<job_id>"}'
```

Changes which queued job starts next. The body sets exactly one of `before` or `after`
(another queued job's id) and `to` (`first` or `last`); anything else is
`400 invalid_request`. The answer is `200 {job}`, the job with its new `queue_order`. A job
that is not queued, or an anchor that is not, is `409 job_not_queued` (the message names
it). A new job, a retried or resubmitted one and a moved one change only their own `queue_order`, so the
overview feed's `download_upserted` for the moved job is all a client needs to re-sort;
when no value is left between the new neighbors, the server renumbers the queue and
records a `job_moved` for every job whose value changed. After a restart, the jobs that
were running go ahead of the queued ones, which keep their order.

```bash
curl -X DELETE http://localhost:18080/api/v1/downloads/{job_id}
```

Deletes a terminal job's history. Its record and files stay. A job that is queued,
running, finalizing or running a hook is `409 job_active`.

## Events

```bash
curl -N 'http://localhost:18080/api/v1/downloads/{job_id}/events?cursor=<event_cursor>'
curl -N 'http://localhost:18080/api/v1/downloads/events?cursor=<event_cursor>'
```

The per-job stream replays the job's events after the cursor, then streams new ones; each
SSE event is named after the event type and its `id` is the event id. It ends once the
job is terminal and every hook has finished. A client that already has everything gets
`204 No Content`, which makes `EventSource` stop reconnecting. A browser reports any
non-200 answer as a generic `error`, so a client whose stream closed refetches
`GET /downloads/{job_id}` to tell "finished" from "broken".

`GET /api/v1/downloads/events` is the cross-job overview feed: it pushes only the
milestones that change how a job appears in the list, so a client can render the whole
downloads page without subscribing to each job. Messages are named `download_upserted`
(data: `{type, job, event_id, status_counts}`, the job's current snapshot),
`download_deleted` (`{type, job_id, event_id, status_counts}`), `resource_updated`
(`{type, resource_kind, resource_id, resource, event_id, status_counts}`, sent right after
the `download_upserted` of a record's run with the record as `GET /library/albums` lists
it) and `check_updated` (`{type, check, event_id, status_counts}`, a disk check that
started, moved on or ended; no event, so it carries the `event_id` already reached);
`status_counts` is global.
`event_id` is also the SSE id and only moves forward; a resent message is idempotent.
A move in the queue is a milestone too: queued jobs sort by `queue_order`, so a client
re-sorts on the upsert.
Progress has no milestone, so every 2 seconds (`internal/api#feedProgressInterval`) the
feed also sends `download_upserted` for each running job whose `progress` moved since it
last sent it, with the `event_id` already reached. The feed never ends on its own. It is the only stream that delivers a motion artwork resolved
after a job finished.

Each event is `{id, job_id, item_id?, type, phase?, message?, payload?, created_at}`
(the `Event` schema), and `payload` is a JSON object. Job lifecycle events carry the same
public snapshot fields as the REST `Job`; item events carry the same fields as the REST
`JobItem`, plus event-only fields such as `download_attempts` and `will_retry`. So after
one initial `GET`, a client can merge subsequent events directly — no re-fetch is needed
on `item_completed`, `item_failed`, `item_skipped` or job-terminal events.

Event types (the `EventType` enum in the spec; Go constants in `internal/domain`):

| | |
| --- | --- |
| Job lifecycle | `job_queued`, `job_started`, `job_recovered`, `job_moved` (new `queue_order`), `job_finished`, `job_failed`, `job_cancelled`, `job_deleted` |
| Resolution | `resolved_input`, `motion_artwork_resolved` |
| Item progress | `item_progress` (its `phase` is the item status), `item_completed`, `item_failed`, `item_skipped`, `item_unavailable` (the slot is not offered), `lyrics_refreshed` (a lyrics reset wrote the lyrics) |
| Codec and retries | `codec_selected`, `codec_fallback`, `codec_failed`, `operation_retry`, `operation_exhausted`, `operation_recovered`, `alac_repaired`, `integrity_overruled` |
| Hooks | `hook_started`, `hook_succeeded`, `hook_failed` |
| Other | `standalone_cover_failed` |
| Not emitted since 2.0 | `job_retried`, `item_overwrite`: a retry is a new job; they stay so 1.x history decodes |

A retry inside a codec attempt is an `operation_*` event whose `phase` is the codec.
`job_deleted` and `motion_artwork_resolved` have no payload. Only the list-level
milestones reach the overview feed: the job lifecycle types, `resolved_input`,
`motion_artwork_resolved`, `item_completed`, `item_skipped`, `item_unavailable`,
`lyrics_refreshed` and `item_failed`.
`item_progress` events are kept while a job runs and deleted when it finishes; the
item rows keep their final state.

`download.progress_event_interval_ms` (default 500, range 0–10000) is the minimum gap
between two `item_progress` events for the same item in the same state. It throttles only
the download and decrypt meters (and `progress.download_bytes`, which moves with the download
meter); status changes, stage marks and one-shot events always
publish immediately. A value held back by the interval is flushed just before whatever
event supersedes it, so the last percentage before a state change is never lost and a
client resuming from a cursor sees the same sequence as a live one. It is read once when
a job starts.

## Link lookup

```bash
curl -X POST http://localhost:18080/api/v1/lookup \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://music.apple.com/us/album/igor/1463409338"}'
```

```json
{"input":"https://music.apple.com/us/album/igor/1463409338","type":"album","storefront":"us",
 "id":"1463409338","title":"IGOR","artist_name":"Tyler, The Creator",
 "artist_url":"https://music.apple.com/us/artist/tyler-the-creator/420368335",
 "artwork_url":"https://is1-ssl.mzstatic.com/image/thumb/…/886447710180.jpg/{w}x{h}bb.jpg",
 "artwork_bg_color":"f1a8bf","artwork_text_color1":"171615","artwork_text_color2":"272021",
 "artwork_text_color3":"423337","artwork_text_color4":"4f3b41","release_date":"2019-05-17",
 "genre":"Hip-Hop/Rap","track_count":12,"content_rating":"explicit","apple_digital_master":true,
 "audio_traits":["lossless","lossy-stereo"],
 "resource":{"kind":"album","id":"1463409338","state":"partial","tracks_done":9,"tracks_wanted":12,
   "withheld_count":0,"wanted_all":true,"paused":false}}
```

Previews one link without creating a job, so a client can show the cover, title and artist
as soon as a link is pasted. The link is parsed exactly as a submission parses it: `type`
and `id` are what the job would be, and the `?i=` of an album link follows
`catalog.album_track_url_mode` (reported as `track_id` either way). The metadata fields
have the names and meaning of the job fields; `track_count` is the number of songs a
download would contain (absent for artists and stations, whose track lists are not read),
a song or album carries Apple's `audio_traits` as search hits do, and a station adds
`needs_media_user_token`. Each type costs light catalog reads only (an
album or playlist pages through its track list): never the animated-cover or Enhanced
HLS reads, never a station's next-tracks feed, at most 15 seconds in all.

A song, album or playlist is read in the default account's storefront, as a download with
no pin is, so its `track_count` is what that download gets and a link the storefront lacks is `404
catalog_not_found`; `storefront` stays the link's. A station is read in its own storefront.
An album, playlist or song amdl keeps a record of carries `resource` (a [record
mark](#record-marks)).

The body is `{url}` only. A private playlist's cover is read with the wrapper's
media-user-token; the token is
never echoed.

| Case | Answer |
| --- | --- |
| `url` missing, unknown body field | `400 invalid_request` |
| Not an Apple Music link the backend parses | `400 invalid_request` |
| A music video or a live radio station | `400 invalid_request`, `details.link` |
| Apple has no resource with the link's id (its 404) | `404 catalog_not_found`, `details.link` |
| The catalog read fails otherwise | `502 upstream_error`, `details.link` |
| No answer within 15 seconds | `504 upstream_timeout`, `details.link` |

`details.link` carries `input`, `type`, `storefront`, `id` and `track_id` of the parsed
link, so the client can still show what was pasted.

## Catalog search

```bash
curl 'http://localhost:18080/api/v1/search?term=Taylor+Swift&storefront=us&limit=2'
```

```json
{"term":"Taylor Swift","storefront":"us",
 "songs":{"items":[{"id":"6814997425","type":"song","name":"Patient Zero",
   "url":"https://music.apple.com/us/song/patient-zero/6814997425",
   "artwork_url":"https://is1-ssl.mzstatic.com/image/thumb/…/{w}x{h}bb.jpg","artwork_bg_color":"2b080c",
   "artist_name":"Taylor Swift","album_name":"The Life of a Showgirl: The Encore",
   "release_date":"2026-09-24","genre":"Pop","duration_ms":225868,"apple_digital_master":true,
   "audio_traits":["atmos","lossless","lossy-stereo","spatial"]}, …],"next_offset":2},
 "albums":{"items":[{"id":"6814997249","type":"album","name":"The Life of a Showgirl: The Encore",
   "url":"https://music.apple.com/us/album/the-life-of-a-showgirl-the-encore/6814997249",
   "artist_name":"Taylor Swift","release_date":"2026-09-24","track_count":16,
   "content_rating":"explicit",
   "resource":{"kind":"album","id":"6814997249","state":"complete","tracks_done":16,
     "tracks_wanted":16,"withheld_count":0,"wanted_all":true,"paused":false}, …}, …],"next_offset":2},
 "playlists":{"items":[{"id":"pl.2553030a768547e9ab3e91230de8439f","type":"playlist",
   "name":"Lo-fi beats to study, work, sleep and relax to",
   "url":"https://music.apple.com/us/playlist/lo-fi-beats-to-study-work-sleep-and-relax-to/pl.2553030a768547e9ab3e91230de8439f",
   "artist_name":"BeatTape", …}, …],"next_offset":2},
 "artists":{"items":[{"id":"159260351","type":"artist","name":"Taylor Swift",
   "url":"https://music.apple.com/us/artist/taylor-swift/159260351","genre":"Pop", …}]}}
```

Searches the Apple Music catalog for songs, albums, playlists and artists, one page per group in
Apple's relevance order. Nothing is downloaded and no job is created.

| Parameter | Meaning |
| --- | --- |
| `term` | Required. Runs of white space count as one space; at most 200 characters. |
| `types` | A subset of `songs`, `albums`, `playlists`, `artists`, comma-separated or repeated; default all four. Groups come back in that order whatever order is given. |
| `storefront` | A two-letter storefront code, normally one a logged-in account serves (`GET /accounts` lists their `region`); each hit's `url` carries it. Default `default_storefront` from `GET /system`. |
| `limit`, `offset` | Hits per group, 1-50 (default 10), and where each group's page starts (default 0). |

Each requested group is there, `items` empty when Apple has nothing, and carries
`next_offset` when Apple has more: pass it as `offset` with that group alone in `types` to
read the next page. Apple reports no totals.

A hit's `url` is ready to submit to `POST /downloads` and downloads exactly that item: a
song's is a `/song/` link, which `catalog.album_track_url_mode` cannot turn into the whole
album. The metadata fields have the names and meaning of the `POST /lookup` fields.
A playlist hit has `type` `playlist`, the curator as `artist_name` and no `track_count`:
the catalog states none. `audio_traits` is Apple's list for the song, album or playlist, such as `lossless`,
`hi-res-lossless`, `atmos` and `spatial`, when the catalog sends one.

A hit amdl keeps a record of carries `resource`, a [record mark](#record-marks): an album's
or playlist's own record, a song's album record with `song_state`. When the store cannot be
read the hits come back unmarked rather than failing the search.

`in_apple_library` says whether the item is in the Apple Music library of the account that serves the search's storefront, else of the default
account ([Apple library](#apple-library)). The search asks Apple for its own mark on songs, albums
and playlists with the media-user-token, at no extra request; where Apple leaves it out,
and always for artists, the local Apple library cache answers once that kind has
completed a full sync. The field is absent when neither can tell. A token Apple refuses
never fails the search: it is asked again without the token. `GET /artists/{id}/albums`
marks its artist and albums from the cache.

### Artist albums

```bash
curl 'http://localhost:18080/api/v1/artists/159260351/albums?storefront=us&limit=25'
```

Returns `storefront`, the `artist` as a search hit, and `albums`, one page of the artist's
albums in the order an artist download takes them, as album hits with `next_offset`,
each with its `resource` as in a search. `limit` is 1-100 (default 25). The
artist and the page are read at once, so every page carries the artist.

| Case | Answer |
| --- | --- |
| No `term`, a bad `types`, `storefront`, `limit` or `offset`, an artist id that is not digits | `400 invalid_request` |
| Apple answers 404 (an unknown artist id) | `404 catalog_not_found` |
| The catalog read fails otherwise (an unknown storefront included) | `502 upstream_error` |
| No answer within 10 seconds | `504 upstream_timeout` |

### Album

```bash
curl 'http://localhost:18080/api/v1/albums/1783644603?storefront=cn'
```

```json
{"storefront":"cn","id":"1783644603","url":"https://music.apple.com/cn/album/1783644603",
 "title":"迷跡波","artist_name":"MyGO!!!!!","artist_url":"https://music.apple.com/cn/artist/mygo/1653702916",
 "artwork_url":"https://is1-ssl.mzstatic.com/image/thumb/…/198704278525_Cover.jpg/{w}x{h}bb.jpg",
 "artwork_bg_color":"4e808e","artwork_text_color1":"030607","artwork_text_color2":"131313",
 "artwork_text_color3":"121e22","artwork_text_color4":"1e282b","release_date":"2023-11-01",
 "genre":"动画","track_count":13,"disc_count":1,"apple_digital_master":true,
 "audio_traits":["hi-res-lossless","lossless","lossy-stereo"],
 "resource":{"kind":"album","id":"1783644603","state":"partial","tracks_done":1,"tracks_wanted":13,
   "withheld_count":0,"wanted_all":true,"paused":false},
 "tracks":[{"id":"1783644604","url":"https://music.apple.com/cn/song/%E8%BF%B7%E6%98%9F%E5%8F%AB/1783644604",
   "disc_number":1,"track_number":1,"name":"迷星叫","artist_name":"MyGO!!!!!","duration_ms":211373,
   "apple_digital_master":true,"audio_traits":["hi-res-lossless","lossless","lossy-stereo"],
   "has_lyrics":true,"state":"done"}, …]}
```

Returns the album's metadata and `tracks`, its songs in disc and track order: the songs an
album download takes, music videos excluded. The metadata fields have the names and meaning
of the `POST /lookup` fields (`track_count` is the length of `tracks`, `disc_count` the
highest `disc_number`), and `url` downloads the whole album when submitted to
`POST /downloads`. A song has `id`, `url` (a `/song/` link that downloads that song alone),
`disc_number` (1 when the catalog sends none), `track_number`, `name` and, when the catalog
has them, `artist_name`, `duration_ms`, `content_rating`, `apple_digital_master`,
`audio_traits` and `has_lyrics`. The whole list comes in one answer; there is no paging.
`storefront` is a two-letter code, default `default_storefront` from `GET /system`.

With `catalog.motion_artwork_enabled` on, the album's animated cover is read alongside it:
`motion_artwork_url`, `motion_artwork_tall_url` and the square cover's palette
(`motion_artwork_bg_color`, `motion_artwork_text_color1` to `4`) have the names and meaning
of the job fields. The read is optional: the fields are absent when the album has no animated
cover, the read fails, or it does not answer within 1.5 seconds of the album's own read, and
the album answers either way.

`resource` is the album's record and each song's `state` its main slot's `audio_state`
there; both are absent when amdl has no record of the album.

### Record marks

`resource` on a search hit, a lookup and the album page is `{kind, id, state, tracks_done,
tracks_wanted, withheld_count, wanted_all, active_job_id?, paused, song_state?}`, read from
the [records](#library) and never from the disk: a file moved away still counts until a disk
check marks it missing. A song is marked with the album record that holds it and its main
slot's state as `song_state`. An object amdl has no record of has no `resource`.

| Case | Answer |
| --- | --- |
| An album id that is not digits, a bad `storefront` | `400 invalid_request` |
| Apple answers 404 (an unknown album id, or one the storefront does not have) | `404 catalog_not_found` |
`wanted_all` is false for an album record of songs added one by one: its `state` and counts
cover those songs, so a `complete` one holds the whole album only when `tracks_done`
reaches the album's track count less `withheld_count`. The console, the desktop app and the
TUI show it as partly downloaded otherwise, and their Download adds the whole album, which
queues a run for the tracks it lacks.
| The catalog read fails otherwise | `502 upstream_error` |
| No answer within 15 seconds | `504 upstream_timeout` |

## Quality probe

```bash
curl -X POST http://localhost:18080/api/v1/quality \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://music.apple.com/cn/album/1580904295"}'
```

Reports the codecs and audio quality a song, album, playlist, artist or station URL
currently declares in its master playlist, without creating a job. It reuses the download
path's region validation, collection resolution, metadata refresh, retries, concurrency
scheduling, HLS source selection and codec/ALAC filtering rules. A song the catalog lists
no Enhanced HLS master for reports every quality with `available: false`. Apple's 404 for
the link's id or a track's is `404 catalog_not_found`; any other failure of the lookup,
including a URL it cannot parse, is `502 upstream_error`.

The success path reads one master playlist per track occurrence and summarises every
quality from it; on failure it re-acquires the source per `download.max_attempts`. It does
not read individual media playlists, verify media segments or enter encrypted media
transfer. Region validation requires the wrapper to be ready and the URL's
storefront to be among the regions it reports.

All link types return per-track `tracks`; a song returns one element, collections keep
Apple Music's original order and duplicate track occurrences. Stations use the wrapper's
media-user-token.

## Config and hooks

```bash
curl http://localhost:18080/api/v1/config
curl -X POST http://localhost:18080/api/v1/config/reload
```

`GET /api/v1/config` returns every key except the file-only `server.listen`,
`database.path` and `tools.ffmpeg`, and has no side effects: runtime keys as they run,
restart keys as saved in the file, and `pending_restart`, the keys whose saved value
differs from the running one (a hand edit of a file-only key included). Next to `config`, `persisted` says whether a
`PUT` is written to the file. `POST /api/v1/config/reload` re-reads `configs/config.yaml`
and applies its runtime keys, then answers like `GET`. When the file is unreadable or
invalid, the previous config stays active and the answer is still `200`, with
`reload_error` saying why.

`PUT /api/v1/config` changes every key `GET` returns and rewrites the entire file —
comments and custom formatting are not preserved. Runtime keys apply at once; restart keys
are saved, listed in the answer's `pending_restart`, and apply on the next start
(`POST /api/v1/system/restart`). It validates the result as a start does, so a saved
value always loads, and re-reads the file first, so a hand edit made since the last
reload is kept. Errors: `422 config_key_not_mutable` (a file-only key, or
`wrapper.address` while this machine runs its own wrapper, with `details.reason`
`managed_wrapper`), listing the dotted key names in `details.keys`;
`422 invalid_config` (the result fails validation, or the file on disk no longer
validates); `400 invalid_request` for an unknown key. See [configuration.md](configuration.md).

`GET /api/v1/hooks` returns the master switch plus each entry's name, enabled state, type,
events and job types. A webhook adds its `method`, `max_attempts`, `origin` (scheme and host)
and `header_names`; its path, query, header values and body, and a command or working
directory, are never returned.

```bash
curl -X PUT http://localhost:18080/api/v1/hooks -d '{"enabled": true}'
curl http://localhost:18080/api/v1/hooks/presets
curl -X PUT http://localhost:18080/api/v1/hooks/ntfy -d '{"preset": "ntfy", "values": {"topic": "amdl-downloads"}}'
curl -X PUT http://localhost:18080/api/v1/hooks/jellyfin \
  -d '{"events": ["job_finished"], "url": "http://jellyfin.local:8096/Library/Refresh",
       "headers": {"Authorization": "MediaBrowser Token=\"KEY\""}}'
curl -X POST http://localhost:18080/api/v1/hooks/jellyfin/test
curl -X DELETE http://localhost:18080/api/v1/hooks/jellyfin
```

The write routes edit `hooks.yaml` in place, keeping its comments and other entries, and
apply to the next job event. `PUT /api/v1/hooks` sets the master switch. `PUT
/api/v1/hooks/{name}` creates or changes a webhook entry with the keys of
[automation.md](automation.md#job-hooks); an omitted field keeps its value, so a client can
change a hook's events without sending its URL and tokens again. `GET /api/v1/hooks/presets`
lists the services a webhook can be built for — ntfy, Bark and Telegram notifications, Plex,
Jellyfin and Emby library refreshes, a custom request — with the fields each asks for and
the events a new hook of it runs on. A write with `preset` and `values` has the backend
build `url`, `method`, `headers` and `body` from them, as each service documents, and
records the preset, which `GET` returns as the entry's `preset`; sending `preset` with any
of those four is `400`, and values that do not build (a required field left empty) are
`422 invalid_hook`. A write of a raw request clears the preset. `POST
/api/v1/hooks/{name}/test` sends it once with a sample payload (a finished album titled
"Test from amdl") and answers `200` with `succeeded` and, when it failed, `error`.
Command hooks are only read: writing, deleting or testing one is `409 hook_read_only`, and
a body with a `type` other than `webhook` is `400 invalid_request`. A result that would not
load is `422 invalid_hook`; a `hooks.yaml` on disk that no longer loads is `422
invalid_hooks_file`, and nothing is written until it is fixed.

## Library sync

```bash
curl http://localhost:18080/api/v1/library-sync
```

Watcher status: `enabled` (the master switch `library_sync.enabled`), `interval_minutes`
and `accounts`, one entry per account, the default first: `account_id`, `name`, `region`,
`enabled` (the account's `library_sync` switch), the anchor entry count and the last
pass's result. An account polls while both switches are on and it is active. It answers
even while the feature is disabled, so a client never has to guess.

```bash
curl -X POST http://localhost:18080/api/v1/library-sync/reset -d '{"account":"6f1c…"}'
```

Clears one account's anchor (`account` is optional; absent is the default account;
`404 account_not_found`). The answer is `account`, `cleared` and the new `status`. The next pass re-anchors to the library **as it stands then** and
submits **nothing** — this is "forget history", not "replay history". To re-download an
album, `POST /api/v1/downloads` it. See [automation.md](automation.md).

## Apple library

The backend keeps a local copy of the Apple Music library of each account whose
`apple_library` switch is on (Apple's "Library": albums, songs, playlists with their tracks,
artists) and serves these routes
from it alone, so browsing makes no Apple request. This is not the download library on
disk. A sync fills the copy (`internal/applelibrary`); it never creates or changes a
download job. It needs the media-user-token ([Wrapper](#wrapper)); without one, or while
Apple refuses it, nothing is synced and the routes serve what is already cached.

```bash
curl 'http://localhost:18080/api/v1/apple-library/albums?sort=added&limit=2'
```

```json
{"kind":"albums","total":342,"offset":0,"synced_at":"2026-11-02T08:30:00Z",
 "items":[{"id":"l.QUFYKuq","catalog_id":"6819973419",
   "url":"https://music.apple.com/cn/album/ghost-sequence-single/6819973419",
   "name":"Ghost Sequence - Single","artist_name":"East Of Eden",
   "artwork_url":"https://is1-ssl.mzstatic.com/…/{w}x{h}bb.jpg","track_count":1,
   "release_date":"2026-10-17","genre":"Rock","date_added":"2026-10-26T15:20:21Z"}, …]}
```

`GET /apple-library/{kind}` lists `albums`, `songs`, `playlists` or `artists` of one account:
`?account=<id>`, absent for the default account, `404 account_not_found` for an unknown one.

| Parameter | Meaning |
| --- | --- |
| `sort` | `added` (default; newest first), `name`, or `artist` (albums and songs). Artists sort by `name` only, their default: Apple gives them no add order. |
| `order` | `asc` or `desc`; default `desc` for `added`, `asc` otherwise. |
| `q` | Keeps items whose name, artist or album contains it (case-insensitive for ASCII letters). |
| `limit`, `offset` | 1-200 (default 50) and where the page starts. |

`id` is Apple's library id (`l.`, `i.`, `p.`, `r.`). `url` downloads the item when
submitted to `POST /downloads`; it and `catalog_id` are absent for an item with no
catalog counterpart (an uploaded or withdrawn song or album, an unshared own playlist,
Favorite Songs). Songs carry `album_name`, `duration_ms`, `track_number` and
`disc_number`; playlists `playlist_kind` (`favorites`, `own`, `editorial`, `replay`,
`shared`), `description` and `last_modified`; an album's `track_count` counts its songs
in the library. An own playlist's `artwork_url` is a direct, expiring link with no
`{w}x{h}`; the others are templates. Before the kind's first full sync, `items` is empty
and `synced_at` absent. `503 apple_library_unavailable` (`details.reason` `no_token` or
`token_refused`) comes only when nothing is cached and no usable token can fill the copy.

`GET /apple-library/playlists/{playlist_id}/tracks` (also with `?account=`) returns a playlist's tracks in order,
`{playlist_id, items: [{position, song_id, catalog_id, url, name, artist_name,
album_name, duration_ms, artwork_url}], total, read_at}`, `404 catalog_not_found` for a
playlist that is not cached. It is how a playlist without a catalog link is shown and its
songs downloaded one by one.

```bash
curl -X POST http://localhost:18080/api/v1/apple-library/sync -d '{"mode":"full","account":"6f1c…"}'
curl http://localhost:18080/api/v1/apple-library/status
```

`POST /apple-library/sync` starts a sync of one account (`account`, absent for the default
account) in the background and answers `202` with that account's entry of the status; its
`current.id` names the sync. `mode` is `full` (default) or `incremental`. A
sync under way answers `409 apple_library_sync_running` with `details.sync_id`; no token,
`503 apple_library_unavailable`. `GET /apple-library/status` answers the two schedule keys
(`sync_interval_minutes`, `full_sync_hours`) and `accounts`, one entry per account, the
default first. An entry has `account_id`, `name`, `region`, `default`, `enabled` (the
account's `apple_library` switch), `available` (and `reason` when false: `no_token`,
`token_refused`, or `disabled` for a switch that is off or an account the wrapper no longer
holds), `state` (`idle`, `running`, `failed`), `current` while a sync
runs (`{id, mode, trigger, started_at, kind, pages_read, requests}`), `last_sync`
(`{id, mode, trigger, started_at, finished_at, ok, error, requests, added, removed,
changed, capped}`), `last_full_sync_at`, `next_sync_at` and `counts` per kind. There is no SSE event for the sync: poll the status every couple of seconds while
`state` is `running`.

How a sync keeps up without a change feed (Apple has none, and no per-song add time):

| Pass | Reads | Apple requests |
| --- | --- | --- |
| Incremental, every `apple_library.sync_interval_minutes` (default 30) | songs and albums newest-first until one already cached, the whole playlist list, the artist total, then the tracks of each playlist whose `last_modified` or Apple version changed | about 5 when nothing changed |
| Full, every `apple_library.full_sync_hours` (default 24), on `POST /sync` and while the copy is empty | every page of every kind (100 per request), the changed playlists' tracks and up to 20 track lists older than 7 days | 1 per 100 items: about 66 for 5,000 songs, 500 albums, 100 playlists and 1,000 artists, plus the track lists |

A kind whose Apple total disagrees with the copy after an incremental pass (something was
removed) is read in full, at most once an hour; otherwise removals show after the next
full sync. A full read removes rows only when every page was read and the total did not
move meanwhile, so a sync cut short removes nothing. Each pass is bounded (2 minutes
incremental, 15 full, page caps) and runs one at a time.

A list that reaches its page cap (30,000 songs, 10,000 albums, 10,000 artists, 1,000
playlists, 5,000 tracks of one playlist) is a partial result for that list only: the
rows read are cached, nothing is removed for it, the sync stays `ok` and finishes the other
lists, and `last_sync.capped` names the list (`songs`, or `playlist_tracks:{library id}`).
In search, a capped kind marks only the hits it has cached as `in_apple_library: true`; it
never claims `false`, since a hit may sit past the cap.
