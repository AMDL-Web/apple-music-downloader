# Configuration

> Type: reference

Everything lives in one file. `AMDL_CONFIG` picks its path; the default is:

```text
configs/config.yaml
```

On first start the backend creates it from
[`configs/config.example.yaml`](../configs/config.example.yaml), which is built into the
binary (`configs#Example`), so no example needs to be on disk. **The example file is the documentation** — allowed enum values, units,
defaults and template variables are written into its comments, key by key. `config.yaml`
itself is managed by the backend and its comments do not survive a write.

## Defaults

A key missing from `config.yaml` takes the value `configs/config.example.yaml` sets, so a
sparse file behaves the way the example documents. Tests in `internal/config` hold
`Default()` equal to the example and every `Default:` note in its comments equal to
`Default()`.

Unknown keys fail startup with an error naming each one; keys removed in 2.0 are listed in
[Upgrading to 2.0](upgrading-to-2.0.md#config-and-environment). That includes the old
per-job concurrency keys such as `download.max_parallel_tracks`, replaced by the
process-wide pools in [download-pipeline.md](download-pipeline.md#concurrency).

## Runtime, restart and file-only keys

`config.yaml` is the one source of every setting. `PUT /api/v1/config` (the console's and
the desktop app's Settings) changes every key except three, and rewrites the whole file,
dropping all comments in the process. It validates the result the way a start does, so a
value it saves always loads. `internal/config#classOf` sorts the keys:

| | |
| --- | --- |
| **Runtime** | `logging.level`, `logging.access_log`, `catalog.default_storefront`, `catalog.album_track_url_mode`, `catalog.motion_artwork_enabled`, every `download.*` key except the two pools below, the whole `simulate` section, and the whole `library_sync` and `apple_library` sections. A change applies at once, to new requests and newly started jobs. |
| **Restart** | `wrapper.address`, `wrapper.timeout_seconds`, `wrapper.login_timeout_seconds`, `catalog.language`, `catalog.token_cache_ttl_hours`, `logging.console`, `logging.buffer_size`, and the pool sizes `download.max_running_jobs`, `download.max_parallel_downloads`, `catalog.max_parallel_requests`. `PUT` saves the value; it applies when the backend starts again, since components built at startup hold the old one. |
| **File only** | `server.listen` (a wrong value cuts the console off), `database.path` (a new path looks like lost history) and `tools.ffmpeg` (the desktop app writes the ffmpeg it found on every start; the container image keeps the default, its ffmpeg being on PATH). `PUT` refuses them with `422 config_key_not_mutable`; edit the file and restart. |

While this machine runs its own wrapper (`amdl wrapper install`), the backend writes
`wrapper.address` itself and `PUT` refuses to change it (`config_key_not_mutable`,
`details.reason` `managed_wrapper`).
The desktop app's "Another machine" leaves an installed wrapper stopped and uses the
address it names instead.

The `config.yaml` the desktop app and other clients write for the backend they run
(`internal/local`) drops keys this version removed and names them in the backend log; a
`config.yaml` you keep fails startup on them, as the upgrade guide lists.

`GET /api/v1/config` returns runtime keys as they run and restart keys as saved;
`pending_restart` in its answer, in the `PUT` answer and in `GET /api/v1/system` names the
keys whose saved value differs from the running one, a hand edit of a file-only key
included. `POST /api/v1/system/restart` (the console's save bar, `amdl system
restart`) stops every component the way a shutdown does and starts again in the same
process, on the same address, which applies them; the process start time stays, so
`uptime_seconds` in `GET /api/v1/system` does not reset. No supervisor is needed: `amdl serve`,
the container and the desktop app all restart in place. The desktop app restarts the
backend it runs by itself after such a save.

Hand edits reach the running service like this:

| Trigger | Effect |
| --- | --- |
| `GET /api/v1/config` | Reads the restart and file-only keys from the file to report `pending_restart`; runtime keys come from the running snapshot. |
| `POST /api/v1/config/reload` (the console's "Reload from file") | Re-reads the file and applies its runtime keys. If the file is unreadable or invalid, the previous snapshot stays live and the response reports the error as `reload_error`. |
| `PUT /api/v1/config` | Re-reads the file under the same lock first, then applies the patch, so a hand edit made since the last reload, and a restart key waiting for a restart, are kept rather than overwritten. An invalid file fails the `PUT` and changes nothing. |
| Restart | Applies restart and file-only keys. A reload or `PUT` never makes one live, and a `PUT` keeps the edited value on disk for the next start. |

Set these before first real use:

- `server.listen` — API listen address, in the file.
- `wrapper.address` — the wrapper's URL ([Wrapper](#wrapper)), in Settings.
- `database.path` — SQLite file (default `data/db/amdl.db`), in the file.
- `download.downloads_dir` — where finished files go.
- `logging.*` — level, stdout on or off, in-memory retention. There is no log file.
- `tools.ffmpeg` — path or command name, in the file.

## Environment variables and first values

No environment variable sets a key. Two choose the files:

| Variable | Sets |
| --- | --- |
| `AMDL_CONFIG` | Path of `config.yaml` (default `configs/config.yaml`) |
| `AMDL_HOOKS_CONFIG` | Path of `hooks.yaml` (default `configs/hooks.yaml`, not committed; `configs/hooks.example.yaml` is the commented template) |

Any other `AMDL_*` variable is ignored, and startup logs one warning per variable naming
it; that includes `AMDL_SERVER_LISTEN`, `AMDL_WRAPPER_ADDRESS` and
`AMDL_DATABASE_PATH`, which earlier releases read. Move such a setting into the file or
Settings.

An install type that knows better first values than the example passes them to
`amdl serve --init key=value` (repeatable): they are written into `config.yaml` only when
`serve` creates it, keeping the example's comments, and validated first. The container
image passes `server.listen=:18080` and `wrapper.address=http://host.docker.internal:12340`.
Once the file exists it wins; startup logs each `--init` value the file differs from. The
one exception: a `--init server.listen` that is not loopback rewrites a loopback
`server.listen` in an existing file on every start, with a warning, since a loopback
address cannot be reached from outside a container. Once rewritten, later starts change
nothing.

## Wrapper

The `wrapper` section points the backend at the HTTP service that holds the Apple Music
account sessions. All its keys apply after a restart.

- `wrapper.address` is `http://host:port` or `https://host:port`, with an optional path
  prefix for a reverse proxy; the default is `http://127.0.0.1:12340`. A bare `host:port`
  fails startup. The scheme picks the transport: `http` is plaintext, `https` is TLS 1.2+
  with the certificate and hostname verified against the system trust store. There is no
  option to skip verification.
- The URL is either a wrapper-lite or a wrapper-manager v2 in front of one. Both serve
  manifests, lyrics, licenses and the decryption templates; only wrapper-manager has the
  login API, so only it lets the console's Account page log in. The backend tells them
  apart from `/status` and reports it as `login_supported` in
  `GET /api/v1/wrapper/status`. Container setups for both are in
  [Deployment](deployment.md#the-wrapper); what the backend asks of the wrapper is in
  [Architecture](architecture.md#21-the-wrapper-and-decryption).
- `wrapper.timeout_seconds` (default 30) bounds one wrapper HTTP request; a failed key
  template request is retried up to three more times, 1, 2 and 4 seconds apart. A `/key` or
  `/m3u8` waits at most ten times this for a free playback slot. A `/key` that runs out of
  that wait skips the retries above, though the track's decrypt attempts
  (`download.max_attempts`) still apply; a `/m3u8` falls back to the web manifest.
  Decryption runs in the backend and has no wrapper timeout.
- `wrapper.login_timeout_seconds` (default 120) bounds one console login request to a
  wrapper-manager. A login that needs a two-step code takes two requests.
- `wrapper.token` (default empty) is an optional key for a wrapper behind a gateway that
  checks one, such as a Cloudflare Worker in front of a wrapper-manager, or a reverse
  proxy. The backend sends it as `Authorization: Bearer <key>` on every wrapper request;
  set the same key on the gateway (several backends may share one). `amdl wrapper keygen`,
  or Generate in the desktop app's Connection settings, makes a strong one (32 random
  bytes, base64url). It is a runtime key and write-only: `GET /config` shows a set key as
  `...`, and `GET /wrapper/status` says `token_set`. A gateway that answers `401` fails
  the wrapper requests with `wrapper_unauthorized` (not retried). Empty sends no header, as
  a wrapper on the LAN expects.
- A wrapper at `*.workers.dev` that times out or loses its connection gets a hint in the
  error: mainland China blocks `workers.dev`, so bind a custom domain to the Worker and
  set `wrapper.address` to it.
- The wrapper may hold several accounts: a wrapper-manager holds at most 10. The backend
  learns them from the wrapper's `/status` (a wrapper-manager lists them as instances) and
  keeps one row per account, with its switches, in the database
  ([Accounts](api.md#accounts)); the keys of `library_sync` and `apple_library` are shared
  by all of them.
- Simulate mode (below) skips the wrapper status check, so jobs can be submitted without one.

### The media-user-token

Station downloads, private playlist covers, library sync and the Apple library cache (with
the `in_apple_library` mark on search hits) need the Apple Music
media-user-token of a subscribed account. Each account has its own: library sync and the
Apple library use the token of the account they run for, and any other request uses the
token of the active account that serves the request's storefront (the one with the lowest
position when several do), else the default account's. With one, the backend also fetches lyrics and the
AAC-LC playlist and license from Apple itself rather than through the wrapper
([Download pipeline](download-pipeline.md#lyrics)); without one, the wrapper does that.

The wrapper holds each logged-in account's token and refreshes it itself; the backend
never takes one from the user. It reads the token over HTTP: `GET /token?instance=<id>` on a
wrapper-manager (the wrapper amdl runs itself, `amdl wrapper install`, is read this way
too), `GET /token` on amdl's wrapper-lite fork, so it works for a wrapper on another machine
or in another container with nothing to configure beyond `wrapper.address` and, behind a
gateway, `wrapper.token`. Accounts come from the wrapper-manager's `/status` instances. The backend
asks at startup, again a few seconds after the wrapper's own next refresh
(`next_refresh_at`; at least one minute and at most one hour apart), after a console login
or logout, and when Apple refuses the token.

The backend keeps its own copy of the last token `GET /token` returned for each account, in
`usertoken/` next to the database file (`wrapper-token.json` for the implicit account,
`account-<id>.json` for another; directory mode 0700, file 0600). It lets the backend work
on while the wrapper restarts. It is replaced by every new token, deleted when the wrapper
answers that no account is logged in (`503`) or that it has no `GET /token` (`404`), and not
used once `wrapper.address` points at another wrapper. The token is never logged and no API
response carries it.

A wrapper without `GET /token` (upstream wrapper-lite, older fork builds, a wrapper-manager
that does not forward the route) answers `404`; the backend asks again only every 30
minutes and has no token meanwhile. Albums, songs and playlists still download, with
lyrics and AAC-LC licenses from the wrapper; stations, private playlist covers, library
sync and the Apple library do not work.

`catalog.default_storefront` applies only while no account is known: a fresh install before
the first sign-in, or a backend without a wrapper or with `simulate.enabled`. Search,
browse, record reads and links without a region then use it. Once an account exists, the
default account's region wins, and `GET /api/v1/system` reports the effective value.

## Catalog tokens and the Hi-Res manifest

Every catalog read goes to `amp-api.music.apple.com` with a bearer token the backend
scrapes from `music.apple.com`; `catalog.token_cache_ttl_hours` sets how long it is
trusted. The backend holds no Apple developer key and never signs a token.

`catalog.language` is the language Apple answers in: the genre tag, the album, song and
artist names that go into tags and output paths, and lyrics translations. Set it before
the first download; changing it later renames what new downloads write, so the
"already downloaded" check misses files saved under the old names.

The Enhanced HLS master playlist normally comes from the catalog response (`enhancedHls`).
The web player's playlist can leave out a Hi-Res track's higher ALAC renditions that the
wrapper's device manifest lists: an 88.2, 96 or 192 kHz stream may be missing while 48 kHz
is there, and nothing in the playlist says so. So a track with the `hi-res-lossless` audio
trait takes the device manifest from the wrapper (`/m3u8`) when `download.alac_max_sample_rate`
is above 48000 or is 0 or less (no ceiling) and the codec is ALAC. From 1 to 48000, for Atmos
and AAC, and for any other track, the catalog playlist is used and the wrapper is not asked, because the wrapper serves
`/m3u8` and `/key` from one lock. The answer is remembered per storefront and song for 12
hours (at most 1024 songs), and dropped as soon as the playlist it points to fails to load.
If the wrapper cannot supply a manifest the track keeps the catalog playlist and may be
capped at 48 kHz. `download.alac_max_sample_rate` then picks from whichever playlist was read.

`catalog.motion_artwork_enabled` (default `true`) controls the lookup of Apple's animated
album covers. When on, one extra `amp-api` request per album runs out of band on a context
detached from the job — it can never block or fail a download, and the fields arrive a
moment later via the `motion_artwork_resolved` event. Turn it off if nothing you use
renders animated covers and you would rather not generate undocumented-endpoint traffic.

## Simulate mode

`simulate.enabled` runs the pipeline without downloading or decrypting anything: no
`ffmpeg`, no output file, and the wrapper decryptor status check is skipped so jobs can be
submitted without a running wrapper. Catalog metadata and Enhanced HLS media selection
still run for real, so titles, artwork and the reported bit depth / sample rate / bitrate
are authentic and selection failures fall back through `quality_priority` exactly like a
real download. Every API response, status, progress breakdown and SSE event matches a real
job. Transfer speed is randomised between `min_speed_kbps` and `max_speed_kbps`; raise
both for a Hi-Res album to finish in seconds. A
simulated run records its slots as downloaded like a real one, across restarts, but writes
no file, so a disk check (`POST /api/v1/library/check`) finds them missing.

Without a running wrapper a Hi-Res track keeps the catalog playlist, which may lack its
88.2 kHz and higher ALAC renditions.
