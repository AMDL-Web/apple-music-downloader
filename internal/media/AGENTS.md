# internal/media

The per-track pipeline (`Downloader`): resolve, download, decrypt, remux, verify,
tag and save. The package comment (`go doc ./internal/media`) lists the entry
points and invariants; read it before changing the pipeline.

## Output paths are load-bearing

Users' libraries, media-server scans and the skip-existing logic all key on the exact
path a track lands at. A change to any of these renames or duplicates files on
every install, so it needs the maintainer's OK and a release note:

- template expansion in `outputPath` and its variables (`{Quality}`, `{Codec}`,
  `{SongNumber}`, `{TrackNumber}`, ...). The defaults and the variable list live in
  `configs/config.example.yaml`; README "Where files land" explains them.
- `safeName`: which characters become `_`, what is trimmed and the 240-byte segment
  cap. Changing it renames existing folders.
- `qualityLabel` strings such as `24-bit/96 kHz` and `256Kbps` (`aacLCQualityLabel`),
  which `{Quality}` puts into names after `safeName`.
- Sidecar names: `lyricsSidecarPath` (`<track>.lrc` or `<track>.ttml` next to the
  `.m4a`), `cover.<ext>` in an album or playlist folder, `artist.<ext>` in an artist
  folder.
- A finished file reaches its path only through `finalizeToOutput`;
  `Downloader.handleExistingOutput` relies on that to skip files already there.

`paths_golden_test.go` pins all of these. A golden that has to change is a path
change, not a test fix.

## One pipeline, two backends

`trackRun` and the stage functions it calls (`downloadEnhancedCodec`,
`decryptAACLC`, `finalizeTrackOutput`, ...) own every status, message, progress
band, permit, retry and event. All I/O goes through `mediaBackend`: `realMedia` in
production, `simulatedMedia` in simulate mode. Never branch on `Simulate.Enabled`
inside the pipeline; give the backend a method instead, so simulate keeps matching
the real lifecycle (`TestSimulatedTrackMatchesTheRealLifecycle`). The simulated
backend must never write, delete or run anything.

## Stopping a job

- User cancel: every unfinished item ends `cancelled` with an `item_progress` event,
  written on a detached context (`markCancelled`, `cancelUnfinishedItems`); the job's
  resume checkpoints are removed.
- Shutdown (`errors.Is(context.Cause(ctx), jobs.ErrShutdown)`, see `isShutdown`):
  items and checkpoints are left as they are for recovery. Don't write a terminal
  item state or delete a checkpoint on that path.
- Terminal item writes go through `persistTerminal` (log and retry once); don't
  `_ =` them.

## Transfers

Media transfers have no `http.Client.Timeout`: transport timeouts bound the
connection and `mediaStallTimeout` bounds a body that stops arriving. Both the
in-memory and checkpoint downloads use `rangeTransfer`; change resume rules there,
once.

## External tools

ffmpeg is the only external command (`ToolChecker`). Sample extraction, remuxing,
tags and covers run in-process with mp4ff and go-mp4tag; keep it that way, because
every extra binary is one more thing a deployment can lack.

## Concurrency

The download, decrypt and wrapper-request pools are process-wide and shared by
every job-scoped clone of the `Downloader`. Changes to pools, progress gating or
output locks need `-race`.

## Verify

```bash
go test ./internal/media/          # about 35 s
go test -race ./internal/media/    # pools, progress, output locks
make dev                           # then submit an album (amdl-verify) for lifecycle changes
```
