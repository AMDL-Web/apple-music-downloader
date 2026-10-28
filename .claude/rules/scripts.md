---
paths:
  - "scripts/**"
  - ".github/scripts/**"
  - "tools/**"
---

# Scripts and tools

- Each script has a test next to it: `node:test` for `.mjs` (run by
  `make node-test`), a Go test for tools under `tools/`.
- Bad arguments print usage and exit 2; an unknown flag or a missing value never
  falls back to a default, because a silent default makes a check pass by mistake.
- Gate and verification scripts fail closed: a failed query, missing input or
  unrecognized value is a failure, with a test per case.
- The one deliberate fail-open: `scripts/guard-commands.mjs` passes input that is
  not the hook JSON it expects, because a guard that failed closed on a harness
  format change would block every Bash call. Changes to what it blocks update the
  "Rule -> enforced by" table in AGENTS.md and its test in the same commit.
- Source: [AGENTS.md](../../AGENTS.md#rule---enforced-by).
