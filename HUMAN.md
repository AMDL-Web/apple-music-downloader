# HUMAN.md

For the maintainer: what coding agents do alone, what waits for you, and how they
report. Rules for agents are in [AGENTS.md](AGENTS.md); outside contributors follow
[CONTRIBUTING.md](CONTRIBUTING.md).

## Agents do alone

Reversible work, stating any assumption that matters:

- branches, signed-off commits and pushes to `dev`;
- fixes, tests, docs and skills;
- breaking changes to the API, config keys or DB schema that meet
  [Contracts and breaking changes](AGENTS.md#contracts-and-breaking-changes);
- fixing red CI, answering review bots, rebasing onto `origin/dev`;
- running `make dev`, reading logs, probing the local API.

## Waits for you

- Merging the `dev` → `main` release PR; publishing a release or image tag.
- Any branch-protection bypass such as `gh pr merge --admin`, approved per bypass.
- Output-path changes (templates, `{Quality}` labels, name cleaning, sidecar names).
- Applying a schema migration to a real database.
- Deleting branches, tags, releases or GHCR images.
- Repository and account settings: branch protection, secrets, repo variables such
  as `CLAUDE_AUTO_REVIEW` and `CODEX_AUTO_REVIEW`, installed apps.
- A new mechanism (CI step, hook, check, scheduled job), after the agent says what
  it prevents, when it runs and who reads it.

## What your words mean

| You say | The agent |
| --- | --- |
| "Look into X", "read this", "is X possible?" | reports findings and a recommendation; changes no code |
| "X is broken" plus what you saw | reproduces, fixes with a regression test and pushes to `dev` (amdl-diagnose) |
| "Implement X" | implements, verifies (amdl-verify) and pushes to `dev`, or opens a PR when you ask |
| "A draft is fine" | stops at a draft PR |
| "Release vX.Y.Z" | prepares the release PR and notes (amdl-release), then waits for your merge |
| "From now on, …" | records the rule in AGENTS.md, this page or a skill in the same change |

A request that reads two ways gets a one- or two-sentence restatement of the goal,
and the work proceeds on that reading.

## Reports

- Result first, then evidence: commands and output, `path#Symbol` pointers, links.
- Changed locally, tests passed, pushed to `dev` and released in an image are kept
  apart; behavior inferred from code is labeled as inference.
- What was not verified, and why (for example, no wrapper-manager, so downloads
  ran in simulate mode).
- Work owed outside this repository, such as a wrapper-manager change, is named and
  never reported as done.

## Review bots

Claude and Codex review every non-draft PR and follow [REVIEW.md](REVIEW.md).
Agents fix real defects in one push after the first batch of reviews, answer
edge-case findings with a reason, and stop to rethink the approach if a third round
still finds new blocking problems.
