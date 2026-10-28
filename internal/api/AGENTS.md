# internal/api

The HTTP surface: the route table in `Server.Routes`, the handlers, the SSE
streams, and `internal/api/openapi.yaml`, the contract that the iOS app and
amdl-web mirror by hand.

## Invariants

- `internal/api/openapi.yaml` is the contract. A rename or removal is a breaking
  change: allowed when it fixes a defect or removes duplication, marked `!` in the
  commit title and listed in the upgrade guide, because shipped clients break.
- No auth here, ever (root `AGENTS.md`, "This service has no auth, on purpose").
  `GET /api/v1/developer-token` returns a usable token on purpose.
- Every route is one line in the table in `Server.Routes`. The route/OpenAPI
  parity test keeps that table and the spec equal, so a route is never half-added.
- SSE frames are named events (`event: <type>`), and clients switch on the name:
  per-job streams use the persisted event types, the overview feed uses
  `download_upserted` and `download_deleted`. Renaming a type breaks every client
  that switches on it.
- Resume cursors only move forward. Per-job streams resume from `Last-Event-ID`
  and replay persisted events with a greater id; the overview feed resumes the
  same way on its own `event_id`. Never advance a cursor past an event the client
  was not sent; the comments in the feed loop explain the store-error cases.
- A stream subscribes to the hub before it reads the backlog, and closes only when
  `eventsExhausted` says no more events can arrive (job terminal, finalize done,
  hooks no longer pending, in that order). The handler comments explain each race;
  keep the order.
- A submission's `media_user_token` override is never echoed back in a job,
  item or event (`DownloadOverrides.WithoutMediaUserToken` in internal/config).

## Change together

A route, request or response change lands in one commit with:

1. `internal/api/openapi.yaml`;
2. the handler and its line in `Server.Routes`;
3. a handler test in this package (httptest against `Server.Routes`) and the parity
   test passing;
4. [docs/api.md](../../docs/api.md) and the API table in
   [README.md](../../README.md#api) plus its `README.zh-CN.md` mirror;
5. a line in the report or PR naming the iOS app and amdl-web as clients to
   update. Never write that they are synced.

A new event type also needs its `internal/domain` constant, the OpenAPI event
enum, and the [Events](../../docs/api.md#events) section of docs/api.md.

The full checklist is the
[amdl-api-change](../../.claude/skills/amdl-api-change/SKILL.md) skill.

## Verify

```bash
go test ./internal/api/
go test -race ./internal/api/   # stream, hub or cursor changes
make dev                        # then curl /api/v1 as in amdl-verify
```
