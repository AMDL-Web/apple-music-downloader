# apps/site

The AMDL website: one static page in English and one in Simplified Chinese, each at its
own path (`en/`, `zh-CN/`), plus a root page that sends a visitor to the language they
picked last or, failing that, the one their browser prefers.

It is plain HTML, CSS and inline SVG built by one zero-dependency Node script,
`build.mjs`: a template, two content files and a handful of static assets are all it
takes, so a static site generator would add more than it saves. Fonts are the system's,
and the pages load nothing from another host. `DESIGN.md` is the page's design: its
structure, tokens, the product mocks, the covers, the copy brief and the tests.

## Layout

| Path | What it holds |
| --- | --- |
| `build.mjs` | The template and the build: renders `content/<lang>.json` into a page (header, hero, how it works, formats, the four apps, install, closing band, footer), inlines `static/site.css`, `static/mocks.css` and `static/site.js`, and writes the Content-Security-Policy that allows exactly those |
| `blocks.mjs` | The template's pieces that do not depend on the content: escaping, icons, code blocks with copy buttons, tab lists and their no-JavaScript style |
| `mocks.mjs` | The four product mocks (M1 the desktop app's album page, M2 the web console's Overview, M3 the terminal UI, M4 an agent over MCP), the hero shelf and its file chip, card C's folder tree, the MCP tool list and the library path helper, as HTML and SVG on the demo catalog |
| `covers.mjs` | The eight album covers of the demo catalog, each a full and a small SVG `<symbol>` drawn from one list of layers, and the hidden sprite that holds them |
| `content/` | The page text, one file per language with the same keys (`en.json`, `zh-CN.json`), and the copy brief they were written to (`schema.json`: tone, facts, and each key's purpose, limits and allowed marks). Strings may use `` `code` ``, `**strong**` and `[text](href)` where the key allows; an href may start with `{repo}` or `{blob}` |
| `static/` | The page's stylesheet (`site.css`), the mocks' stylesheet with each app's own tokens (`mocks.css`), the script (`site.js`), the favicon and the Open Graph card's template (`og.html`) |
| `og.mjs` | Draws `og.png` from `static/og.html`, the English hero and three covers with headless Chrome |
| `serve.mjs` | The preview server behind `--serve` |
| `build.test.mjs` | The tests (see [Tests](#tests)) |
| `DESIGN.md` | The design the page is built from |

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
https://amdl.dev/`, then `og.mjs`, which draws `og.png` into the built site with
headless Chrome. `CHROME` names the browser binary when it is not in the usual places.
It writes `apps/site/dist/` (`SITE_OUT` elsewhere).

## Tests

`build.test.mjs` checks:

- the arguments, and that bad ones print the usage and exit 2;
- that both content files have the same keys and list lengths and no empty strings, and
  the copy brief: `content/schema.json` names exactly their keys, every string is within
  its key's limit (links count as their text, `` ` `` and `**` not at all), uses only the
  marks and links its key allows, contains its key's verbatim strings, and has no `!`;
  the hero lead has one highlight and the hero title two lines; and that
  `content/schema.json` equals the schema block of `DESIGN.md` 8.2;
- `blocks.mjs#commands`, `build.mjs#inline` and `build.mjs#markLead`;
- that the MCP tool list equals the tools `internal/capability/capability.go` declares,
  and that every path the page shows (the file chip, card C's tree, the desktop mock's
  SAVED TO, the agent's reply) follows the default `library_path_format` in
  `configs/config.example.yaml`;
- that the covers module holds the eight albums of `internal/demo/catalog.go` with their
  ids, titles, artists, years and colors, each with a full, a mid and a small symbol
  painted in its album's two colors (the mid and small ones without sleeve type), that
  the console's live cards take their genre, year and track count from it, and that
  every track and playlist a mock shows is in the catalog;
- the built pages: each section's `h2` holding only its title, the nav's four sections,
  the apps stepper (four tabs, each panel one
  stage with one `role="img"` mock labelled from the content and two arrows outside it),
  that no mock contains a button, link or input, that a mock's frame has no `lang` and
  its English words a `lang="en"` wrapper, that the mocks show each album's catalog year
  and track count, the install tabs' label, the TUI's wide and narrow layouts, every MCP
  tool and the `mcpServers` entry, the shelf's five and three covers, the one highlight, the file chip, card C's tree, the two theme switches,
  languages and alternates, links and anchors (`<use href>` and `url(#…)` included),
  local-only resources, the Content-Security-Policy and private addresses;
- the Open Graph card's text and covers, and that Chrome draws it at 1200 × 630 (skipped
  without Chrome).

## Product mocks

The apps section is a stepper of four panels, each a mock of one real screen:

| Panel | Mock | Recreates |
| --- | --- | --- |
| Desktop app | M1, the album page while the album downloads, connected to `nas` | `apps/desktop/internal/golden/testdata/album-{light,dark}.png` (1024 × 640), tokens from `apps/desktop/internal/ui/theme.go` |
| Web console | M2, Overview with jobs running | the console's Overview (`apps/console`, `overview.js`) in its `music` skin, `apps/console/static/css/skins/music.css` |
| Terminal | M3, the TUI at 100 columns, and at 46 on a phone | `apps/amdl/internal/tui/testdata/frame-100.ansi`, `frame-46.ansi`, colors from `apps/amdl/internal/tui/theme.go` |
| Agents (MCP) | M4, a neutral chat in the site's tokens with three tool calls; beside it the `mcpServers` entry and the full tool list | `internal/capability/capability.go`, `docs/cli.md#mcp-server` |

They are not the apps: each is HTML and SVG in `mocks.mjs` on the demo catalog
(`internal/demo`). Each is a picture: `role="img"` with an `aria-label` from the content
files, its inside `aria-hidden`, and no button, link or form control. The apps are in
English only, so the words inside a mock are English on both pages: the inner
`aria-hidden` wrapper carries `lang="en"`, while the frame and its label stay in the
page's language (in M4 only the tool calls are English; the chat rows are page copy).
Each mock follows the page's theme with its app's own light and dark tokens
(`mocks.css`).

A mock is drawn in its app's pixels and scales as one picture: one design pixel is
`--u`, `100cqw` over the design width clamped to 0.4–1px (M3, narrower than the stage,
may grow to 1.2px), and `mocks.css` writes lengths
as `Nu`, which `build.mjs` turns into `calc(N * var(--u))`. At 700px and below each mock
switches to its own phone layout, at most 343px wide.

When an app changes a name, a color or a layout a mock borrows, change the mock to
match. Tests keep the MCP tool list equal to `capability.go`, the paths equal to the
default template and the demo data equal to `catalog.go`; nothing checks the rest. To
compare, render the real screen (the golden PNGs, the console, an ANSI frame) next to
the mock at the same size.

## Covers

`covers.mjs` draws the eight albums of the demo catalog as record sleeves: each keeps its
album's two colors from `internal/demo/catalog.go` and adds its own concept, composition,
texture and type, so that no two share a silhouette, even at 40px (`DESIGN.md` section
6). Each album is three symbols in one hidden sprite after `<body>`: full (`cv-<key>`,
160 design px and up), mid (`cv-<key>-m`, 96 to 159: the picture and its texture, no
sleeve type) and small (`cv-<key>-s`, under 96: the detail layers left out too); the full
and mid symbols share one picture group (`cv-<key>-art`). A cover on the page is
`<svg><use href="#cv-<key>"/></svg>`, sized by CSS; `cover(key, { size })` picks the
variant. Everything is SVG presentation
attributes (no `style`), noise comes from SVG filters, and the random parts are seeded by
the album's id, so every build draws the same covers. They appear on the hero shelf, the
formats card, the desktop and console mocks and the Open Graph card.

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

The domain is the maintainer's Cloudflare zone `amdl.dev`: the apex is a CNAME to
`amdl-web.github.io` with the proxy on (Cloudflare flattens it), and the repository's
Pages settings name `amdl.dev` as the custom domain. `www.amdl.dev` and the old
address `amdl.lyjw.me` answer with a `301` to the same path on `amdl.dev` (redirect
rules in their zones). Cloudflare holds the certificate, redirects `http`
to `https` (the zone's Always Use HTTPS) and sends the files Brotli-compressed: GitHub Pages answers with
gzip, which Cloudflare would pass through, so a compression rule on `amdl.dev`
(Brotli first, then Auto) has it re-encode them. It reaches
GitHub over HTTPS without checking GitHub's certificate (the zone's SSL mode Full):
GitHub cannot issue a certificate for a proxied name, so Pages' own certificate and its
Enforce HTTPS setting stay off.

## Release day

The site follows `dev` until a release and `main` from the release on.

1. **Point the site at `main`.** One commit on `dev`, before the release PR:
   `build.mjs#REF` becomes `'main'`, and `.github/workflows/pages.yml` runs on pushes to
   `main` instead of `dev`. Everything temporary goes: the hero's
   `<span class="prerelease">` (the 2.0 badge before the eyebrow) and its key
   `hero.prereleaseBadge`, and the install section's 2.0 callout
   (`.callout-prerelease` in `build.mjs` and the keys `install.prereleaseLabel` and
   `install.prerelease`), in both content files and in `content/schema.json`, with their
   rules in `static/site.css`; the rest of the eyebrow stays. Also drop the last sentence
   of `hero.eyebrow`'s purpose in `content/schema.json`, which names
   `hero.prereleaseBadge`. Every schema change goes into `DESIGN.md` 8.2 too (a test keeps
   the two equal). Every temporary key and class carries the word `prerelease`, so
   `rg -n "prerelease" apps/site` finds everything that must go, whatever the copy says.
   From this commit on, pushes to `dev` no longer publish the site. Check:
   `node --test apps/site/`, and `rg -n "prerelease" apps/site --glob '!*.md'` finds
   nothing.
2. **Merge the release PR.** Its push to `main` runs the Pages workflow. Check:
   `gh run list --workflow pages.yml --branch main --limit 1`, then
   `timeout 900 gh run watch <run-id> --exit-status`; afterwards
   `curl -sI https://amdl.dev/en/` answers `200`.
3. **Check the page's links.** Every link into the repository on the live page (the docs,
   the install scripts, the Compose file) answers `200` and points at `main`, and the
   install script finds the release:

   ```bash
   curl -s https://amdl.dev/en/ \
     | grep -oE 'https://(raw\.githubusercontent\.com|github\.com)/AMDL-Web/[^"<# ]*' | sort -u \
     | xargs -n1 curl -so /dev/null -w '%{http_code} %{url_effective}\n'
   curl -fsSL https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/main/deploy/install/install.sh | sh -s -- --dry-run
   ```

## Changing the content

- The page text is written to the copy brief in `content/schema.json` (tone, facts, and
  each key's purpose, limits, marks and verbatim strings); the tests hold both files to
  it. Change both `content/en.json` and `content/zh-CN.json` in the same change, and the
  brief with them when a key is added or its purpose changes. The Chinese is written for
  Chinese readers, not translated word for word.
- The template holds no prose: every visible sentence comes from the content files. What
  the template writes itself is verbatim data: the mocks' English UI labels, commands,
  paths and the demo catalog's names.
- Every claim comes from the repository: the root README, `docs/`, or what the code
  does (the facts in the brief name their sources). A feature that is not released says
  so.
- There are no screenshots to update: the product mocks and the covers are HTML and SVG,
  and `og.png` is drawn from `static/og.html` and the English hero at build time.
- The mocks' data is the demo catalog. A path a mock shows follows the default
  `library_path_format` in `configs/config.example.yaml`, which a test checks.
- The page carries no `style` attributes or inline handlers, since the
  Content-Security-Policy allows only the hashed inline stylesheet and scripts: a width
  or a position that varies is a class (`sh-3`, `lc-tidal`) or an SVG presentation attribute.

## Theme and language

The page follows `prefers-color-scheme` until the visitor picks Light or Dark in the
three-way switch (in the header above 700px, in the Sections menu below), which sets
`data-theme` on `<html>` and keeps the choice in `localStorage` (`amdl-site-theme`);
System goes back to following the browser. Each language page records itself as the
visitor's language (`amdl-site-lang`) for the root page to use next time.
