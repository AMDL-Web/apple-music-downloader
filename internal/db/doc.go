// Package db is the SQLite persistence layer for download jobs, their items,
// the job event log and the library-sync anchor.
//
// Open returns a Store on a single-connection pool, so every statement is
// serialized. Jobs: CreateJob, GetJob, ListJobs, DeleteJob, FinalizeJob.
// Items: CreateItem, UpdateItem, ListItems. Events: AddEvent, ListEventsAfter.
//
// Invariants:
//   - Schema changes are additive (CREATE ... IF NOT EXISTS plus ensureColumn),
//     so existing rows survive every Open.
//   - Timestamps are stored via formatTime in fixed-width form so TEXT order is
//     chronological; normalizeTimestampColumns rewrites legacy rows on Open.
//   - At most one queued/running job per canonical_key; CreateJob reports a
//     collision as ErrDuplicateActive.
//   - FinalizeJob and DeleteJob write the job change and its event in one tx.
package db
