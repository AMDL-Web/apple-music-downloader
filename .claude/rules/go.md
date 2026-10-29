---
paths:
  - "**/*.go"
---

# Go code

- Exported identifiers keep a godoc comment that starts with the name.
- Other comments stay only where deleting them would lead someone to break the
  code: surprising behavior (a workaround, a runtime quirk, a race), design intent
  (an invariant, a deliberate threshold), or a cross-component contract (field
  meaning and units, "keep in sync with X" on both sides when it can't be a test or
  a shared constant). Name symbols, not line numbers, callers, PRs or history.
  A TODO carries an issue: `TODO(#123)`.
- An error discarded on purpose gets a comment saying why.
- Expected values in tests come from independent facts (a documented shape, a
  known-good file name), not from rerunning the implementation's logic.
- A package with an `AGENTS.md` has rules on top of these; read it first.
