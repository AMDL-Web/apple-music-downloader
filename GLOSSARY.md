# Glossary

Words the maintainer, contributors and agents use in issues, PRs and docs. Each
entry says in one line what the word means and points to its source; the source
wins when they disagree, so details are not copied here.

## Apple Music

| Term | Meaning | Source |
| --- | --- | --- |
| amp-api | `amp-api.music.apple.com`: Apple's undocumented web-player API, called with a scraped JWT and an `Origin` header; the only source of some fields | `internal/applemusic#CatalogClient.doWithWebAuth` |
| Web-player JWT | The token scraped from music.apple.com's JS bundle that amp-api requires | `internal/applemusic#CatalogClient.doWithWebAuth` |
| Media-user-token | A signed-in account's token; radio stations, private playlists, library sync and the Apple library need it, and with it the backend fetches lyrics and the AAC-LC playlist and license from Apple itself. The wrapper's own (its `GET /token` or `MUSIC_TOKEN` file) when the backend can get it, else `catalog.media_user_token` | `internal/usertoken#Provider.Token` |
| Storefront | The country code in a link (`us`, `cn`) that selects Apple's regional catalog | `configs/config.example.yaml#catalog.default_storefront` |
| enhancedHls | The amp-api-only HLS manifest that lists the lossless and spatial variants | `internal/applemusic#Song` |
| editorialVideo, motion artwork | Animated album covers, amp-api only; absence is normal, never an error | `internal/applemusic#CatalogClient.MotionArtworkViaWebToken` |
| L3 | The Widevine security level the pipeline runs at; it caps video quality, which is why music videos are out of scope | [docs/download-pipeline.md](docs/download-pipeline.md#download-pipeline) |

## Jobs and pipeline

| Term | Meaning | Source |
| --- | --- | --- |
| wrapper, wrapper-lite | The HTTP service that holds the Apple account session and serves device manifests and per-key decryption templates, and lyrics and AAC-LC licenses when the backend has no media-user-token to ask Apple itself; required for real downloads | `internal/wrapper#Client` |
| wrapper-manager | Upstream v2 gateway in front of one wrapper-lite per account; the only upstream wrapper with the login API behind the console's Account page (`login_supported`) | `internal/wrapper#Client.StartLogin` |
| Managed wrapper | A wrapper-lite that amdl runs itself in a QEMU guest on the backend's machine (`amdl wrapper install`); it logs in by booting the guest with the account, and `GET /wrapper/status` describes it in `managed` | `internal/wrapperhost#Supervisor` |
| Key template | The `ctx` and `state` tables wrapper-lite's `/key` returns for a FairPlay key URI; `internal/fairplay` decrypts samples with it, in-process | `internal/fairplay#Template` |
| Job | One submitted link (song, album, playlist, artist or station) and its lifecycle | `internal/domain#Job` |
| Item | One track inside a job | `internal/domain#JobItem` |
| Stage breakdown | An item's durable per-stage flags (resolved, remuxed, verified, tagged, saved) that let a reconnecting client redraw progress | `internal/domain#ItemProgress` |
| Terminal status | `completed`, `failed` or `cancelled`; written once, with its event | `internal/domain#JobStatus.IsTerminal` |
| Canonical key | A job's dedup identity: type, storefront and id | `internal/jobs#canonicalKey` |
| Run order | The order queued jobs start in, lowest `queue_order` first; a move changes it | `internal/db#Store.MoveQueuedJob` |
| Quality priority | The ordered codec list a track is tried in | `configs/config.example.yaml#download.quality_priority` |
| Codec fallback | Moving to the next codec when one codec's attempts run out; AAC-LC over WebPlayback is the implicit last resort | [docs/download-pipeline.md](docs/download-pipeline.md#retries-and-codec-fallback) |
| Checkpoint | The resumable on-disk copy of an Enhanced HLS track's encrypted media in `download.temp_dir`, read back fragment by fragment during decrypt | [docs/download-pipeline.md](docs/download-pipeline.md#encrypted-checkpoints) |
| Simulate mode | Real catalog data and media selection, then the real pipeline over a pretend media backend: paced transfers, no disk writes, no wrapper or ffmpeg | `internal/media#simulatedMedia` |
| Media backend | The I/O side of the per-track pipeline (transfers, wrapper, ffmpeg, disk); production and simulate mode differ only here | `internal/media#mediaBackend` |
| Path template | The format that decides where a finished track lands: one library template for songs, albums and artists, one each for playlists and stations | `internal/media#libraryTrackPath`, `internal/media#outputPath` |
| Library, library layout | The folder tree songs, albums and artists share under `download.library_path_format`: one path per track whichever job type downloads it, and a file there counts as downloaded. Playlists and stations are not part of it | [docs/download-pipeline.md](docs/download-pipeline.md#library-and-skipping) |
| On disk, library state | Whether a track's file is at its library path; `library` (`complete`, `partial` with a count, `none`) sums it for an album or song, exact from its track list, estimated from a folder count for album cards, or one stat for a song hit | `internal/domain#LibraryState` |
| Queue entry | The one job row a link keeps: a new submit of a finished link queues that row again under its id (`requeued`), and `prior_job` names it; stations get a new row each time | `internal/jobs#Manager.SubmitBatch` |
| Download again | A submit with `force_overwrite`: nothing on disk is skipped, and each file is replaced once its new copy is complete | [docs/download-pipeline.md](docs/download-pipeline.md#library-and-skipping) |
| Quality label | The `{Quality}` path value, such as `24-bit/96 kHz` or `256Kbps` | `internal/media#qualityLabel` |
| Standalone cover | `cover.<ext>` or `artist.<ext>` written next to the audio | [docs/download-pipeline.md](docs/download-pipeline.md#standalone-covers) |

## API, config and automation

| Term | Meaning | Source |
| --- | --- | --- |
| Event | A persisted job event; its id is the SSE resume cursor | `internal/domain#Event` |
| Overview feed | `GET /api/v1/downloads/events`: one message per changed job for list views | `internal/api#Server.downloadsFeed` |
| Event cursor | `event_cursor` in `GET /downloads` and `GET /downloads/{id}`: the event id a client resumes the matching stream from (`?cursor=` or `Last-Event-ID`) | `internal/api#Server.listDownloads` |
| Error code | The stable `code` of an error response; clients switch on it, never on the message | `internal/api#ErrorCode` |
| Runtime key, restart key, file-only key | Config keys `PUT /api/v1/config` applies at once, keys it saves for the next start (`pending_restart`), and the three it refuses (`server.listen`, `database.path`, `tools.ffmpeg`) | `internal/config#classOf` |
| Seed | A first value `amdl serve --init key=value` writes into a config.yaml it creates | `internal/config#Seed` |
| Job overrides | The per-submission `overrides` object that overlays the quality and `force_overwrite` keys of the runtime config for one batch | `internal/config#DownloadOverrides` |
| Job hook | A webhook or command fired on `job_queued` or a terminal job event | `internal/hooks#Dispatcher` |
| Library sync | The poller that turns newly added library songs into album jobs | `internal/librarysync#Watcher` |
| Apple library | The signed-in Apple Music account's own library (albums, songs, playlists, artists), as Apple Music calls it; amdl keeps a local copy for `/api/v1/apple-library` and `in_apple_library`. Not the download library on disk, which the console and `amdl library list` call the library | `internal/applelibrary#Engine` |
| Watermark | Library sync's position-based anchor in the newest-first library list | [docs/automation.md](docs/automation.md#why-the-watermark-is-a-position-not-a-timestamp) |
| Skin | The console's look, one skin (`music`) on `<html data-skin>`, separate from the light/dark theme | [apps/console/DESIGN.md](apps/console/DESIGN.md#skin) |

## Repository workflow

| Term | Meaning | Source |
| --- | --- | --- |
| dev, main | Work lands on `dev`; `main` changes only through the release PR | [AGENTS.md](AGENTS.md#commits-and-releases) |
| DCO sign-off | The `Signed-off-by` trailer every commit needs (`git commit -s`) | [CONTRIBUTING.md](CONTRIBUTING.md#sign-off-dco) |
| Move-only split | Moving declarations between files of one package with no code change, proven by `tools/splitdecls` verify | [amdl-refactor](.claude/skills/amdl-refactor/SKILL.md) |
| Revert proof | `tools/revertproof`: a regression test must fail an assertion with the fix reverted, then pass every repeat at HEAD | [amdl-verify](.claude/skills/amdl-verify/SKILL.md#revert-proof) |
| Repo checks | Go tests that keep docs pointers and agent files honest | `tools/repocheck` |
| Guard hook | The Claude Code PreToolUse script that blocks forbidden git and gh commands | `.claude/hooks/guard-commands.mjs` |
| Dev server | `make dev`: simulate mode on 127.0.0.1:18080 with data under `data/dev/` | `Makefile#dev` |
