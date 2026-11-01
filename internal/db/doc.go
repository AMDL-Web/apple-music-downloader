// Package db is the SQLite persistence layer for download jobs, their items,
// the job event log and the library-sync anchor.
//
// Open applies the pending schema migrations and returns a Store on a
// single-connection pool, so every statement is serialized. Jobs: CreateJob,
// GetJob, ListJobs, UpdateJob, DeleteJob, FinalizeJob, FindActiveJobByKey,
// FindLatestFinishedJobByKey, RequeueJob. Run order: NextQueueOrder,
// SetQueueOrder, NextQueuedJobID, QueuedJobIDs, MoveQueuedJob. Items: CreateItem,
// UpdateItem, ListItems. Events: AddEvent, ListEventsAfterLimit,
// ListMilestoneEventsAfterLimit, ListHookEvents.
//
// Invariants:
//   - The schema version is PRAGMA user_version. The migrations slice is the
//     ordered history; each entry runs once, in its own transaction, preserves
//     user data, and is never edited after it ships.
//   - Every Job the store returns carries item counters derived from job_items
//     by the jobColumns select list; no counter is stored.
//   - Timestamps are stored via formatTime in fixed-width form so TEXT order is
//     chronological.
//   - At most one queued/running job per canonical_key; CreateJob reports a
//     collision as ErrDuplicateActive.
//   - Queued jobs run in (queue_order, rowid) order. queue_order is written
//     only by CreateJob, SetQueueOrder, MoveQueuedJob and RequeueJob, never by
//     UpdateJob, whose callers may hold a stale Job.
//   - FinalizeJob and DeleteJob write the job change and its event in one tx;
//     FinalizeJob also prunes the job's item_progress events.
package db
