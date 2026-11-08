# apps/site

The AMDL website: one static page in English and one in Simplified Chinese, each at its
own path (`en/`, `zh-CN/`), plus a root page that sends a visitor to the language they
picked last or, failing that, the one their browser prefers.

It is plain HTML and CSS built by one zero-dependency Node script, `build.mjs`: a
template, two content files and a handful of static assets are all it takes, so a static
site generator would add more than it saves. Fonts are the system's, and the pages load
nothing from another host.

## Layout

| Path | What it holds |
| --- | --- |
| `build.mjs` | The template and the build: renders `content/<lang>.json` into a page, inlines `static/site.css`, `static/mocks.css` and `static/site.js`, and writes the Content-Security-Policy that allows exactly those |
| `mocks.mjs` | The product mocks (the desktop app, the web console, the terminal UI, a CLI session, an agent conversation, the hooks card), the hero's waveform and the small figures beside the specs, as HTML on made-up data |
| `blocks.mjs` | The template's pieces that do not depend on the content: escaping, icons, code blocks with copy buttons, tab lists |
| `content/` | The page text, one file per language with the same keys. Strings may use `` `code` ``, `**strong**` and `[text](href)`; an href may start with `{repo}` or `{blob}` |
| `static/` | The page's stylesheet (`site.css`), the mocks' stylesheet with each app's own tokens (`mocks.css`), the script (`site.js`), the favicon and the Open Graph card's template (`og.html`) |
| `og.mjs` | Draws `og.png` from `static/og.html` and the English hero with headless Chrome |
| `serve.mjs` | The preview server behind `--serve` |
| `build.test.mjs` | Arguments, content parity, the MCP tool list against `internal/capability`, the product mocks and their interactions, links and anchors, local-only resources, the Content-Security-Policy, the Open Graph card |

## Build and preview

Node.js 26 (the version the rest of the repository uses):

```bash
node apps/site/build.mjs --serve 8080
```

builds into `apps/site/dist/` and serves it on `http://127.0.0.1:8080/`. Without
`--serve` it only builds. `--out <dir>` writes elsewhere, and `--base <url>` sets the
absolute address the site will be served from, which adds the canonical, `hreflang` and
Open Graph URLs:

```bash
node apps/site/build.mjs --base https://amdl.example.org/
```

Every link inside the site is relative, so the output works at a domain's root or under
a subpath such as a GitHub Pages project site. Links into the repository point at the
branch in `build.mjs#REF`.

```bash
node --test apps/site/
```

runs the tests; `make node-test` includes them.

`make site` builds the page as published: the same build with `--base
https://amdl.lyjw.me/`, then `og.mjs`, which draws `og.png` into the built site with
headless Chrome. `CHROME` names the browser binary when it is not in the usual places.
It writes `apps/site/dist/` (`SITE_OUT` elsewhere).

## Product mocks

Each product is shown with a mock of one of its real screens, labelled with the product
it comes from:

| Section | Mock | Recreates |
| --- | --- | --- |
| Desktop app | Queue, Library, an album (tabs) | `apps/desktop/internal/golden/testdata/{queue,library,album}-{light,dark}.png` |
| Web console | New download with results, Overview, Logs (tabs) | the console in simulate mode (`make dev`) |
| Terminal | the TUI at 100 columns, and at 46 on a phone | `apps/amdl/internal/tui/testdata/frame-100.ansi`, `frame-46.ansi` |
| Terminal | a CLI session | `amdl search`, `amdl download --watch` and `amdl jobs list` as `docs/cli.md` documents them |
| Agents | an MCP conversation, the tool list, the `mcpServers` entry | `internal/capability/capability.go`, `amdl cli setup` |
| Agents | Settings › Hooks with no hooks yet | the console's hooks card and `internal/hookpreset` |

They are not the apps: each is HTML in `mocks.mjs` on a made-up catalog (the artists and
albums of `internal/demo`, each cover drawn the way `internal/demo/catalog.go` draws it).
`mocks.css` styles them with each app's own tokens and type: the desktop app's from
`apps/desktop/internal/ui/theme.go` in its monospaced face, the console's from
`apps/console/static/css/skins/music.css` in the system face, the TUI's from
`apps/amdl/internal/tui/theme.go`. A frame is drawn at the app's size in design pixels
and scales with its container (`--u`); below 700px it reflows to a phone layout. The
apps are in English only, so the words inside a mock are English on both pages; each
mock's description for screen readers comes from the content files.

Three of them have one thing that works, in `site.js`: the desktop Queue's Pause
button, a search result's Download button in the console, and the hooks card's Run hooks
switch. Without JavaScript the tabs' screens are shown one after another.

When an app changes a name, a color or a layout a mock borrows, change the mock to
match. A test keeps the MCP tool list equal to `capability.go`; nothing checks the rest.
To compare, render the real screen (the golden PNGs, the console under `make dev`, an
ANSI frame) next to the mock at the same size.

## Publishing

GitHub Pages serves the site, built by `.github/workflows/pages.yml`. A push to the
branch in `build.mjs#REF` that touches the site, the Makefile or the workflow runs `make site` (see [Build and preview](#build-and-preview))
and deploys the result; `gh workflow run pages.yml --ref <branch>` runs it by hand. The
workflow refuses a branch other than the one `REF` names, so the page's install commands
and repository links point at the branch it was built from. The `github-pages`
environment lets `dev` and `main` deploy:

```bash
gh api repos/AMDL-Web/apple-music-downloader/environments/github-pages/deployment-branch-policies \
  --jq '.branch_policies[].name'
```

The domain is in the maintainer's Cloudflare zone: `amdl` is a CNAME to
`amdl-web.github.io` with the proxy on, and the repository's Pages settings name
`amdl.lyjw.me` as the custom domain. Cloudflare holds the certificate, redirects `http`
to `https` (the zone's Always Use HTTPS) and sends the files Brotli-compressed: GitHub Pages answers with
gzip, which Cloudflare would pass through, so a compression rule on `amdl.lyjw.me`
(Brotli first, then Auto) has it re-encode them. It reaches
GitHub over HTTPS without checking GitHub's certificate (the zone's SSL mode Full):
GitHub cannot issue a certificate for a proxied name, so Pages' own certificate and its
Enforce HTTPS setting stay off.

## Release day

The site follows `dev` until a release and `main` from the release on.

1. **Point the site at `main`.** One commit on `dev`, before the release PR:
   `build.mjs#REF` becomes `'main'`, the hero eyebrow in both content files drops "in
   development" ("2.0 · Self-hosted · Open source", "2.0 · 自托管 · 开源"), and
   `.github/workflows/pages.yml` runs on pushes to `main` instead of `dev`. The install
   section's 2.0 note (`install.prerelease` in both content files, and the callout that
   shows it in `build.mjs`) goes too, since the install scripts and `latest` then mean
   2.0. From this
   commit on, pushes to `dev` no longer publish the site. Check: `node --test apps/site/`,
   and `rg -n "in development|开发中|prerelease" apps/site/content apps/site/build.mjs`
   finds nothing.
2. **Merge the release PR.** Its push to `main` runs the Pages workflow. Check:
   `gh run list --workflow pages.yml --branch main --limit 1`, then
   `timeout 900 gh run watch <run-id> --exit-status`; afterwards
   `curl -sI https://amdl.lyjw.me/en/` answers `200`.
3. **Check the page's links.** Every link into the repository on the live page (the docs,
   the install scripts, the Compose file) answers `200` and points at `main`, and the
   install script finds the release:

   ```bash
   curl -s https://amdl.lyjw.me/en/ \
     | grep -oE 'https://(raw\.githubusercontent\.com|github\.com)/AMDL-Web/[^"<# ]*' | sort -u \
     | xargs -n1 curl -so /dev/null -w '%{http_code} %{url_effective}\n'
   curl -fsSL https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/main/deploy/install/install.sh | sh -s -- --dry-run
   ```

## Changing the content

- Edit both `content/en.json` and `content/zh-CN.json` in the same change. The Chinese
  is written for Chinese readers, not translated word for word.
- Every claim comes from the repository: the root README, `docs/`, or what the code
  does. A feature that is not released says so.
- There are no screenshots to update: the product mocks are HTML in `mocks.mjs`, and
  `og.png` is drawn from `static/og.html` and the English hero at build time.
- The mocks' data is made up. A path a mock shows follows the default
  `library_path_format` in `configs/config.example.yaml`, which a test checks.
- The page carries no `style` attributes or inline handlers, since the
  Content-Security-Policy allows only the hashed inline stylesheet and scripts: a width
  or a bar's length is a class (`f3`, `cv-glass`).

## Theme and language

The page follows `prefers-color-scheme` until the visitor picks Light or Dark in the
header's three-way switch, which sets `data-theme` on `<html>` and keeps the choice in
`localStorage` (`amdl-site-theme`); System goes back to following the browser. Each language page records itself as the visitor's language
(`amdl-site-lang`) for the root page to use next time.
