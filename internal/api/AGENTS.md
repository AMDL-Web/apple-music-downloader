# internal/api

The HTTP surface: the route table in `Server.routes`, the handlers, the SSE
streams, and `internal/api/openapi.yaml`, the contract that the iOS app and
amdl-web mirror by hand. Built by `New(Deps)`; `Server.Routes` is the handler.

## Invariants

- `internal/api/openapi.yaml` is the contract. A rename or removal is a breaking
  change: allowed when it fixes a defect or removes duplication, marked `!` in the
  commit title and listed in the upgrade guide, because shipped clients break.
- No auth here, ever (root `AGENTS.md`, "This service has no auth, on purpose").
  `GET /api/v1/developer-token` returns a usable token on purpose.
- Every route is one line in `Server.routes`. `TestRoutesMatchSpec` keeps the
  `/api/v1/` rows and the spec's operations equal in both directions. Misses under
  `/api/` go to `apiFallback`: 404 `route_not_found`, or 405 `method_not_allowed`
  with `Allow` computed from the table. Never let `http.ServeMux` answer there.
- Every non-2xx JSON response goes through `writeError`/`writeErrorDetails`:
  `{"error": {"code", "message", "details"?}}`. Codes are the `ErrorCode`
  constants in `errors.go`, equal to the spec's enum (`TestErrorCodesMatchSpec`).
  The one exception is the 503 of `GET /health` (`HealthResponse`, which
  monitors and the console read).
- A job lookup is 404 `job_not_found` only for a missing row
  (`writeJobLookupError`); any other store error is 500 `store_error`.
- `Deps`: Config, Store, Hub and Manager are required. Logging, Wrapper, Quality,
  DeveloperToken and LibrarySync are optional; a route whose dependency is nil
  answers 503 `feature_unavailable`.
- Streams are SSE only, started by `startSSE` (Content-Type, `Cache-Control:
  no-cache`, `X-Accel-Buffering: no`, head flushed, no `Connection` header). Every
  stream sends `: keepalive` every `sseKeepaliveInterval`; job streams also
  re-read the store every `storeRedrainInterval`. Keep both. A write error ends
  the loop.
- Resume cursors: `Last-Event-ID` wins over `?cursor=` (`streamCursor`); a
  malformed one is 400 `invalid_request`. Snapshots return `event_cursor`, read
  before the snapshot. Cursors only move forward; never advance one past an event
  the client was not sent (the feed loop comments explain the store-error cases).
- SSE frames are named events (`event: <type>`): per-job streams use the persisted
  event types, the overview feed `download_upserted` and `download_deleted`.
  Renaming a type breaks every client that switches on it.
- A job stream subscribes to the hub before it reads the backlog, and ends only
  when `eventsExhausted` says no more events can arrive (job terminal, finalize
  done, hooks no longer pending, in that order). Caught up and exhausted at
  connect is 204, not an error. The comments explain each race; keep the order.
- `GET /config` is a pure read; `POST /config/reload` re-reads the file.
- A submission's `media_user_token` override is never echoed back in a job,
  item or event (`DownloadOverrides.WithoutMediaUserToken` in internal/config).

## Change together

A route, request or response change lands in one commit with:

1. `internal/api/openapi.yaml`;
2. the handler and its line in `Server.routes`;
3. a handler test in this package (httptest against `Server.Routes`, errors
   checked with `assertErrorCode`) and the parity tests passing;
4. [docs/api.md](../../docs/api.md) and the API table in
   [README.md](../../README.md#api) plus its `README.zh-CN.md` mirror;
5. a line in the report or PR naming the iOS app and amdl-web as clients to
   update. Never write that they are synced.

A new error code needs its constant in `errors.go`, the spec's `ErrorCode` enum
and the table in docs/api.md. A new per-URL submit code needs the spec's
`SubmitErrorCode` enum (`TestSubmitErrorCodesMatchProducers`). A new event type
needs its `internal/domain` constant, the OpenAPI event enum, and the
[Events](../../docs/api.md#events) section of docs/api.md.

The full checklist is the
[amdl-api-change](../../.claude/skills/amdl-api-change/SKILL.md) skill.

## Verify

```bash
go test ./internal/api/
go test -race ./internal/api/   # stream, hub or cursor changes
make dev                        # then curl /api/v1 as in amdl-verify
```
