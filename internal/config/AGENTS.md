# internal/config

Config loading, validation and persistence. The key classes and the
`Default()`-equals-example rule are in the package comment
(`go doc ./internal/config`) and its tests. What a key's example comment must say
is in root `AGENTS.md` "Config".

- Classify every new key in `classOf`: runtime when every reader takes it from the
  live snapshot, restart when a component built at startup captures it, file only
  when a wrong value through the API would break the install. A key that falls to
  the file class by default fails `TestFileClassIsTheThreeInstallKeys`.
- No environment variable sets a key; a new install type seeds its first values
  with `amdl serve --init` (`Seed`).
- API writes go through `Store.UpdateAndSave`, which re-reads the file under its
  lock so a hand edit is not overwritten; `Store.Set` does not touch the file.
- Unknown keys fail startup, so renaming or removing a key is a breaking change
  (root `AGENTS.md` "Contracts and breaking changes").
- A runtime `download.*` key either gets a same-named `DownloadOverrides` field or
  an entry in `overrideExclusions` with the reason in the `DownloadOverrides`
  godoc.
- `configs/hooks.example.yaml` belongs to `internal/hooks`; its comments document
  it the same way.

The full list of files a new key touches is in
[amdl-api-change](../../.claude/skills/amdl-api-change/SKILL.md).

## Verify

```bash
go test ./internal/config/ ./internal/api/ ./apps/console/
```
