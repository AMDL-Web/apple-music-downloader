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

## Integrity check

`download.check_integrity` turns the check on or off; `download.integrity_mode` picks how
it checks (`internal/media#MP4Processor.checkIntegrityQuick`). What it guards against, and
where in a file each sits:

- A malformed ALAC packet terminator: a single packet, anywhere in the track. An ALAC
  track that fails either mode gets one repair attempt (`alac_repaired` event) and is
  checked again.
- Garbage from the first frame on, such as a stale fragment data offset that makes the
  flatten read past the media data.
- A file cut short: samples past the end of its media data, or a box past the end of the
  file.

`full` decodes the whole track with ffmpeg. `quick` (default) checks every sample's
position against the mdat boxes, parses every ALAC packet in Go down to its END element
(what ffmpeg's decoder requires), and decodes only the first and last 5 seconds with
ffmpeg. On a 24-bit/96 kHz ALAC track it costs a quarter to a third of the CPU of `full`;
damage confined to the middle of an AAC or Dolby track passes it.

A `quick` failure counts only once a full decode confirms it
(`internal/media#Downloader.verifyStaged`). When the decode passes, the file is kept and
an `integrity_overruled` event carries the quick error; otherwise the track goes on to the
repair or failure path as in `full`. A quick false positive would otherwise fail the codec
and fall back to a lossy one.

With `check_integrity` false neither the check nor the ALAC repair runs: a damaged file is
saved, and later jobs skip it because it is already on disk.

## Transfers

Encrypted media transfers have no whole-request deadline, because a 24-bit/192 kHz
track can be several hundred MB. Connecting, the TLS handshake and waiting for the
response headers are bounded by the HTTP transport; after that a transfer fails only
when no byte of the body arrives for 60 seconds, with an error naming the stall. An
Enhanced HLS checkpoint keeps the bytes that did arrive, so the retry resumes from there.
Playlists and manifests keep a 30-second limit on the whole request.

A checkpoint resumes with `Range`/`If-Range`: a resumed response is used only when its
`Content-Range` and validator prove it belongs to the bytes already held, and a server
that ignores the range restarts the transfer from zero rather than mixing two
representations. AAC-LC's in-memory transfer never resumes; its retry starts over.

## Concurrency

All pools are process-wide, shared across every job, sized at startup, and not shared
between backend replicas. The three config keys need a restart to change; the rest are
fixed sizes in `internal/config` (`MaxParallelDecrypts`, `MaxParallelWrapperRequests`,
`CatalogRequestsPerSecond`, `CatalogRequestBurst`) and `internal/wrapper`
(`playbackRequestLimit`, `manifestRequestLimit`). A `/key` or `/m3u8` gives up after
waiting 10 wrapper request timeouts for its permits (`playbackWaitTimeouts`), however
often higher-priority requests overtake it.

The wrapper pools are small because of how wrapper-lite serves HTTP: a fixed set of
threads, each held by one connection until that connection has been idle for 30 seconds.
A request sent on a connection beyond them is not read until one idles out, which takes
as long as the default request timeout. The client therefore keeps no more connections
than the wrapper permits plus one `/status` call (`internal/wrapper#connectionLimit`),
which leaves a thread for another client, and closes an idle one after a few seconds
(`internal/wrapper#idleConnectionTimeout`).

| Pool | Size | Bounds |
| --- | ---: | --- |
| `download.max_running_jobs` | 3 (default) | Worker slots — concurrently running jobs |
| `download.max_parallel_downloads` | 16 (default) | Encrypted media transfers in flight |
| Decrypts | CPUs (`GOMAXPROCS`) | Tracks in the decrypt stage, and the goroutines decrypting their samples |
| Wrapper requests | 4 | Wrapper lyrics, web playback and license requests |
| Wrapper playback requests | 2 | Wrapper `/key` and `/m3u8`, which wrapper-lite answers one at a time |
| Wrapper manifest requests | 1 | Wrapper `/m3u8`, and `/key` fetched ahead of need, holding or waiting for a playback permit, so they never hold both |
| `catalog.max_parallel_requests` | 16 (default) | Apple control-plane HTTP: Catalog API, web token, HLS manifests, artwork |
| Catalog rate | 10 per second | Sustained authenticated Catalog / amp-api rate |
| Catalog burst | 16 | Token-bucket burst for the same traffic |

Decryption runs in the backend: wrapper-lite's `/key` returns a white-box template per
key URI, which is cached across jobs (`templateCacheSize` entries; the prefetch key's
template is kept for the life of the process), so each key costs one `/key` request.
wrapper-lite answers `/key` and `/m3u8` one at a time, a couple of seconds each, while
decrypting a track with its template takes a fraction of a second. So once a track holds
its in-flight permit, its keys are fetched in the background while its media downloads
(`internal/media#keyPrefetch`, `internal/wrapper#Client.PrefetchKeys`), and a downloaded
track whose keys are not in yet stays `waiting_decrypt`, without a decrypt permit, until
they are. The fetch belongs to the codec attempt, so a transfer retry does not ask
again. A key fetched ahead of need waits in the manifest lane behind every queued
`/m3u8` of its job and older jobs (`internal/wrapper#Client.withKeyPermits`): a track
cannot start its download before its stream is chosen, and its key is only needed once
that download ends. When the track's download ends first, the decrypt stage's wait takes
the key out of that lane to compete at the track's own rank. An album's tracks then
queue for their keys during the transfers instead of one after another at the decrypt
stage, but the wrapper still answers one key at a time, so on a fast link the tracks of
an album finish about one key apart.
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

## Encrypted checkpoints

Every Enhanced HLS track streams its encrypted media into a resumable checkpoint in
`download.temp_dir` and is read back fragment by fragment during decrypt, straight into
`ffmpeg`; no plaintext whole-track file is written. RAM stays fragment-sized, but the
encrypted checkpoint and the flattened output coexist in `temp_dir` — roughly two
track-sized files per held track. A track holds its checkpoint from its download until its
decrypt ends, so up to `D+X` tracks (the in-flight ceiling above) hold one at a time.

A checkpoint resumes with a validated `Range` request after an interrupted transfer, a
retry or a restart, and belongs to its job: a failed or recoverable job keeps it for the
retry, while a completed, cancelled or deleted job removes it. Pointing `temp_dir` at a
tmpfs keeps the checkpoints in RAM, which then has to hold that peak.

AAC-LC WebPlayback has no checkpoint: its encrypted track stays in memory as the retry
buffer (at most 512 MiB) and the decrypted output streams straight into `ffmpeg`. The
Widevine parser may still hold decoded track structures until that stream completes.

2.0 removed `download.memory_mode`, whose `high` value kept each encrypted track in RAM
instead of a checkpoint;
[benchmarks.md](benchmarks.md#memory-modes-after-in-process-decryption) has the measurements
behind it.

## Lyrics

Each track with lyrics takes one wrapper request, sent by the first codec attempt whose
output path is free, so a track already on disk takes none: `/lyrics` with `syllable=1`, which
returns Apple's word-timed TTML with the translation in `catalog.language` (when Apple has
one) and the romanization. wrapper-lite answers from its own account's storefront. A song
with only plain-text lyrics has no such document, so a `404` asks once more with
`syllable=0`, which falls back to the plain text; a second `404` is final and not retried.
Everything else is derived from that one document:

- `download.embed_lyrics` — write LRC into the MP4 tag.
- `download.lyrics_files` — sidecars next to the audio: `lrc` (the same LRC as the tag)
  and `ttml` (Apple's document unchanged, which keeps word timing, the translation, the
  romanization, background vocals and duet voices). A song with only plain-text lyrics
  gets no `.ttml`, whether the wrapper sends bare text or a TTML document marked
  `itunes:timing="None"`.
- `download.lyrics_timing` — `line` stamps each LRC line; `word` writes enhanced LRC
  (`[mm:ss.xx]<mm:ss.xx>word <mm:ss.xx>word<mm:ss.xx>`). Songs Apple does not time word by
  word stay line by line. Background vocals stay on their line, in Apple's parentheses.
- `download.lyrics_extras` — `translation`, `pronunciation` (romanization) as extra LRC
  lines with the line's timestamp, skipped when they read the same as the line.

A lyrics fetch or conversion failure never aborts the audio download: the file is saved
without lyrics and the item's status explains why. Each item also carries a
`lyrics_status` of `fetched`, `failed`, `none` (catalog reports no lyrics) or `disabled`
(`embed_lyrics` is off and `lyrics_files` is empty).

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
