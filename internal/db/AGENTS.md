# internal/db

The SQLite store. Installs carry years of rows in this file. The schema version,
migration order and query invariants are in the package comment
(`go doc ./internal/db`).

## Migrations

- Append a new function to `migrations`; never edit a shipped one, because
  databases that ran it will not run it again. Its SQL spells statuses and event
  types as literals, not Go constants that may change later.
- Keep user data: jobs, items, milestone events and the library-sync anchor. A
  column may go only when its information lives on elsewhere or is derived;
  pruning is limited to telemetry (`item_progress`).
- Test it in `migrations_test.go` against the frozen 1.x schema (`schema1x`,
  `rows1x`): every row survives with its values, the new shape is in place, and
  reopening changes nothing. Add fixture rows for data the fixture lacks; never
  edit `schema1x`.
- Before applying it to a real database, tell the maintainer which tables and rows
  it touches, whether it can be undone, and what an older binary does with the
  migrated file, and wait for confirmation.

## Contracts held here

- Event ids are the SSE resume cursors in `internal/api`: they only increase and
  are never reused or renumbered.
- The partial unique index over queued and running jobs makes job dedup safe under
  concurrent submits (`internal/jobs#canonicalKey`).

## Verify

```bash
go test ./internal/db/
go test -race ./internal/db/ ./internal/jobs/
```

For a migration, also start the binary once against a copy of a real database
(never the original, via `AMDL_DATABASE_PATH`) and compare row counts before and
after.
