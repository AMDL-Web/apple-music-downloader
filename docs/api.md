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
| 400 | `invalid_request` (query or body validation, a malformed stream cursor), `invalid_json` (the body is not well-formed JSON) |
| 401 | `authentication_failed` |
| 404 | `route_not_found`, `job_not_found`, `login_not_found`, `account_not_found` |
| 405 | `method_not_allowed`, with an `Allow` header |
| 409 | `job_active`, `job_not_retryable`, `job_finalizing`, `duplicate_active`, `already_logged_in`, `login_pending` |
| 413 | `request_too_large` (JSON bodies are capped at 1 MiB) |
| 422 | `invalid_overrides`, `config_key_not_mutable`, `config_key_pinned`, `invalid_config`, `no_urls_accepted` |
| 500 | `internal_error`, `store_error` (the job store failed; retrying may succeed) |
| 502 | `upstream_error` |
| 503 | `queue_full`, `wrapper_unavailable`, `feature_unavailable` |
| 504 | `upstream_timeout` |

An unknown path under `/api/` answers `404 route_not_found`; a path that exists with other
methods answers `405 method_not_allowed`. `feature_unavailable` means the component behind
the route (log buffer, wrapper client, quality probe, catalog client, library watcher) is
not wired into this process; the log routes also answer it when
`logging.buffer_size` is `0`. A job lookup answers `404 job_not_found` only
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
  feed without a cursor: that replays the whole retained milestone history.
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
- `simulate` (`simulate.enabled`), `signing_mode` (`local` when the three
  `catalog.apple_music_*` keys are set, else `legacy`), `motion_artwork_enabled`,
  `default_storefront` (`catalog.default_storefront`, a startup key). The API refuses a
  link without a storefront in its path (`invalid_url`); a client that accepts such links, like the
  web console, completes them with this value.
- `jobs`: `workers` (`download.max_running_jobs` at startup), `active`, `queue_depth` of
  `queue_capacity` (a full queue rejects submissions with `queue_full`), and
  `recovery_backlog`, the jobs resumed at startup that still wait for a worker.
- `pools`: the startup sizes of the download, decrypt and wrapper-request pools.
- `tools`: each external command (`ffmpeg`) with `available` and its resolved `path`, or
  the lookup `error`.
- `storage`: `free_bytes` and `total_bytes` of the file systems holding
  `download.downloads_dir` and `download.temp_dir`, or an `error` when a directory
  cannot be inspected (it does not exist yet, or the platform is not Linux, macOS or
  FreeBSD).

No secret is returned; `signing_mode` only says whether signing keys are configured.

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
`GET /logs` and `GET /logs/stream` answer `503 feature_unavailable`; read the console or
file output instead. File output is off by default and rotates by
`max_size_mb` / `max_backups` / `max_age_days`, optionally gzipped. `logging.level` and
`logging.access_log` are runtime-mutable; format, destination, buffer size and rotation
need a restart.

## Wrapper

```bash
curl http://localhost:18080/api/v1/wrapper/status
```

Returns `ready`, `status`, `regions`, `client_count` and `accounts` (the logged-in account
identifiers, `[]` when none). The gRPC connection is lazy — the backend starts fine without
a reachable wrapper, and this endpoint is how you verify connectivity; an unreachable
wrapper is `503 wrapper_unavailable`.

```bash
curl -X POST http://localhost:18080/api/v1/wrapper/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"apple-id@example.com","password":"password"}'
```

The username is trimmed; the password is forwarded exactly as sent, because spaces can be
part of it. If the response contains `"status":"needs_2fa"`, submit the code within 30
seconds using the `login_id` from that response:

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

Login errors: `401 authentication_failed`, `409 already_logged_in` (the account already
has a wrapper), `409 login_pending` (a login for the account, or this session's two-step
code, is already in progress), `404 login_not_found` (the two-step session expired),
`404 account_not_found` (logout of an account with no wrapper), `502 upstream_error`,
`504 upstream_timeout`.

## Downloads

### Create

```bash
curl -X POST http://localhost:18080/api/v1/downloads \
  -H 'Content-Type: application/json' \
  -d '{"urls":["https://music.apple.com/us/album/example/123456789?i=987654321"],
       "overrides":{"force_overwrite":true}}'
```

`urls` is a list; each entry may itself hold several URLs separated by whitespace, commas
or semicolons, up to 100 in all. Each URL is reported back in `results[]` with a `status`
and, unless it was accepted, an `error` object `{code, message, details?}`:

| `status` | `error.code` |
| --- | --- |
| `accepted` | — (the new `job` is included) |
| `invalid` | `invalid_url`, `unsupported_input`, `unsupported_storefront` (`details`: `storefront`, `supported_storefronts`), `decryptor_unavailable`, `invalid_configuration` |
| `duplicate_in_request` | `duplicate_in_request` |
| `duplicate_active` | `duplicate_active` (`existing_job_id` names the running job) |
| `queue_full` | `queue_full` |
| `error` | `store_error`, `internal_error` — the server failed; the same submit may succeed when retried |

The response is `202` with `{accepted, rejected, results}` when at least one URL was
accepted. When none was, it is an error whose `details` is that same body: `500
store_error` if any URL failed in the job store (`500 internal_error` if the only server
failure was an unexpected validator error), else `422 no_urls_accepted`.

For album links carrying `?i=<song_id>`, `catalog.album_track_url_mode` decides the task
type: `song` (default) treats it as a single-song job using the `i` parameter, `album`
ignores `i` and downloads the whole album.

### Job overrides

`overrides` overlays the job-mutable runtime config for that submission only. Omitted
fields keep the global value; a value that fails validation is `422 invalid_overrides`.

| Field | Notes |
| --- | --- |
| `quality_priority`, `codec_alternative`, `memory_mode`, `max_attempts` | Per-batch pipeline behaviour |
| `downloads_dir`, `temp_dir`, `*_path_format` | Per-batch output destination; a template is a relative path inside the downloads folder, with no `..` segment and no `{Var:spec}` other than `:02d` |
| `cover_size`, `cover_format`, `embed_cover`, `save_album_cover`, `save_artist_cover`, `save_playlist_cover` | Artwork; `cover_size` is `{width}x{height}` in pixels, such as `5000x5000` |
| `embed_lyrics`, `save_lyrics_file`, `lyrics_format`, `lyrics_type`, `lyrics_extras` | Lyrics |
| `alac_max_sample_rate`, `alac_max_bit_depth`, `check_integrity` | Audio limits and the integrity gate |
| `force_overwrite` | Overwrite existing output files and their lyrics sidecars instead of skipping |
| `hooks` | Allow-list of hook entry names |
| `media_user_token` | Apple Music subscription token for stations and private playlists |

The request has no top-level `force` or `media_user_token` field; sending one is
`400 invalid_request` (unknown field).

**`force_overwrite`** — `true` overwrites an existing audio file and its lyrics sidecar;
`false` (the default, from `download.force_overwrite`) skips tracks whose output already
exists.

**`hooks`** — values are `name`s from [`configs/hooks.yaml`](../configs/hooks.yaml). Call
`GET /api/v1/hooks` first to build the list; it returns each entry's name, enabled state,
type, events and job types, and never returns URLs, headers, commands or working
directories. Omitting the field keeps the default (every enabled, matching hook may run);
`[]` runs no hooks for this batch; a non-empty list restricts to those names. The list
cannot enable a `enabled: false` entry and never bypasses `events` / `job_types` matching.
Duplicate names behave like one entry; unknown names return `422 invalid_overrides` when
any hook entry is configured. The selection is persisted with the job: the `job_queued`
hook dispatches only on first submission (never on retry or restart recovery), and
terminal-event hooks arriving after a retry or restart stay bound to the same allow-list.

**`media_user_token`** — three-state: omitted uses the global
`catalog.media_user_token` fallback, a non-empty string uses that value for this batch,
and an explicit `""` clears the fallback for this batch.

The token is persisted only onto the jobs that actually need it — stations and private
playlists (`pl.u-xxx`). Songs, albums, artists and other playlists in the same batch do
not store it. It is cleared when a job completes, and retained on failure or cancellation
so a retry can still resolve. It is never echoed back in create, list, detail or SSE
responses. The global fallback in `catalog.media_user_token` does persist to the
config file and may be returned by `GET /api/v1/config`.

### Radio stations

Station links (`https://music.apple.com/us/station/.../ra.xxxx`) are supported when they
resolve to a track list — personalised and curated stations. They need an Apple Music
subscription token, per job via `overrides.media_user_token` or globally via
`catalog.media_user_token`.

```bash
curl -X POST http://localhost:18080/api/v1/downloads \
  -H 'Content-Type: application/json' \
  -d '{"urls":["https://music.apple.com/us/station/example/ra.978194965"],
       "overrides":{"media_user_token":"<your media-user-token>"}}'
```

Live stations such as Apple Music 1 have no static track list and end with an explicit
error. Station tracks come from Apple's rolling "next tracks" list, so one download
captures whatever is offered at that moment rather than a fixed catalogue. Output goes to
its own directory per `download.station_path_format`.

A token also unlocks private-playlist artwork: the public catalog has no cover for a
private playlist, so with a token the backend reads it from the library copy as the user.
Without one the playlist still downloads, just without its cover.

### Query, cancel, retry, delete

```bash
curl http://localhost:18080/api/v1/downloads/{job_id}
```

Returns `{job, items, hooks, event_cursor}`.

```bash
curl 'http://localhost:18080/api/v1/downloads?limit=20&offset=0&status=failed,cancelled&type=album&q=beta&sort=updated_at&order=desc'
```

Parameters: `limit` (default 50, max 200), `offset`, `status`, `type`, `q` (substring of
the title, artist name or input), `sort` (`created_at` | `updated_at`), `order`
(`asc` | `desc`); other parameters are ignored. Jobs that tie on the sort field, such as a batch submitted in one request,
list in submission order whichever `order` is asked for, so a batch reads in paste order.
Multi-value parameters accept repeated keys or comma-separated values. The response adds `status_counts` (for the same
filter), `total`, `limit`, `offset` and the global `event_cursor`.

Every job carries `total_items` (tracks the input resolved to) and four disjoint item
counters, always present: `completed_items` (written by this job), `skipped_items`
(already on disk), `failed_items` and `cancelled_items`. They are derived from the job's
items on every read, so a running job's counters match its `items`. `started_at` is set
when a worker first starts the current run and kept across restarts; a retry starts a
new run and clears it. `finished_at` is set when the job reaches a terminal status and
cleared by a retry. Both are RFC 3339 and absent until set. Forcing an overwrite is `overrides.force_overwrite`.

```bash
curl -X POST http://localhost:18080/api/v1/downloads/{job_id}/cancel
```

```bash
curl -X POST http://localhost:18080/api/v1/downloads/{job_id}/retry
```

Only `failed` and `cancelled` jobs can be retried: otherwise `409 job_not_retryable`.
Finished tracks stay done; failed and cancelled ones run again. A job still finishing
its previous run is `409 job_finalizing` (retry a moment later), one whose input is
already queued or running again is `409 duplicate_active`, and a full queue is
`503 queue_full`.

```bash
curl -X DELETE http://localhost:18080/api/v1/downloads/{job_id}
```

Deletes a terminal job and its records. A job that is queued, running, finalizing or
running a hook is `409 job_active`.

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
(data: `{type, job, event_id, status_counts}`, the job's current snapshot) and
`download_deleted` (`{type, job_id, event_id, status_counts}`); `status_counts` is global.
`event_id` is also the SSE id and only moves forward; a resent message is idempotent. The
feed never ends on its own. It is the only stream that delivers a motion artwork resolved
after a job finished.

Each event is `{id, job_id, item_id?, type, phase?, message?, payload?, created_at}`
(the `Event` schema), and `payload` is a JSON object. Job lifecycle events carry the same
public snapshot fields as the REST `Job`; item events carry the same fields as the REST
`JobItem`, plus event-only fields such as `download_attempts` and `will_retry`. So after
one initial `GET`, a client can merge subsequent events directly — no re-fetch is needed
on `item_completed`, `item_failed`, `item_skipped` or job-terminal events.
`media_user_token` stays hidden here exactly as in every other response.

Event types (the `EventType` enum in the spec; Go constants in `internal/domain`):

| | |
| --- | --- |
| Job lifecycle | `job_queued`, `job_started`, `job_retried`, `job_recovered`, `job_finished`, `job_failed`, `job_cancelled`, `job_deleted` |
| Resolution | `resolved_input`, `motion_artwork_resolved` |
| Item progress | `item_progress` (its `phase` is the item status), `item_completed`, `item_failed`, `item_skipped`, `item_overwrite` |
| Codec and retries | `codec_selected`, `codec_fallback`, `codec_failed`, `operation_retry`, `operation_exhausted`, `operation_recovered`, `alac_repaired` |
| Hooks | `hook_started`, `hook_succeeded`, `hook_failed` |
| Other | `standalone_cover_failed` |

A retry inside a codec attempt is an `operation_*` event whose `phase` is the codec.
`job_deleted` and `motion_artwork_resolved` have no payload. Only the list-level
milestones reach the overview feed: the job lifecycle types, `resolved_input`,
`motion_artwork_resolved`, `item_completed`, `item_skipped` and `item_failed`.
`item_progress` events are kept while a job runs and deleted when it finishes; the
item rows keep their final state.

`download.progress_event_interval_ms` (default 500, range 0–10000) is the minimum gap
between two `item_progress` events for the same item in the same state. It throttles only
the download and decrypt meters; status changes, stage marks and one-shot events always
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
 "genre":"Hip-Hop/Rap","track_count":12,"content_rating":"explicit","apple_digital_master":true}
```

Previews one link without creating a job, so a client can show the cover, title and artist
as soon as a link is pasted. The link is parsed exactly as a submission parses it: `type`
and `id` are what the job would be, and the `?i=` of an album link follows
`catalog.album_track_url_mode` (reported as `track_id` either way). The metadata fields
have the names and meaning of the job fields; `track_count` is the number of songs a
download would contain (absent for artists and stations, whose track lists are not read),
and a station adds `needs_media_user_token`. Each type costs light catalog reads only (an
album or playlist pages through its track list): never the amp-api-only reads, never a
station's next-tracks feed, at most 15 seconds in all.

The optional `media_user_token` overrides `catalog.media_user_token` for this lookup (an
empty string clears it); only a private playlist's cover uses it, and it is never echoed.

| Case | Answer |
| --- | --- |
| `url` missing, unknown body field | `400 invalid_request` |
| Not an Apple Music link the backend parses | `400 invalid_request` |
| A music video or a live radio station | `400 invalid_request`, `details.link` |
| The catalog read fails (Apple's 404 for an unknown id included) | `502 upstream_error`, `details.link` |
| No answer within 15 seconds | `504 upstream_timeout`, `details.link` |
| No catalog client in this process | `503 feature_unavailable` |

`details.link` carries `input`, `type`, `storefront`, `id` and `track_id` of the parsed
link, so the client can still show what was pasted.

## Quality probe

```bash
curl -X POST http://localhost:18080/api/v1/quality \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://music.apple.com/cn/album/1580904295"}'
```

Reports the codecs and audio quality a song, album, playlist, artist or station URL
currently declares in its master playlist, without creating a job. It reuses the download
path's region validation, collection resolution, metadata refresh, retries, concurrency
scheduling, HLS source selection and codec/ALAC filtering rules. Any failure of the lookup,
including a URL it cannot parse, is `502 upstream_error`.

The success path reads one master playlist per track occurrence and summarises every
quality from it; on failure it re-acquires the source per `download.max_attempts`. It does
not read individual media playlists, verify media segments or enter encrypted media
transfer. Region validation requires the wrapper/decryptor to be ready and the URL's
storefront to be among the regions it reports.

All link types return per-track `tracks`; a song returns one element, collections keep
Apple Music's original order and duplicate track occurrences. Stations use the runtime
`catalog.media_user_token`.

## Config and hooks

```bash
curl http://localhost:18080/api/v1/config
curl -X POST http://localhost:18080/api/v1/config/reload
```

`GET /api/v1/config` returns the runtime-mutable keys of the in-memory config and has no
side effects; it does not read the file. Next to `config` and `persisted`, `pinned` lists
the runtime keys an `AMDL_*` variable currently sets, sorted by key, for example
`[{"key":"download.downloads_dir","env":"AMDL_DOWNLOAD_DOWNLOADS_DIR"}]` (`[]` when none):
a `PUT` changing one of them is rejected, so a client should disable those fields. Every
answer that returns the config (`GET`, `PUT`, reload) carries it. `POST /api/v1/config/reload` (or SIGHUP) re-reads
`configs/config.yaml` and applies its runtime keys, then answers like `GET`. When the file
is unreadable or invalid, the previous config stays active and the answer is still `200`,
with `reload_error` saying why.

`PUT /api/v1/config` changes runtime-mutable keys only, but rewrites the entire file
including startup keys — comments and custom formatting are not preserved. It re-reads the
file first, so a hand edit made since the last reload is kept. Errors: `422
config_key_not_mutable` (a startup key), `422 config_key_pinned` (a key set by an
`AMDL_*` variable), both listing the keys in `details.keys` — plain dotted key
names for `config_key_not_mutable`, `"<key> (<ENV_VAR>)"` entries such as
`"download.downloads_dir (AMDL_DOWNLOAD_DOWNLOADS_DIR)"` for `config_key_pinned`;
`422 invalid_config` (the
result fails validation, or the file on disk no longer validates); `400 invalid_request`
for an unknown key. See [configuration.md](configuration.md).

`GET /api/v1/hooks` returns the master switch plus each entry's name, enabled state, type,
events and job types — never URLs, headers, commands or working directories.

## Library sync

```bash
curl http://localhost:18080/api/v1/library-sync
```

Watcher status: enabled flag, interval, anchor entry count and the last pass's result.
It answers even while the feature is disabled, so a client never has to guess.

```bash
curl -X POST http://localhost:18080/api/v1/library-sync/reset
```

Clears the anchor. The next pass re-anchors to the library **as it stands then** and
submits **nothing** — this is "forget history", not "replay history". To re-download an
album, `POST /api/v1/downloads` it. See [automation.md](automation.md).
