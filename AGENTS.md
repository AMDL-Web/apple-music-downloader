# amdl

Downloads music from Apple Music: resolve a link, then download, decrypt, remux and
tag each track. Code map: the [README reading map](README.md#reading-map);
vocabulary: `GLOSSARY.md`; docs index: `docs/README.md`.

## Layout

- `apps/amdl`: the one binary. `amdl serve` is the backend (`internal/server#Run`,
  REST and SSE under `/api/v1`, SQLite); the other commands are the CLI client.
- `apps/console`: the web console, embedded into the server.
- `apps/desktop`: the desktop app; it embeds the server in standalone mode.
- `apps/site`: the static website.
- `internal/`: the shared Go core. It never imports a product.

## No auth, on purpose

Every endpoint is unauthenticated; access control belongs to the deployment layer
in front of it. Each install serves one user. Don't add auth or multi-user features,
and don't report missing auth in a review.

## Work and commits

- Work lands on `dev`; rebase onto `origin/dev`. Commits follow Conventional
  Commits.
- Everything in the repository is English (code, comments, docs, commit messages),
  except `README.zh-CN.md`, `apps/site/content/zh-CN.json` and release notes.
- A DB schema change is a new migration that keeps every existing row. Before
  applying one to a real database, describe its impact and get the maintainer's
  confirmation.

## Verification

Run what the change touches; the test layers and golden files are in
[CONTRIBUTING.md](CONTRIBUTING.md#tests).

| Change | Run |
| --- | --- |
| Any Go code | the package's tests, then `make verify` |
| Concurrency (jobs, media pools, events, SSE) | `make race` |
| Web console | `make console-e2e` |
| Docs | `go test ./tools/repocheck/` |
| Scripts, hooks, release tooling | `make node-test` |
| Decryption, wrapper guest, upgrades | `make temaridiff`, `make guestboot`, `make upgradecheck` |
| Dockerfile, dependencies | `make docker`, `make vuln` |

Say what you could not verify and why.
