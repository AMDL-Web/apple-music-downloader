---
paths:
  - "**/*.md"
---

# Markdown docs

- Every `docs/*.md` page states its type on the first line after the H1:
  `> Type: reference` (current facts; code wins on conflict), `runbook` (steps),
  `decision` (a settled trade-off, not edited later) or `record` (a snapshot with
  `> Verified at <sha> on <date>; snapshot, not maintained.`). Add the page to
  `docs/README.md` with the same type.
- One fact, one source. Elsewhere, point to it instead of copying: `path#Symbol`
  for code (a package directory works too, and survives file splits), a link with
  `#anchor` for docs. Never cite line numbers.
- Reference docs describe the current state; history belongs in git log and
  release notes, not in "previously" or "since" wording.
- Backticked repo paths and `path#Symbol` pointers are checked by
  `internal/repocheck`; write something that does not exist yet in plain text.
- AGENTS.md files hold only hard rules, invariants, traps, change-together lists
  and verify commands (root at most 12000 bytes, nested 5000), each next to a
  CLAUDE.md that is exactly `@AGENTS.md`. Procedures go into `.claude/skills/`.
- Source: [docs/README.md](../../docs/README.md) and `internal/repocheck`.
