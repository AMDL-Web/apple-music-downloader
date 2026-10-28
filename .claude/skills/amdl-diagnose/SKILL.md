---
name: amdl-diagnose
description: Find and fix a bug in amdl-backend by reproducing it first. Gathers evidence, builds one command that goes red on the reported symptom (go test -run, httptest against Server.Routes, make dev plus curl, a saved Apple response fixture, a copy of the user's SQLite database, -race -count=N, git bisect run), then forms hypotheses, fixes, and proves the regression test. Use when told "X is broken", "this job fails", "SSE stops", "wrong file name", a flaky or failing test, a crash, a slowdown, or an error from a deployed instance.
---

# Diagnose a bug

Hard bugs are hard because you can't see them reliably, not because the code is
hard to read. Get one command that goes red first; then a cause can be proven and a
fix can be shown to work. Copy this checklist and work through it:

```
- [ ] 1 Evidence
- [ ] 2 A loop: one command that goes red
- [ ] 3 Minimal reproduction
- [ ] 4 Hypotheses, falsified one at a time
- [ ] 5 Fix with a regression test
- [ ] 6 Wrap up
```

## 1. Evidence

- The reporter's exact words: which job or link, what they saw, what they expected,
  which version or image tag.
- The job record and its events: `GET /api/v1/downloads/{id}` and the replayed
  `GET /api/v1/downloads/{id}/events`; for a user's install, the matching rows in
  their database (`jobs`, `job_items`, `job_events`).
- Logs: `GET /api/v1/logs?request_id=<id>` gathers one request's access and job logs.
- Which Apple host answered. Before suspecting parsing, check whether the request
  went to `api.music.apple.com` or `amp-api.music.apple.com`
  (`internal/applemusic/AGENTS.md`); an empty `enhancedHls` or `editorialVideo` on
  the public host is expected.
- Before saving any captured request or response, strip `Authorization`,
  `Media-User-Token`, cookies, the developer token and the scraped JWT. Save
  captures in the session scratchpad, not the repo, until they are sanitized test
  fixtures.

## 2. Build the loop

Done when you can show one command, already run, that:

- goes through the real failing path and asserts the reported symptom, so it turns
  green when the bug is fixed;
- gives the same answer every run (for a flake: a reproduction rate high enough to
  debug);
- finishes in seconds to tens of seconds and needs no human.

Until it exists, read code only to build it. Use the first source that reaches:

1. A package test: `go test -count=1 -run 'TestX' ./internal/<pkg>/`.
2. An HTTP test: `httptest` against `Server.Routes` in internal/api, with a test
   store; covers handlers, SSE framing and cursors.
3. The local server: `make dev`, then `curl` the endpoint or follow
   `/api/v1/downloads/{id}/events` (recipe in
   [amdl-verify](../amdl-verify/SKILL.md#3-live-api-check-with-make-dev)).
4. A saved Apple response replayed through the catalog code as a fixture (the
   tests in internal/applemusic show the pattern).
5. State bugs: copy the user's SQLite file into `data/` (never open the original)
   and point `AMDL_DATABASE_PATH` at the copy.
6. Concurrency: `go test -race -count=200 -run 'TestX' ./internal/<pkg>/`; pin time,
   seeds and pool sizes to raise the reproduction rate.
7. "It used to work": `git bisect run <the command above>` between a good tag and
   `origin/dev`.

If none reproduces it, write down what you tried and ask for what would: logs with
a request id, the database copy, the exact link.

## 3. Minimize

Remove one input, step, config key or caller at a time and rerun. Done when
removing anything left makes the command green.

## 4. Hypotheses

- Write 3 to 5, ranked, each falsifiable: "if it is X, changing Y makes the symptom
  go away". A guess with no prediction is not a hypothesis.
- Change one variable per probe.
- Temporary logs carry a unique prefix such as `[DEBUG-7f3a]` so one
  `rg '\[DEBUG-'` finds them all at the end.
- Performance: measure a baseline (`go test -bench`, `-cpuprofile`, timings in
  logs) before changing anything.

## 5. Fix with a regression test

- Put the test at the right seam: the public behavior, called the way real callers
  call it. Expected values come from independent facts (Apple's documented shape,
  the reporter's example, a known-good file name), not from re-running the code's
  own logic.
- Prove it guards the bug: commit the test alone (a temporary local commit, not a
  stash), run it and see it fail on an assertion (a build error does not count),
  then add the fix and see it pass. Squash or reorder before pushing.
- If no seam reaches the bug, say so in the report instead of writing a test that
  restates the implementation.
- Rerun the original, unminimized command from step 2.

## 6. Wrap up

- [ ] The original loop no longer reproduces the symptom
- [ ] Regression test proven red then green, or the missing seam explained
- [ ] `rg '\[DEBUG-'` is empty; scratch scripts and captures stay out of the repo
- [ ] The report names the root cause: which hypothesis held and the evidence
- [ ] Verified with [amdl-verify](../amdl-verify/SKILL.md); commits signed off
      (`git commit -s`) and pushed to `dev`
- [ ] Contract follow-ups named (iOS app, amdl-web) if the fix changed the API
