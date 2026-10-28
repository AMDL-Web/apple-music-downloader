# internal/db

SQLite persistence for jobs, items, events and library-sync state (`Store`,
opened by `Open`). Deployed installs carry years of rows in this file; treat the
schema as load-bearing.

## Invariants

- Migrations run on every `Open`, so each step is idempotent and preserves rows.
  Add columns through `Store.ensureColumn` (`ALTER TABLE ... ADD COLUMN` with a
  `DEFAULT`), backfill only the new columns (`Store.backfillItemProgress` is the
  model), and never drop, rename or rebuild a table. A column nothing reads any
  more stays in place; the comment in `Store.initSchema` says why.
- Before applying a migration to any real database, describe its impact and get
  the maintainer's confirmation: tables and rows touched, whether it can be
  undone, and what an older binary does when it opens the migrated file
  (rollback to the previous image must keep working).
- Timestamps are TEXT in one fixed-width UTC layout (`timeLayoutFixed`, written by
  `formatTime`) so string order equals time order in comparisons and `ORDER BY`.
  Write every timestamp through `formatTime`; `Store.normalizeTimestampColumns`
  rewrites legacy variable-width rows on `Open`.
- Event ids are the SSE and WebSocket resume cursors in internal/api, so they only
  ever increase; don't reuse or renumber them.
- The partial unique index over queued and running jobs is what makes job dedup
  safe under concurrent submits (internal/jobs canonical key). Keep it when
  touching the jobs table.

## Change together

A new column lands in one commit with: the `ensureColumn` entry, the read and write
queries, the `internal/domain` field, `internal/api/openapi.yaml` and
[docs/api.md](../../docs/api.md) if the field is exposed, and a test that opens a
database created with the previous schema and finds its rows intact.

## Verify

```bash
go test ./internal/db/
go test -race ./internal/db/ ./internal/jobs/
```

For a migration, also run the binary once against a copy of a real database (never
the original) and compare row counts before and after; the
[amdl-diagnose](../../.claude/skills/amdl-diagnose/SKILL.md) skill has the recipe.
