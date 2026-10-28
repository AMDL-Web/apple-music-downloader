# internal/db

SQLite persistence for jobs, items, events and library-sync state (`Store`,
opened by `Open`). Deployed installs carry years of rows in this file; treat the
schema as load-bearing.

## Migrations

- The schema version is `PRAGMA user_version`, and `internal/db#migrations` is the
  ordered history: entry N brings a database from version N-1 to N, once, in its
  own transaction together with the version bump. `Open` runs the pending ones and
  nothing schema-shaped after that. A database newer than the binary is refused.
- Add a schema change as a new function appended to that slice. Never edit a shipped
  migration: databases that already ran it will not run it again. Its SQL spells
  statuses and event types as literals, not Go constants that may change later.
- Every migration preserves user data: jobs, items, milestone events and the
  library-sync anchor. A column may be dropped only when its information lives on
  elsewhere (migration 2 folds `jobs.force` into `overrides.force_overwrite`) or is
  derived (item counters). Pruning is limited to telemetry (`item_progress`).
- Test each migration against the frozen 1.x schema in
  `internal/db/migrations_test.go` (`schema1x`, `rows1x`): every row survives with
  its values, the new shape is in place, and reopening changes nothing. Add fixture
  rows when a migration touches data the fixture lacks; never edit `schema1x`.
- Before applying a migration to any real database, describe its impact and get
  the maintainer's confirmation: tables and rows touched, whether it can be
  undone, and what an older binary does with the migrated file (rollback means
  restoring the backup taken before the upgrade).

## Invariants

- Job item counters are never stored. `jobColumns` derives them from `job_items`;
  every query that feeds `scanJob` selects it, so every returned Job has them.
- Timestamps are TEXT in one fixed-width UTC layout (`timeLayoutFixed`, written by
  `formatTime`) so string order equals time order in comparisons, `MIN`/`MAX` and
  `ORDER BY`. `started_at`/`finished_at` are NULL until set.
- Event ids are the stream resume cursors in internal/api, so they only ever
  increase; don't reuse or renumber them. `FinalizeJob` deletes the job's
  `item_progress` events in the transaction that writes the terminal status.
- The partial unique index over queued and running jobs is what makes job dedup
  safe under concurrent submits (internal/jobs canonical key). Keep it when
  touching the jobs table.

## Change together

A new column lands in one commit with: a new migration, the read and write
queries (`jobColumns` and `scanJob` for jobs), the `internal/domain` field,
`internal/api/openapi.yaml` and [docs/api.md](../../docs/api.md) if the field is
exposed, and a case in the migration test.

## Verify

```bash
go test ./internal/db/
go test -race ./internal/db/ ./internal/jobs/
```

For a migration, also run the binary once against a copy of a real database (never
the original) and compare row counts before and after; the
[amdl-diagnose](../../.claude/skills/amdl-diagnose/SKILL.md) skill has the recipe.
