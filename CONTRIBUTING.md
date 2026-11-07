# Contributing

Thanks for helping. This page is the contributor workflow.

## Branches

- `dev` is where work lands. Maintainers push small changes to it directly; base
  your branch and your pull request on `dev`.
- `main` is release-only and changes through a `dev` → `main` pull request. Don't
  branch from `main`: the work ends up in the wrong place.
- Keep up to date with `git fetch origin && git rebase origin/dev`.

## Commits

Non-merge commits follow [Conventional Commits](https://www.conventionalcommits.org/):
`type(scope): summary`, for example `fix(download): keep upgrades on their path layout`.
Write commit messages, code, comments and docs in English.

## Sign-off (DCO)

All commits must be signed off under the
[Developer Certificate of Origin](https://developercertificate.org/). By signing off,
you certify that you wrote the change or otherwise have the right to submit it under
this project's license.

Sign off every commit with the `-s` flag:

```sh
git commit -s -m "fix(api): reject empty url lists"
```

This appends a `Signed-off-by` trailer using your configured `git config user.name` /
`user.email`:

```
Signed-off-by: Your Name <you@example.com>
```

If you forgot to sign off the last commit, amend it:

```sh
git commit --amend -s --no-edit
```

For several commits on your branch:

```sh
git rebase --signoff origin/dev
```

Pull requests are checked by the [DCO GitHub App](https://github.com/apps/dco) and
fail if any commit lacks a valid `Signed-off-by` trailer matching its author or
committer.

## Checks

Run the same gate CI runs before you push:

```sh
make check
```

It runs, in order: `gofmt` check, build, vet, staticcheck, tests, the race detector,
`govulncheck` and the Node script tests. `make verify` is the quicker subset to run
before a commit. CI runs the tests with a coverage summary (`make cover`) and the race
detector (`make race`) as parallel jobs, and the console's browser tests
(`make console-e2e`) as a job of their own. `make help` lists the narrower targets, and
`make dev` starts the API in simulate mode on `127.0.0.1:18080`, which needs no wrapper.

## Tests

| Layer | Where | Notes |
| --- | --- | --- |
| Static | `make vet`, `make lint` (staticcheck), `tools/repocheck` | repocheck keeps Markdown links and path pointers honest |
| Unit | `*_test.go` next to the code | Table-driven; concurrency and timeouts on `testing/synctest`'s virtual clock, not real sleeps |
| Integration | real listeners (`httptest`), a real SQLite store; `internal/media/integration` | The slow ones skip under `go test -short` |
| End to end | `apps/amdl/testdata/script/*.txtar` | testscript drives the real `amdl` binary: CLI, MCP, `serve`, a download against the demo backend |
| Contract | `internal/api/openapitest` | Every API test response is checked against `internal/api/openapi.yaml` |
| Browser | `apps/console/e2e` | Playwright on the console's main paths, axe accessibility checks, a few screenshots |

A bug fix comes with a test that fails without the fix. Golden files live under each
package's `testdata/` and are rewritten with `go test ./<package> -update` after a
deliberate change; review the diff before committing. Exceptions: the output-path goldens
in `internal/media` have no `-update` and are edited by hand, since a changed path makes
libraries download again, and the desktop stills are rewritten with
`go test ./apps/desktop/internal/golden -update` (review the PNG diff).

The API in `internal/api/openapi.yaml`, config keys, the DB schema and output paths
are contracts.

## Pull requests

Fill in [the template](.github/pull_request_template.md): what and why, contract
impact, how you verified it, and what you could not verify. Claude and Codex post
automatic reviews (in Simplified Chinese); a maintainer decides what to act on.

## Releases

Maintainers cut releases by merging the `dev` → `main` pull request with a `vX.Y.Z`
version in its title, or by running the `Release` workflow with a version. Both
create the GitHub Release and publish the multi-arch image to GHCR.

Release notes may be hand-written at `.github/release-notes/<version>.md` (English
or Chinese); the workflow uses that file when it is non-empty and generates notes
otherwise. Each entry lists its commit author and every `Co-authored-by`
contributor, deduplicated, as `@username` when the account is known:
`Codex <noreply@openai.com>` is `@codex`, and any `Claude … <noreply@anthropic.com>`
co-author is `@claude`.
