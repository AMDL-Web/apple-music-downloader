<p align="center">
  <img src="./assets/readme/hero.svg" width="100%"
       alt="AMDL Backend — paste an Apple Music link and get tagged lossless files on your own disk, through a resolve, download, decrypt, remux, verify, tag and save pipeline">
</p>

<p align="center">
  <a href="https://github.com/AMDL-Web/apple-music-downloader-backend/actions/workflows/ci.yml"><img src="https://github.com/AMDL-Web/apple-music-downloader-backend/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/AMDL-Web/apple-music-downloader-backend/releases"><img src="https://img.shields.io/github/v/release/AMDL-Web/apple-music-downloader-backend?label=release" alt="Latest release"></a>
  <a href="https://github.com/AMDL-Web/apple-music-downloader-backend/pkgs/container/apple-music-downloader-backend"><img src="https://img.shields.io/badge/ghcr.io-amd64%20%2B%20arm64-blue" alt="Container image on GHCR"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/AMDL-Web/apple-music-downloader-backend" alt="Go version">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0-green" alt="AGPL-3.0"></a>
</p>

<p align="center"><a href="README.zh-CN.md">中文</a></p>

**Send it an Apple Music URL. It resolves the songs, pulls the encrypted Enhanced HLS
media, decrypts it, remuxes and tags it, and writes a finished file to your disk —
publishing every stage as it happens.**

One Go binary, one SQLite file, a REST + Server-Sent Events API on `/api/v1` described by
an OpenAPI 3.1 spec, and a web console built on that API. It handles songs, albums,
playlists, artists and radio stations.

---

## Quick start

You need a reachable wrapper, either [wrapper-manager v2](#how-it-works) (recommended: you
log in from the console) or a bare wrapper-lite. The backend gets device manifests,
licenses, lyrics and decryption templates from it and cannot download anything without one.

```bash
docker compose up -d
```

That pulls the multi-arch image from GHCR (no local build), seeds `configs/config.yaml`
from the bundled example, and listens on `:18080`. Point it at your wrapper with
`AMDL_WRAPPER_ADDRESS` in `docker-compose.yml`, which also carries ready-to-uncomment wrapper
services ([docs/deployment.md](docs/deployment.md#the-wrapper)). Upgrading from 1.x?
Read [docs/upgrading-to-2.0.md](docs/upgrading-to-2.0.md) first.

From source instead — Go per [`go.mod`](go.mod), plus `ffmpeg` on `PATH`:

```bash
go run ./cmd/amdl-api
```

Submit an album:

```bash
curl -X POST http://localhost:18080/api/v1/downloads \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://music.apple.com/cn/album/1580904295"}'
```

Watch it run — one line per stage transition, per track:

```bash
curl -N http://localhost:18080/api/v1/downloads/{job_id}/events
```

And when it finishes:

```text
data/downloads/albums/<artist>/<album>/01. Song.m4a   ← ALAC, cover and lyrics embedded
                                       cover.jpg      ← standalone artwork
```

API reference: [docs/api.md](docs/api.md) and [`internal/api/openapi.yaml`](internal/api/openapi.yaml).

## How it works

<p align="center">
  <img src="./assets/readme/architecture.svg" width="100%"
       alt="The web console and your scripts call the backend over REST and Server-Sent Events; the backend keeps its job queue in SQLite, runs the download pipeline and writes files to disk; it requires a wrapper over HTTP, decrypts in-process, and reads the catalog from Apple's amp-api">
</p>

**A wrapper is not optional.** It holds the Apple Music account session and serves the
device manifest, Widevine licenses, lyrics and, for each FairPlay key, a decryption
template, over HTTP; the backend runs the templates itself, in pure Go
(`internal/fairplay`, a translation of [Temari](https://github.com/WorldObservationLog/Temari)).
Two upstream wrappers work: [wrapper-lite](https://github.com/WorldObservationLog/wrapper/tree/lite)
(one account; log in on its command line) and [wrapper-manager
v2](https://github.com/WorldObservationLog/wrapper-manager/tree/v2) in front of it (many
accounts; the console's Account page logs in through it). The backend does not contact the
wrapper at startup, so it starts without it — `GET /api/v1/wrapper/status` tells you whether
it is actually reachable and whether it can log in.

**Apple's catalog is read from its undocumented web-player API.**
`amp-api.music.apple.com` answers only to a JWT scraped from `music.apple.com`, serves every
catalog read, and is the **only** source of `enhancedHls` manifests and `editorialVideo`
animated covers. The documented `api.music.apple.com` needs a developer token signed with an
Apple key; the backend holds none and never calls it. Being undocumented, amp-api can change
without notice. The reads a download needs (the link's song, album, playlist, station or
artist, and each track's metadata) fail the job or that track once a retryable error has
used up its attempts, or at once on an error a retry cannot fix. The
extras (animated covers, the album details added to a single song, a private playlist's own
cover, the artist picture) degrade to "this album doesn't have that" rather than failing a
job, and a track with no usable web or device manifest falls back to the next codec. Only a Hi-Res Lossless track that may exceed 48 kHz asks the wrapper for its
device manifest too, because the web player's playlist can leave out the higher ALAC
renditions.

**There is no authentication here, on purpose.** Every endpoint is open. This is the
download core — access control belongs to the reverse proxy, gateway or session layer you
put in front of it.

## How a track is downloaded

Every track walks the same path. Each transition is published as an `item_progress`
event whose `phase` carries the item status:

```text
queued → resolving → waiting_download → downloading → waiting_decrypt
       → decrypting → remuxing → saving → tagging → completed
```

Alongside the status, each item carries a durable stage breakdown — `resolved`,
`remuxed`, `verified`, `tagged`, `saved` — so a client that reconnects mid-job can
redraw the exact same progress without replaying the stream.

Media is decrypted in this process, fragment by fragment, straight into `ffmpeg`; no plaintext whole track
is ever written to disk. The flatten, integrity check and tag rewrite all happen in
`download.temp_dir`, and only the finished file moves to `downloads_dir`.

### Codec fallback

<p align="center">
  <img src="./assets/readme/codec-fallback.svg" width="100%"
       alt="Codecs from download.quality_priority are tried in order; when one codec's attempts are exhausted the next is tried, with AAC-LC over WebPlayback always appended as the implicit final fallback">
</p>

`download.quality_priority` is an ordered list — `alac`, `aac`, `aac-binaural`,
`aac-downmix`, `ec3`, `ac3`. Each codec gets `download.max_attempts` tries, with the
download and decrypt phases counted separately, and retries use exponential backoff with
jitter. AAC-LC never needs listing: it is appended automatically as the last-resort
WebPlayback format. Set `codec_alternative: false` to stop after the first codec.

### Concurrency

Three process-wide pools, shared across all jobs and sized at startup:
`max_parallel_downloads` (16), one decrypt per CPU (`GOMAXPROCS`) and 24 wrapper requests,
plus `max_running_jobs` (3) worker slots. When a
pool is full, permits go to the earliest-submitted unfinished job first, so jobs tend to
finish one after another instead of all creeping forward together. Interactive API calls
never wait for those pools; Apple catalog reads, `POST /api/v1/lookup` included, share
one request gate (`catalog.max_parallel_requests`) with the jobs' catalog reads.

`download.memory_mode` trades RAM for temp-disk I/O: `low` (default) checkpoints the
encrypted track to disk and re-reads it; `high` keeps it in RAM instead. See
[docs/download-pipeline.md](docs/download-pipeline.md) and the measured numbers in
[docs/benchmarks.md](docs/benchmarks.md).

## Where files land

One template per task type, relative to `download.downloads_dir`. The last segment is
the file name and gets `.m4a` appended.

| Config key | Default |
| --- | --- |
| `download.song_path_format` | `songs/{ArtistName}/{AlbumName}/{TrackNumber:02d}. {SongName}` |
| `download.album_path_format` | `albums/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}` |
| `download.artist_path_format` | `artists/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}` |
| `download.playlist_path_format` | `playlists/{PlaylistName}/{SongNumber:02d}. {SongName}` |
| `download.station_path_format` | `stations/{StationName}/{SongNumber:02d}. {SongName}` |

Variables like `{AlbumArtist}`, `{ReleaseYear}`, `{UPC}`, `{DiscNumber}` and `{Codec}`
are listed in [`configs/config.example.yaml`](configs/config.example.yaml); numeric ones
take `:02d` padding. In directory segments `{ArtistName}` resolves to the collection's
grouping artist so an album stays in one folder; in the file-name segment it is the
track's own artist. `{SongNumber}` is the position within the collection — within the
album for album and artist jobs, counting on across discs — while `{TrackNumber}` is
Apple's own number, which restarts at 1 on every disc; pair that one with `{DiscNumber}`
if you use it. Standalone `cover.jpg` / `artist.jpg` files and `.lrc` / `.ttml`
lyrics sidecars are written next to the audio when enabled.

## API

[`internal/api/openapi.yaml`](internal/api/openapi.yaml) is the source of truth for every
request and response shape. Every path below is under `/api/v1`. The web console at
`/console/` is the API's only browser client: responses carry no CORS headers, so a page
on another origin cannot call it.

| | |
| --- | --- |
| **Downloads** | `POST /downloads` · `GET /downloads` · `GET/DELETE /downloads/{id}` · `POST /downloads/{id}/cancel` · `POST /downloads/{id}/retry` |
| **Events** | `GET /downloads/{id}/events` per job · `GET /downloads/events` overview feed (SSE) |
| **Lookup** | `POST /lookup` — preview a link's title, artist and cover without creating a job |
| **Quality** | `POST /quality` — probe a URL's declared codecs and audio quality without creating a job |
| **Wrapper** | `GET /wrapper/status` · `POST /wrapper/login` · `POST /wrapper/login/{login_id}/2fa` · `POST /wrapper/logout` |
| **Config** | `GET /config` · `PUT /config` · `POST /config/reload` · `GET /hooks` |
| **Library sync** | `GET /library-sync` · `POST /library-sync/reset` |
| **System** | `GET /health` · `GET /system` build, capacity and free space · `GET /stats` download statistics · `GET /logs` · `GET /logs/stream` |

Submissions take `urls`, and an `overrides` object that overlays the quality keys
(`quality_priority`, `codec_alternative`, the ALAC limits) and `force_overwrite` for that
batch only. Everything else, the `catalog.media_user_token` that radio stations and private
playlists need included, comes from the runtime config, and every enabled matching hook
runs. The token is a runtime key, so `GET /api/v1/config` returns it.

Errors share one body, `{"error": {"code", "message", "details"?}}`; clients switch on
`code`. Streams are Server-Sent Events that resume from `Last-Event-ID` or `?cursor=`,
seeded from the `event_cursor` of the matching snapshot. `GET /config` only reads; a
hand-edited config file is applied by `POST /config/reload`.

Every response carries a server-generated `X-Request-ID`;
`GET /logs?request_id=<id>` aggregates that request's access and job logs.

Full walkthrough with curl examples: [docs/api.md](docs/api.md).

## Configuration

A single file, `configs/config.yaml`, bootstrapped from
[`configs/config.example.yaml`](configs/config.example.yaml) on first start. The example
is the documentation — every key's allowed values, units and defaults live in its comments.

- **Runtime keys** (quality, paths, lyrics, covers, retries, simulate, library sync) apply
  immediately via `PUT /api/v1/config`, which rewrites the whole file and drops comments.
- **Startup keys** (listen address, database path, wrapper address, pool sizes, log
  output) need a restart.
- **Environment**: `AMDL_SERVER_LISTEN`, `AMDL_WRAPPER_ADDRESS` and `AMDL_DATABASE_PATH`
  override their keys, and `AMDL_CONFIG` / `AMDL_HOOKS_CONFIG` choose the files. Every
  other key is set in the file; any other `AMDL_*` variable is ignored with a warning.

Defaults, hand edits and reload, and the Docker specifics: [docs/configuration.md](docs/configuration.md)
and [docs/deployment.md](docs/deployment.md).

## Automation

**Job hooks** ([`configs/hooks.yaml`](configs/hooks.yaml)) fire a webhook or a local
command when a job is queued or reaches a terminal state — refresh a media server, run a
post-processing script. Disabled by default.

**Library sync** polls your signed-in Apple Music library and submits the *album* behind
any newly added song as a normal download job: favourite one track on your phone, get the
album on your NAS. Disabled by default — it is the only feature that creates jobs on its
own. Because Apple exposes no reliable "added at" timestamp for library songs, it anchors
on the *order* of `sort=-dateAdded` instead of a clock.

Both are covered in [docs/automation.md](docs/automation.md).

## Limits

- **Audio only.** Apple Music music videos will not be supported: under L3 the pipeline
  can only reach low-resolution video, which does not meet this project's quality bar.
- **One user, no accounts.** There is no concept of a user anywhere in this codebase, and
  no auth. Put a gateway in front of it.
- **Radio is a rolling list.** Stations resolve to Apple's current "next tracks", so a
  download captures what is offered now, not a fixed tracklist. Live stations
  (Apple Music 1 and friends) fail with an explicit error.
- **Some catalog data is undocumented.** `enhancedHls` and animated covers come from
  `amp-api.music.apple.com` and may disappear without warning.

## Documentation

| | |
| --- | --- |
| [docs/api.md](docs/api.md) | Every endpoint with curl examples, job overrides, error codes, SSE stream semantics |
| [docs/configuration.md](docs/configuration.md) | Config file, defaults, env overrides, runtime vs startup keys, reload |
| [docs/deployment.md](docs/deployment.md) | Docker, the wrapper service, mounts, `PUID`/`PGID`, seeding, releases and image tags |
| [docs/upgrading-to-2.0.md](docs/upgrading-to-2.0.md) | Every breaking change from 1.x, with old and new shapes and an upgrade checklist |
| [docs/download-pipeline.md](docs/download-pipeline.md) | Retries, codec fallback, concurrency pools, memory modes, lyrics |
| [docs/automation.md](docs/automation.md) | Job hooks and library sync |
| [docs/benchmarks.md](docs/benchmarks.md) | Measured post-decrypt and end-to-end results |

## Reading map

Where to start reading for a given question. Each pointer names a package and a
symbol, so it stays valid when files inside the package are split.

| Question | Start at |
| --- | --- |
| How is the process wired together? | `cmd/amdl-api/main.go` |
| Which HTTP routes exist? | `internal/api#Server.Routes` |
| What exactly does the API accept and return? | `internal/api/openapi.yaml` |
| How are jobs queued, run, cancelled and retried? | `internal/jobs#Manager` |
| How is one track downloaded, decrypted, remuxed and tagged? | `internal/media#Downloader` |
| Which Apple host answers a request? | `internal/applemusic#ampAPIBase`, `internal/applemusic#CatalogClient.doWithWebAuth` |
| How does the backend talk to the wrapper? | `internal/wrapper#Client` |
| How are samples decrypted? | `internal/fairplay#Template` |
| Where are jobs stored, and how does the schema migrate? | `internal/db#Store` |
| How is config loaded and validated? | `internal/config#Load`, `internal/config#Config.Validate` |
| What do job hooks send? | `internal/hooks#Dispatcher` |
| How does library sync pick albums? | `internal/librarysync#Watcher` |
| Web console | internal/console (web console) |

## Development

You need Go per [`go.mod`](go.mod), `ffmpeg` on `PATH`, and Node.js 24 for the script
tests.

| Command | What it does |
| --- | --- |
| `make check` | The CI gate: gofmt check, build, vet, tests, race detector, `govulncheck`, Node script tests |
| `make test`, `make race` | The test suite, without or with the race detector |
| `make dev` | The API in simulate mode on `127.0.0.1:18080`, with a throwaway config and database under `data/dev/` |
| `make docker` | Build the container image locally |
| `make help` | Every target |

`make dev` needs no wrapper: simulate mode resolves real catalog data and
selects real media, then simulates the transfer, decryption and file writes while
publishing the same statuses and events as a real download. It is the quickest way
to exercise the API, SSE streams and clients locally.

CI runs the same steps on every push to `main` and `dev` and on every pull request,
and also builds the Docker image.

`ffmpeg` is the only external command the pipeline needs. Sample extraction, remuxing,
metadata and cover art are all done in-process with `mp4ff` and `go-mp4tag`.

Work lands on `dev`; `main` is release-only and requires a pull request. Every commit
needs a DCO sign-off (`git commit -s`); see [CONTRIBUTING.md](CONTRIBUTING.md).
Releases are cut by merging the `dev` → `main` PR with a version in its title, or by
running the `Release` workflow manually; both create the GitHub Release and push the
multi-arch image to GHCR.

Coding agents start at [AGENTS.md](AGENTS.md) (rules, verification, contracts), with
[REVIEW.md](REVIEW.md) for review bots, [GLOSSARY.md](GLOSSARY.md) for project
vocabulary, `.claude/skills/` for procedures such as verifying a change or cutting a
release, and [docs/README.md](docs/README.md) for the docs index.

## License

[AGPL-3.0](LICENSE).
