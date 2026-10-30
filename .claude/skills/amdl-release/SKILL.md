---
name: amdl-release
description: Cut an amdl release - confirm dev is green, write release notes at .github/release-notes/<version>.md with the author and co-author attribution, open the dev -> main PR titled with the vX.Y.Z version, wait for the maintainer's merge, then check the GitHub Release and the GHCR image tags. Use when asked to "release", "cut vX.Y.Z", "ship to main", "write release notes" or "publish an image".
---

# Cut a release

Merging the `dev` → `main` PR runs `.github/workflows/release.yml`: it tags the
merge commit, creates the GitHub Release, and calls
`.github/workflows/docker-publish.yml` to push the multi-arch image. The maintainer
merges; agents prepare and check.

## 1. dev is ready

- `git fetch origin && git log --oneline origin/main..origin/dev` lists what ships.
- CI is green on the tip of `dev`:
  `timeout 900 gh run watch $(gh run list --branch dev --workflow CI --limit 1 --json databaseId --jq '.[0].databaseId') --exit-status`.

## 2. Version and notes

- `vX.Y.Z`, optionally with a suffix such as `-rc.1` (the pattern release.yml
  accepts). Minor for features, patch for fixes only; a breaking change needs the
  maintainer's decision on the version.
- Notes go to `.github/release-notes/<version>.md`, in English or Chinese, shaped
  like the latest file there; an empty or missing file makes the workflow generate
  notes from commits. Breaking and migration-relevant changes (API, config keys,
  schema, output paths) come first.
- Attribution follows [CONTRIBUTING.md](../../../CONTRIBUTING.md#releases). Gather
  the names with
  `git log --format='%an <%ae>%n%(trailers:key=Co-authored-by,valueonly)' origin/main..origin/dev | sort -u`.
- Commit the notes to `dev`: `git commit -s -m "docs(release): add <version> notes"`.

## 3. Release PR

```bash
gh pr create --base main --head dev --title "Release <version>" --body-file <notes>
```

release.yml reads the first `vX.Y.Z` in the title and fails without one; the
fallback is `gh workflow run release.yml -f version=<version>`. Review-bot findings
are handled per `REVIEW.md` and `HUMAN.md`.

## 4. Merge

The maintainer merges. When branch protection blocks it, report what blocks it and
wait (root `AGENTS.md` "Merging with `--admin`").

## 5. After the merge

Bound each wait with `timeout 900`; on timeout record FAIL with the last state.

- `gh run list --workflow release.yml --limit 1` shows success.
- `gh release view <version>` shows the notes.
- `docker buildx imagetools inspect ghcr.io/amdl-web/apple-music-downloader-backend:<X.Y.Z>`
  lists `linux/amd64` and `linux/arm64`, and the `X.Y` and `latest` tags have the
  same digest.
- `git log --oneline origin/dev..origin/main` shows only the merge commit.

Report the version, release URL, image digest, and anything skipped.
