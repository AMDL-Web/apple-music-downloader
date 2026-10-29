# REVIEW.md

What pull-request review agents (Claude review, Codex, Devin Review) report. The
rules being checked are in [AGENTS.md](AGENTS.md) and the `AGENTS.md` files under
`internal/`.

Write the summary and comments in Simplified Chinese; keep identifiers, paths and
quoted code verbatim.

## Defects

Report a change that breaks one of these when used as intended:

- A contract break without its marking: a route, field, enum value, SSE event type,
  config key or DB column renamed or removed without a `!` commit title and an
  upgrade-guide row. Breaking changes themselves are allowed; check the stated
  reason and the documentation.
- An output-path change (path templates, `safeName`, `{Quality}` labels, sidecar
  names) without the maintainer's OK and an upgrade note.
- A route, request or response change without the matching
  `internal/api/openapi.yaml`, [docs/api.md](docs/api.md) and README API table
  update, or without the console change when `internal/console` uses it.
- A config key added or changed without its full comment in
  `configs/config.example.yaml`.
- A migration that loses user data (jobs, items, milestone events, the library-sync
  anchor), edits a shipped migration, or is not idempotent across restarts.
- A read from `amp-api.music.apple.com` that can fail a job.
- Docs or comments that contradict the code they describe.
- Committed secrets or local details: tokens, `*.p8` keys, LAN IPs, absolute home
  paths, a `configs/config.yaml`.

## Not reported

- Missing authentication or authorization (AGENTS.md "This service has no auth, on
  purpose").
- What CI already gates: formatting, build, vet, tests, race detector, govulncheck,
  Node tests, the Docker build, `internal/repocheck`.
- `.github/release-notes/`, `go.sum`, and `docs/` pages of type `record`.
- Follow-ups for the iOS app or amdl-web.
- Problems in code the PR did not touch, unless marked non-blocking.
- Edge cases that need rare input, a hypothetical race or a misbehaving external
  service: one optional item at most, never blocking.

## Format

- The first line of the summary gives the number of findings, or says there is
  nothing to change.
- Back each finding with a file and line, name its category, and list every other
  place in the PR with the same defect.
- On a re-review, look only at commits pushed since the last review and don't repeat
  earlier findings.
- At most three style or naming remarks, marked optional.
