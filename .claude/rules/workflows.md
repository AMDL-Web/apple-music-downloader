---
paths:
  - ".github/**"
---

# GitHub workflows and repo automation

- Grant least privilege: a top-level `permissions:` block, widened per job only
  where a step needs it. Jobs that run pull-request code get `contents: read`; a
  job that holds a secret or posts as a bot runs default-branch code only.
- Pin actions the way the existing workflows do (major version tags of the
  official actions); don't introduce `@main` or unpinned third-party actions.
- Pass `${{ }}` values that come from inputs, PR titles or other user-controlled
  text through `env:` and quote them in `run:`; never interpolate them into shell.
- Comments and messages are English.
- A gate fails closed: a failed query, a missing artifact, an unrecognized input or
  a skipped step is a failure, never a pass. An advisory check fails open
  (`continue-on-error`), but a check that did not run is reported as not run,
  never as "no findings".
- CI steps call `make` targets, so local `make check` and CI cannot drift; change
  the Makefile and `.github/workflows/ci.yml` together.
- Source: [AGENTS.md](../../AGENTS.md#rule---enforced-by).
