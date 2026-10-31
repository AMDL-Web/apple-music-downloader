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
| `build.mjs` | The template and the build: renders `content/<lang>.json` into a page, inlines `static/site.css` and `static/site.js`, copies the images |
| `content/` | The page text, one file per language with the same keys. Strings may use `` `code` ``, `**strong**` and `[text](href)`; an href may start with `{repo}` or `{blob}` |
| `static/` | Stylesheet, script, favicon, Open Graph card, the hero animation (`hero.html`), the terminal capture (`tui.html`), the CLI and MCP samples, and the screenshots in `img/` |
| `serve.mjs` | The preview server behind `--serve` |
| `build.test.mjs` | Content parity, links and anchors, local-only resources, image sizes, arguments |
| `wrangler.jsonc` | The Cloudflare Workers project that serves `dist/` at the site's domain |

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

## Publishing

The site is Cloudflare Workers static assets, deployed by hand with the maintainer's
Cloudflare login. Build with the domain from `wrangler.jsonc` as `--base`, then deploy:

```bash
node apps/site/build.mjs --base https://amdl.lyjw.me/
npx wrangler deploy --config apps/site/wrangler.jsonc
```

Links into the repository, the install scripts and the Compose file come from the
branch in `build.mjs#REF`, so it names the branch the published page describes.

At the 2.0 release the site moves to GitHub Pages, built from `main` by
`.github/workflows/pages.yml`: it runs on a push to `main` that touches the site, or by
hand, and refuses to deploy a build whose `REF` is not `main`.

## Release day

The move from Workers to Pages, in order. Run each check before the next step; until
step 8 the Workers copy can take the domain back (see the end of this section). Steps 4
and 5 are in the Cloudflare dashboard; the rest are commands, with
`R=AMDL-Web/apple-music-downloader`.

1. **Describe the release.** One commit on `dev`: `build.mjs#REF` becomes `'main'`, and
   the hero eyebrow in both content files drops "in development" ("2.0 · Self-hosted ·
   Open source", "2.0 · 自托管 · 开源"). Check: `node --test apps/site/`, and
   `rg -n "in development|开发中" apps/site/content` shows only the desktop badge.
2. **Enable Pages** with Actions as its source, and let `main` deploy to the
   `github-pages` environment (the repository's default branch is `dev`):

   ```bash
   gh api -X POST repos/$R/pages -f build_type=workflow
   gh api -X PUT repos/$R/environments/github-pages --input - <<'EOF'
   {"deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}}
   EOF
   gh api -X POST repos/$R/environments/github-pages/deployment-branch-policies \
     -f name=main -f type=branch
   ```

   Check: `gh api repos/$R/pages --jq .build_type` prints `workflow`, and
   `gh api repos/$R/environments/github-pages/deployment-branch-policies --jq '.branch_policies[].name'`
   prints `main`.
3. **Merge `dev` into `main`** with the release PR. The push runs the Pages workflow.
   Check: `gh run list --workflow pages.yml --branch main --limit 1`, then
   `timeout 900 gh run watch <run-id> --exit-status`; afterwards
   `curl -sI https://amdl-web.github.io/apple-music-downloader/en/` answers `200`. If
   the run started before step 2, run it again with
   `gh workflow run pages.yml --ref main`.
4. **Take the domain off Workers.** In the Cloudflare dashboard: Workers & Pages →
   `amdl-site` → Settings → Domains & Routes → `amdl.lyjw.me` → Remove. That also deletes
   the DNS record Workers made. Check:
   `curl -s -H 'accept: application/dns-json' 'https://cloudflare-dns.com/dns-query?name=amdl.lyjw.me&type=A'`
   has no `Answer`. The site is down from here until step 6 ends.
5. **Point DNS at GitHub.** In the `lyjw.me` zone add `CNAME amdl → amdl-web.github.io`
   with the proxy off (DNS only), so GitHub can issue the certificate. Check: the same
   DNS query with `type=CNAME` answers `amdl-web.github.io.`
6. **Give Pages the domain** and turn on HTTPS once its certificate is issued:

   ```bash
   gh api -X PUT repos/$R/pages -f cname=amdl.lyjw.me
   gh api repos/$R/pages --jq '.cname, .https_certificate.state'
   gh api -X PUT repos/$R/pages -F https_enforced=true
   ```

   Run the second line until the state is `approved`, then the third. Check:
   `curl -sI https://amdl.lyjw.me/en/` answers `200` with `server: GitHub.com`, and
   `curl -sI http://amdl.lyjw.me/` redirects to `https://`.
7. **Check the page's links.** Every link into the repository on the live page (the docs,
   the install scripts, the Compose file) answers `200` and points at `main`, and the
   install script finds the release:

   ```bash
   curl -s https://amdl.lyjw.me/en/ \
     | grep -oE 'https://(raw\.githubusercontent\.com|github\.com)/AMDL-Web/[^"<# ]*' | sort -u \
     | xargs -n1 curl -so /dev/null -w '%{http_code} %{url_effective}\n'
   curl -fsSL https://raw.githubusercontent.com/$R/main/deploy/install/install.sh | sh -s -- --dry-run
   ```

8. **Retire Workers.** `npx wrangler delete --name amdl-site`. In the next commit on
   `dev`, delete `apps/site/wrangler.jsonc` and the `/.wrangler/` line in `.gitignore`,
   and make the Publishing section above describe Pages only. Check: `amdl-site` is gone
   from the dashboard's Workers & Pages list.

To go back before step 8: clear the Pages domain with
`echo '{"cname": null}' | gh api -X PUT repos/$R/pages --input -`, delete the `amdl`
CNAME, then run `npx wrangler deploy --config apps/site/wrangler.jsonc`, which makes the
Workers custom domain and its DNS record again.

## Changing the content

- Edit both `content/en.json` and `content/zh-CN.json` in the same change. The Chinese
  is written for Chinese readers, not translated word for word.
- Every claim comes from the repository: the root README, `docs/`, or what the code
  does. A feature that is not released says so.
- Screenshots are WebP in `static/img`, named `<shot>-<light|dark>-<width>.webp`, two
  widths per theme as `build.mjs#SHOTS` lists. Take them from a simulate-mode server
  (`make dev`) or a test install, with no personal paths, addresses or account names in
  view.
- `static/hero.html` is the hero animation: one inline SVG with its own `<style>`
  (`ah-*` classes, `--ah-*` custom properties for both themes) and an optional script
  that pauses it off screen and adds pointer parallax. Its ids are `ah-*`, so a page holds
  one. `--ah-hole` is the page background, the color the record's centre hole shows.
- The terminal capture is `tmux capture-pane -e` output converted to HTML spans; a wide
  (CJK) character sits in a `<span class="w">` so the columns line up.

## Theme and language

The page follows `prefers-color-scheme` until the visitor picks a theme with the toggle,
which sets `data-theme` on `<html>` and keeps the choice in `localStorage`
(`amdl-site-theme`). Each language page records itself as the visitor's language
(`amdl-site-lang`) for the root page to use next time.
