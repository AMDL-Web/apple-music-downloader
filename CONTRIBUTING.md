# Contributing

Thanks for helping. This page is the human workflow; coding agents also follow
[AGENTS.md](AGENTS.md), and [HUMAN.md](HUMAN.md) describes how the maintainer works
with them. Review bots comment on pull requests following [REVIEW.md](REVIEW.md).

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

It runs, in order: `gofmt` check, build, vet, tests, the race detector, `govulncheck`
and the Node script tests. `make help` lists the narrower targets, and `make dev`
starts the API in simulate mode on `127.0.0.1:18080`, which needs no wrapper-manager.

The API in `internal/api/openapi.yaml`, config keys, the DB schema and output paths
are contracts. If your change touches one of them, read
[Contracts and breaking changes](AGENTS.md#contracts-and-breaking-changes) first.

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
