---
paths:
  - "**/*.md"
---

# Markdown docs

- Every `docs/*.md` page has `> Type: reference|runbook|decision|record` as the
  first line after its H1 and a row in `docs/README.md` with the same type. A
  record page also carries `> Verified at <sha> on <date>; snapshot, not
  maintained.`
- Write the current state. History belongs in git log and release notes, not in
  "previously", "now" or "since" wording.
- One fact, one place; elsewhere, point to it: `path#Symbol` for code (a package
  directory survives file splits), a link with `#anchor` for docs. Point at
  constants instead of copying their values.
- `tools/repocheck` checks links, anchors, backticked repo paths and
  `path#Symbol` pointers, so write something that does not exist yet in plain text.
- An `AGENTS.md` holds only rules, invariants, traps, change-together lists and
  verify commands, within `tools/repocheck#rootAgentsMaxBytes` (root) or
  `tools/repocheck#nestedAgentsMaxBytes` (nested), next to a `CLAUDE.md` that is
  exactly `@AGENTS.md`. Procedures go into `.claude/skills/`.
