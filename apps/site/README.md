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
| `build.mjs` | The template and the build: renders `content/<lang>.json` into a page, inlines `static/site.css` and `static/site.js`, and writes the Content-Security-Policy that allows exactly those |
| `mocks.mjs` | The hand-drawn product views (Search, Queue, Library), the hero's waveform and the small figures beside the specs, as HTML on made-up data |
| `blocks.mjs` | The template's pieces that do not depend on the content: escaping, icons, code blocks with copy buttons, tab lists |
| `content/` | The page text, one file per language with the same keys. Strings may use `` `code` ``, `**strong**` and `[text](href)`; an href may start with `{repo}` or `{blob}` |
| `static/` | The stylesheet (`site.css`), the script (`site.js`), the favicon and the Open Graph card's template (`og.html`) |
| `og.mjs` | Draws `og.png` from `static/og.html` and the English hero with headless Chrome |
| `serve.mjs` | The preview server behind `--serve` |
| `build.test.mjs` | Arguments, content parity, the product views, links and anchors, local-only resources, the Content-Security-Policy, the Open Graph card |

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

## Product views

The products section shows three views of the web console and the desktop app: Search
(the New download page), the Queue and an album's record in the Library. They are not
the apps: each is a few dozen lines of HTML in `mocks.mjs`, styled by `site.css` with the
console's colors, on a made-up catalog (the artists and albums of `internal/demo`, with
each cover a CSS color field). Their words come from the content files, so both
languages show them.

Each view has one thing that works, in `site.js`: a search result's Download button
queues it, the failed album's Retry puts it back in the queue, and the record's Download
missing fills its missing track. The running album moves through its stages on a timer
while its view is on screen, unless the visitor prefers reduced motion. Without
JavaScript the three views are shown one after another.

When the console or the desktop app changes a name or a layout the views borrow, change
the view to match; nothing checks it.

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
   `.github/workflows/pages.yml` runs on pushes to `main` instead of `dev`. From this
   commit on, pushes to `dev` no longer publish the site. Check: `node --test apps/site/`,
   and `rg -n "in development|开发中" apps/site/content` finds nothing.
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
- There are no screenshots to update: the product views are HTML in `mocks.mjs`, and
  `og.png` is drawn from `static/og.html` and the English hero at build time.
- The views' data is made up. A path a view shows follows the default
  `library_path_format` in `configs/config.example.yaml`, which a test checks.
- The page carries no `style` attributes or inline handlers, since the
  Content-Security-Policy allows only the hashed inline stylesheet and scripts: a width
  is a class (`w-60`), and `site.js` sets custom properties with `style.setProperty`.

## Theme and language

The page follows `prefers-color-scheme` until the visitor picks Light or Dark in the
header's three-way switch, which sets `data-theme` on `<html>` and keeps the choice in
`localStorage` (`amdl-site-theme`); System goes back to following the browser. Each language page records itself as the visitor's language
(`amdl-site-lang`) for the root page to use next time.
