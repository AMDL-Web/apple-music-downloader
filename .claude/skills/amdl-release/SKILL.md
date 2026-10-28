---
name: amdl-release
description: Cut an amdl-backend release: check dev is green, write hand-written release notes at .github/release-notes/<version>.md with the author and co-author attribution rules (@codex, @claude), open the dev -> main pull request titled with the vX.Y.Z version, wait for the maintainer's merge, then confirm the GitHub Release and the GHCR image tags. Use when asked to "release", "cut vX.Y.Z", "ship to main", "write release notes" or "publish an image".
---

# Cut a release

Releases go from `dev` to `main` through a pull request; merging it runs
`.github/workflows/release.yml`, which tags the merge commit, creates the GitHub
Release and calls `.github/workflows/docker-publish.yml` to push the multi-arch
image. The maintainer merges; agents prepare and verify.

```
- [ ] 1 dev is ready
- [ ] 2 Version and release notes
- [ ] 3 Release PR
- [ ] 4 Maintainer merges
- [ ] 5 Post-release checks
```

## 1. dev is ready

- `git fetch origin && git log --oneline origin/main..origin/dev` lists what ships.
- CI is green on the tip of `dev`:
  `timeout 900 gh run watch $(gh run list --branch dev --workflow CI --limit 1 --json databaseId --jq '.[0].databaseId') --exit-status`.
- `make check` passes locally on `origin/dev` if anything is in doubt.

## 2. Version and release notes

- Version format: `vX.Y.Z` (an optional suffix like `-rc.1`), the same pattern
  release.yml accepts. Bump minor for features, patch for fixes only; a breaking
  contract change needs the maintainer's decision first.
- Hand-written notes go to `.github/release-notes/<version>.md`; if the file is
  missing or empty, the workflow generates notes from commits instead. Notes may be
  English or Chinese; follow the shape of the latest file in that directory.
- Each entry lists its commit author and every `Co-authored-by` contributor,
  deduplicated, as `@username` whenever the GitHub account is known, so the avatar
  shows. `Codex <noreply@openai.com>` is `@codex`; any
  `Claude … <noreply@anthropic.com>` co-author, whatever the model name, is
  `@claude`. Gather them with
  `git log --format='%an <%ae>%n%(trailers:key=Co-authored-by,valueonly)' origin/main..origin/dev | sort -u`.
- Call out breaking or migration-relevant changes (config keys, schema, output
  paths) and client follow-ups at the top.
- Commit the notes to `dev` with a sign-off:
  `git commit -s -m "docs(release): add <version> notes"`.

## 3. Release PR

```bash
gh pr create --base main --head dev --title "Release <version>" --body-file <notes or summary>
```

The title must contain the version: release.yml reads the first `vX.Y.Z` from it
and fails the run otherwise. Claude and Codex review the PR; handle their findings
per REVIEW.md (real defects fixed on `dev` in one push, edge cases answered).

## 4. Merge

The maintainer merges. If branch protection blocks the merge (reviews, checks,
unresolved conversations), report exactly what blocks it and wait: `--admin` needs
approval for that specific bypass (AGENTS.md "Merging with `--admin`").

The fallback when the title lacked a version: run the `Release` workflow by hand
with the version input (`gh workflow run release.yml -f version=<version>`); it
validates the same pattern.

## 5. Post-release checks

Bound each wait (`timeout 900`), and record FAIL with the last state on timeout.

- [ ] The release run succeeded: `gh run list --workflow release.yml --limit 1`.
- [ ] The tag and Release exist: `gh release view <version>` shows the notes you wrote.
- [ ] The image is published:
      `docker buildx imagetools inspect ghcr.io/amdl-web/apple-music-downloader-backend:<X.Y.Z>`
      lists `linux/amd64` and `linux/arm64`; the `X.Y` and `latest` tags point at
      the same digest.
- [ ] `main` and `dev` agree: `git log --oneline origin/dev..origin/main` shows only
      the merge commit.

Report the version, the release URL, the image digest, and anything skipped.
