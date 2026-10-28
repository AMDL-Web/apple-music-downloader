---
name: amdl-verify
description: Verify an amdl-backend change before pushing to dev or reporting it done. Picks the local gates by change type (make targets), runs a live API check against `make dev` in simulate mode with curl and SSE on /api/v1, checks the web console in a browser when it is touched, and reports PASS/FAIL/SKIPPED with evidence and a "not verified" column. Use after any code, config, API, docs or workflow change, and when asked to "verify", "test this", "check it works", "is it done" or "ready to push?".
---

# Verify an amdl-backend change

The main thread decides what to verify and judges the result; long mechanical runs
(`make check`, a live API session) can go to a cheaper subagent with the brief in
"Delegating" below. Copy this checklist and work through it:

```
- [ ] 1 Local gates for the change type
- [ ] 2 Docs-sync and new-comment self-checks
- [ ] 3 Live API check (API, SSE, pipeline, config behavior)
- [ ] 4 Console UI check (only when the web console changed)
- [ ] 5 Report
```

## 1. Local gates

CI (`.github/workflows/ci.yml`) is the authority; locally run what the change
touches, and `make check` before pushing to `dev`.

| Change | Run | Done when |
| --- | --- | --- |
| Docs, AGENTS.md, skills, rules | `go test -count=1 ./internal/repocheck/` | ok |
| Go logic in one package | `go test ./internal/<pkg>/`, then `make test` | ok, no skipped packages you touched |
| Concurrency: jobs, media pools, events, limits, SSE | `make race` (add `-count=20 -run TestX` for a suspect test) | ok |
| API route, schema, SSE event | `go test ./internal/api/` + step 3 | ok + live check PASS |
| Config key | `go test ./internal/config/` + reread the key's comment in `configs/config.example.yaml` | ok + comment states values, units, default, runtime/startup |
| DB schema | `go test ./internal/db/` + run against a copy of a real database (`internal/db/AGENTS.md`) | row counts unchanged |
| go.mod, go.sum | `make vuln` and `make test` | no reachable vulns |
| Scripts, release tooling | `make node-test` | all pass |
| Dockerfile, entrypoint, .dockerignore | `make docker` (CI builds it too) | image builds |
| Workflows | read the diff against `.claude/rules/workflows.md`; CI on the pushed commit | run green |
| Anything, before push | `make check` | exit 0 |

A fix with a regression test needs the red-then-green proof from
[amdl-diagnose](../amdl-diagnose/SKILL.md#5-fix-with-a-regression-test).

## 2. Self-checks before committing

- Docs sync: list the symbols, config keys, routes and rule words you changed (old
  names too) and search for leftovers:
  `rg -n -e 'old_name' -e 'new_name' README.md README.zh-CN.md docs/ AGENTS.md REVIEW.md GLOSSARY.md HUMAN.md .claude/ internal/ configs/`
- New comments: `git diff -U0 origin/dev -- '*.go' | rg '^\+.*//'` and check each
  against `.claude/rules/go.md` (no history, callers, line numbers or bare TODOs).

## 3. Live API check with `make dev`

`make dev` (or the `amdl-dev` entry in `.claude/launch.json`) serves the API on
`127.0.0.1:18080` in simulate mode: real catalog lookups and media selection,
simulated transfer and decryption, no wrapper-manager, no audio written. Config and
database live under `data/dev/`; the config is recreated from the example on each
start. It needs network access to Apple.

```bash
B=http://127.0.0.1:18080/api/v1
timeout 60 sh -c "until curl -sf $B/health; do sleep 1; done"   # {"status":"ok"}
JOB=$(curl -s -X POST $B/downloads -H 'Content-Type: application/json' \
  -d '{"urls":["https://music.apple.com/us/album/1708308989"]}' \
  | jq -r '.results[0].job.id // .results[0].existing_job_id')
timeout 120 curl -sN $B/downloads/$JOB/events | tee /tmp/amdl-events.txt | rg -m1 'event: job_finished'
curl -s $B/downloads/$JOB | jq '{status: .job.status, items: (.items | length)}'
```

Done when: health is `ok`; the stream shows `job_queued`, `job_started`,
`item_progress` and `job_finished` frames with increasing `id:` lines; the job ends
`completed`. Adapt to the change:

- Resume: reconnect with `-H "Last-Event-ID: <id>"` (SSE) or
  `?last_event_id=<id>` (WebSocket) and confirm only later ids arrive.
- Overview feed: `curl -sN $B/downloads/events` while submitting a job; expect
  `download_upserted` frames.
- Config: `curl -s $B/config | jq .`, then `PUT` a runtime key and read it back; a
  startup key must be rejected.
- New route: hit it with valid and invalid input and compare the response with
  `internal/api/openapi.yaml`; the spec is served at `http://127.0.0.1:18080/docs`.

Real downloads, decryption, lyrics and wrapper login need wrapper-manager; in its
absence they are "not verified", never assumed. Stop the server when done: Ctrl-C
in the foreground, or `kill $(lsof -ti tcp:18080)` for a background run, because
killing `make` leaves the `go run` child listening.

## 4. Console UI check

Only when the embedded web console (internal/console, served on `/console/`)
exists and the change touches it. With `make dev` running, open
`http://127.0.0.1:18080/console/` in a browser and check at 1280 px and 375 px wide,
in light and in dark color scheme: the changed view renders, the browser console has
no errors, and a submitted job's progress updates live. Attach one screenshot per
width and theme that shows the change.

## 5. Report

```
Verified <what> at <short sha>, by <model id>.

| Check | Result | Evidence |
| --- | --- | --- |
| make check | PASS | exit 0, 15 packages ok |
| live: album job in simulate mode | PASS | job_finished id 1290, status completed |
| real download | SKIPPED | no wrapper-manager here |

Not verified: <each gap and why>. What these checks cannot detect: <one line>.
```

Results are PASS, FAIL or SKIPPED, each with evidence (command and exit code, last
lines of output, ids, file paths). Don't round up: an unrun check is SKIPPED with
its reason. Keep changed locally, tested, pushed and released apart.

## Delegating

A subagent brief carries: what it may not do (edit files, commit, push, comment on
GitHub); the change with measurable expectations; the ordered checks from steps
1–4 as concrete commands; where to put artifacts (the session scratchpad); and the
report format from step 5. The main thread spot-checks one piece of evidence
instead of rerunning everything.

## Waiting

Every wait has a bound: `timeout` on shell loops, 15 minutes for CI and review
bots (`timeout 900 gh pr checks <pr> --watch`). On timeout, record FAIL with the
last state seen and continue with work that does not depend on it.
