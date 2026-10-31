# Deployment

> Type: runbook

This page runs amdl in Docker. To install the `amdl` binary and run the backend
without Docker, see [install.md](install.md).

## Docker

[`deploy/docker-compose.yml`](../deploy/docker-compose.yml) pulls the prebuilt multi-arch
image (linux/amd64 + linux/arm64) from GHCR, so nothing is built locally. Copy it to an
empty directory and start it there:

```bash
docker compose up -d
```

Image tags follow the release version — `v1.2.3` publishes `1.2.3`, `1.2` and `latest`.
Pin a version by replacing `:latest` in the compose file with a concrete tag.

> Anonymous pulls need the GHCR package to be public. This repository's is. If you fork
> and self-host, your first push may create a **private** package, and then
> `docker compose up -d` fails with `unauthorized` / `denied` — flip it to public in your
> repository's Packages settings, or build locally instead.

To build from source, uncomment the `build:` block in `deploy/docker-compose.yml` in a
clone and start Compose from `deploy/`. The build context is the repository root and the
Dockerfile is `deploy/server/Dockerfile`; the configs and data then live under `deploy/`:

```bash
cd deploy && docker compose up -d --build
```

Or without compose:

```bash
docker build -f deploy/server/Dockerfile -t amdl-backend .
docker run -d --name amdl-backend \
  --stop-timeout 120 \
  -p 18080:18080 \
  -v ./configs:/app/configs \
  -v ./data/db:/app/data/db \
  -v ./data/downloads:/app/data/downloads \
  --add-host host.docker.internal:host-gateway \
  amdl-backend
```

The Dockerfile needs no BuildKit: `docker build` works with the classic builder too, as
on Synology DSM, whose Docker has no buildx, and builds for the host's architecture.
Multi-arch images (`--platform linux/amd64,linux/arm64`) need buildx.

Two build args point the build at mirrors where `proxy.golang.org` or
`dl-cdn.alpinelinux.org` is unreachable; the defaults leave both unchanged:

| Build arg | Default | Meaning |
| --- | --- | --- |
| `GOPROXY` | `https://proxy.golang.org,direct` | Go module proxy for the build stage. `go.sum` pins every module, so `GOSUMDB` needs no change. |
| `ALPINE_MIRROR` | `https://dl-cdn.alpinelinux.org/alpine` | Replaces that URL in the runtime stage's `/etc/apk/repositories` before `apk add`; the mirror stays in the image's file. |

```bash
docker build -f deploy/server/Dockerfile -t amdl-backend \
  --build-arg GOPROXY=https://goproxy.cn,direct \
  --build-arg ALPINE_MIRROR=https://mirrors.tuna.tsinghua.edu.cn/alpine .
```

With compose, uncomment the `args:` under `build:`; `deploy/docker-compose.yml` has them
as a comment.

The image is a multi-stage build: the build stage cross-compiles a static binary
(`CGO_ENABLED=0`, pure-Go SQLite), and the runtime stage is Alpine with `ffmpeg`,
`ca-certificates`, `tzdata` and `su-exec`. The container starts the entrypoint as root,
seeds config and gives the backend's own state to `PUID:PGID` (default `1000:1000`), then
drops to that user via `su-exec` to run the backend.

The container health check hits `GET /api/v1/health`, deriving the port from
`AMDL_SERVER_LISTEN`.

## Stopping and restarting

`docker compose stop`, `restart` and `up -d` after an image update are safe while jobs
run. On SIGTERM the backend stops taking requests, ends open event streams, and stops
its workers without cancelling their jobs: those stay `running` in the database and
resume on the next start (`job_recovered`), skipping tracks that already finished.
Partial transfers resume too while the container's `data/tmp` survives (a restart keeps
it; recreating the container starts those tracks over). Each shutdown step is bounded,
about 105 s in the worst case, so the container needs more than Docker's default 10 s:
`deploy/docker-compose.yml` sets `stop_grace_period: 2m`, and `docker run` needs
`--stop-timeout 120`. With less, Docker kills the process before it closes the database.
The order of the steps is in [Architecture](architecture.md#1-process-and-lifecycle).

After a hand edit of `config.yaml`, `POST /api/v1/config/reload` (the console's "Reload
from file") applies its runtime keys and logging level; an invalid file is reported and
the running config stays.

## Behind a reverse proxy

The service has no authentication of its own; put access control in the proxy in front
of it. Its event streams (`/api/v1/downloads/events`, `/api/v1/downloads/{id}/events`,
`/api/v1/logs/stream`) are Server-Sent Events and send `X-Accel-Buffering: no`, which
nginx honors; for other proxies, or an nginx that ignores the header
(`proxy_ignore_headers`), turn response buffering off for those paths
(`proxy_buffering off;` in nginx), and allow a read timeout above the 15 s keepalive
interval.

## Config seeding

The entrypoint syncs the image's bundled examples into `/app/configs`:

- `config.example.yaml` — copied from the current image on **every** start, so the field
  documentation tracks the version you are running. The backend's bootstrap requires it
  next to `config.yaml`.
- `hooks.yaml` — a fully commented template, copied only when the file is missing, and
  never overwritten.

`config.yaml` is generated by the backend on first start and never touched by the
entrypoint, so what `PUT /api/v1/config` writes survives a container rebuild.

Two repository defaults are unusable inside a container, so the entrypoint rewrites them
on every start through two of the environment overrides rather than baking them into the
file: `server.listen` becomes `AMDL_SERVER_LISTEN` (default `:18080` — a
container must not listen on loopback) and `wrapper.address` becomes
`AMDL_WRAPPER_ADDRESS` (default `http://host.docker.internal:12340`).

## Environment variables

The backend reads six `AMDL_*` variables (see
[configuration.md](configuration.md#environment-overrides)); every other key is set in
`config.yaml`, and any other `AMDL_*` variable is ignored with a startup warning.
Container variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `PUID` | `1000` | UID the backend runs as. Applied on every start, when the entrypoint gives the backend's own state to `PUID:PGID` (see [Ownership](#ownership)); the downloads directory is never re-owned. |
| `PGID` | `1000` | GID the backend runs as. Same behaviour as `PUID`. |
| `TZ` | UTC | Container timezone (IANA name, e.g. `Asia/Shanghai`). Affects log timestamps and the local time exec hooks see. `tzdata` is in the image. |
| `AMDL_SERVER_LISTEN` | `:18080` (entrypoint default) | Overrides `server.listen` on every start. The health check derives its port from this. |
| `AMDL_WRAPPER_ADDRESS` | `http://host.docker.internal:12340` (entrypoint default) | Overrides `wrapper.address` on every start. |
| `AMDL_WRAPPER_DATA_DIR` | `wrapper.data_dir` from `config.yaml` | Overrides `wrapper.data_dir` on every start: where the backend reads the wrapper's media-user-token ([The wrapper](#the-wrapper)). |
| `AMDL_CONFIG` | `/app/configs/config.yaml` | Config file path (native backend support). |
| `AMDL_HOOKS_CONFIG` | `/app/configs/hooks.yaml` | Hooks file path (native backend support). |
| `AMDL_DATABASE_PATH` | `database.path` from `config.yaml` | Overrides `database.path` on every start. |

Two ways to change configuration: runtime keys via `PUT /api/v1/config` (immediate, the
console's Settings page); or by editing `config.yaml` in the mounted directory — runtime
keys apply on `POST /api/v1/config/reload` (see
[Stopping and restarting](#stopping-and-restarting)), startup keys need a container
restart. Inside the container the listen and wrapper
addresses always come from the environment. Remember that a `PUT` rewrites the whole file
and drops comments.

> **Breaking change**: 2.0 reads only the six `AMDL_*` variables above. The old names
> `AMDL_LISTEN` and `AMDL_WRAPPER_ADDR`, and the per-key overrides such as
> `AMDL_SIMULATE_ENABLED`, are ignored with a warning; see
> [Upgrading to 2.0](upgrading-to-2.0.md#config-and-environment).

To avoid running as root at all, use `docker run --user <uid>:<gid>` (or compose's
`user:`). The entrypoint then skips the privilege drop and the ownership fixes, ignores
`PUID`/`PGID`, and you are responsible for making the mounts writable by that user; the
image's own directories, `data/tmp` included, already are (see [Mounts](#mounts)).

### Shares granted through a group (Synology)

`PUID`/`PGID` mode runs the backend with exactly one group, `PGID`: `su-exec` drops every
supplementary group, and Compose's `group_add` has no effect there. On a share whose
write access comes through a supplementary group, such as Synology DSM's
`administrators` (gid 101), run the container as that user instead, with the extra group
added:

```yaml
services:
  amdl-backend:
    user: "1026:100"   # the DSM user's uid and its primary gid (users)
    group_add:
      - "101"          # administrators, which grants write on the share
```

Check the ids with `id <user>` on the NAS. This shape ran on a Synology DS923+ (DSM 7.3.2).
The mounts still need write access for that uid or one of its groups.

## Mounts

`deploy/docker-compose.yml` bind-mounts three directories:

| Host | Container | Contents |
| --- | --- | --- |
| `./configs` | `/app/configs` | `config.yaml`, `config.example.yaml`, `hooks.yaml` |
| `./data/db` | `/app/data/db` | SQLite database (`database.path`, default `data/db/amdl.db`) |
| `./data/downloads` | `/app/data/downloads` | Finished files. Change the host side to put them elsewhere, e.g. `/path/to/music:/app/data/downloads` |

`data/tmp` stays inside the container and needs no mount, whatever uid the backend runs
as: the image creates `/app/configs` and `/app/data` with its `db`, `tmp` and `downloads`
directories world-writable with the sticky bit, like `/tmp`, so a `user:` uid can write
to any of them that is not mounted. A mount brings the host directory's own permissions
instead. `data/tmp` is the staging area for the
download, decrypt, remux, verify and tag steps (see `download.temp_dir` in
[`configs/config.example.yaml`](../configs/config.example.yaml)). If your storage driver
is slow, mount it separately onto a fast host disk, kept apart from `data/downloads`.

Logs go to stdout (`docker logs amdl-backend`) and to the in-memory buffer the console's
Logs page reads; the backend writes no log file, so there is no log mount.

### Ownership

Set `PUID`/`PGID` to the uid/gid that owns the host directories. Started as root, the
entrypoint gives only the backend's own state to `PUID:PGID`, and only entries whose owner
differs:

- the config directory itself, `config.yaml`, `config.example.yaml` and `hooks.yaml`;
- `/app/data` itself;
- the database directory and its files (`amdl.db`, `-wal`, `-shm`, `-journal`), at
  `AMDL_DATABASE_PATH` or the default `data/db/amdl.db`;
- `/app/data/tmp` and everything in it, without crossing into another mount.

It never touches `/app/data/downloads`, which is usually a music library. That directory
must already be writable by `PUID:PGID`: create the host directory before the first start
(Docker creates a missing bind-mount source as root) and give it to that uid/gid, for
example `mkdir -p data/downloads && chown 1000:1000 data/downloads`. The entrypoint logs a
warning at start when it is not writable. A `database.path` or `download.temp_dir` set
only in `config.yaml` to another place is not re-owned either.

To check the entrypoint after changing it, start the container as root with a `PUID` that
differs from the owner of a bind-mounted downloads directory holding a file, then compare
`stat -c '%u:%g'` of that directory and file before and after: neither may change, while
`/app/data/tmp` and the database files now belong to `PUID:PGID`.

## The wrapper

Real downloads need a wrapper: the HTTP service that holds the Apple Music account
session and serves device manifests, lyrics, web playback and Widevine licenses, plus a
decryption template per key that the backend runs in-process. Upstream publishes no
container image for either option below, so the compose file builds them from their
repositories. Neither the backend nor the console needs the wrapper to start; verify the
connection with `GET /api/v1/wrapper/status`.

| Option | Upstream | Port | Login |
| --- | --- | --- | --- |
| wrapper-manager v2 (recommended) | [wrapper-manager `v2` branch](https://github.com/WorldObservationLog/wrapper-manager/tree/v2): supervises one wrapper-lite per account behind one HTTP port | 8080 | From the console's Account page, through its `POST /login` |
| wrapper-lite | [wrapper `lite` branch](https://github.com/WorldObservationLog/wrapper/tree/lite): one account | 12340 | On its own command line (`--login user:pass`, with `--code-from-file` for 2FA); the console shows that command |

Run natively or in upstream's Docker image, both use `wrapper-lite-rootless`, which needs
user namespaces and `chroot` on a Linux host (x86_64 or arm64): `unshare -Ur true` must
succeed there. Docker blocks them by default, so the wrapper-manager service runs
`privileged: true` (upstream's compose) and the wrapper-lite service
`security_opt: seccomp:unconfined` (upstream's compose); a seccomp profile that allows
`unshare` is the alternative.

A host without user namespaces, such as Synology DSM, cannot run that launcher. Run
wrapper-manager v2's QEMU variant instead (upstream's section
["Run on any OS via QEMU (wrapper-manager-qemu)"](https://github.com/WorldObservationLog/wrapper-manager/tree/v2)),
which runs the manager inside a QEMU guest with its own kernel and needs `/dev/kvm`:

- In Docker, `--device /dev/kvm` was the only extra flag needed; no `privileged` and no
  added capabilities.
- Start the launcher with `--host 0.0.0.0` so the published port reaches it.
- The QEMU in the container must have the `user` (slirp) network backend. DSM's own QEMU
  8.1.5 lacks it; Alpine's `qemu-system-x86_64` package has it.
- A guest started this way answered `/status` with `ready: true` on a Synology DS923+
  (DSM 7.3.2). Logging in an Apple account inside the guest is not verified.

`deploy/docker-compose.yml` has both as commented services. To use one, uncomment it and set
`AMDL_WRAPPER_ADDRESS` to its URL:

- wrapper-manager as a compose service: `http://wrapper-manager:8080`. It keeps its
  accounts and the wrapper-lite payload in the `wrapper-data` volume; the first start
  downloads the payload from nightly.link, so it needs internet access. Log in on the
  console's Account page, then check that `GET /api/v1/wrapper/status` lists a region.
- wrapper-lite as a compose service: `http://wrapper-lite:12340`. Put `USERNAME` and
  `PASSWORD` in a `.env` file next to the compose file for the first start, which logs
  in; with a 2FA code, write it to `2fa.txt` in the mounted data directory. The image
  build downloads the Android NDK and is slow.
- wrapper-lite on the host: keep the default `http://host.docker.internal:12340`. The
  compose file's `extra_hosts: host-gateway` entry makes that name resolvable on Linux
  too. Start it with `--host 0.0.0.0` so the container can reach it, as `--host` defaults
  to `127.0.0.1`.
- Outside Docker, set `wrapper.address` in `config.yaml` to the URL, `http://127.0.0.1:12340`
  by default. A bare `host:port` fails startup, and `https://` verifies the certificate
  against the system trust store.

The backend uses the media-user-token the wrapper keeps for its account (`MUSIC_TOKEN`)
when it can read the wrapper's files, so station downloads, private playlist covers and
library sync need no token pasted into Settings
([configuration.md](configuration.md#the-media-user-token)). With a compose service, mount
its data into the backend read-only and point `AMDL_WRAPPER_DATA_DIR` at the mount; the
compose file has both commented:

- wrapper-manager: mount the `wrapper-data` volume at `/app/data/wrapper-manager:ro` and
  set `AMDL_WRAPPER_DATA_DIR` to `/app/data/wrapper-manager/wrapper/rootfs/data`, where it
  keeps `instances/<id>/MUSIC_TOKEN`.
- wrapper-lite: mount `./data/wrapper` at `/app/data/wrapper:ro` and set
  `AMDL_WRAPPER_DATA_DIR` to `/app/data/wrapper`.
- wrapper-lite on the same host outside Docker: mount its base directory the same way
  (`<launcher directory>/rootfs/<base directory>`). Natively, set `wrapper.data_dir` to it.

The backend reads the file as the user it runs as (`PUID`), so the file must be readable
by that user. `GET /api/v1/wrapper/status` shows the token it reads, masked, as
`media_user_token`; `""` means it reads none and `catalog.media_user_token` applies.

wrapper-lite's API has no authentication, upstream says so, and wrapper-manager's README
documents none, so keep both ports off any network you do not trust, as with this
service's own.

## Releases

Two entry points, both of which run the full test suite, create the tag and GitHub
Release, then cascade into `Docker Publish`:

- Merging a `dev` → `main` pull request whose title carries a version (e.g.
  `Release v1.2.0: …`). A merged PR without a parseable version fails the run rather than
  silently skipping it.
- Running the `Release` workflow manually with an explicit version — the fallback path.

Pushing a tag does not trigger a release.

Release notes are read from the repository first:

```text
.github/release-notes/<version>.md
```

`v1.4.0` uses `.github/release-notes/v1.4.0.md`. Only when that file is missing or empty
does the workflow fall back to generating a changelog from commits, via
`.github/scripts/generate-changelog.js`. Generated entries list the commit author plus any
`Co-authored-by` collaborators, deduplicated.

`Docker Publish` builds linux/amd64 + linux/arm64 and pushes to
`ghcr.io/amdl-web/apple-music-downloader` with tags `{version}`, `{major}.{minor}`
and, conditionally, `latest`. It can also be dispatched manually against an existing tag
to re-issue an image; manual runs do **not** move `latest` unless you tick the option, so
re-publishing an old version cannot drag `latest` backwards. Releases published by hand
from the GitHub UI trigger it too, and prereleases never move `latest`.
