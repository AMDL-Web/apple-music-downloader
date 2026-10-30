# apps/site

The static website, built by `apps/site/build.mjs` from one template and a content file
per language. How to build, preview and add screenshots: [README.md](README.md).

## Rules

- No dependencies, no CDN, no web fonts, no analytics: the pages load nothing from
  another host. `build.test.mjs` fails on a remote script, stylesheet, image or CSS
  import.
- `content/en.json` and `content/zh-CN.json` keep the same keys and list lengths and
  change in the same commit. The Chinese is natural Simplified Chinese, not a literal
  translation.
- The site describes what is on the branch it is published from. A feature or product
  that is not released is marked as in development; a behavior change that touches a
  claim here updates the content in the same commit.
- Links into the repository go through `{repo}` and `{blob}`, which follow
  `build.mjs#REF`; links inside the site stay relative.
- Screenshots come from simulate mode, a test install or the reports, with no personal
  paths, LAN addresses or account names in view; the tests reject private addresses in
  the built pages.
- `static/hero.html` stays decorative (`aria-hidden`), animates only `transform` and
  `opacity`, and stops on `prefers-reduced-motion`.
- Theme follows `prefers-color-scheme` unless `data-theme` is set on `<html>`, and every
  color is a custom property on `:root` in `static/site.css`, defined for both themes.

## Verify

```bash
node --test apps/site/
node apps/site/build.mjs --serve 8080
```

Then check both languages at 375, 768 and 1440 px, in light and dark.
