# amdl-backend

Go service that resolves Apple Music links and runs the download, decrypt, remux
and tag pipeline: REST and SSE under `/api/v1`, a SQLite store, and the web console
in `internal/console`, the one client this repository maintains. Code map: the
[README reading map](README.md#reading-map); vocabulary: `GLOSSARY.md`; docs index:
`docs/README.md`.

## Working agreements

- What you decide alone and what waits for the maintainer is in `HUMAN.md`.
- Do the lightest fix first. A new mechanism (check, hook, CI step, scheduled job)
  needs the maintainer's OK after you state what it prevents, when it runs and who
  reads the result.
- Before changing a source file over about 20 KB, split one responsibility out of
  it in a move-only commit ([amdl-refactor](.claude/skills/amdl-refactor/SKILL.md)),
  or say there is no natural seam.
- Every wait and every outbound call is bounded: `timeout` on shell waits, a context
  deadline or client timeout in Go.
- Docs change in the commit that changes the behavior. `rg` the changed symbols,
  keys and rule words across `README.md`, `README.zh-CN.md`, `docs/`, the AGENTS.md
  files, `REVIEW.md`, `GLOSSARY.md`, `.claude/`, `internal/api/openapi.yaml` and
  `configs/config.example.yaml`. When a doc and the code disagree, fix the doc.
- Reports keep changed, tested, pushed and released apart, and label behavior
  inferred from reading code as inference.
- Rules for some paths live in `.claude/rules/` and in `AGENTS.md` files under
  `internal/`; open the matching one before editing there.

## This service has no auth, on purpose

Every endpoint is unauthenticated; access control belongs to the deployment layer
in front of it (reverse proxy, gateway). Each install serves one user, and the
project is distributed as open source. Don't add auth middleware or multi-user
features, and don't report missing auth in a review.

## Contracts and breaking changes

The HTTP API (`internal/api/openapi.yaml`), config keys, the DB schema, SSE event
types and output paths are contracts that installs and the console depend on.

- A breaking change needs a reason: it fixes a defect, removes duplication or makes
  a contract machine-checkable.
- Mark it with a `!` Conventional Commit (`feat(api)!: …`) and add a row (old
  shape, new shape, affected clients) to the upgrade guide in `docs/` for the next
  major version. Rename directly; no compatibility shims.
- Output paths are the exception. A change to path templates, `{Quality}` labels,
  name cleaning or sidecar names makes the "already downloaded" check miss and
  libraries download again, so it needs the maintainer's OK and an upgrade note
  (`internal/media/AGENTS.md`).
- What changes together with each surface (spec, handler, docs, README table,
  console, config example, migration) is listed in
  [amdl-api-change](.claude/skills/amdl-api-change/SKILL.md).
- The console changes in the same commit as an API it uses. The iOS app and amdl-web
  are set aside and get no follow-ups.

## Config

`configs/config.yaml` is generated: copied from `configs/config.example.yaml` on
first start, then rewritten without comments by `PUT /api/v1/config`. The example
is therefore the documentation. Each key's comment states allowed values, units,
the behavior when the key is absent, whether it is a runtime or a startup key, and
any template variables or list options.

## Two Apple hosts

`api.music.apple.com` is the documented API and takes a self-signed developer token;
the backend does not sign one and never calls it. `amp-api.music.apple.com` takes a
JWT scraped from the music.apple.com web player plus an `Origin` header, answers
every catalog read, and is the only host that returns `enhancedHls` and
`editorialVideo`. amp-api is undocumented, so anything read from it degrades to
"not available" instead of failing a job (`internal/applemusic/AGENTS.md`).

## Data safety

- A schema change is a new migration that keeps every existing row
  (`internal/db/AGENTS.md`). Before applying one to a real database, describe its
  impact and get the maintainer's confirmation.
- Never commit `configs/config.yaml`, `*.p8` files, `keys/` or `data/`: they hold
  tokens, private keys and local state.

## Language

Everything in the repository is English: code, comments, docs, config comments, CI,
OpenAPI descriptions and commit messages. Exceptions: `README.zh-CN.md` (a
translation of `README.md`), hand-written release notes (may be Chinese), and
review-bot PR comments (Simplified Chinese, set in `REVIEW.md`).

## Verification

CI (`.github/workflows/ci.yml`) runs the `make check` steps on pushes to `main` and
`dev` and on pull requests, and builds the Docker image. Locally, run what the
change touches; [amdl-verify](.claude/skills/amdl-verify/SKILL.md) has the live API
and console checks and the report format.

| Change | Run |
| --- | --- |
| Docs, AGENTS.md, rules, skills | `go test -count=1 ./internal/repocheck/` |
| Go logic | the package's tests, then `make test` |
| Decryption (`internal/fairplay`, `tools/temaridiff`) | `go test ./internal/fairplay/` and `make temaridiff`, which `make check` does not run |
| Concurrency (jobs, media pools, events, SSE) | `make race` |
| API routes, schemas, SSE | `go test ./internal/api/` and a live check with `make dev` |
| Config keys | `go test ./internal/config/ ./internal/api/`, then reread the key's example comment |
| DB schema | `go test ./internal/db/` and a run against a copy of a real database |
| Console | the commands in `internal/console/AGENTS.md` and a browser check |
| Dependencies | `make vuln` |
| Scripts, release tooling | `make node-test` |
| Dockerfile, entrypoint | `make docker` |
| Before pushing to dev | `make check` |

Add a behavior test where a regression is likely, not one that restates the
implementation. Say what you could not verify and why.

## Commits and releases

- Work lands on `dev`; small changes need no branch of their own. `main` changes
  only through the `dev` → `main` release PR. Rebase onto `origin/dev`.
- Every commit carries a DCO `Signed-off-by` trailer (`git commit -s`). Non-merge
  commits follow Conventional Commits.
- Only Codex appends `Co-authored-by: Codex <noreply@openai.com>`; other agents use
  their own attribution.
- Releases follow [amdl-release](.claude/skills/amdl-release/SKILL.md); release-note
  attribution is in [CONTRIBUTING.md](CONTRIBUTING.md#releases).

## Merging with `--admin`

A branch-protection bypass needs the maintainer's approval for that specific
bypass; "open and merge the PR" is not it. When reviews, checks or unresolved
conversations block a merge, report what blocks it and wait.

## Related repositories

[wrapper-lite](https://github.com/WorldObservationLog/wrapper/tree/lite) holds the
Apple Music account session and serves manifests, licenses, lyrics and per-key
decryption templates over HTTP (`internal/wrapper`); the backend decrypts in-process.

## Rule -> enforced by

| Rule | Enforced by |
| --- | --- |
| gofmt, build, vet, tests, race detector, govulncheck, Node tests | CI, one step per `make` target |
| The Docker image builds | CI docker build job |
| Config example key set and defaults match the struct | tests in `internal/config` |
| Routes, error codes and runtime config keys match `internal/api/openapi.yaml` | parity tests in `internal/api` |
| Output paths | golden tests in `internal/media` |
| `internal/fairplay` decrypts like upstream Temari, and `internal/fairplay/rounds_gen.go` is the pinned version's translation | CI Temari parity job (`make temaridiff`); Renovate opens a PR per Temari release |
| Console: API paths exist in the spec, Settings cover runtime keys, design guard, vendor checksum | tests in `internal/console` |
| Docs links, backticked paths, `path#Symbol` pointers, AGENTS.md size, CLAUDE.md pairs, docs types and index | `internal/repocheck` tests |
| DCO sign-off | DCO GitHub App on PRs; `scripts/guard-commands.mjs` on `git commit` |
| `main` only through a PR | branch protection; guard hook on `git push` |
| No `--admin` merge without approval | guard hook on `gh pr merge` (Claude Code only) |
| Never commit config, keys or data | `.gitignore`; guard hook on `git add -f` |
| No auth, breaking-change marking, console updated with the API, full config comments, row-preserving migrations, amp-api degrades | text and `REVIEW.md` |

The guard hook runs where Claude Code loads `.claude/settings.json`; other agents
follow the text.

## Code review rules

@REVIEW.md
