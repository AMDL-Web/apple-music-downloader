// Package db is the SQLite persistence layer for records (albums, playlists,
// their tracks and per-slot files), the runs that download them with their
// items and event log, pending adds, tombstones and the library-sync anchor.
//
// OpenWith applies the pending schema migrations and returns a Store on a
// single-connection pool, so every statement is serialized. Records:
// GetAlbum, GetPlaylist, ListAlbumTracks, ListPlaylistTracks, Tombstoned,
// RecordSources, ListEligibleRecords. Files: GetFile, ListFiles. Pending
// adds: AddPending, ListPending, DeletePending, RecordPendingFailure,
// RetryPending. Runs (domain.Job): CreateJob, GetJob,
// ListJobs, UpdateJob, DeleteJob, FinalizeJob, FindActiveJobByKey,
// FindLatestFinishedJobByKey, RequeueJob. Run order: NextQueueOrder,
// SetQueueOrder, NextQueuedJobID, QueuedJobIDs, MoveQueuedJob. Items:
// CreateItem, UpdateItem, ListItems. Events: AddEvent, ListEventsAfterLimit,
// ListMilestoneEventsAfterLimit, ListHookEvents.
//
// Invariants:
//   - The schema version is PRAGMA user_version. The migrations version map
//     is the history; each step runs once, in its own transaction, preserves
//     user data, and is never edited after it ships in a release. Versions 2
//     to 5 (the 2.0 release candidates) are refused at open.
//   - Every Job the store returns carries item counters derived from run_items
//     by the jobColumns select list; no counter is stored.
//   - A record's rollup (tracks_wanted, tracks_done, the flag counts, state)
//     is stored and recomputed in the transaction of every write to its tracks
//     or files, so it always equals computeRollup.
//   - files.path is unique and stored relative to the downloads dir when the
//     file is under it (storedPath).
//   - Timestamps are stored via formatTime in fixed-width form so TEXT order is
//     chronological.
//   - At most one queued/running run per record and per canonical_key;
//     CreateJob reports a collision as ErrDuplicateActive.
//   - Queued runs run in (priority, queue_order, rowid) order. queue_order is
//     written only by CreateJob, SetQueueOrder, MoveQueuedJob and RequeueJob,
//     never by UpdateJob, whose callers may hold a stale Job.
//   - FinalizeJob and DeleteJob write the run change and its event in one tx;
//     FinalizeJob also prunes the run's item_progress events.
package db
