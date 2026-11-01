---
name: amdl-verify
description: Verify an amdl change before pushing to dev or reporting it done - the local gates for the change type, a live API and SSE check against `make dev` in simulate mode, a browser check of the web console when it changed, and a PASS/FAIL/SKIPPED report with evidence. Use after any code, config, API, docs or workflow change, and when asked to "verify", "test this", "is it done" or "ready to push?".
---

# Verify a change

## Local gates

Run the rows of root `AGENTS.md` "Verification" that the change touches, and
`make check` before pushing to `dev`. A bug fix with a regression test also needs
the [revert proof](#revert-proof). Before committing, check each added comment
against `.claude/rules/go.md`:

```bash
git diff -U0 origin/dev -- '*.go' | rg '^\+.*//'
```

## Revert proof

When a commit fixes a bug and adds or changes a regression test, prove after
committing that the test guards the bug:

```bash
go run ./tools/revertproof -test apps/desktop/internal/widgets/cover_test.go
go run ./tools/revertproof -test apps/console/jstest/album-progress.test.mjs \
  -cmd 'node --test apps/console/jstest/album-progress.test.mjs'
```

In a temporary worktree at HEAD it puts every changed non-test file back to its
merge-base with `origin/dev` (`-base` picks another ref) and runs the test file's
`Test` functions (`-run` narrows them), which must fail an assertion; then it runs
them at HEAD 10 times (`-count`) and once with `-cpu 1`, which must all pass. The
working tree and index stay as they are, and uncommitted changes are not part of the
proof. Test files, `go.mod`, `go.sum`, `-keep` paths and files the change added
stay at HEAD, so a fix inside an added file cannot go red (`addedKept`).

| Verdict | Means |
| --- | --- |
| `proved` | Red on an assertion, then green in every run. Exit 0. |
| `flaky` | A test failed some of its runs at HEAD; neither run shows anything until it is fixed. |
| `not-red` | The test passes without the fix, or nothing was reverted. |
| `red-not-assertion` | Without the fix the tests did not build, panicked, matched nothing or all skipped. |
| `not-green` | A test failed every run at HEAD. |
| `error` | git failed or a run timed out. |

The report gets a row with the verdict and the failure lines from `red.tail`;
check that they describe the bug. Any other verdict goes under "Not verified"
with its reason. A test that uses a field or function the fix adds cannot build
without it (`red-not-assertion`); say so rather than splitting the fix to please
the tool.

## Live API check

`make dev` (or the `amdl-dev` entry in `.claude/launch.json`) serves the API on
`127.0.0.1:18080` in simulate mode: real catalog lookups and media selection,
simulated transfer and decryption, no wrapper, nothing written to the
library. Config and database live under `data/dev/`, and the config is recreated
from the example on each start. It needs network access to Apple.

```bash
B=http://127.0.0.1:18080/api/v1
timeout 60 sh -c "until curl -sf $B/health; do sleep 1; done"
JOB=$(curl -s -X POST $B/downloads -H 'Content-Type: application/json' \
  -d '{"urls":["https://music.apple.com/us/album/1708308989"]}' \
  | jq -r '.results[0].job.id // .results[0].existing_job_id')
timeout 120 curl -sN $B/downloads/$JOB/events | rg -m1 'event: job_finished'
curl -s $B/downloads/$JOB | jq '{status: .job.status, items: (.items | length)}'
```

Done when health is `ok`, the stream shows `job_queued`, `job_started`,
`item_progress` and `job_finished` frames with increasing `id:` lines, and the job
ends `completed`. Adapt it to the change:

- Resume: reconnect with `-H "Last-Event-ID: <id>"` or `?cursor=<id>`; only later
  ids arrive.
- Overview feed: `curl -sN $B/downloads/events` while submitting a job shows
  `download_upserted` frames.
- Config: `PUT` a runtime key and read it back; a startup key is rejected.
- Stats are cached for `internal/api#statsCacheTTL`.
- A new route: valid and invalid input, compared with `internal/api/openapi.yaml`.

Real downloads, decryption, lyrics and wrapper login need a logged-in wrapper; without
it they are SKIPPED, never assumed. Stop the server afterwards with Ctrl-C, or
`kill $(lsof -ti tcp:18080)` for a background run, because killing `make` leaves
the `go run` child listening.

## Console check

When `apps/console` changed: with `make dev` running, open
`http://127.0.0.1:18080/console/` at desktop width and 375 px, light and dark. The
changed view renders, the browser console has no errors, and a submitted job
updates live. Attach a screenshot per width and theme.

Before showing console work to the maintainer, run
[apps/console/tools/ui-audit](../../../apps/console/tools/ui-audit/README.md),
read its layout-shift and flicker findings, and look at the screenshots yourself,
for every page and state, not only the one you changed. Fix what it finds in this order: layout that jumps during an action,
misalignment, then polish. Also look for zero-value or empty rows that add height,
hover shapes that do not match their container's corners, flicker and cramped rows.
A person's flaw report about one screen is a prompt to check the same flaw on every
screen. For a review you did not write, use a reviewer with a clean context.

## Report

```
Verified <what> at <short sha>.

| Check | Result | Evidence |
| --- | --- | --- |
| make check | PASS | exit 0 |
| revert proof | PASS | proved; red: cover_test.go:46: the cover shows red 0: it left at once |
| live: album job in simulate mode | PASS | job_finished id 1290, status completed |
| real download | SKIPPED | no wrapper here |

Not verified: <each gap and why>.
```

Each result is PASS, FAIL or SKIPPED with evidence (command and exit code, output
lines, ids). A check that did not run is SKIPPED with its reason.
