// Package jobs schedules downloads over records (design: resource records).
// Manager (built by NewManager) is the record service and the run queue:
//
//   - Adds. SubmitBatch adds links: albums and catalog playlists become
//     records and queue their run in the same step; songs without their album
//     and artists become pending adds the resolver reads (Catalog); stations
//     are one-off runs. AddAlbums is the library monitor's add.
//   - The picker queues one run per record that has work
//     (db.Store.ListEligibleRecords), by priority class, while the wait list
//     has room; it wakes on adds, resolves, resets, resumes, run ends and a
//     60 s tick, so a failed slot's backoff (30 min, then 2 h) ends on its own.
//   - Workers claim queued runs in run order (priority class, then
//     queue_order, changed by Move and DownloadNow) and run them through a
//     Processor. A record run plans its slots after its resolve and reports
//     each slot's outcome (RecordReporter); settleRecordRun puts what it left
//     to rest when it ends.
//   - Pause, Resume, Reset and Retry steer records; Cancel pauses a record
//     run's record and cancels a station run. Delete, RecoverUnfinished and
//     Start/Shutdown/Wait complete the API.
//
// Invariants: a record has at most one queued or running run, backed by the
// database's partial unique index over runs(resource_kind, resource_id). A
// one-off run's input has at most one active run (canonical_key's index). A
// retry is a new run; no row is ever queued twice. A record run never leaves
// a slot running once it ends: a stopped run releases its slots to pending, any
// other ending fails the slots it left, so the picker never re-queues a record
// forever on the same abandoned slots. Every terminal status is written by
// persistTerminal together with its terminal event in one store call, and
// hooks are dispatched only after that write succeeds. A run interrupted by
// Shutdown (cause ErrShutdown) gets no terminal write and stays running for
// RecoverUnfinished, which first sets every running slot back to pending.
// Delete refuses a run while m.cancels or m.finalizing still holds it or one
// of its hooks is still pending.
//
// Locking: submitMu serializes SubmitBatch, Retry and RecoverUnfinished. mu
// guards cancels, finalizing, waiting, unclaimable and backoff, and every
// change to a run's queued status or queue order happens under it: admission
// (adds, the picker, resets), a worker's claim, Cancel, Delete, Move and
// recovery. Where both are held, mu is nested inside submitMu: the order is
// submitMu, then mu, then the hook dispatcher's own lock (hooks.Pending and
// Dispatch are called under mu). lifecycleMu guards the worker lifecycle
// fields; catalogMu guards the resolver's Catalog. dispatchHooks must not be
// called while holding mu.
package jobs
