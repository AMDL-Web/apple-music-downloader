---
paths:
  - "**/*.go"
---

# Go code

- Exported identifiers keep a godoc comment that starts with the name. Other
  comments are kept only if deleting them would lead someone to break the code:
  surprising behavior (a workaround, a runtime quirk, a race), design intent
  (an invariant, a deliberate threshold), or a cross-component contract (field
  meaning and units, "keep in sync with X" written on both sides when it can't be a
  test or a shared constant).
- No comments that restate the code, mention callers, tasks or PRs, tell history
  ("previously", "now"), or cite line numbers; name the symbol instead. A TODO is
  written as `TODO(#123)` with an issue. `//go:` directives and `//nolint` are code.
- Wrap errors with `%w` and the context a reader needs, for example
  `fmt.Errorf("load hooks config %s: %w", path, err)`; don't discard an error
  without a comment saying why.
- Every outbound HTTP or gRPC call is bounded by a context deadline or a client
  timeout; an unbounded call can hang a job forever. Apple requests go through the
  helpers in internal/applemusic (see `internal/applemusic/AGENTS.md`).
- Tests sit next to the code in the same package, table-driven where cases share a
  shape. Expected values come from independent facts, not from re-running the
  implementation's own logic.
- A package with its own AGENTS.md (`internal/api`, `internal/applemusic`,
  `internal/config`, `internal/db`, `internal/jobs`, `internal/media`) has rules
  that apply on top of these; read it before editing there.
- Source: [AGENTS.md](../../AGENTS.md#working-agreements).
