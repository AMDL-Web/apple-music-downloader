# Glossary

Words the maintainer, contributors and agents use in issues, PRs and docs. Each
entry says in one line what the word means and points to its source; the source
wins when they disagree, so details are not copied here.

## Apple Music

| Term | Meaning | Source |
| --- | --- | --- |
| Public API host | `api.music.apple.com`: Apple's documented Music API, called with the developer token | `internal/applemusic#CatalogClient.apiBase` |
| amp-api | `amp-api.music.apple.com`: Apple's undocumented web-player API, called with a scraped JWT and an `Origin` header; the only source of some fields | `internal/applemusic#CatalogClient.doWithWebAuth` |
| Developer token | Self-signed Apple Music JWT made from a `.p8` key, used for the backend's own catalog reads | `internal/applemusic#CatalogClient.InitDeveloperToken` |
| Web-player JWT | The token scraped from music.apple.com's JS bundle that amp-api requires | `internal/applemusic#CatalogClient.doWithWebAuth` |
| Signed vs legacy mode | Whether developer-token signing is configured; it decides which host catalog reads use | [docs/configuration.md](docs/configuration.md#developer-token-signing) |
| Media-user-token | A signed-in account's token; radio stations, private playlists and library sync need it | `configs/config.example.yaml#catalog.media_user_token` |
| Storefront | The country code in a link (`us`, `cn`) that selects Apple's regional catalog | `configs/config.example.yaml#catalog.default_storefront` |
| enhancedHls | The amp-api-only HLS manifest that lists the lossless and spatial variants | `internal/applemusic#CatalogClient.EnhancedHLSViaWebToken` |
| editorialVideo, motion artwork | Animated album covers, amp-api only; absence is normal, never an error | `internal/applemusic#CatalogClient.MotionArtworkViaWebToken` |
| L3 | The Widevine security level the pipeline runs at; it caps video quality, which is why music videos are out of scope | [docs/download-pipeline.md](docs/download-pipeline.md#download-pipeline) |

## Jobs and pipeline

| Term | Meaning | Source |
| --- | --- | --- |
| wrapper, wrapper-manager | The gRPC service that holds the Apple account session and serves device manifests, licenses and lyrics; required for real downloads | `internal/wrapper#Client` |
| Job | One submitted link (song, album, playlist, artist or station) and its lifecycle | `internal/domain#Job` |
| Item | One track inside a job | `internal/domain#JobItem` |
| Stage breakdown | An item's durable per-stage flags (resolved, remuxed, verified, tagged, saved) that let a reconnecting client redraw progress | `internal/domain#ItemProgress` |
| Terminal status | `completed`, `failed` or `cancelled`; written once, with its event | `internal/domain#JobStatus.IsTerminal` |
| Canonical key | A job's dedup identity: type, storefront and id | `internal/jobs#canonicalKey` |
| Quality priority | The ordered codec list a track is tried in | `configs/config.example.yaml#download.quality_priority` |
| Codec fallback | Moving to the next codec when one codec's attempts run out; AAC-LC over WebPlayback is the implicit last resort | [docs/download-pipeline.md](docs/download-pipeline.md#retries-and-codec-fallback) |
| Memory mode | `low` checkpoints the encrypted track to disk, `high` keeps it in RAM | `configs/config.example.yaml#download.memory_mode` |
| Simulate mode | Real catalog data and media selection, then the real pipeline over a pretend media backend: paced transfers, no disk writes, no wrapper or ffmpeg | `internal/media#simulatedMedia` |
| Media backend | The I/O side of the per-track pipeline (transfers, wrapper, ffmpeg, disk); production and simulate mode differ only here | `internal/media#mediaBackend` |
| Path template | The per-job-type format that decides where a finished track lands | `internal/media#outputPath` |
| Quality label | The `{Quality}` path value, such as `24-bit/96 kHz` or `256Kbps` | `internal/media#qualityLabel` |
| Standalone cover | `cover.<ext>` or `artist.<ext>` written next to the audio | [docs/download-pipeline.md](docs/download-pipeline.md#standalone-covers) |

## API, config and automation

| Term | Meaning | Source |
| --- | --- | --- |
| Event | A persisted job event; its id is the SSE resume cursor | `internal/domain#Event` |
| Overview feed | `GET /api/v1/downloads/events`: one message per changed job for list views | `internal/api#Server.downloadsFeed` |
| Event cursor | `event_cursor` in `GET /downloads` and `GET /downloads/{id}`: the event id a client resumes the matching stream from (`?cursor=` or `Last-Event-ID`) | `internal/api#Server.listDownloads` |
| Error code | The stable `code` of an error response; clients switch on it, never on the message | `internal/api#ErrorCode` |
| Runtime key, startup key | Config keys that apply through `PUT /api/v1/config` vs keys that need a restart | `internal/config#isRuntimeKey` |
| Env override | `AMDL_SERVER_LISTEN`, `AMDL_WRAPPER_ADDRESS` and `AMDL_DATABASE_PATH`, which sit on top of the config file | `internal/config#envOverrides` |
| Job overrides | The per-submission `overrides` object that overlays the quality and `force_overwrite` keys of the runtime config for one batch | `internal/config#DownloadOverrides` |
| Job hook | A webhook or command fired on `job_queued` or a terminal job event | `internal/hooks#Dispatcher` |
| Library sync | The poller that turns newly added library songs into album jobs | `internal/librarysync#Watcher` |
| Watermark | Library sync's position-based anchor in the newest-first library list | [docs/automation.md](docs/automation.md#why-the-watermark-is-a-position-not-a-timestamp) |
| Skin | The console's look, one skin (`music`) on `<html data-skin>`, separate from the light/dark theme | [internal/console/DESIGN.md](internal/console/DESIGN.md#skin) |

## Repository workflow

| Term | Meaning | Source |
| --- | --- | --- |
| dev, main | Work lands on `dev`; `main` changes only through the release PR | [AGENTS.md](AGENTS.md#commits-and-releases) |
| DCO sign-off | The `Signed-off-by` trailer every commit needs (`git commit -s`) | [CONTRIBUTING.md](CONTRIBUTING.md#sign-off-dco) |
| Move-only split | Moving declarations between files of one package with no code change, proven by `tools/splitdecls` verify | [amdl-refactor](.claude/skills/amdl-refactor/SKILL.md) |
| Repo checks | Go tests that keep docs pointers and agent files honest | `internal/repocheck` |
| Guard hook | The Claude Code PreToolUse script that blocks forbidden git and gh commands | `scripts/guard-commands.mjs` |
| Dev server | `make dev`: simulate mode on 127.0.0.1:18080 with data under `data/dev/` | `Makefile#dev` |
