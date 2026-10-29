# internal/media

The per-track pipeline. Entry points and invariants are in the package comment
(`go doc ./internal/media`).

## Output paths are load-bearing

Libraries, media-server scans and the skip-existing check key on the exact path a
track lands at, so changing any of these renames or duplicates files on every
install and needs the maintainer's OK and an upgrade note:

- template expansion in `outputPath` and its variables (defaults and the variable
  list are in `configs/config.example.yaml`);
- `safeName`: replaced characters, trimming, the segment byte cap;
- `qualityLabel` strings, which `{Quality}` puts into names;
- sidecar names: `lyricsSidecarPath`, `cover.<ext>`, `artist.<ext>`.

`paths_golden_test.go` pins these. A golden that has to change is a path change,
not a test fix.

## Pipeline

- `trackRun` owns statuses, messages, progress, retries and events; all I/O goes
  through `mediaBackend`. Don't branch on `Simulate.Enabled` inside the pipeline:
  give the backend a method, so simulate mode keeps walking the real lifecycle. The
  simulated backend never writes, deletes or runs anything.
- On shutdown (`isShutdown`), leave items and resume checkpoints as they are for
  recovery; only a user cancel writes terminal item states.
- Media transfers have no `http.Client.Timeout`; transport timeouts and
  `mediaStallTimeout` bound them. Resume rules live once, in `rangeTransfer`.
- ffmpeg is the only external command (`ToolChecker`); sample extraction, remuxing
  and tagging stay in-process so a deployment has one less binary to lack.
- The download, decrypt and wrapper-request pools are process-wide and shared by
  every job-scoped `Downloader` clone; changes there run under `-race`.

## Verify

```bash
go test ./internal/media/
go test -race ./internal/media/   # pools, progress, output locks
```

Lifecycle changes also get a simulate-mode album through `make dev`
([amdl-verify](../../.claude/skills/amdl-verify/SKILL.md)).
