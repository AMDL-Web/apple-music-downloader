# amdl-backend

Go service that resolves Apple Music links and runs the download → decrypt →
remux → tag pipeline. REST + SSE on `/api/v1`, SQLite store. It owns
`internal/api/openapi.yaml` and the one maintained client, the web console in
`internal/console`.

Its HTTP API, config keys, DB schema and output paths are contracts that installs
and clients depend on; see [Contracts and breaking changes](#contracts-and-breaking-changes).

## Working agreements

- Decide routine, reversible choices yourself and state any assumption that matters. Ask only when missing information would change the result or the next step goes past what was asked, and keep doing the parts that don't depend on the answer.
- When a request reads more than one way, restate the goal in a sentence or two and proceed on that reading, so a wrong reading costs one reply instead of a finished change.
- The maintainer's explicit request beats this file and skill defaults. When a skill says stop, name the file and the rule.
- Do the lightest fix first (wording, one config line). A new mechanism (check, hook, CI step, scheduled job) states what it prevents, when it runs and who reads its result, because every later change pays for it.
- Before saying something can't be done, `rg` the repo, run `git log -S` / `--grep`, and read the official docs or upstream issues. Before "fixing" an inconsistency, check history for a deliberate choice.
- Every wait (CI, review bots, background loops) and every outbound call has a bound: `timeout` on shell waits, a context deadline or client timeout in Go. An unbounded wait hangs silently.
- When a change touches a source file over about 20 KB, first split one responsibility out of it, move-only, with `tools/splitdecls` ([amdl-refactor](.claude/skills/amdl-refactor/SKILL.md)). If there is no natural seam, skip it and say so.
- Update affected docs in the same commit: `rg` the changed symbols, keys and rule words across `README.md`, `README.zh-CN.md`, `docs/`, every AGENTS.md, `REVIEW.md`, `GLOSSARY.md`, `.claude/`, `internal/api/openapi.yaml` and `configs/config.example.yaml`. When docs and code disagree, code wins and the doc is fixed now.
- Report the result first, then evidence. Keep changed, tested, pushed and released apart, and label behavior inferred from reading code as inference, with what would confirm it.
- Path rules live in `.claude/rules/` (loaded when you Read a matching file) and nested AGENTS.md files; open them yourself when editing through the shell.

## This service has no auth, on purpose

It is the download core. Every endpoint is unauthenticated, including
`GET /api/v1/developer-token`, which returns a usable Apple Music developer
token. Access control is the responsibility of the deployment layer above this
service (reverse proxy, gateway, or frontend session), not this codebase.

Don't add auth middleware here, and don't report missing auth as a finding in a
review: it's a deliberate architecture boundary, not an oversight.

## Contracts and breaking changes

The API, config keys, DB schema and output paths may change, breaking changes included, when the change fixes a defect, removes duplication or makes a contract machine-checkable. Taste alone is not a reason: every break costs each install and client an upgrade.

- A breaking change is a `!` Conventional Commit (`feat(api)!: …`), which the changelog lists under Breaking, and a row (old shape, new shape, affected clients) in the upgrade guide under `docs/` for the next major version.
- A route or response change lands in one commit with: `internal/api/openapi.yaml`, the handler and route table in `internal/api`, the route/OpenAPI parity test, [docs/api.md](docs/api.md) and the API table in [README.md](README.md#api).
- A config key needs its full comment in `configs/config.example.yaml` ([Config](#config)).
- A schema change needs a row-preserving migration ([Data safety](#data-safety)).
- Output paths are the exception: a change to path templates, `{Quality}` labels or name cleaning makes the "already downloaded" check miss, and libraries download again. Change them only with the maintainer's OK and an upgrade note; see `internal/media/AGENTS.md`.
- A route or response the console uses changes together with the console. API changes are not held back for other clients ([Related repositories](#related-repositories)).

## Config

`configs/config.yaml` is generated: bootstrapped from `configs/config.example.yaml`
on first start, then rewritten by `PUT /api/v1/config`, which drops all comments.
Hand edits survive only until the next runtime config write.

So `configs/config.example.yaml` is the documentation, and each key carries its full
comment: allowed enum values, valid booleans, numeric units and default behavior,
list item options, supported template variables. A test in `internal/config` fails
when the example's key set drifts from the struct; it can't judge the comment.
Details in `internal/config/AGENTS.md`.

## Two Apple hosts, not one

`api.music.apple.com` (developer token) and `amp-api.music.apple.com` (scraped JWT
plus `Origin`) are not interchangeable: `enhancedHls` and `editorialVideo` exist only
on amp-api. Anything read from amp-api degrades to "this album doesn't have that"
instead of failing a job. Read `internal/applemusic/AGENTS.md` first.

## Data safety

Schema changes need a migration that preserves existing rows, and confirmation
before you apply one. Describe the migration impact rather than assuming a
destructive rebuild is acceptable.

Never commit `configs/config.yaml`, `*.p8` files, `keys/` or `data/`: they hold
tokens, private keys and local state, which is why git ignores them.

## Language

Everything in the repository is English: code, comments, docs, config comments, CI,
OpenAPI descriptions and commit messages, so every contributor and agent reads one
language. Deliberate exceptions: `README.zh-CN.md` (a translation; `README.md` is the
source), hand-written release notes (may be Chinese), and review-bot PR comments
(Simplified Chinese, set in `REVIEW.md`).

## Where to start

- Code map: [README "Reading map"](README.md#reading-map). Vocabulary: `GLOSSARY.md`. Docs index and doc types: `docs/README.md`.
- Package rules: `internal/api/AGENTS.md`, `internal/applemusic/AGENTS.md`, `internal/config/AGENTS.md`, `internal/db/AGENTS.md`, `internal/jobs/AGENTS.md`, `internal/media/AGENTS.md`. Read the one for a package before changing it.
- Who decides what, and how to report: `HUMAN.md`. Human contributor workflow: `CONTRIBUTING.md`.

## Verification

Run what the change touches. CI (`.github/workflows/ci.yml`) is the authority: it runs
the `make check` steps on pushes to `main` and `dev` and on pull requests, and builds
the Docker image.

| Change | Run |
| --- | --- |
| Docs, AGENTS.md, skills | `go test -count=1 ./internal/repocheck/` |
| Go logic | the package's tests, then `make test` |
| Concurrency (jobs, media pools, events, limits) | `make race` |
| API routes or schemas | `go test ./internal/api/` and a live check with `make dev` ([amdl-verify](.claude/skills/amdl-verify/SKILL.md)) |
| Config keys | `go test ./internal/config/`, then reread the key's example comment |
| Dependencies | `make vuln` |
| Scripts, release tooling | `make node-test` |
| Dockerfile, entrypoint | `make docker` |
| Before pushing to dev | `make check` |

Add a behavior test where a regression is likely, not tests that restate the
implementation. Say what you could not verify and why.

## Skills

Procedures live in `.claude/skills/` (also reachable as `.agents/skills`). Open the one that fits:

- [amdl-verify](.claude/skills/amdl-verify/SKILL.md): after any change, before pushing or reporting done.
- [amdl-diagnose](.claude/skills/amdl-diagnose/SKILL.md): something is broken, failing, flaky or slow; reproduce first, then fix.
- [amdl-api-change](.claude/skills/amdl-api-change/SKILL.md): adding or changing a route, schema, SSE event, config key or DB column.
- [amdl-release](.claude/skills/amdl-release/SKILL.md): cutting a release (dev → main PR, release notes, image check).
- [amdl-refactor](.claude/skills/amdl-refactor/SKILL.md): splitting a large file or moving declarations without behavior change.

## Commits and releases

Full workflow in [CONTRIBUTING.md](CONTRIBUTING.md). What bites in practice:

- Only `main` requires a pull request. Work lands on `dev` directly — small
  changes don't need their own branch, and cutting one off `main` puts the work
  in the wrong place. `dev` is promoted into `main` by PR for releases. Rebase onto
  `origin/dev`, never `origin/main`.
- Every commit needs a DCO `Signed-off-by` trailer (`git commit -s`; for commits
  already made, `git rebase --signoff origin/dev`). The
  [DCO app](https://github.com/apps/dco) blocks the PR otherwise. Non-merge
  commits follow [Conventional Commits](https://www.conventionalcommits.org/).
- Codex — and only Codex — appends `Co-authored-by: Codex <noreply@openai.com>`.
  Other agents use their own attribution and leave this footer alone.
- Release notes may be hand-written at `.github/release-notes/<version>.md`; the
  release workflow prefers that file when non-empty, else auto-generates.
- In those hand-written notes, each entry lists its commit author and all
  `Co-authored-by` contributors, deduplicated. Use the `@username` form whenever
  the GitHub account is identifiable — plain model-name text renders as text, not
  a mention, so the avatar never shows up. `Codex <noreply@openai.com>` → `@codex`;
  any `Claude … <noreply@anthropic.com>` co-author, whatever the model name → `@claude`.

## Merging with `--admin`

Bypassing branch protection needs the user's approval **for that specific bypass**.
"Open and merge the PR" is not it. If required reviews, status checks, or
conversation resolution block the merge, report what's blocking and wait.

## Related repositories

- amdl-web and the AMDL iOS app: set aside, not kept in sync with this API.
- [wrapper-manager](https://github.com/AMDL-Web/wrapper-manager): the Apple Music account session and decryptor, over gRPC; its proto module is a Go dependency.

## Rule -> enforced by

| Rule | Enforced by |
| --- | --- |
| gofmt, build, vet, tests, race detector, govulncheck, Node script tests | CI `.github/workflows/ci.yml`, one step per `make` target |
| The Docker image builds | CI docker build job |
| Config example key set matches the config struct | test in `internal/config` |
| Every route is in `internal/api/openapi.yaml` and the reverse | internal/api route/OpenAPI parity test |
| Docs links, backticked paths, `path#Symbol` pointers, AGENTS.md sizes, CLAUDE.md pairs, docs types and index | `internal/repocheck` tests |
| DCO sign-off on every commit | DCO GitHub App on PRs; `scripts/guard-commands.mjs` on `git commit` |
| `main` changes only through a PR | branch protection on `main`; guard hook on `git push` |
| No `--admin` merge without approval for that bypass | guard hook on `gh pr merge` (Claude Code only); text elsewhere |
| Never commit config, keys or data | `.gitignore`; guard hook on `git add -f` |
| No auth middleware | text only; `REVIEW.md` tells reviewers not to ask for it |
| Breaking changes marked `!` with an upgrade-guide row; console updated with the API; full config comments | text only; `REVIEW.md` defects; the changelog's Breaking section |
| Row-preserving migrations, confirmed before applying | text only; `REVIEW.md` defect |
| amp-api reads degrade instead of failing a job | text only; `REVIEW.md` defect |
| Codex-only footer, release-note attribution, language policy | text only |

The guard hook runs where Claude Code loads `.claude/settings.json`; other agents
and cloud environments that skip repository hooks follow the text.

## Code review rules

@REVIEW.md
