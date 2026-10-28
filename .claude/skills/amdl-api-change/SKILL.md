---
name: amdl-api-change
description: Checklist for changing an amdl-backend contract surface: HTTP routes, request and response schemas, SSE event types, config keys, DB columns and output paths. Covers when a breaking change is justified and how to mark it, what must change in the same commit (openapi.yaml, handler, route table, parity test, docs/api.md, README API table, config example, migration) and the console update that goes with an API change. Use when adding or changing an endpoint, field, enum value, event, config key or column, or when asked to "add an API", "expose X", "add a setting".
---

# Change a contract surface

Installs, scripts and the embedded console (`internal/console`) depend on these
surfaces; the iOS app and amdl-web are set aside and not kept in sync. Breaking
changes are allowed when they fix a defect, remove duplication or make a contract
checkable (AGENTS.md "Contracts and breaking changes"); they are marked and
documented so every client knows what to change.
Output paths are the exception: they stay as they are without the maintainer's OK.

```
- [ ] 1 Classify the change
- [ ] 2 Design it
- [ ] 3 Change everything that moves together, in one commit
- [ ] 4 Verify
- [ ] 5 Report
```

## 1. Classify

| Surface | Compatible | Breaking (mark `!`, add an upgrade-guide row) |
| --- | --- | --- |
| Route | new route | removed or renamed route, new required parameter |
| Request/response | new optional field, new enum value clients may ignore | removed or renamed field, changed type or meaning, newly required field |
| SSE event | new event type, new payload field | renamed event, removed field, cursor semantics |
| Config key | new key with a default that keeps behavior | renamed or removed key, changed default behavior |
| DB schema | new column with `DEFAULT`, new table | dropped, renamed or rebuilt table or column (user data still survives the migration) |
| Output path | new template variable | changed default template, `safeName`, quality labels, sidecar names: stop and ask, existing libraries re-download |

Breaking: say in one line which defect or duplication it removes. No compatibility
shim: rename directly, use a `!` Conventional Commit (`feat(api)!: …`) so the
changelog lists it under Breaking, and add a row (old shape, new shape, affected
clients) to the upgrade guide under `docs/` for the next major version.

## 2. Design

- Name fields like their neighbors in `internal/api/openapi.yaml` (snake_case JSON,
  existing enum spellings).
- New fields are always present or clearly optional; say which in the spec.
- No auth fields or middleware (AGENTS.md "This service has no auth, on purpose").
- Anything sourced from amp-api is optional and may be absent
  (`internal/applemusic/AGENTS.md`).

## 3. Change together

Route, request or response:

- [ ] `internal/api/openapi.yaml` (path, schema, examples, error responses)
- [ ] the handler and its line in `Server.Routes` (internal/api)
- [ ] a handler test via httptest, and the route/OpenAPI parity test passing
- [ ] [docs/api.md](../../../docs/api.md) section and curl example
- [ ] README API table in `README.md` and `README.zh-CN.md`
- [ ] the console (`internal/console`) when it calls the route or reads the field

Event type: the `internal/domain` constant, the OpenAPI event enum, the stream close
rules in internal/api, the Events section of docs/api.md, and for hook-visible
events internal/hooks, `configs/hooks.yaml` and
[docs/automation.md](../../../docs/automation.md).

Config key: follow "Change together" in `internal/config/AGENTS.md` (struct,
`Default()`, validation, the full comment in `configs/config.example.yaml`,
runtime or startup, job overrides and their OpenAPI schema when overridable).

DB column: follow `internal/db/AGENTS.md` (a new numbered migration that keeps user
data, tested against the frozen 1.x schema; never edit a shipped migration).

Output path: follow `internal/media/AGENTS.md`; any change to existing names is
breaking.

Then `rg` the old and new names across `README.md`, `README.zh-CN.md`, `docs/`,
every AGENTS.md, `GLOSSARY.md` and `.claude/` and fix what is stale.

## 4. Verify

```bash
go test ./internal/api/ ./internal/config/ ./internal/db/
go test -count=1 ./internal/repocheck/
make dev   # then exercise the change with curl, per amdl-verify step 3
```

Done when the tests pass and a live request returns exactly what the spec says,
including the error cases.

## 5. Report

State the surface, compatible or breaking, the files changed, and the verification
evidence. Say whether the console uses the change and where it was updated; if it
does not use it yet, say so. No follow-ups are owed to the iOS app or amdl-web.
