# Glossary

Words the maintainer, contributors and agents use in issues, PRs and docs. Each
entry says in one line what the word means and points to its source; the source
wins when they disagree, so details are not copied here.

## Apple Music

| Term | Meaning | Source |
| --- | --- | --- |
| amp-api | `amp-api.music.apple.com`: Apple's undocumented web-player API, called with a scraped JWT and an `Origin` header; the only source of some fields | `internal/applemusic#CatalogClient.doWithWebAuth` |
| Web-player JWT | The token scraped from music.apple.com's JS bundle that amp-api requires | `internal/applemusic#CatalogClient.doWithWebAuth` |
| Media-user-token | An account's token; radio stations, private playlists, library sync and the Apple library need it, and with it the backend fetches lyrics and the AAC-LC playlist and license from Apple itself. Read from the wrapper over HTTP for that account (`GET /token`, with `?instance=<id>` on a wrapper-manager); a wrapper without that route gives none | `internal/usertoken#Provider.Token` |
| Storefront | The country code in a link (`us`, `cn`) that selects Apple's regional catalog; `catalog.default_storefront` stands in only while no account is known | `configs/config.example.yaml#catalog.default_storefront` |
| Account | One Apple Music account a wrapper holds (a wrapper-manager instance, at most 10), with amdl's `library_sync` and `apple_library` switches and a position; `GET /api/v1/accounts` | `internal/accounts` |
| Default account | The account at position 0: it reads records with no pinned storefront and and links whose storefront no account serves | `internal/usertoken#Provider` |
| Implicit account | The one account (id `wrapper`) of a wrapper that does not list its accounts, such as a bare wrapper-lite | `internal/domain#ImplicitAccountID` |
| Pinned storefront | The storefront a record's runs read it in (`pinned_storefront`, a pending add's `storefront`): the link's when an account serves it, else the default account's; not `storefront_read`, which is the storefront of the last track-list read | `internal/media#Downloader.RunStorefront` |
| enhancedHls | The amp-api-only HLS manifest that lists the lossless and spatial variants | `internal/applemusic#Song` |
| editorialVideo, motion artwork | Animated album covers, amp-api only; absence is normal, never an error | `internal/applemusic#CatalogClient.MotionArtworkViaWebToken` |
| L3 | The Widevine security level the pipeline runs at; it caps video quality, which is why music videos are out of scope | [docs/download-pipeline.md](docs/download-pipeline.md#download-pipeline) |

## Jobs and pipeline

| Term | Meaning | Source |
| --- | --- | --- |
| wrapper, wrapper-lite | The HTTP service that holds the Apple account session and serves device manifests and per-key decryption templates, and lyrics and AAC-LC licenses when the backend has no media-user-token to ask Apple itself; required for real downloads | `internal/wrapper#Client` |
| wrapper-manager | Upstream v2 gateway in front of one wrapper-lite per account (at most 10 accounts); the only upstream wrapper with the login API behind the console's Account page (`login_supported`) | `internal/wrapper#Client.StartLogin` |
| Managed wrapper | A wrapper-lite that amdl runs itself on the backend's machine, under wrapper-manager in a QEMU guest or, on Linux x86_64, natively (`amdl wrapper install`); it logs in through the manager's HTTP login, and `GET /wrapper/status` describes it in `managed` | `internal/wrapperhost#Supervisor` |
| Key template | The `ctx` and `state` tables wrapper-lite's `/key` returns for a FairPlay key URI; `internal/fairplay` decrypts samples with it, in-process | `internal/fairplay#Template` |
| Record | An album or catalog playlist amdl keeps: its tracks and per track its slots; the only truth of what is downloaded. The API calls it a resource | `internal/domain#Album`, `internal/domain#Playlist` |
| Slot | One quality of one track copy: `main`, the first quality with the plain file name, or a codec, another quality with a suffixed name | `internal/domain#File` |
| Job, run | One download pass over one record (or a station): `/api/v1/downloads` and hooks call it a job | `internal/domain#Job` |
| Item | One planned slot (a station's track) inside a job | `internal/domain#JobItem` |
| Stage breakdown | An item's durable per-stage flags (resolved, remuxed, verified, tagged, saved) that let a reconnecting client redraw progress | `internal/domain#ItemProgress` |
| Terminal status | `completed`, `failed` or `cancelled`; written once, with its event | `internal/domain#JobStatus.IsTerminal` |
| Canonical key | A one-off run's (a station's) dedup identity: type, storefront and id; record runs dedup by their record | `internal/jobs#canonicalKey` |
| Run order | The order queued jobs start in, lowest `queue_order` first; a move changes it | `internal/db#Store.MoveQueuedJob` |
| Quality priority | The ordered codec list a track is tried in | `configs/config.example.yaml#download.quality_priority` |
| Codec fallback | Moving to the next codec when one codec's attempts run out; AAC-LC over WebPlayback is the implicit last resort | [docs/download-pipeline.md](docs/download-pipeline.md#retries-and-codec-fallback) |
| Checkpoint | The resumable on-disk copy of an Enhanced HLS track's encrypted media in `download.temp_dir`, read back fragment by fragment during decrypt | [docs/download-pipeline.md](docs/download-pipeline.md#encrypted-checkpoints) |
| Simulate mode | Real catalog data and media selection, then the real pipeline over a pretend media backend: paced transfers, no disk writes, no wrapper or ffmpeg | `internal/media#simulatedMedia` |
| Media backend | The I/O side of the per-track pipeline (transfers, wrapper, ffmpeg, disk); production and simulate mode differ only here | `internal/media#mediaBackend` |
| Path template | The format that decides where a finished track lands: one library template for songs, albums and artists, one each for playlists and stations | `internal/media#libraryTrackPath`, `internal/media#outputPath` |
| Library, library layout | The folder tree songs, albums and artists share under `download.library_path_format`: one path per track copy and slot whichever add downloads it; another quality gets a quality suffix. Playlists and stations are not part of it | [docs/download-pipeline.md](docs/download-pipeline.md#library-records-and-slots) |
| Record mark | The `resource` field of search hits, lookups and the album page: the record holding the item and its state, read from the records, never the disk | `internal/domain#ResourceMark` |
| Disk check | `POST /api/v1/library/check`: stats the stored path of every done slot and marks the missing ones; it marks nothing when `downloads_dir` is gone or more than half are missing, unless forced | `internal/jobs#Manager.StartCheck` |
| Pending add | An add that needs a catalog read before it becomes records (a song without its album, an artist); the resolver reads it | `internal/domain#PendingAdd` |
| Queue entry | A queued run. A record has at most one queued or running run; a new add, a retry or a reset of it is a new run, never the old row queued again | `internal/jobs#Manager.SubmitBatch` |
| Download again | A reset of a record's slots: each done file is downloaded again to its stored path and replaced once its new copy is complete | `internal/jobs#Manager.Reset` |
| Overwrite existing | `download.overwrite_existing`: a download that finds a file already at its track's path replaces it instead of adopting it as `skipped_existing`; it changes no record's state | `internal/media#trackRun.handleExistingOutput` |
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
| Job overrides | The per-add `overrides` object that overlays the quality keys of the runtime config; stored on the record (or a station run) and applied each time a run starts | `internal/config#DownloadOverrides` |
| Job hook | A webhook or command fired on `job_queued` or a terminal job event | `internal/hooks#Dispatcher` |
| Library sync | The poller that turns newly added library songs of each account whose switch is on into album records | `internal/librarysync#Watcher` |
| Apple library | An Apple Music account's own library (albums, songs, playlists, artists), as Apple Music calls it; amdl keeps a local copy per account for `/api/v1/apple-library` and `in_apple_library`. Not amdl's own records of what it downloaded, which `/api/v1/library` and `amdl library list` call the library | `internal/applelibrary#Engine` |
| Watermark | Library sync's position-based anchor in the newest-first library list | [docs/automation.md](docs/automation.md#why-the-watermark-is-a-position-not-a-timestamp) |
| Skin | The console's look, one skin (`music`) on `<html data-skin>`, separate from the light/dark theme | [apps/console/DESIGN.md](apps/console/DESIGN.md#skin) |

## Repository workflow

| Term | Meaning | Source |
| --- | --- | --- |
| dev, main | Work lands on `dev`; `main` changes only through the release PR | [CONTRIBUTING.md](CONTRIBUTING.md#branches) |
| DCO sign-off | The `Signed-off-by` trailer every commit needs (`git commit -s`) | [CONTRIBUTING.md](CONTRIBUTING.md#sign-off-dco) |
| Move-only split | Moving declarations between files of one package with no code change, proven by `tools/splitdecls` verify | `tools/splitdecls` |
| Revert proof | `tools/revertproof`: a regression test must fail an assertion with the fix reverted, then pass every repeat at HEAD | `tools/revertproof` |
| Repo checks | Go tests that keep docs pointers honest | `tools/repocheck` |
| Dev server | `make dev`: simulate mode on 127.0.0.1:18080 with data under `data/dev/` | `Makefile#dev` |
