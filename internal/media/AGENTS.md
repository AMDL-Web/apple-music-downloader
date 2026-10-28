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
- `safeName`: which characters become `_` and what is trimmed. Changing it renames
  existing folders.
- `qualityLabel` strings such as `24-bit/96 kHz` and `256Kbps`, which `{Quality}`
  puts into names after `safeName`.
- Sidecar names: `<track>.lrc` or `<track>.ttml` next to the `.m4a`, `cover.<ext>`
  in an album or playlist folder, `artist.<ext>` in an artist folder.
- A finished file reaches its path only through `finalizeToOutput`;
  `Downloader.handleExistingOutput` relies on that to skip files already there.

## Simulate mode mirrors the real lifecycle

`Downloader.simulateTrack` replays `Downloader.processTrack` (statuses, progress
bands, DB fields, events) with real catalog data and media selection but a
simulated transfer, and never touches the disk. A change to the real lifecycle
changes simulate in the same commit: `make dev` and client development rely on
the two matching.

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
