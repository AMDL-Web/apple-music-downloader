// Package jobs owns the download job queue. Manager (built by NewManager)
// admits submissions via SubmitBatch and Retry, runs each job on a fixed
// worker pool through a Processor, and persists every status change and event
// to db.Store; Cancel, Delete, RecoverUnfinished and Start/Shutdown/Wait
// complete the API. Manager is also the Reporter a Processor reports through.
//
// Invariants: canonicalKey is a job's dedup identity, backed by the database's
// partial unique index over queued/running jobs. Every terminal status is
// written by persistTerminal together with its terminal event in one store
// call, and hooks are dispatched only after that write succeeds. Delete and
// Retry refuse a job while m.cancels or m.finalizing still holds it.
//
// Locking: submitMu serializes SubmitBatch, Retry and RecoverUnfinished. mu
// guards cancels and finalizing, and serializes a worker's startup claim with
// Cancel, Retry and Delete. Where both are held (Retry), mu is nested inside
// submitMu. recoveryMu guards recoveryQueue and lifecycleMu the worker
// lifecycle fields. dispatchHooks must not be called while holding mu.
package jobs
