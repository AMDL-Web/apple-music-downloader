# Configuration

> Type: reference

Everything lives in one file. `AMDL_CONFIG` picks its path; the default is:

```text
configs/config.yaml
```

On first start the backend generates it from
[`configs/config.example.yaml`](../configs/config.example.yaml), which must stay in the
same directory. **The example file is the documentation** — allowed enum values, units,
defaults and template variables are written into its comments, key by key. `config.yaml`
itself is managed by the backend and its comments do not survive a write.

## Defaults

A key missing from `config.yaml` takes the value `configs/config.example.yaml` sets, so a
sparse file behaves the way the example documents. The one exception is
`download.album_path_format` and `download.artist_path_format`: a config without them
predates `{SongNumber}` paths and keeps the older `{TrackNumber}` layout, so files already
downloaded are still found. Tests in `internal/config` hold `Default()` equal to the
example and every `Default:` note in its comments equal to `Default()`.

Unknown keys fail startup with an error naming each one; keys removed in 2.0 are listed in
[Upgrading to 2.0](upgrading-to-2.0.md#config-and-environment). That includes the old
per-job concurrency keys such as `download.max_parallel_tracks`, replaced by the
process-wide pools in [download-pipeline.md](download-pipeline.md#concurrency).

## Runtime vs startup keys

`PUT /api/v1/config` may only change runtime-mutable keys, but it rewrites the whole file
including startup keys, dropping all comments in the process.

| | |
| --- | --- |
| **Startup** | `server.listen`, `database.path`, `wrapper.*`, `tools.ffmpeg`, most of `logging.*`, the developer-token signing keys, and every pool size (`download.max_running_jobs`, `max_parallel_downloads`, `max_parallel_decrypts`, `max_parallel_wrapper_requests`, `catalog.max_parallel_requests`, `catalog.requests_per_second`, `catalog.request_burst`) |
| **Runtime** | `logging.level`, `logging.access_log`, `catalog.album_track_url_mode`, `catalog.media_user_token`, `catalog.signed_mode_hls_source`, `catalog.motion_artwork_enabled`, all remaining `download.*` keys, the whole `simulate` section and the whole `library_sync` section |

Hand edits reach the running service like this:

| Trigger | Effect |
| --- | --- |
| `GET /api/v1/config` | Returns the in-memory snapshot; never reads the file. |
| `POST /api/v1/config/reload` or `SIGHUP` | Re-reads the file and applies its runtime keys. If the file is unreadable or invalid, the previous snapshot stays live; the endpoint reports the error as `reload_error`, and `SIGHUP` logs it. |
| `PUT /api/v1/config` | Re-reads the file under the same lock first, then applies the patch, so a hand edit made since the last reload is kept rather than overwritten. An invalid file fails the `PUT` and changes nothing. |
| Restart | Applies startup keys. A reload or `PUT` never makes a startup-key edit live, and a `PUT` keeps the edited value on disk for the next start. |

Set these before first real use:

- `server.listen` — API listen address.
- `wrapper.address` — `wrapper-manager` gRPC address.
- `database.path` — SQLite file (default `data/db/amdl.db`).
- `download.downloads_dir` — where finished files go.
- `logging.*` — format, in-memory retention, optional rotating file.
- `tools.ffmpeg` — path or command name.

## Environment overrides

Any key can be overridden with `AMDL_<SECTION>_<KEY>` — the YAML path uppercased with `_`
as the separator: `AMDL_SERVER_LISTEN`, `AMDL_WRAPPER_ADDRESS`, `AMDL_DATABASE_PATH`,
`AMDL_LOGGING_LEVEL`, `AMDL_DOWNLOAD_QUALITY_PRIORITY`.

- Overrides sit on top of the file on every start, reload and `PUT`. Loading alone never
  writes them back.
- Value syntax: strings verbatim; booleans `true`/`false`; integers as digits; string
  lists comma-separated (`alac,aac`), with an empty value meaning an empty list.
- Unrecognised `AMDL_*` variables **fail startup**, so a typo can never be silently
  ignored. Only `AMDL_CONFIG` (path of `config.yaml`) and `AMDL_HOOKS_CONFIG` (path of
  `hooks.yaml`) are exempt; they are file locations, not keys. Variables that no longer
  exist fail the same way, for example `AMDL_RUNTIME_CONFIG`, `AMDL_LISTEN` (use
  `AMDL_SERVER_LISTEN`) and `AMDL_WRAPPER_ADDR` (use `AMDL_WRAPPER_ADDRESS`).
- A field pinned by an environment variable cannot be changed through
  `PUT /api/v1/config` — it returns `422`. Change the variable and restart. Every config
  answer lists the pinned runtime keys with their variables in `pinned`, so a client can
  disable those fields.
- A `PUT` persists the effective runtime snapshot, so an unchanged runtime field pinned by
  an environment variable may be written into the file during an unrelated `PUT`.
  Startup-field overrides are never baked in.

## Developer-token signing

The three `catalog.apple_music_*` keys form one unit.

| State | Behaviour |
| --- | --- |
| All three empty | Legacy mode. The bearer token is scraped from `music.apple.com` and catalog metadata is read from `amp-api.music.apple.com`, with `enhancedHls` included in the response. |
| All three set | The backend signs its own 24-hour ES256 developer token at startup (any signing error fails startup) and reads catalog metadata from the official `api.music.apple.com`. That token cannot read `enhancedHls`, so the Enhanced HLS source is chosen separately by `catalog.signed_mode_hls_source`. |
| Only some set | Startup fails with a configuration error. |

`catalog.signed_mode_hls_source` only matters in signed mode: `wrapper` (default) takes
the master playlist from the wrapper's authorized device manifest; `web_token` scrapes a
web-player JWT and reads `enhancedHls` from `amp-api.music.apple.com` independently of the
signed token. Use `web_token` when the wrapper cannot supply a usable device manifest.

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
job. Transfer speed is randomised between `min_speed_kbps` and `max_speed_kbps`.

Note that with developer-token signing enabled, manifests normally come from the wrapper —
without one, selection falls back to a faked AAC-LC.
