# internal/config

Loads, validates and persists the service config (`Load`, `Config.Validate`,
`Store`), applies `AMDL_*` environment overrides, and builds per-job
`DownloadOverrides`. These rules also cover `configs/`.

## Invariants

- `configs/config.example.yaml` is the documentation and the bootstrap source:
  `EnsureFile` copies it from the config file's own directory to
  `configs/config.yaml` on first start. Each key's comment states allowed values,
  valid booleans, units, the behavior when the key is absent, list item options and
  supported template variables.
- `configs/config.yaml` is generated and git-ignored, and `PUT /api/v1/config`
  rewrites it without comments. Nothing is documented only there.
- `Default()` is what an install gets for a key its file omits. When you touch a
  key, make its "Default:" comment in the example agree with `Default()`.
- Runtime keys (`isRuntimeKey`) apply through `PUT /api/v1/config`; every other key
  is a startup key and needs a restart. Classify each new key and say which kind it
  is in its example comment.
- Every key is overridable as `AMDL_<SECTION>_<KEY>`; `envFields` derives the names
  from the yaml tags. An unknown `AMDL_*` variable fails startup, PUT rejects
  env-pinned keys with 422, and the variables that are not keys are listed in
  `envIgnored`.
- Renaming or removing a key is a breaking change: unknown keys fail startup, so
  it needs a `!` commit and an upgrade-guide row telling users what to edit.
- `configs/hooks.yaml` belongs to internal/hooks; its comments are its
  documentation in the same way.

## Change together

A new key lands in one commit with: the struct field (yaml and json tags),
`Default()`, `Config.Validate`, the full comment in `configs/config.example.yaml`,
the runtime or startup classification, and, when a job may override it,
`DownloadOverrides` with its `DownloadOverrides.Apply` branch plus the
`DownloadOverrides` schema in `internal/api/openapi.yaml` and the overrides table in
[docs/api.md](../../docs/api.md). Concept changes also go to
[docs/configuration.md](../../docs/configuration.md); user-facing ones to the README
Configuration section.

## Verify

```bash
go test ./internal/config/
```

`TestCommittedExampleDocumentsEveryConfigKey` fails when the example's key set drifts
from the struct and `TestEnvFieldsCoverConfig` when env coverage does; neither can
judge whether a comment is complete, so reread it.
