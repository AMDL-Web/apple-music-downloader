# internal/jobs

The job queue and lifecycle (`Manager`). The package comment
(`go doc ./internal/jobs`) is the source for the locking order and the
invariants; read it before touching any mutex, and keep it current when you change
one.

## Invariants

- Locking: follow the order in the package comment (`mu` nests inside `submitMu`;
  `recoveryMu` and `lifecycleMu` guard their own fields). Never call
  `Manager.dispatchHooks` while holding `mu`. A new lock gets its place in that
  comment in the same commit.
- Terminal states are `completed`, `failed` and `cancelled`
  (`internal/domain#JobStatus`). Each is written once, by `Manager.persistTerminal`,
  together with its terminal event in one store call; hooks dispatch only after
  that write succeeds. Don't write a terminal status anywhere else.
- `Manager.Shutdown` cancels workers with the cause `ErrShutdown`. `run()` then writes
  nothing terminal: the row stays `running` with its items, no terminal event or hook, and `RecoverUnfinished` resumes it on the next start. Only
  `Shutdown` may set that cause; cancelling `Start`'s context is a user cancel. A panic
  in `ProcessJob` fails the job (`errProcessorPanic`), even during shutdown.
- Only failed and cancelled jobs can be retried (`ErrJobNotRetryable`). Retry and
  Delete refuse a job that is still finalizing (`ErrJobFinalizing`) rather than
  racing the old worker's cleanup.
- A job's dedup identity is `canonicalKey` (`type:storefront:id`), backed by a
  partial unique index in internal/db. Changing how the key is built changes which
  submissions dedupe against existing jobs, so it is a contract change, and
  `ParseCanonicalKey` keeps reading every layout older rows carry.
- Hooks: `job_queued` fires on admission, before enqueue; `job_finished`,
  `job_failed` and `job_cancelled` fire after `persistTerminal`.
  `Manager.FinalizeInFlight` and `Manager.HooksPending` tell the API streams when
  no more events can arrive; internal/api reads them in that order.

## Change together

A new job status or event type lands with: its `internal/domain` constant, the
internal/db queries that filter on status, the OpenAPI enums, the stream close rules
in internal/api, [docs/api.md](../../docs/api.md), and, for hook events, the allowed
list in internal/hooks plus `configs/hooks.yaml` and
[docs/automation.md](../../docs/automation.md).

## Verify

```bash
go test -race ./internal/jobs/
go test -race -count=50 -run 'TestName' ./internal/jobs/   # suspected flake
```
