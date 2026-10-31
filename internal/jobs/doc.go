// Package jobs owns the download job queue. Manager (built by NewManager)
// admits submissions via SubmitBatch and Retry, runs queued jobs on a fixed
// worker pool through a Processor in the run order the store keeps
// (queue_order, changed by Move), and persists every status change and event
// to db.Store; Cancel, Delete, RecoverUnfinished and Start/Shutdown/Wait
// complete the API. Manager is also the Reporter a Processor reports through.
//
// Invariants: canonicalKey is a job's dedup identity, backed by the database's
// partial unique index over queued/running jobs. A worker always claims the
// first queued job in run order that it can load. Every terminal status is
// written by persistTerminal together with its terminal event in one store
// call, and hooks are dispatched only after that write succeeds. A job
// interrupted by Shutdown (cause ErrShutdown) gets no terminal write at all and
// stays running for RecoverUnfinished. Delete and Retry refuse a job while
// m.cancels or m.finalizing still holds it.
//
// Locking: submitMu serializes SubmitBatch, Retry and RecoverUnfinished. mu
// guards cancels, finalizing, waiting and unclaimable, and every change to a
// job's queued status or queue order happens under it: admission, a worker's
// claim, Cancel, Retry, Delete, Move and recovery. Where both are held, mu is
// nested inside submitMu. lifecycleMu guards the worker lifecycle fields.
// dispatchHooks must not be called while holding mu.
package jobs
