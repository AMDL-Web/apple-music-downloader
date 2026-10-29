---
paths:
  - ".github/**"
---

# GitHub workflows

- Least privilege: a top-level `permissions:` block, widened per job only where a
  step needs it. Jobs that run pull-request code get `contents: read`; a job that
  holds a secret or posts as a bot runs default-branch code only.
- Pin actions the way the existing workflows do; no `@main` or unpinned
  third-party actions.
- Values from inputs, PR titles or other user-controlled text reach `run:` through
  `env:`, quoted, never interpolated into the shell.
- A gate fails closed: a failed query, missing artifact, unrecognized input or
  skipped step is a failure. An advisory check may fail open, and one that did not
  run is reported as not run, not as "no findings".
- CI steps call `make` targets, so the Makefile and `.github/workflows/ci.yml`
  change together.
