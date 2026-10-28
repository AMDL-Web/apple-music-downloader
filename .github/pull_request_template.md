## What and why

<!-- One or two sentences: what changes and the reason. Link the issue if there is one. -->

## Contract impact

<!-- Tick what this changes; each ticked line needs the matching update in this PR. -->

- [ ] None of the below
- [ ] API: `internal/api/openapi.yaml`, handler, tests, `docs/api.md`, README API table
- [ ] Config keys: full comment in `configs/config.example.yaml`
- [ ] DB schema: row-preserving migration, impact described below
- [ ] Output paths or file names: called out as a breaking change
- [ ] Mirrors to update outside this repo (iOS app, amdl-web): listed here as follow-ups

## Verification

- [ ] `make check` (or the narrower targets for this change: name them)
- [ ] Live check with `make dev` (API, SSE or pipeline changes): commands and results
- [ ] Docs updated in this PR (`rg` for the changed symbols, keys and rule words)
- [ ] Every commit signed off (`git commit -s`)

## Not verified

<!-- What you could not check and why, e.g. a real download needs wrapper-manager. -->
