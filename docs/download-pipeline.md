# Download pipeline

> Type: reference

Audio only. Apple Music music videos will not be supported: under L3 the pipeline can
only reach low-resolution video, which does not meet this project's quality bar.

## Stages

Every track walks the same path, and each transition publishes an `item_progress` event
whose `phase` is the item status:

```text
queued → resolving → waiting_download → downloading → waiting_decrypt
       → decrypting → remuxing → saving → tagging → completed
```

Terminal item states are `completed`, `failed`, `skipped_existing` and `cancelled`.
Alongside the status, each item carries a durable stage breakdown — `resolved`,
`remuxed`, `verified`, `tagged`, `saved` — so a client reconnecting mid-job can redraw
the same progress without replaying the stream.

The normal path is: encrypted input → fragment-by-fragment decrypt → `ffmpeg` stdin →
flattened file → integrity check → tagging → final save. No plaintext whole track is ever
written to disk. Flatten, verify and tag all happen in `download.temp_dir`, so a possibly
slow downloads volume only ever sees one sequential write.

Tagging rewrites only the smaller `moov` and artwork region for the tail-`moov` layout
`ffmpeg` normally produces, leaving the media in place; layouts that cannot be updated
safely in place fall back to the old whole-track rewrite.

If `temp_dir` and `downloads_dir` share a filesystem, the final move is an atomic rename;
across filesystems it is one copy through a `.part` file, which preserves complete-file
publication and failure-cleanup semantics.

Lyrics sidecars and standalone covers are written through a temp file in the same
folder and a rename, so an interrupted write never leaves a truncated file behind. That
matters for covers in particular: a cover already on disk is never fetched again.

### Cancel and shutdown

A job stops in one of two ways, and they end its items differently:

- **Cancelled by the user** (`POST /downloads/{id}/cancel`): every item that has not
  finished ends `cancelled`, including tracks that were still waiting for a permit or
  never started. For a running job each gets an `item_progress` event with
  `phase: cancelled` carrying the full item, so a client that follows only the stream
  sees it stop; these writes run on a context detached from the cancelled job, with a
  short timeout. A job cancelled while still queued has its items ended in one store
  write with no per-item events, so clients read the job again. The job's resume
  checkpoints are deleted.
- **Interrupted by shutdown**: items are left exactly as they were and the resume
  checkpoints stay, so the recovered job continues the transfers on the next start
  (see [Architecture](architecture.md#1-process-and-lifecycle)).

A terminal item write (completed, skipped, failed, cancelled) that fails is retried
once on a context that is not cancelled and logged with the job and item ids, because
it is the only record of how the track ended. Intermediate progress writes are
best-effort. A panic inside one track's pipeline fails that item with the panic as its
error, logs the stack, and leaves the other tracks running.

`ffmpeg` (`tools.ffmpeg`) is the only external command required — it flattens the remuxed
MP4 and runs the optional integrity check. Sample extraction, re-encapsulation, metadata
and cover art are all handled in-process by the Go `mp4ff` and `go-mp4tag` libraries, so
`gpac`, `MP4Box` and the Bento4 tools (`mp4extract`, `mp4edit`) are no longer needed.

## Retries and codec fallback

- `download.quality_priority` — the ordered Enhanced HLS fallback chain. Supported:
  `alac`, `aac`, `aac-binaural`, `aac-downmix`, `ec3`, `ac3`.
- `aac-lc` never needs listing. With codec fallback enabled it is appended automatically
  as the final WebPlayback floor.
- `download.codec_alternative` — whether to continue down the chain after a codec's
  retries are exhausted. Off means only the first codec is tried.
- `download.max_attempts` — maximum total attempts (including the first) for metadata,
  cover, lyrics, and each codec's download and decrypt phase. Positive values allow
  `1-10`; `<= 0` behaves as 1 (one try, no retry). Every codec in the chain, including the
  implicit AAC-LC floor, gets its own budget, and its download and decrypt phases count
  separately.
- Retryable errors use exponential backoff with jitter. When Apple's Catalog API returns
  `Retry-After`, the wait is never shorter than that hint, so a batch of requests cannot
  synchronously replay.
- Errors that cannot change on retry stop a codec at once. A song without an Enhanced
  HLS manifest, or whose manifest lacks the codec, falls back to the next codec after
  one attempt.
- A failed integrity check carries ffmpeg's own output (the first 1 KiB), so the item's
  error says what was wrong with the file.
- Retries, exhaustion, recovery and codec fallback all surface as job SSE events. Each
  item in the job detail also carries `retry_kind`, `attempt`, `max_attempts` and
  `status_message`, where `attempt` is the 1-based try number within the current
  `retry_kind` phase.

## Transfers

Encrypted media transfers have no whole-request deadline, because a 24-bit/192 kHz
track can be several hundred MB. Connecting, the TLS handshake and waiting for the
response headers are bounded by the HTTP transport; after that a transfer fails only
when no byte of the body arrives for 60 seconds, with an error naming the stall. A
low-memory checkpoint keeps the bytes that did arrive, so the retry resumes from there.
Playlists and manifests keep a 30-second limit on the whole request.

The in-memory and on-disk transfers share one `Range`/`If-Range` resume state machine:
a resumed response is used only when its `Content-Range` and validator prove it belongs
to the bytes already held, and a server that ignores the range restarts the transfer
from zero rather than mixing two representations.

## Concurrency

All pools are process-wide, shared across every job, sized at startup, and not shared
between backend replicas. Changing any of them requires a restart.

| Key | Default | Bounds |
| --- | ---: | --- |
| `download.max_running_jobs` | 3 | Worker slots — concurrently running jobs |
| `download.max_parallel_downloads` | 16 | Encrypted media transfers in flight |
| `download.max_parallel_decrypts` | 32 | Decrypt operations in flight |
| `download.max_parallel_wrapper_requests` | 24 | Wrapper data RPCs — M3U8, lyrics, web playback, license |
| `catalog.max_parallel_requests` | 16 | Apple control-plane HTTP: Catalog API, web token, HLS manifests, artwork |
| `catalog.requests_per_second` | 10 | Sustained authenticated Catalog / amp-api rate |
| `catalog.request_burst` | 16 | Token-bucket burst for the same traffic |

Wrapper login and logout are exempt from `max_parallel_wrapper_requests` so operator
access is never starved; decrypt streams are bounded by `max_parallel_decrypts` instead.
Media already downloaded but not yet decrypted is additionally held back by internal
in-flight backpressure — with `D` download and `X` decrypt permits the internal in-flight
ceiling is `D+X`.

When Apple returns 429 the backend honours `Retry-After`, triggers a global cooldown and
retries once automatically. The global cooldown is capped at 60 seconds (a longer hint is
logged and clamped), because it holds back every Apple request, interactive ones included.

When several jobs compete for a full pool, permits go to the **earliest submitted**
unfinished job, so jobs tend to complete one after another rather than all creeping
forward together. Recovered jobs keep their original submission time and do not lose their
place across a restart. Priority only matters while the pool is saturated — when the
leading job cannot fill it, spare permits go straight to later jobs, so no throughput is
wasted. URL validation, quality probes and other interactive API requests never queue
behind jobs.

`download.progress_event_interval_ms` is documented in [api.md](api.md#events).

## Memory modes

`download.memory_mode` controls the per-track RAM/disk trade-off on the Enhanced HLS path.
Both modes decrypt fragment by fragment straight into `ffmpeg`; neither writes a plaintext
whole-track file.

**`low` (default)** streams the encrypted media into a resumable checkpoint on disk and
reads it back fragment by fragment. RAM stays fragment-sized, but the encrypted checkpoint
and the flattened output coexist in `temp_dir` — roughly two track-sized files per
in-flight track.

**`high`** keeps one encrypted track in RAM instead of that checkpoint, so only the
flattened output remains in `temp_dir`. The observed Go heap can temporarily approach twice
the track size because the garbage collector retains allocation headroom, and both process
memory and temp usage scale with the pools above. In-memory media is capped at 512 MiB per
track; larger responses fail explicitly rather than risking an OOM. If a CDN response
carries a usable `ETag` or `Last-Modified` validator and is interrupted after making
progress, high mode retries once with a `Range` request from its in-memory prefix — it does
not persist that prefix, so a process or container restart still begins at byte zero.

AAC-LC WebPlayback is unaffected by this option: both modes keep its encrypted retry buffer
in memory and stream the decrypted output straight into `ffmpeg`. The Widevine parser may
still hold decoded track structures until that stream completes.

A per-job `overrides.memory_mode` can pick a different mode without changing the runtime
default.

**Which to use.** Keep `low` unless you have measured a temp-disk bottleneck. High mode is
5–19% faster in tests that exclude CDN variance, but the gain comes from skipping the
encrypted checkpoint's filesystem I/O, and it is easily hidden by a slow network or a fast
local temp disk. Its memory cost cannot be estimated per worker or per average track:
every downloaded-but-not-yet-decrypted track holds a complete encrypted buffer until
decryption consumes it. Start at 1/1 or 2/2 concurrency and only raise it once the peak is
comfortable. Full numbers in [benchmarks.md](benchmarks.md).

## Lyrics

- `download.embed_lyrics` — write lyrics into the MP4 tag.
- `download.save_lyrics_file` — save an `.lrc` or `.ttml` sidecar next to the audio.
- `download.lyrics_format` — `lrc` or `ttml`; `ttml` preserves the wrapper's original TTML.
- `download.lyrics_type` — `lyrics` or `syllable-lyrics` (word-timed).
- `download.lyrics_extras` — `translation`, `pronunciation`; only applied when
  `lyrics_format: "lrc"`.

Lyrics, word-timed lyrics, translations and transliterations all require `wrapper-manager`
to hold a valid Apple Music subscription session. A lyrics fetch or conversion failure
never aborts the audio download: the file is saved without lyrics and the item's status
explains why. Each item also carries a `lyrics_status` of `fetched`, `failed`, `none`
(catalog reports no lyrics) or `disabled` (neither embedding nor sidecars are on).

## Simulate mode

`simulate.enabled` swaps the media backend, not the pipeline. Catalog metadata and
Enhanced HLS media selection run for real, and then the same pipeline code runs as for
a real download: the same statuses, progress bands, status messages, events, retries
and codec fallback. Only the backend's work is pretended: transfers and decryption are
paced by a speed drawn from `simulate.min_speed_kbps`..`max_speed_kbps`, the AAC-LC
WebPlayback asset, cover art and lyrics are stand-ins, and nothing is written, deleted
or run (no ffmpeg, no wrapper). An output that already exists on disk is still reported
as skipped, and `force_overwrite` never deletes it in simulate mode.

## Output paths

One template per task type, resolved relative to `download.downloads_dir`. The last
segment is the file name and gets `.m4a` appended. Every segment is sanitised
independently, so a template value can never create extra directories. How a segment is
cleaned (reserved characters, leading and trailing dots, the 240-byte cap) is in
[Architecture](architecture.md#5-output-paths).

| Key | Default |
| --- | --- |
| `download.song_path_format` | `songs/{ArtistName}/{AlbumName}/{TrackNumber:02d}. {SongName}` |
| `download.album_path_format` | `albums/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}` |
| `download.artist_path_format` | `artists/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}` |
| `download.playlist_path_format` | `playlists/{PlaylistName}/{SongNumber:02d}. {SongName}` |
| `download.station_path_format` | `stations/{StationName}/{SongNumber:02d}. {SongName}` |

Artist jobs expand into that artist's albums and songs.

`{SongNumber}` is the track's 1-based position within the collection it is downloaded as:
its position in the playlist or station, and for album and artist jobs its position
within that album — Apple orders an album's tracks disc by disc, so the numbering counts
on across the disc boundary instead of restarting. `{TrackNumber}` is Apple's own number,
which does restart at 1 on every disc, and `{TrackCount}` matches it: the number of
tracks on that track's disc, not the whole release.

Those two defaults apply to a fresh install, whose `config.yaml` is a verbatim copy of
`configs/config.example.yaml`. An install upgrading from a version that numbered album and
artist paths by `{TrackNumber}` keeps that layout — its `config.yaml` already carries the
old value, and a config omitting the key falls back to the old value as well, so nothing
already downloaded is stranded under a name the backend no longer produces. Switch by
editing the key or via `PUT /api/v1/config`.

That is why the album and artist defaults number by `{SongNumber}`. A file-name segment
built from `{TrackNumber}` alone repeats `01, 02, …` once per disc on a multi-disc album,
and two identically titled tracks on different discs — a deluxe edition whose bonus disc
repeats the main disc's titles, say — then resolve to the same path, where the second is
skipped as already downloaded. Pair it with `{DiscNumber}` (for example
`{DiscNumber}-{TrackNumber:02d}. {SongName}`) to keep Apple's numbering instead.

With defaults, an album track lands at:

```text
data/downloads/albums/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}.m4a
```

The full variable list is in the `download` section of
[`configs/config.example.yaml`](../configs/config.example.yaml). In directory segments
`{ArtistName}` resolves to the collection's grouping artist (album artist when available)
so all of an album's tracks share one folder; in the file-name segment it is the track's
own artist. Numeric variables — `{SongNumber}`, `{DiscNumber}`, `{DiscCount}`,
`{TrackNumber}`, `{TrackCount}` — support `:02d` padding. Unknown variables are preserved
literally.

### Standalone covers

- `download.save_album_cover: true` — `cover.jpg` (or `.png`) in the album directory.
- `download.save_artist_cover: true` — `artist.jpg` (or `.png`) in the artist directory.
- `download.save_playlist_cover: true` — `cover.jpg` (or `.png`) in the playlist directory.

Cover destinations are located through the path template's variables: the album cover goes
into the deepest directory segment referencing `{AlbumName}` or `{AlbumId}` (falling back
to the directory holding the audio), and the artist cover into the deepest segment
referencing `{ArtistName}`, `{UrlArtistName}`, `{AlbumArtist}` or `{ArtistId}` — if no
directory segment references the artist, the artist cover is skipped. The extension follows
`download.cover_format`. Playlists and stations are flat directories: they can hold their
own cover, but no album or artist covers are written there.
