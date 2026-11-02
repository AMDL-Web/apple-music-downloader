# internal/db

The SQLite store. Installs carry years of rows in this file. The schema version,
migration order and query invariants are in the package comment
(`go doc ./internal/db`).

## Migrations

- `migrations` is a version map: `0 → 1` (`migrateTo1`, the frozen 1.x schema)
  and `1 → 6` (`migrateTo6`, the 2.0 schema and the 1.x import). Versions 2 to 5
  are reserved: the 2.0 release candidates wrote them, their migrations were
  folded into `migrateTo6` by the maintainer's decision, and `Open` refuses such
  a file without writing to it.
- Append a new step to `migrations`; never edit one that shipped in a release
  (release candidates excepted, as above), because databases that ran it will
  not run it again. Its SQL spells statuses and event types as literals, not Go
  constants that may change later.
- Keep user data: runs, items, milestone events, records, their tracks and
  files, pending adds, tombstones and the library-sync anchor. A column may go
  only when its information lives on elsewhere or is derived; pruning is limited
  to telemetry (`item_progress`).
- Test it in `migrations_test.go` against the frozen 1.x schema (`schema1x`,
  `rows1x`): counts per table, every row survives with its values, the new
  shape is in place, and reopening changes nothing. Add fixture rows for data
  the fixture lacks; never edit `schema1x`.
- Before applying it to a real database, tell the maintainer which tables and rows
  it touches, whether it can be undone, and what an older binary does with the
  migrated file, and wait for confirmation.

## Contracts held here

- Event ids are the SSE resume cursors in `internal/api`: they only increase and
  are never reused or renumbered.
- The partial unique indexes over queued and running runs make dedup safe under
  concurrent adds: one active run per record (`idx_runs_active_resource`) and,
  for a one-off run such as a station, per canonical key
  (`internal/jobs#canonicalKey`).
- A record's rollup columns are stored and recomputed in the transaction of
  every write to its tracks or files; `files.path` is unique, so one file has
  one owning slot.

## Verify

```bash
go test ./internal/db/
go test -race ./internal/db/ ./internal/jobs/
```

For a migration, also start the binary once against a copy of a real database
(never the original: copy the config and point its `database.path` at the copy) and compare row counts before and
after.
