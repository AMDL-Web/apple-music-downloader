# Automation

> Type: reference

Two features act without you asking each time: **job hooks** react to jobs you created,
and **library sync** creates jobs of its own. Both are off by default.

## Job hooks

[`configs/hooks.yaml`](../configs/hooks.yaml) — its comments are the reference; this is the
summary. The file is loaded independently of `config.yaml`, so hooks can be added, removed
or toggled without touching the main configuration. A missing file is not an error; hooks
are simply disabled. **The file is read once at startup — changes need a restart.**

Top-level keys:

| Key | Default | Meaning |
| --- | --- | --- |
| `enabled` | `false` | Master switch. When off, no entry runs regardless of its own flag. |
| `timeout_seconds` | 30 | Default per-hook timeout, used when an entry sets none. |
| `max_concurrent` | 2 | Maximum hook executions running at once across all jobs and entries. |

Each entry in `entries` needs a unique `name`, a `type` (`webhook` or `exec`), and at least
one event from `job_queued`, `job_finished`, `job_failed`, `job_cancelled`. `job_types`
filters by `song`, `album`, `playlist`, `artist`, `station`; empty or omitted matches all.
Every entry whose `enabled`, `events` and `job_types` all match runs concurrently, bounded
by `max_concurrent`.

- `job_queued` fires right after creation, before any track is resolved — the payload has
  no `items` and `total_items` is 0. It **cannot** share an entry with a terminal event;
  put creation and terminal hooks in separate entries, which may point at the same URL or
  command.
- `job_finished` means zero failed items; `job_failed` means at least one failed item or an
  aborted job; `job_cancelled` means cancelled through the API. A job that was running
  when the backend stopped fires none of them then: it resumes on the next start and fires
  its terminal hook when it finishes.

**Webhook entries** POST to `url`, which must be an absolute `http` or `https` URL (an
invalid one fails startup and names the entry), with optional `headers`.
`send_payload: false` sends an empty body, which suits refresh-style endpoints that ignore
it. `max_attempts` counts the first try, and retries wait one second between attempts.

**Exec entries** run `command` through `sh -c` with the JSON payload on stdin, optionally
in `workdir`. The command inherits the backend's environment (`PATH`, `HOME`, `TZ`, …)
**without** any `AMDL_*` variable: those configure the backend, and some, such as
`AMDL_CATALOG_MEDIA_USER_TOKEN`, are secrets. Pass what a script needs explicitly. These
variables are set instead:

```text
AMDL_EVENT            job_queued | job_finished | job_failed | job_cancelled
AMDL_JOB_ID           job ID
AMDL_JOB_TYPE         song | album | playlist | artist | station
AMDL_JOB_STATUS       queued | completed | failed | cancelled
AMDL_JOB_INPUT        original Apple Music URL
AMDL_STOREFRONT       storefront code, e.g. "us", "cn"
AMDL_TOTAL_ITEMS      total track count
AMDL_COMPLETED_ITEMS  tracks this job wrote
AMDL_SKIPPED_ITEMS    tracks already on disk
AMDL_FAILED_ITEMS     failed tracks
AMDL_CANCELLED_ITEMS  tracks cancelled before they finished
AMDL_OUTPUT_PATHS     newline-separated absolute output paths
```

A failing exec hook records the last 4 KiB of its stderr in its `hook_failed` event.

The JSON payload — sent as the webhook body when `send_payload` is true, and written to an
exec hook's stdin — looks like this. The `job` counters are the API's (see
[api.md](api.md#query-cancel-retry-delete)); `error` is present only when the job has one, `started_at` and
`finished_at` only once set, and `output_path` is absolute:

```json
{
  "event": "job_finished",
  "timestamp": "2026-07-04T10:30:00Z",
  "job": {
    "id": "job_abc123",
    "input": "https://music.apple.com/us/album/...",
    "type": "album",
    "storefront": "us",
    "status": "completed",
    "total_items": 12,
    "completed_items": 11,
    "skipped_items": 1,
    "failed_items": 0,
    "cancelled_items": 0,
    "started_at": "2026-07-04T10:28:41Z",
    "finished_at": "2026-07-04T10:30:00Z"
  },
  "items": [
    {
      "id": "item_1",
      "title": "Song 1",
      "artist": "Artist Name",
      "album": "Album Name",
      "status": "completed",
      "codec": "alac",
      "output_path": "/app/data/downloads/albums/Album Name/01. Song 1.m4a"
    }
  ]
}
```

Hook execution surfaces as `hook_started`, `hook_succeeded` and `hook_failed` events.
Every enabled entry whose `events` and `job_types` match runs for every job; a submission
cannot choose among them. On shutdown, hooks get a short drain: retries stop at once,
and an attempt still running when the drain ends is cancelled and recorded as
`hook_failed`.

## Library sync

The watcher polls your signed-in Apple Music library and submits the **album** behind any
newly added song as an ordinary download job. Favourite one track on your phone, and the
album lands on your NAS without a manual submission.

```yaml
library_sync:
  enabled: false          # the only feature that creates jobs on its own
  interval_minutes: 15    # 1..1440
```

Both keys are runtime-mutable through `PUT /api/v1/config`, or a manual file edit applied
with `POST /api/v1/config/reload` — the polling loop reads them at the start of every
pass, so turning it off really stops the polling rather than just discarding results, and
shortening the interval takes effect immediately instead of after the current wait.

It depends on `catalog.media_user_token`: a personal library is readable only with a
subscription token, and a developer token alone will not do. With that field empty the
watcher idles and warns once instead of erroring every pass.

Submissions go through `jobs.Manager.SubmitBatch`, not through the backend's own HTTP API,
so they are identical to jobs you submit by hand — same deduplication, same validation,
same hooks. Catalog reads use the backend's own developer token or scraped web-player
token, like every other catalog read.

### Why the watermark is a position, not a timestamp

Apple does not expose when a library song was added. `library-songs` has no `dateAdded`
attribute, and neither `extend=` nor `fields[library-songs]` produces one. `library-albums`
does have `dateAdded`, but it is the moment the album's *first* song entered the library
and does not move when you add more songs to it — an album added in 2024 and topped up
today still reads 2024. `sort=-dateModified` is rejected outright with a 400.

One thing is reliable: the **order** returned by `sort=-dateAdded`.

So the watcher records the ids of the newest batch of songs as an anchor, and on each pass
walks back through the pages until it sees the oldest anchored song again. Anything above
that boundary and not in the anchor is new. No clock is involved, so clock drift cannot
affect it, and the answer is exact rather than approximate.

Enabling it for the first time only drops the anchor and submits **nothing** — everything
already in the library at that moment counts as existing. Backfilling an entire library was
never the point.

Three details worth stating outright:

- Each pass deliberately reads a little past the anchor so an ordinary addition resolves in
  a single request. Those extra entries are by definition *older* than every anchored one;
  they are absent from the anchor only because the anchor holds a fixed count. Treating
  "not in the anchor" as "new" on its own would resubmit those same old albums every pass —
  the boundary is the anchored oldest song's **position**.
- If that oldest anchored song has disappeared from the library, there is no comparable
  boundary left. The watcher re-anchors and submits nothing, rather than queueing every
  album it just scanned.
- When a new album's library-to-catalog lookup fails, the anchor stays where it was so the
  next pass tries that album again; the albums that did resolve are submitted again too,
  which an active job dedupes and a finished one answers by skipping files on disk. A
  lookup that fails with a client error (a 4xx other than 408 and 429) three passes in a
  row is skipped with a warning, so one album Apple refuses cannot hold the anchor back
  forever. An album with no catalog counterpart (404) is skipped at once.

### Cost

A steady-state pass is usually **one** Apple request — one page covers the anchor window —
plus one request per **new** album to translate a library id (`l.xxx`) into a catalog link,
which is what can actually be downloaded. At a 15-minute interval with nothing new, that is
roughly 100 requests a day.

### Status and reset

`GET /api/v1/library-sync` returns the enabled flag, interval, anchor entry count and the
last pass's result. It answers while disabled too, so a client never has to guess.

`POST /api/v1/library-sync/reset` clears the anchor. The next pass re-anchors to the
library **as it stands then** and submits **nothing**. This is "stop treating my current
library as new", not "fetch it all again" — use it when the watcher's idea of what you
already have has drifted from reality. To re-download one album, `POST /api/v1/downloads`
it directly.
