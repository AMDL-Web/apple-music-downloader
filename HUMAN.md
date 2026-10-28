# HUMAN.md

For the maintainer: how work is split between you and coding agents, what agents
do alone, what waits for you, and how they report. Hard rules for agents live in
[AGENTS.md](AGENTS.md); outside contributors follow [CONTRIBUTING.md](CONTRIBUTING.md).
This page points to them rather than repeating them.

## What agents do alone

Reversible work, with any assumption that matters stated in the report:

- branches, commits signed off with `git commit -s`, and pushes to `dev`;
- docs, skills, tests and fixes, including breaking changes that follow
  [Contracts and breaking changes](AGENTS.md#contracts-and-breaking-changes);
- fixing red CI, answering review-bot comments, rebasing onto `origin/dev`;
- starting `make dev`, reading logs, probing the local API.

## What waits for you

- Merging the `dev` → `main` release PR, and publishing a release or image tag.
- Any `gh pr merge --admin` or other branch-protection bypass: approval is per
  bypass ([AGENTS.md](AGENTS.md#merging-with---admin)).
- Applying a schema migration to a real database ([Data safety](AGENTS.md#data-safety)).
- Breaking changes to the API, config keys, DB schema or output paths, and any
  rename or removal on those surfaces.
- Deleting branches, tags, releases or GHCR images.
- Repository and account settings: branch protection, secrets, repo variables such
  as `CLAUDE_AUTO_REVIEW` and `CODEX_AUTO_REVIEW`, installed apps.
- A new mechanism (CI step, hook, check, scheduled job): the agent states what it
  prevents, when it runs and who reads it, then waits for your OK.

## What your words mean

| You say | The agent does |
| --- | --- |
| "Look into X", "read this", "is X possible?" | Reports findings and a recommendation; changes no code. |
| "X is broken" plus what you saw | Treats it as a fix request: reproduces, fixes with a regression test, pushes to `dev` (amdl-diagnose). |
| "Implement X" | Implements, verifies (amdl-verify) and pushes to `dev`, or opens a PR when you ask for one. |
| "A draft is fine" | Stops at a draft PR. |
| "Release vX.Y.Z" | Prepares the release PR and notes (amdl-release) and waits for your merge. |
| "From now on, …" | A new standing rule: the agent updates AGENTS.md, this page or a skill in the same change. |

When a request can be read two ways, the agent restates the goal in a sentence or
two and proceeds on that reading; one reply corrects it.

## How agents report

- Result first, then evidence: commands with their output, file and symbol
  pointers, links.
- Four states kept apart: changed locally, tests passed, pushed to `dev`, released
  in an image.
- Behavior inferred from reading code is labeled as inference, with what would
  confirm it.
- What was not verified, and why (for example, no wrapper-manager in a cloud
  session, so real downloads were simulated).
- Follow-ups outside this repository named explicitly, such as "the iOS app and
  amdl-web need the new field"; never reported as done.

## Review bots

Claude and Codex review every non-draft PR opened by a person; both follow
[REVIEW.md](REVIEW.md) and write their comments in Simplified Chinese. Agents fix
real defects in one push after the first batch of reviews, answer edge-case
findings with a reason instead of patching, and stop to rethink if a third round
still finds new blocking problems.
