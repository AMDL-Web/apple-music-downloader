---
paths:
  - "scripts/**"
  - ".github/scripts/**"
  - "tools/**"
---

# Scripts and tools

- Each script has a test next to it: `node:test` for `.mjs` (run by
  `make node-test`), a Go test for `tools/`.
- Bad arguments print usage and exit 2. An unknown flag or missing value never
  falls back to a default, because a silent default makes a check pass by mistake.
- Gate and verification scripts fail closed: a failed query, missing input or
  unrecognized value is a failure, with a test per case.
- The exception is `scripts/guard-commands.mjs`, which passes input that is not the
  hook JSON it expects, so a harness format change cannot block every Bash call. A
  change to what it blocks updates the "Rule -> enforced by" table in `AGENTS.md`
  and its test in the same commit.
