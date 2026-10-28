# Console design

The shared contract for the embedded console: structure, states, accessibility,
motion and the skin mechanism. How the console *looks* belongs to a skin; each
skin's spec is in `internal/console/skins/` ([music](skins/music.md),
[editorial](skins/editorial.md)). When this page and the code disagree, the code
wins and this page is fixed in the same change. Sources are listed in
[docs/references.md](../../docs/references.md#console-design-references).

## Principles

1. **Show only what the API knows.** Track counts, never an aggregate pipeline
   percentage (`ItemProgress` has none on purpose). Simulate mode is announced
   in a banner.
2. **Status is never color alone.** Every status has an icon (or glyph), a label
   and a color role; a skin may change the look of a role, never drop the label.
3. **Artwork colors are data.** Apple's per-album palette reaches the page as
   custom properties; text colors are contrast-checked before they are used.
4. **Both themes, every skin.** Each skin ships a light and a dark theme; light
   is designed, not inverted.
5. **English UI copy**, `lang="en"`. Apple-provided text (titles, artists,
   genres) is shown as received with `dir="auto"`.

## Information architecture

| Route | Page | Content |
| --- | --- | --- |
| `#/` (also `#/overview`) | Overview | Sections (in DOM order) a server-health strip (`.health-strip`, one line when healthy, a notice naming each unhealthy component otherwise, linking to System), `live` (8 active jobs, then up to 2 jobs that failed in the last 24 h), `recent`, `kpis`, `health`, `throughput` (from `/stats`), `logs` (12-line tail) |
| `#/queue?tab=active\|completed\|failed\|cancelled\|all&type=&q=` | Queue | Tabs with counts from the feed's `status_counts`, search over title, artist and URL, type filter, dense job list, Load more, selection mode (Retry, Cancel, Delete on many jobs), Retry all on the Failed tab |
| `#/queue/:id`, `#/showcase/:id`, `#/new/:id` | Job drawer | Over its page: header, palette, track strip, track list with stage meters, hooks, Activity (persisted history plus live events), Saved to (completed jobs), Cancel/Retry/Delete |
| `#/showcase?type=album\|playlist\|song` | Showcase | "Latest" hero in Apple's colors, cover grid, Load more |
| `#/logs?level=&component=&job_id=&request_id=&q=` | Logs | Level chips, component, search, filter chips, live tail, expandable rows |
| `#/system` | System | About, runtime, health, storage, wrapper, library sync, hooks with outcomes, a configuration summary that links to Settings, API links |
| `#/new?url=` | New download | Sections `compose` (text box, media-user-token when needed, Download N), `links` (a row per link found), `results` (after a submit), and beside them at 1024 px and up `options` and `bookmarklet`. `url` carries one or more links, newline- or space-separated and URL-encoded (`lib/route.js` `newDownloadHash`, `newDownloadUrls`), and follows the text box; `lib/links.js` parses them |
| `#/settings?section=` | Settings | A section list (a select under 640 px) and one open section: `downloads` (the default), `files`, `artwork`, `lyrics`, `catalog`, `library_sync`, `logging`, `simulate`, `account`, `config_file` (`lib/settings.js` `SETTINGS_SECTIONS`; `section` is lowercase words joined by `_`, `settingsSection`). The palette finds every section and field (`lib/palette.js`) |

New download is the primary action and has these entry points: the
`.sidebar-new` button first in the sidebar (icon-only with a tooltip on the
640-1023 rail), the Overview page head's `.page-actions` button, "Start a
download" in the Overview live-queue, Queue and Showcase empty states, the
tab bar's center tab under 640 px, `n` (and `g n`), the palette's "New
download", and a global paste: pasting text that holds at least one
music.apple.com link while focus is outside a text field and no overlay or
dialog is open opens `#/new?url=<the links>` (on `#/new` the links join those
already in `url`). Under 640 px the tab bar holds Overview, Queue, New,
Showcase and More; More opens a bottom sheet (`MoreSheet`) listing Logs,
System and Settings with the current page marked, and the More tab is
`aria-current` while one of them is shown.

Filters live in the hash query, so a filtered view is a link. KPI tiles open the
matching Queue tab; the drawer's "View logs" opens `#/logs?job_id=…`; a log
row's job chip filters to that job. The tab title reads `"{Page} · AMDL"`, and
`"{n} running · {Page} · AMDL"` while jobs run.

## Layout

The DOM frame is fixed; a skin restyles it (the editorial skin turns the
sidebar into a horizontal masthead at 640 px and up).

```
.shell
  .sidebar (nav: .brand > .brand-mark + .brand-text > .brand-name, .brand-sub; a.sidebar-new; .nav > .nav-item; .sidebar-foot)
  .main-col
    header.topbar (.topbar-right: .conn, .search-trigger, .skin, .theme)
    main.main[data-page] (.page-head > h1.page-title + .page-sub + .page-actions; .banners; .page.page-<name>)
  nav.tabbar (> .tab x2, a.tab.tab-new, .tab, button.tab.tab-more, under 640 px)
```

| Width | Base frame | Notes |
| --- | --- | --- |
| ≥1024 | `--sidebar-w` sidebar, `--topbar-h` top bar | Overview is a 12-column grid: `live` spans 8, `health` 4, the rest 12 |
| 640-1023 | `--rail-w` icon rail (labels kept for screen readers) | two-column cards stack |
| <640 | top bar, `--tabbar-h` bottom tab bar + safe area, 16 px gutters | single column, 44 px targets, theme and skin become cycle buttons |

Siblings are spaced by the parent's `gap` (`--page-gap` between sections,
`--grid-gap` inside grids, `--card-gap` inside cards), never by child margins.
No horizontal page scroll at 390 px in any skin or theme.

## Skins

A skin is the console's look: colors, radii, shadows, font stacks, display
type and a few component rules. The theme (system, light, dark) is a separate
axis; every skin supports all three.

### Mechanism

- `<html data-skin="music|editorial">`, stored per browser under
  `localStorage["amdl.console.skin"]` with the same guarded read and write as
  the theme (`lib/prefs.js`: `SKINS`, `DEFAULT_SKIN`, `normalizeSkin`,
  `readPref`, `writePref`). The default is `music`, and `index.html` carries it
  statically so the first frame has every token before `main.js` runs;
  `main.js` then applies the stored skin and theme before mounting.
- The switch is a segmented control labelled "Style" in the top bar beside the
  theme control (a cycle button under 640 px), and the command palette has
  "Style: Apple Music" and "Style: Editorial".
- The theme is `<html data-theme="light|dark">` or no attribute for "system";
  `css/tokens.css` sets `color-scheme` from it, and skins write every color as
  `light-dark(<light>, <dark>)`, which resolves against that. Skins never set
  `color-scheme`.

### Files and what each may do

| File | Holds | May | May not |
| --- | --- | --- | --- |
| `static/css/tokens.css` | Skin-neutral scales: type scale `--fs-*`/`--lh-*`, weights, spacing `--s-*`, sizes, motion, `--on-art-light/dark`, system font fallbacks | change a scale every skin shares | hold a skin's color, radius, shadow or font stack |
| `static/css/*.css` (the base: `base.css`, `shell.css`, `controls.css`, `layout.css`, one file per page area, `overlays.css`, `responsive.css`, then the newer pages' `download.css` and `settings.css`) | Structure: layout, component structure and behavior, all visual values through tokens, one file per area. `index.html` links them after `tokens.css` and before the skins, and that link order is the cascade order: `responsive.css` follows the older areas, and `download.css` and `settings.css` follow it, so each carries its own media queries | add structure both skins need, in the file of its area | contain a color literal, `!important` (outside reduced motion) or a skin-specific choice |
| `static/css/skins/<name>.css` | One `:root[data-skin="<name>"]` token block (light and dark via `light-dark()`), responsive token overrides, and that skin's component rules, every selector prefixed `:root[data-skin="<name>"]` | set any token, override any base rule (repeating its selector chain when it is deeper than three classes), show opt-in hooks, reorder overview sections, use CSS counters and `::before` text | touch another skin's scope, add JS, use `font-size` other than `var(--fs-*)`, use `transition: all`, rely on `color-scheme` it sets itself |
| `skins/<name>.md` | The skin's spec: intent, token table with contrast, component notes | | restate the shared contract |

The prefix `:root[data-skin="<name>"]` adds (0,2,0) (`:root` is a
pseudo-class), so a skin selector with one class wins over any base selector
of up to three classes, attributes and pseudo-classes (a tie goes to the
skin, linked later), including those inside base media queries; a skin
therefore carries its own responsive overrides. For a deeper base selector
(the chart hover chain, the Submitted row chain
`.job-list.job-list-compact.new-results > .new-result > .job-row`), repeat
its chain under the skin prefix.

### Tokens a skin must declare

`TestSkinsDeclareEveryBaseToken` fails when a skin misses one that a base
stylesheet reads and `tokens.css` does not declare. Declare the whole table anyway (for
example every role's `-text`, though the base reads only `--danger-text` and
`--warning-text`), so skin rules can rely on it.

| Group | Tokens |
| --- | --- |
| Surfaces | `--bg-canvas`, `--bg-surface`, `--bg-raised`, `--bg-hover`, `--bg-selected`, `--bg-chrome` (sidebar, top bar, tab bar) |
| Lines | `--border-subtle` (decorative), `--border-control` (3:1 outlines), `--chrome-border`, `--card-divider`, `--row-divider` (border shorthands) |
| Text | `--text-primary`, `--text-secondary` (4.5:1 on `--bg-surface`), `--text-disabled` (placeholders only) |
| Accent | `--ink`/`--on-ink` (skip link), `--accent`/`--on-accent` (primary button), `--accent-text` (links), `--focus` (2 px ring), `--brand`/`--on-brand` (logo mark) |
| Status roles | `--info`, `--success`, `--danger`, `--warning`, `--neutral`, `--skipped`, each with `-tint` and `-text` |
| Data | `--track` (empty bars and cells), `--chart-bar`, `--chart-grid`, `--scrim` |
| Type | `--font-sans`, `--font-display`, `--font-mono`, `--fs-body`/`--lh-body`, `--tracking-body`, `--fs-page-title`/`--lh-page-title`, `--fs-card-title`/`--lh-card-title`, `--fs-kpi`/`--lh-kpi`, `--fs-hero-title`/`--lh-hero-title`, `--w-display`, `--w-title`, `--tracking-display`, `--tracking-title` |
| Shape | `--r-badge`, `--r-bar`, `--r-control`, `--r-control-inner`, `--r-card`, `--r-art`, `--r-overlay`, `--r-drawer`, `--r-notice`, `--bar-h`, `--cell-h`, `--cell-gap` |
| Cards | `--card-bg`, `--card-border` (shorthand), `--card-shadow`, `--card-pad`, `--card-pad-x` (flush rows), `--card-gap` |
| Layout | `--page-gap`, `--grid-gap` |
| Elevation | `--shadow-art` (covers), `--shadow-float` (drawer, palette, toasts) |

### DOM hooks

Stable class names and attributes a skin can style without touching JS:

| Surface | Hooks |
| --- | --- |
| Section / card | `section.card[data-section]` > `header.card-head` (`h2.card-title`, `.card-note`, `.card-actions`) + `.card-body`; `.card-flush` for edge-to-edge lists. Overview sections: `live`, `recent`, `kpis`, `health`, `throughput`, `logs`; Queue `jobs`; Logs `log` |
| Page | `main.main[data-page]`, `.page-head`, `.page-title`, `.page-sub`, `.page-actions` (Overview: `a.btn.btn-primary.page-new`), `.banners`, `.page.page-<name>` (`page-new`, `page-settings` included) |
| KPI | `a.kpi[data-key=queued\|running\|completed\|failed\|total][data-role][data-zero]` > `.kpi-label`, `.kpi-icon`, `.kpi-value`, `.kpi-meta` |
| Job row | `a.job-row[data-status][.has-palette]` > `.art`, `.job-main` (`.job-facts` > `.job-fact[data-key=type\|genre\|year\|started]`, `.job-title[.is-unresolved]`, `.job-sub`, `.job-error` (failed jobs: one trimmed line, full text in the title attribute)), `.job-type`, `.job-progress`, `.job-status`, `.job-updated`, `.job-chevron` |
| Progress | `.jprog[data-mode=cells\|bar][data-working]` (a running job with nothing finished yet shows a striped remaining track): up to 60 tracks `.pcells` (`--n`) > `.pcell[data-state=completed\|skipped\|failed\|cancelled\|remaining][data-role]` in that order (the list payload has counts, not per-track order); above 60 `.pbar` > `.pbar-seg[data-role]`; both followed by `.jprog-text` ("12 / 14") and `.jprog-legend` > `.jprog-key[data-state][data-role][data-zero]` > `.jprog-key-n` + `.jprog-key-label` (saved, on disk, failed, cancelled, remaining; every state, zeros included). The list payload cannot tell a track in flight from a waiting one, so "remaining" covers both and there is no byte percentage |
| Health | `.health-row[data-role]` > `.health-icon`, `.health-name`, `.health-status` (badge), `.health-detail`; runtime facts `dl.facts.health-facts` |
| Facts | `dl.facts` > `.fact[data-key][data-role]` > `dt.fact-label` + `dd.fact-value` (health facts, throughput totals `.tp-totals` with `tracks`, `tracks-failed`, `bytes`, `jobs`, `jobs-failed`, the failed rows only when non-zero and `data-role=danger`, hero facts `.hero-facts`) |
| Recent strip | `a.strip-tile[.has-palette][.is-overflow]` > `.art`, `.strip-title`, `.strip-sub`, `.strip-meta`; every completed job renders, those past the whole-tile count carry `.is-overflow` |
| Showcase | `a.hero.has-palette` > `.hero-art`, `.hero-text` (`.hero-kicker` > `.hero-no`, `.hero-title`, `.hero-artist`, `.hero-facts`), then its sibling `div.hero-actions` (same palette properties) > `.btn` "Tracks" (the drawer) and "Open in Apple Music"; `a.tile[.has-palette]` > `.tile-head` (`.tile-index`, `.tile-year`), `.tile-art`, `.tile-title`, `.tile-sub`, `.tile-facts` > `.tile-fact[data-key=genre\|year\|tracks\|finished]` |
| Status strip, Activity, failure | Overview `p.health-strip[data-state=ok]` or `.health-strip.notice[data-state=problem][data-role=danger\|warning]` > `.health-strip-link`; drawer `details.drawer-section.activity[open]` > `ul.activity-list` > `li.activity-row[data-role]` (`.activity-time`, `.activity-label`, `.activity-msg`), failure notice with `details.notice-more` > `pre.notice-raw`, `.drawer-files` > `code.drawer-files-dir`; Logs `.log-err`, `.attr-chip-job` > `.attr-chip-name`, `.filter-chip-name`, rows `[data-seq]` with the one tab stop |
| Live failures | `ul.attention` after the live list > `li.attention-row[data-role=danger]` > `.attention-icon`, `.attention-title` (`.attention-label` "Failed", `a.attention-link` to the drawer, mono `.attention-input`), `.attention-detail` (`.attention-error`, relative time), Retry `.btn` |
| Throughput | `.tp-totals` (Facts), `.tp-formats` > `.tp-formats-title`, `.formats-bar` > `.formats-seg[data-key]` (`--w` share, `--i` index), `ul.formats-list` > `li.formats-item[data-key]` (`--i`) > `.formats-swatch`, `.formats-label`, `.formats-count`; `.chart-xaxis` > one `span[data-day]` per day, `.is-first`, `.is-mid`, `.is-latest` on the three the base shows |
| Logs | `.log-entry[data-level]` on tail rows (`.log-tail-row`) and log page rows (`.log-row`) > `.log-time`, `.log-level[data-role]`, `.log-comp`, `.log-msg` (`.log-attrs` in the tail) |
| Shell | `.brand-sub`, `a.sidebar-new.btn.btn-primary[aria-current]` > `.icon` + `.sidebar-new-label` (visually hidden on the rail), `.nav-item[aria-current]`, `.nav-badge`, `.tab[data-page][aria-current]`, `a.tab.tab-new` > `.tab-new-mark` (accent pill around the plus; it rises over the icon line with a negative `margin-top`, so its label shares the other labels' line, and a skin that resizes the mark or the tab icons restates it) + `.tab-label` "New", `button.tab.tab-more[aria-expanded][aria-current]`, `.skin`/`.seg-skin`/`.skin-cycle`, `.theme`/`.seg-icons`/`.theme-cycle`; loading shell `.boot` > `.boot-mark` + `.boot-text` (static in `index.html`, replaced on mount); `svg.sprite` at the start of `body`, the in-document icon sprite (`ui/icons.js`), never styled by a skin; `meta[name=theme-color]` set from the body background; `.conn[data-role][data-state=connecting\|live\|reconnecting\|offline\|paused]` > `.conn-dot` + `.conn-label` (the base hides the label under 640 px) |
| Dialogs | `.overlay-host[data-placement=center\|bottom]` > `.scrim` + the panel `[role=dialog\|alertdialog][aria-modal][data-dialog=<name>]` (`ui/dialogs.js` `Dialog`). More sheet: `.sheet.sheet-more[data-dialog=more]` > `.sheet-head`, `ul.more-list` > `a.more-item[data-page][aria-current]` > `.icon`, `.more-label`, `.more-current` ("Current"), chevron. Confirm: `.sheet.dialog-confirm[role=alertdialog][data-dialog=confirm]` > `h2.dialog-title`, `.dialog-body`, `.dialog-actions` > `.btn.dialog-cancel` + `.btn.dialog-confirm-btn[aria-busy]` (`btn-danger` when destructive); a busy button is `.btn[aria-disabled=true]`. Shortcut sheet `[data-dialog=shortcuts]` |
| Status | `[data-role=info\|success\|danger\|warning\|neutral\|skipped]` sets `--role` and `--role-tint` for everything inside it; `.badge`, `.status-text`, `.notice` |
| Settings | `.page-settings[data-section-active][data-dirty]` > `nav.settings-nav` (`ul.settings-nav-list` > `a.settings-nav-item[data-section][aria-current=true][data-dirty][data-invalid]` > `.icon`, `.settings-nav-label`, `.settings-nav-mark` (`.settings-nav-dot` when unsaved); `label.settings-jump` > `.settings-jump-label` + `select`, shown under 640 px), `.settings-main` (a size container) > `.settings-error` + `section.card.settings-section[data-section=<id>]` > `.settings-intro`, `.field-list` > `.field[data-key][data-type][data-wide][data-changed][data-pinned][data-invalid]` > `.field-text` (`.field-label`, `.field-changed`, `button.field-revert`, `.field-help`, `code.field-key`), `.field-control` (`input.field-switch[role=switch]` + `.field-switch-state`, `.field-select` > `.select` + `.field-option-hint`, `.field-number` > `.field-input` + `.field-unit`, `.field-input`, `.field-secret`, `.list-editor` > `ol.list-items` > `li.list-item[data-item]` (`.list-rank`, `.list-label`, `.list-code`, `.list-actions` > `.list-up`, `.list-down`, `.list-remove`) + `.list-add`, `.check-group` > `label.check`), then `.field-default` (`code.field-default-value`), `.field-fixed` (a pinned folder or template shown whole), `.field-pin`, `.field-error`, `.field-warning`; `.list-min-note`; `.settings-updated` and `.settings-elsewhere` (`.settings-elsewhere-actions`) above the section; panels `.settings-panel[data-panel=library-sync\|config-file]` (`.settings-panel-head`, `.settings-panel-actions`, `.sync-reset`, `.sync-state`, `.config-reload`, `.config-outcome`, `.pinned-table`), `details.template-ref`, `.account` > `.account-block` (`.account-block-head`, `ul.account-rows` > `.account-row` > `.account-name` + `.account-logout`), `form.login-form[data-step=credentials\|code]` > `.form-row`, `.login-blocked`, `.form-actions`; `.save-bar[data-state=dirty\|saving]` > `.save-bar-text` (`.save-bar-count`, `.save-bar-where`) + `.save-bar-actions` (`.save-bar-discard`, `.save-bar-save`) |
| Job selection (Queue) | Toolbar `.queue-tools` > `.btn.queue-retry-all` (Failed tab) + `.btn.queue-select[aria-pressed]`; `.bulk-bar[data-state=select\|run]` (sticky at `top: var(--topbar-h)`; a skin whose top bar does not stick overrides `top`) > `label.bulk-all` > `input.bulk-check` (indeterminate when some), `.bulk-count`, `.bulk-actions` > `.btn.bulk-action[data-action=retry\|cancel\|delete]` > `.bulk-n` (count) or `.bulk-hint`, or during a run `.bulk-progress` > `.bulk-progress-text` + `.bulk-meter[role=progressbar]` > `.bulk-meter-fill` (`--f`); `ul.job-list.is-selecting` > `li.job-item[data-job-id][data-selected]` > `label.job-check` > `input.job-check-input` + `a.job-row[data-selected]`. Drawer: `.btn.drawer-delete` (finished jobs) |
| New download | `.page.page-new` > `.new-main` (cards `compose`, `links`, `results`) + `.new-aside` (`options`, `bookmarklet`), two columns at 1024 px and up (the aside 260-300 px wide below 1280, 300-380 from 1280). Compose: `form.new-form` > `textarea#new-input.new-input`, `.new-hint`, `.new-token` > `input.new-token-input` (while a station or private playlist can be sent), `.new-submit` (sticky above the tab bar under 640 px) > `.new-submit-info` (`.new-count`, `button.new-options-jump`) + `.btn.new-submit-btn[aria-busy]`. Links: `ul.new-links` > `li.new-link[data-kind][data-state=queued\|loading\|ready\|error\|idle\|invalid][data-issue=not_apple\|no_storefront\|unsupported\|music_video\|bad_storefront\|refused\|not_found\|needs_token\|preview_failed][data-blocking][.has-palette]` > `.art.art-48.new-link-art`, `.new-link-main` (`.new-link-tags` > `.chip`s and `.new-link-loading`, `.new-link-title[.is-url]`, `.new-link-sub` > `.new-link-artist`, `.new-link-album`, `.new-link-fact[data-key=tracks\|year\|length\|genre]`, mono `.new-link-url`, `.new-link-note[data-note=artist\|storefront]`, `a.new-link-prior[data-state=active\|done]`, `.new-link-issue.status-text[data-role]`), `.new-link-actions` > `.new-link-quality` (albums and songs), `.new-link-retry`, `.new-link-remove`; `.new-links-all` > `.new-links-toggle[aria-expanded]`, `.new-links-more`, `.new-links-legend` > `.new-legend-item` > `.chip`. Results: `#new-results-summary.new-results-summary[role=status][tabindex=-1]`, `ul.job-list.job-list-compact.new-results` > `li.new-result[data-status=accepted\|invalid\|duplicate_active\|duplicate_in_request\|queue_full\|error\|deleted]` > a Job row, or `.new-result-row[data-role]` > `.new-result-icon`, `.new-result-main` (`.new-result-label`, `.new-result-title` (a deleted job), `.new-result-url`, `.new-result-msg`, `.new-result-detail`), `.new-result-actions`; head `.new-results-retry`, `.new-results-clear`. Options: `section.card.new-options` > `.new-options-toggle[aria-expanded]`, `.new-options-reset`, `#new-options-body.new-options-body[data-open]` > `.new-options-summary[data-changed]` (`.new-options-count`, `.new-options-text`) or `.new-options-form` > `fieldset.new-fieldset[data-group=quality\|lyrics\|covers\|behavior\|hooks][data-changed]` > `label.new-check[data-key][data-changed]` (`.new-check-label`, `.new-check-hint`), `.new-field[data-key][data-changed]` (`.new-field-label` + `.select`), `.new-field-row`, `.new-check-group`, `.new-codecs` > `ol.new-codec-list` > `li.new-codec[data-codec]` (`.new-codec-rank`, `.new-codec-name` > `.new-codec-hint`, `.new-codec-actions` > `.new-codec-up`, `.new-codec-remove`) + `.new-codec-add` > `.new-codec-add-btn[data-codec]`, then `.new-options-foot`; the base puts an accent dot after the label of a value that differs from the server default. Bookmarklet: `.new-bookmarklet` > `code#new-bookmarklet-code.new-bookmarklet-code` + `.new-bookmarklet-copy`, `ol.new-steps`. Quality sheet `.sheet.sheet-quality[data-dialog=quality]` > `.quality-for`, `.quality-wait`, `.quality-body[role=region][tabindex=0]` (the part that scrolls, so the head and its Close stay) > `ul.quality-slots` > `li.quality-slot[data-id]` (`.quality-slot-label` > `.quality-slot-codec`, `.quality-slot-count`, `.quality-slot-best`), `ol.quality-tracks` > `li.quality-track` > `.quality-track-head` (`.quality-track-no`, `.quality-track-name`, `.chip`) + `.quality-track-opts` > `.quality-opt[data-id]` (`.quality-opt-label`, `.quality-opt-spec`) |

**Artwork palette.** `.job-row`, `.strip-tile`, `.tile` and `.hero` carry
`--art-bg` and `--art-t1`, `--art-t2`, `--art-t3` (from `lib/format.js`
`artPalette`/`paletteStyle`) plus `.has-palette` when Apple sent one; the
drawer's `.drawer-head` carries the properties without the class. Each `--art-t*` already reaches 4.5:1 on `--art-bg` or is replaced by
`--on-art-light`/`--on-art-dark`. A playing motion cover switches to the
`motion_artwork_*` palette; the two are never mixed. `.art` also sets `--art-bg`
behind a cover while it loads or when it fails.

**Opt-in hooks** are hidden by the base and shown by a skin that wants them:
`.brand-sub`, `.job-facts`, `.health-icon`, `.hero-no`, `.hero-actions`,
`.tile-head`, `.strip-tile.is-overflow`, `.jprog-legend`,
`.attention`, and the x-axis day labels other than the first, middle and
latest.

### Adding a skin

1. Add `{ value, label, short }` to `SKINS` in `static/js/lib/prefs.js` and a
   test case in `jstest/prefs.test.mjs`.
2. Create `static/css/skins/<value>.css` with a `:root[data-skin="<value>"]`
   block declaring every token in the table above, then its rules.
3. Link it from `index.html` after the base stylesheets (`css/responsive.css`
   is the last of them).
4. Write `skins/<value>.md`.
5. `go test ./internal/console/` checks the token set, the link, the scope and
   the color and font-size guards; then check every page in light and dark at
   1440 and 390 px.

## Color and contrast rules

Every skin meets these; its spec lists the measured values.

- Body and secondary text 4.5:1 on `--bg-surface` and `--bg-canvas`;
  `--border-control`, focus rings and progress fills 3:1 against neighbors.
- `--*-text` is used only on `--bg-surface`. A role may share a color with
  another (the editorial skin keeps running and completed in ink) as long as
  the icon and label differ.
- The danger and warning roles never look like the accent of a healthy state.
- Under `forced-colors`, badges, bars, cells and dots get borders (base);
  a progress cell with an outcome stays filled in `CanvasText`.

## Type, spacing, shape, motion

System fonts only: no web fonts, no CDN. A skin picks stacks for `--font-sans`,
`--font-display` and `--font-mono` and falls back to `--font-system` or
`--font-system-mono`. Every `font-size` is a `--fs-*` token (a Go test
checks every CSS file); a skin adds display roles as tokens. Numbers that
update live use `tabular-nums`. Long titles truncate with the full text in
`title`.

Spacing steps (px): 2, 4, 6, 8, 12, 16, 20, 24, 32, 40, 48, 64 (`--s-0_5` …
`--s-16`). Controls are 32 px (28 in toolbars), 44 px under 640 px. Radii and
shadows are skin tokens.

Motion: 120 ms hover color fades, 200 ms drawer/palette/toast/insert,
`--ease-out` in. Movement uses only `transform` and `opacity` (stage and meter
fills: `scaleX`, linear); never `transition: all`. The running pulse and the
skeleton pulse are the only loops a skin gets for free; any other loop (an
equalizer glyph, a live dot) is the skin's and must stop under
`prefers-reduced-motion`, which also removes the drawer slide, cover movement
and motion covers.

## Components

- **Badge**: icon (or the running pulse) + label on the role tint. Mappings
  live in `lib/status.js` (`jobStatusView`, `itemStatusView`, `hookStatusView`,
  health views), unit-tested. A completed job whose tracks were all on disk is
  "Already in library" (skipped role). An item with `attempt > 1` adds a
  warning "Retry n/m" badge.
- **KPI tile**: label, value, meta, role icon; the tile links to its Queue tab.
  Values switch without count-up; "—" while disconnected.
- **Job row**: cover, facts, title, artist/curator · type · storefront,
  progress, status badge, relative time, chevron. Indeterminate "Resolving"
  while `total_items` is 0. A job without a title shows its link (host and path)
  muted, with a hint line instead of an artist. A failed job shows a one-line
  reason (`errorSummary` in `lib/format.js`), never raw JSON. Under 640 px it is a two-line card.
- **Stage track**: seven segments (Resolve, Download, Decrypt, Remux, Verify,
  Tag, Save). Download and decrypt fill to their fraction; download at 0 while
  downloading is striped ("size unknown"); a completed item with
  `verified=false` shows a hollow "Not verified" segment, never red.
- **Job drawer**: dialog with focus trap, Esc and Back close it, focus returns
  to the row. Header with cover, artist link, meta, status, live tag;
  created/started/finished/duration; Cancel (inline confirm) for queued or
  running, Retry for failed, View logs, Open in Apple Music, Delete (a
  `ConfirmDialog` beside the panel) for completed, failed or cancelled jobs,
  which closes the drawer; palette swatches;
  per-track strip; track list (codec chip such as `ALAC 24/96`, length, size,
  error); hooks; Activity: the job's persisted events (the job stream replays
  them from the start; `lib/store.js` keeps those at or before the snapshot cursor
  out of the job and items) plus live ones, newest first, without per-item
  progress. A failed job shows a notice with the trimmed reason and the full error
  under a disclosure; a completed job shows the downloads folder with a link to
  Settings, since the API has no per-job path.
- **Showcase**: hero for the newest release, then a grid with `srcset`
  300w/600w and lazy loading. Covers keep pre-signed URLs without placeholders
  verbatim. Motion covers play muted on hover or focus, one at a time, only
  where `video.canPlayType` accepts HLS and motion is allowed. Titles underline
  on hover in every skin.
- **Throughput**: totals as facts, the formats mix of every retained job
  (`/stats` `codecs`, the top four with the rest as "Other"; nothing before
  a track completes), then two small multiples sharing the date
  axis (tracks per day, bytes per day), each with its own 0/max axis plus a
  max/2 gridline and label once max reaches 2 (for tracks only when max/2 is
  whole), a hover readout, and a screen-reader table.
- **Log viewer**: `role="log"`, `aria-live="off"`, mono rows (time, level
  marker, component, message), job/request chips that filter (the job chip shows the job's title),
  a trimmed `attrs.error` inline, one tab stop for the list with arrow keys, Home and End moving between rows, expandable pretty
  JSON with Copy, follow-bottom within 24 px else an "N new lines" pill, at most
  2,000 rows. Warn and error rows are marked in the skin's warning and danger
  roles.
- **Health rows**: name · status · detail for backend, wrapper, library sync
  (503 is "Not configured", not an error) and hooks, plus workers, queue,
  downloads disk and uptime from `/system`; polled every 30 s and on focus.
- **Config summary** (System): a few `GET /config` values (codec priority,
  downloads folder, simulate, library sync, log level, whether the media user
  token is set, pinned key count) and "Open settings".
- **Settings**: one field per `RuntimeConfig` leaf (`lib/settings.js`
  `FIELDS`; `TestSettingsFieldsCoverRuntimeConfig` fails on a leaf with
  neither a field nor a `NOT_EDITED` reason): label, help condensed from
  `configs/config.example.yaml`, the dotted key, and a control by type
  (switch, select, number with unit, text, path, template, secret with
  reveal, ordered list, checkbox set); every field states its default (the
  five path templates and two folders carry `defaultValue`, which a test holds
  to `configs/config.example.yaml`). A key in `ConfigResponse.pinned` is
  disabled and names its variable; a pinned folder or template shows whole
  and wrapped (`.field-fixed`), not cut off. The codec list says why its last
  entry cannot be removed. Edits stay in a draft; the sticky save bar
  counts them (announced politely, debounced), Discard drops them and reads
  `GET /config` again (it asks first when more than one field is edited), Save (or
  Cmd/Ctrl+S, unless a dialog is up) sends `PUT /config` with only the
  changed leaves, lists whole (`buildPatch`); an edit made while the save
  runs stays in the draft (`draftAfterSave`). A section opens at its top. A refused save
  keeps the draft: a notice says why, and `fieldErrors` puts the message on
  each field it names (`details.keys` for `config_key_pinned` and
  `config_key_not_mutable`, the dotted keys in the message for
  `invalid_config` and 400). Only unsendable numbers block Save on the
  client; an odd cover size or unknown template variable is a warning, and a
  saved template keeps its warning. When the tab regains focus (and after a
  reconnect) the page reads `GET /config` again: with no edits it refreshes and
  says "Updated from another tab or the file"; under edits it shows a notice
  with "Show the saved values" and "Keep editing", and the last write wins.
  Library sync shows `GET /library-sync` (read again after a save changes the
  switch; "On, idle" without a token) and "Reset anchor" (a `ConfirmDialog`,
  disabled with a reason while there is no anchor); Apple Music account shows
  the wrapper status, Log out per account (a `ConfirmDialog`) and the login
  form with its two-step code step, which is disabled with a reason while the
  wrapper-manager service is unreachable; Configuration file shows
  `persisted`, the pinned keys, `reload_error` and "Reload from file", which
  reports how many settings changed.
- **Primitives**: buttons (primary, secondary, ghost, danger), segmented
  control, search field, select, chips, notices, toasts (`role="status"`; 5 s,
  8 s with an action, errors 10 s, every timer paused while the pointer or
  focus is on them; `sticky` ones stay until dismissed or the page changes;
  `.toast` > `.icon`, `.toast-msg`, `a.toast-action`, Dismiss),
  completion toasts (`app.watchJobs(ids)`: when watched jobs end, one toast
  per 3 s burst, "Album X finished" with Open to its drawer or "3 downloads
  finished" with a link to the Queue, `lib/watch.js`; such a job is not also
  sent to the milestone region), command palette (Cmd/Ctrl+K, never over a
  modal dialog; combobox + listbox; focus handling from `useDialog`, so
  focus returns to the opener; rows from `lib/palette.js`: a query holding
  Apple Music links offers "Download this link" first, Settings sections and
  fields match by label, key or panel, job rows add status and date; a
  visually hidden polite status reads the result count once the job search
  answers; under `(hover: none)` a `.palette-close` button shows and the key
  hints and `.palette-foot` hide), shortcut sheet (`?`).
- **Dialogs** (`ui/dialogs.js`): `Dialog` is a scrim plus a modal panel
  (`useDialog`: focus the first `[data-autofocus]`, trap Tab, Escape closes
  without reaching the page shortcuts, focus returns to the opener).
  `ConfirmDialog` asks before a destructive or irreversible action: an
  alertdialog with the title, the consequence, Cancel (focused) and the
  confirm button; while busy both are disabled and Escape does nothing. The
  More sheet and the shortcut sheet are `Dialog`s. A change of page or drawer
  (a link, Back, a shortcut) closes the palette and the shell's sheets; a sheet pushes
  no history entry, so Back leaves the page rather than closing it first.
- **Queue selection** (`views/queue-select.js`, rules in `lib/jobactions.js`):
  Select (or `x` on a row) adds a checkbox column; a plain click on a row
  then selects it (Shift selects a range), Cmd/Ctrl-click still opens it.
  The bar offers only what the selection allows, with counts: Retry for
  failed jobs (the API retries nothing else), Cancel for queued or running,
  Delete for completed, failed or cancelled; Cancel and Delete ask first.
  Requests run at most 4 at a time (`lib/bulk.js` `runLimited`) with
  "Deleting 3 of 12" in the bar and the dialog, then one toast names the
  failures by reason. The selection is a set of ids checked against the live
  list, so a deleted job leaves it and a changed status changes the actions;
  rows leave through the feed, never by hand.
- **New download** (views/download.js and its download-* parts; the logic is
  pure in `lib/links.js` `scanLinks`/`removeLink`, `lib/lookup.js`,
  `lib/submit.js` and `lib/overrides.js`): the text box is the source of the
  links; `?url=` prefills it and the page writes its links back (replace), so a
  reload and the global paste keep them, also after `n` or the sidebar opened
  a bare `#/new`. It takes focus one frame after the
  shell focuses the heading, with a fine pointer only. Each row shows the
  link's kind and storefront at once, then a `POST /lookup` preview: the first
  100 previewable links (one submit chunk), 4 at a time, a lookup the text no
  longer holds is aborted, answers are cached per tab (an album `?i=` link's
  only under the `catalog.album_track_url_mode` it was read with); after a
  feed resync (`app.epoch`) failed lookups run again. The first 12 rows are
  listed plus every flagged one, then "Show all N". Legacy
  `itunes.apple.com` links and `id`-prefixed ids read as music.apple.com
  links; a link without a storefront takes the first one another link in the
  text has (the server's `catalog.default_storefront` is not in the API), else
  it is flagged; two links glued together split at the second `http`.
  Repeats are merged and counted in the card note, and a legend explains the
  storefront, E and ADM chips the rows show. Links that are not Apple Music, not a
  downloadable kind, music videos, refused by the lookup (400), in a
  storefront Apple does not have, or unknown to Apple (its 404, private
  playlists included: the catalog read never carries the token) are flagged
  and not sent; the count line counts them as flagged only, and warnings
  apart. Albums, songs and playlists also ask `GET /downloads?q=<id>` (2 at a
  time, cached per tab, `lib/lookup.js` `priorJob`) and show "Downloaded
  Oct 29" or "Already in the queue", linking to that job; downloading again
  stays possible. An artist row says it queues the whole discography, and a
  submit with artist links asks first; a sendable station or
  private playlist brings a media-user-token password field. A station
  without a token (typed or the server's) gets a warning; a private
  playlist downloads without one and uses it only for the owner's cover.
  The token is never stored: once it has settled for 350 ms, private
  playlists are looked up again with it (a station's preview does not use
  it), and a request carries it as `overrides.media_user_token` only while
  one of its links can use it (`lib/submit.js` `requestOverrides`).
  Qualities (albums and songs only) opens a sheet from `POST /quality`: a summary per quality slot,
  then each track's formats; errors show inline with the server's message.
  Options start collapsed with a line naming what differs from `GET /config`
  `download`; a submit sends only those as `overrides` (`buildOverrides`:
  `quality_priority` order matters, `lyrics_extras` is a set, `hooks` only
  when the checked set differs from the enabled entries, `[]` for none).
  Reset restores the defaults; "Remember these options on this device" keeps
  them in `localStorage["amdl.console.download.options"]`. Download N
  (Cmd/Ctrl+Enter, hinted only with a fine pointer) posts `POST /downloads` in chunks of 100, hands the
  accepted job ids to `app.watchJobs`, and the per-URL
  results replace the previous ones: accepted jobs as live Job rows (feed
  batches through `applyFeedToResults`) that open the drawer over the page,
  from 640 px up in two lines so the title and artist keep the main
  column's width (the art beside both, title and artist across the row,
  progress and status under them, no relative time; the base sets it with a
  five-class chain that outranks the skins' `.job-list-compact` rules),
  the rest with their reason, View job for `duplicate_active`, Retry for
  `queue_full`, `decryptor_unavailable` and server errors. The handled
  links then leave the text box; the rest of it stays, links typed during
  the request included, and focus moves to the results summary, whose
  counts follow each job's live status. The whole text box lasts for the
  tab when the page is left. The results stay until Clear (which moves focus to
  the text box) or the next submit, for the life of the tab (module state in
  `views/download-results.js` `useResults`, and
  `sessionStorage["amdl.console.download.results"]` over a reload, without
  the token): coming back to the page shows them, and their jobs (at most
  25) are read again since the page missed the feed meanwhile. The Options
  card uses the Settings labels and value lists (`lib/settings.js`
  `FIELDS`), says in text why the last codec cannot be removed, hides the
  ALAC limits while ALAC is not listed, and keeps focus in the codec list
  after an add or remove. The bookmarklet card shows
  `javascript:location.href='<this page>#/new?url='+encodeURIComponent(location.href)`
  as copyable text, never as a link.

## States

| Surface | Loading | Empty | Error |
| --- | --- | --- | --- |
| KPI | skeleton of final size | zeros | "—" while disconnected |
| Queue | 6 skeleton rows | per tab, with Clear filters when filtered; Active and All offer "Start a download" | inline note + Retry |
| Drawer | skeleton tracks | "No tracks resolved yet" | 404 on first load "No download with this id"; 404 after it loaded "This job was deleted"; a failed first load retries when the feed reconnects; 204 on the stream means finished |
| Showcase | skeleton hero and tiles (cover plus two text lines) | "Completed albums will appear here." with a link to the Queue and "Start a download" | inline note + Retry; broken cover shows bg color + glyph |
| Logs | 12 skeleton lines | "No records match" + Clear filters | the API's validation message |
| System, throughput | skeleton rows | per card | per card; a 404 endpoint shows a quiet "not available" note |
| Settings | 6 skeleton lines in the section | | inline note + Retry; a refused save keeps the draft with a notice and per-field errors; wrapper errors as notices in plain words with the server's text under "Details"; a load error reloads after a reconnect |
| New download | a skeleton cover and line per link being looked up; 3 skeleton lines for the options until `GET /config` answers | "No links yet" under the text box | a failed lookup flags its row (warning, Retry) and the link is still sent, and it runs again after a feed resync; options: inline note + Retry (also reloaded after a resync), and a submit sends no overrides; a refused submit shows a notice, `invalid_overrides` on the Options card |
| Feed | | | "Retrying in N s" (the wait before the next attempt); banner after 10 s; Disconnected after 60 s with Retry; the browser's `online` event retries at once |
| First load | static "Loading the AMDL console…" in `#app` (`.boot`, `index.html`) once the stylesheets are in, on the skin's canvas in the system theme; replaced on mount | | |

## Accessibility

Landmarks (`nav`, `header`, `main`), a skip button, one `h1` per page (focused on
navigation). Every control shows the 2 px focus ring in `--focus`. Keyboard: `n`
opens New download, `g` then `o/q/s/l/y/,/n` (`,` is Settings), `/` focuses
the page filter, `j`/`k` move between rows, Enter opens, `x` selects the
focused Queue row, Esc closes (then leaves selection mode; in a field it
clears a search first, then leaves the field), Cmd/Ctrl+K, Cmd/Ctrl+S saves
Settings,
`?`; single keys and the global paste are ignored while typing,
while the palette or a sheet is open and while any `[data-dialog]` is up. The
skin and theme controls are button groups with `aria-pressed` and an
accessible name ("Style", "Theme"). Milestones ("Album X completed") go to one
polite status region, at most one announcement per 5 s. Progress carries
`role="progressbar"` with `aria-valuetext` such as "12 of 14 tracks, 1 failed",
whether it renders cells or a bar. Text a skin adds with `::before` (section
numbers, "No.") is decoration and repeats nothing a screen reader needs.
With unsaved settings, leaving the page asks first: a page sets
`setLeaveGuard` from the app context, every hash change passes it (links,
the palette, shortcuts, Back), and a refused change is undone through the
history, so no entry is added (a pushed entry is dropped with Back, a Back
or Forward is reversed; entries carry `history.state.amdlIdx`,
`lib/route.js` `refusedMove`) while the page shows its `ConfirmDialog`;
leaving after all repeats the same move (`resumeLeave`). The browser's own
prompt covers closing the tab. The dialog names the count, the sections and
the fields that would be lost. Moving between settings sections never asks.

## Data flow

`GET /downloads?limit=1` seeds counts and `event_cursor`; the feed stream then
resumes from that cursor and every page applies its batches (at most 10 a
second) to its own list (`lib/store.js`). The drawer and logs page each open one
secondary stream from their snapshot cursor. Streams pause 30 s after the tab
hides and resume from the stored cursor. When the feed is live again after
any outage, or the browser fires `online`, `app.epoch` (AppContext) goes up:
every page refetches what it loads, and a page showing a load error reloads.

## Verification

Run the backend with `make dev` (simulate mode), open `/console/`, switch the
style in the top bar, and check each skin at 1440 and 390 px wide in light and
dark: Overview, Queue with the drawer, Showcase, Logs, System, New
download, Settings, the command palette, and under 640 px the tab bar and its
More sheet. No horizontal scroll at 390 px, status icon plus label everywhere,
visible focus rings.
