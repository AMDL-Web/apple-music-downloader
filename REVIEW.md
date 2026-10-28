# REVIEW.md

Guidance for pull-request review agents (Claude review, Codex, Devin Review). The
project's rules themselves are in [AGENTS.md](AGENTS.md) and the nested AGENTS.md
files; this page says only what to report, what to skip, and how to report it.

## Output language

Write the review summary and comments in Simplified Chinese; keep identifiers, paths and quoted code verbatim.

## What counts as a defect

- An unmarked break: a route, field, enum value, config key or DB column renamed
  or removed without a `!` commit title and a row in the upgrade guide, or a
  migration that loses user data (jobs, items, milestone events, library-sync
  anchor). Breaking changes themselves are allowed (AGENTS.md "Contracts and
  breaking changes"); review whether they are justified and documented.
- A change to output paths or file names (path templates, `safeName`, `{Quality}`
  labels, sidecar names) without the maintainer's OK and an upgrade note: it makes
  existing libraries download again.
- A handler, route, request or response changed without the matching
  `internal/api/openapi.yaml`, [docs/api.md](docs/api.md) and README API table
  update, or a route or response the console uses changed without updating the
  console (`internal/console`) in the same change. The iOS app and amdl-web are
  set aside; never ask for follow-ups there.
- A config key added or changed without its full comment in
  `configs/config.example.yaml` (allowed values, units, default when absent,
  runtime or startup). `configs/config.yaml` is generated and untracked; never ask
  for changes there.
- A schema change without a migration that preserves existing rows, or a migration
  that is not idempotent across restarts.
- A read from `amp-api.music.apple.com` that can fail a job instead of degrading to
  "not available".
- Docs or comments that contradict the code they describe.
- Committed secrets or local details: tokens, `*.p8` keys, LAN IPs, absolute home
  paths, a live `configs/config.yaml`.

## What not to report

- Missing authentication or authorization. The service has no auth on purpose
  (AGENTS.md "This service has no auth, on purpose").
- Anything CI already gates: gofmt, build, vet, test failures, the race detector,
  govulncheck, the Node script tests, the Docker build, and the doc checks in
  `internal/repocheck`.
- Generated or record files: `.github/release-notes/`, `go.sum`, and `docs/` pages
  whose type is `record`.
- Problems in code the pull request did not touch: mention them if useful, marked
  non-blocking.
- Edge cases and defensive hardening that need rare input, a hypothetical race or
  timing, or an external service misbehaving: merge them into one optional item
  and never give them a blocking label. Blocking is for defects that break the
  change when it is used as intended.

## How to report

- Back every claim with a file and line; don't infer behavior from names alone.
- Name the defect category and list every other place in the pull request with
  the same defect, so the author fixes them in one pass.
- Don't repeat findings already posted on this pull request. A re-review looks only
  at commits pushed since the last review.
- At most three style or naming remarks, marked optional.
- The first line of the summary gives the number of findings; if there is nothing
  to change, say so plainly.
