---
name: amdl-release
description: Cut an amdl release - confirm dev is green, write release notes at .github/release-notes/<version>.md with the author and co-author attribution, open the dev -> main PR titled with the vX.Y.Z version, wait for the maintainer's merge, then check the GitHub Release and the GHCR image tags. Use when asked to "release", "cut vX.Y.Z", "ship to main", "write release notes" or "publish an image".
---

# Cut a release

Merging the `dev` → `main` PR runs `.github/workflows/release.yml`: it tags the
merge commit, creates the GitHub Release, attaches the `amdl` binaries with the
Homebrew formula and Scoop manifest rendered for them, and calls
`.github/workflows/docker-publish.yml` to push the multi-arch image. The maintainer
merges; agents prepare and check.

## 1. dev is ready

- `git fetch origin && git log --oneline origin/main..origin/dev` lists what ships.
- CI is green on the tip of `dev`:
  `timeout 900 gh run watch $(gh run list --branch dev --workflow CI --limit 1 --json databaseId --jq '.[0].databaseId') --exit-status`.
- CI's "Release files" job (`make dist-check`) is green: the release's binaries job
  runs only after the tag and the Release exist.
- `gh api repos/AMDL-Web/apple-music-downloader/code-scanning/alerts?state=open --jq length`
  is `0`; fix an alert or dismiss it with its reason.
- The website describes the release: `apps/site/build.mjs#REF` is `main`, and the
  hero eyebrow in both `apps/site/content/` files no longer says "in development".

## 2. Version and notes

- `vX.Y.Z`, optionally with a suffix such as `-rc.1` (the pattern release.yml
  accepts). A suffixed version is a GitHub prerelease: it moves neither the latest
  release, which the install scripts read, nor the image's `latest` and `X.Y` tags,
  and skips the tap and the bucket, so it can rehearse a release. Minor for features,
  patch for fixes only; a breaking change needs the maintainer's decision on the
  version.
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
wait (root `AGENTS.md` "Merging with `--admin`"): an `--admin` merge is asked of the
maintainer for that release, never assumed from an earlier one.

## 5. After the merge

Bound each wait with `timeout 900`; on timeout record FAIL with the last state.

- `gh run list --workflow release.yml --limit 1` shows success.
- `gh release view <version>` shows the notes and eight assets: five archives,
  `SHA256SUMS`, `amdl.rb` and `amdl.json`. When the binaries job failed after the
  Release was created, attach them by hand from the tag:
  `make dist VERSION=<version> && node deploy/packaging/render.mjs <version> dist/SHA256SUMS dist && gh release upload <version> dist/* --clobber`.
- `curl -fsSL https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/main/deploy/install/install.sh | sh -s -- --dry-run`
  resolves `<version>` as the latest (not for a prerelease).
- `docker buildx imagetools inspect ghcr.io/amdl-web/apple-music-downloader:<X.Y.Z>`
  lists `linux/amd64` and `linux/arm64`, and the `X.Y` and `latest` tags have the
  same digest.
- `git log --oneline origin/dev..origin/main` shows only the merge commit.
- Republish the website from `main` ([apps/site/README.md](../../../apps/site/README.md#publishing)).

Report the version, release URL, image digest, and anything skipped.

## 2.0.0 only

The first 2.0 release changes the image name and the website's host and starts the
wrapper pin. These steps are the maintainer's unless marked; record each in the report.

- **Image visibility.** The first push to `ghcr.io/amdl-web/apple-music-downloader`
  creates a private package. As soon as the release's push finishes, the maintainer
  sets it public in the package settings; then an anonymous pull works:
  `DOCKER_CONFIG=$(mktemp -d) docker pull ghcr.io/amdl-web/apple-music-downloader:2.0.0`.
- **Wrapper pin** (agent). `gh api repos/AMDL-Web/apple-music-downloader/branches/wrapper-pins`
  answers, and the manifest at `internal/wrapperhost#ManifestURL` has the same
  `sha256` as `internal/wrapperhost#Builtin` and an `expires_at` weeks away. Otherwise
  run `gh workflow run wrapper-pin.yml`, and bump `Builtin` when upstream has a newer
  build.
- **Website.** It moves from Cloudflare Workers back to GitHub Pages, built from
  `main`; follow the cutover in [apps/site/README.md](../../../apps/site/README.md#publishing)
  instead of republishing to Workers.
- **NAS.** Redeploy the maintainer's own install on the new image name after the
  release (the `ssh-remote-ops` skill covers the host).
- **Desktop app.** Not part of 2.0.0: the release workflow builds no desktop app. It
  ships as a release of its own once a signing account is in place, and the v2.0.0
  notes say so in one sentence.
- **Homebrew and Scoop.** Not published for 2.0.0; they start with 2.0.1. For 2.0.0
  `PACKAGING_PUBLISH` stays unset, and the rendered `amdl.rb` and `amdl.json` are
  still attached to the Release. Before 2.0.1, set them up as
  [deploy/packaging/README.md](../../../deploy/packaging/README.md) describes.
