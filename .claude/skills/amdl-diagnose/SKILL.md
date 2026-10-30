---
name: amdl-diagnose
description: Find and fix an amdl bug by reproducing it first - evidence from the job record, events and logs, one command that goes red on the reported symptom (go test -run, httptest against Server.Routes, make dev plus curl, a saved Apple response, a copy of the user's database, -race -count=N, git bisect run), then a fix with a regression test proven red then green. Use when told "X is broken", "this job fails", "SSE stops", "wrong file name", for a flaky or failing test, a crash, a slowdown, or an error from a deployed instance.
---

# Diagnose a bug

Get one command that goes red on the reported symptom before guessing at causes:
it is what proves the cause and the fix. Until it exists, read code only to build
it.

## Evidence

- The reporter's words: which job or link, what they saw and expected, which
  version or image tag.
- The job and its events: `GET /api/v1/downloads/{id}` and
  `GET /api/v1/downloads/{id}/events`; for a user's install, the `jobs`,
  `job_items` and `job_events` rows.
- One request's access and job logs: `GET /api/v1/logs?request_id=<id>`.
- Which Apple host answered; an empty `enhancedHls` or `editorialVideo` from the
  public host is expected (root `AGENTS.md` "Two Apple hosts").
- Captures stay in the session scratchpad until they are sanitized fixtures
  (`internal/applemusic/AGENTS.md`).

## The loop

One command, already run, that goes through the real failing path, asserts the
reported symptom, gives the same answer every run (for a flake, a usable
reproduction rate) and finishes in seconds without a human. Sources, nearest
first:

1. `go test -count=1 -run 'TestX' ./internal/<pkg>/`.
2. httptest against `Server.Routes` with a test store: handlers, SSE framing,
   cursors.
3. `make dev`, then curl the endpoint or follow its stream
   ([amdl-verify](../amdl-verify/SKILL.md#live-api-check)).
4. A saved Apple response replayed as a fixture, as the internal/applemusic tests
   do.
5. State bugs: a copy of the user's SQLite file under `data/` (never the original),
   with `AMDL_DATABASE_PATH` pointing at it.
6. Concurrency: `go test -race -count=200 -run 'TestX' ./internal/<pkg>/`, with
   time, seeds and pool sizes pinned.
7. "It used to work": `git bisect run <command>` between a good tag and
   `origin/dev`.

If none reproduces it, say what you tried and ask for what would: logs with a
request id, a database copy, the exact link. Then shrink the input until removing
anything left turns the command green, and test one falsifiable hypothesis at a
time. Temporary logs carry a unique prefix such as `[DEBUG-7f3a]` so
`rg '\[DEBUG-'` finds them all.

## Fix with a regression test

- Test at the public seam, called the way real callers call it, with expected
  values from independent facts.
- Prove it guards the bug: commit the test alone (a temporary local commit, not a
  stash), see it fail on an assertion (a build error does not count), add the fix,
  see it pass, then squash before pushing.
- If no seam reaches the bug, say so instead of writing a test that restates the
  implementation.
- Rerun the original, unshrunk command.

Done when the original command no longer reproduces the symptom, `rg '\[DEBUG-'`
is empty, the report names the hypothesis that held and its evidence, and the
change passes [amdl-verify](../amdl-verify/SKILL.md).
