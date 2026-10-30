# Console design

The shared contract for the embedded console: structure, states, accessibility,
motion and the skin layer. How the console *looks* belongs to its one skin,
`music`, specified in [skins/music.md](skins/music.md). When this page and the code disagree, the code
wins and this page is fixed in the same change. Sources are listed in
[docs/references.md](../../docs/references.md#console-design-references).

## Principles

1. **Show only what the API knows.** Track counts, never an aggregate pipeline
   percentage (`ItemProgress` has none on purpose). Simulate mode is announced
   by a "Simulate" tag in the top bar, whose title says what it means.
2. **Status is never color alone.** Every status has an icon (or glyph), a label
   and a color role; the skin may change the look of a role, never drop the label.
3. **Artwork colors are data.** Apple's per-album palette reaches the page as
   custom properties; text colors are contrast-checked before they are used.
4. **Both themes.** The skin ships a light and a dark theme; light is
   designed, not inverted.
5. **English UI copy**, `lang="en"`. Apple-provided text (titles, artists,
   genres) is shown as received with `dir="auto"`.

## Information architecture

| Route | Page | Content |
| --- | --- | --- |
| `#/` (also `#/overview`) | Overview | Sections (in DOM order) a server-health strip (`.health-strip`, one line when healthy, a notice naming each unhealthy component and a disk System marks red otherwise, linking to System), `attention` (only while a job failed in the last 24 h: up to 2, each with Retry), `live` (8 active jobs; one line when none), `recent` (one tile per album), `kpis`, `throughput` (from `/stats`; one sentence under three days of data) |
| `#/queue?tab=active\|completed\|failed\|cancelled\|all&type=&q=` | Queue | Tabs with counts from the feed's `status_counts`, search over title, artist and URL, type filter, dense job list, Load more, selection mode (Retry, Cancel, Delete on many jobs), Retry all on the Failed and Cancelled tabs |
| `#/queue/:id`, `#/showcase/:id`, `#/new/:id` | Job drawer | Over its page: header with a summary line ("ALAC 24/44.1 · 14 tracks · 692 MB"), the failure reason, Cancel/Retry/Remove, the tracks (one line each; stage meters only on a track that is working or has an outcome), Files (completed jobs), and a closed Details (times, job ID, Activity with persisted history plus live events, hooks) |
| `#/showcase?type=album\|playlist\|song` | Showcase | "Latest" hero in Apple's colors, cover grid, Load more |
| `#/logs?level=&component=&job_id=&request_id=&q=` | Logs | Level chips, component, search, filter chips, live tail, expandable rows |
| `#/system` | System | Cards in this order, so health is first on a phone: health, wrapper, storage (the downloads and temporary folders by name, each path once, middle-cut with Copy; folders on one disk share one meter and one "Low space" line), a configuration summary that links to Settings (a warning only when changes are not saved), library sync (one line when off), hooks with outcomes (one line when none), runtime, about; paired so neighbours are of similar height |
| `#/new?url=` | New download | One centered column (760 px at most): sections `compose` (the text box), `links` (a row per link found), the submit bar (quality preset, Overwrite, count, Download), `results` (after a submit), then a closed "Browser shortcut" disclosure. `url` carries one or more links, newline- or space-separated and URL-encoded (`lib/route.js` `newDownloadHash`, `newDownloadUrls`), and follows the text box; `lib/links.js` parses them |
| `#/settings?section=&field=` | Settings | A section list beside one open section (under 640 px, `#/settings` without a section is the list of sections, icon, label, one-line summary and chevron, and a section opens as its own view headed by a "Settings" back link and the section title, the page title kept for screen readers only): `account` (the default), `quality`, `files`, `lyrics_artwork`, `advanced` (`lib/settings.js` `SETTINGS_SECTIONS`; `section` is lowercase words joined by `_`, `settingsSection`). Advanced has sub-headed groups (`SETTINGS_GROUPS`: `downloading`, `apple_requests`, `library_sync`, `logging`, `simulate`, `config_file`); a group id as `section` opens Advanced scrolled to it, and the retired ids `downloads`, `artwork`, `lyrics` and `catalog` open the section that holds their settings now (`sectionById`). A known `field` wins over `section`: its own section opens. The palette finds every section and field, also by words such as `parallel` or `template` (`lib/palette.js`); a field result opens `#/settings?section=<id>&field=<config key>`, which focuses that field's enabled control, if any, and scrolls it to the middle (`ui/field-focus.js`) |

New download is the primary action and has these entry points: the
`.sidebar-new` button first in the sidebar (icon-only with a tooltip on the
640-1023 rail), "Start a
download" in the Overview live-queue, Queue and Showcase empty states, the
tab bar's center tab under 640 px, `n` (and `g n`), the palette's "New
download", and a global paste: pasting text that holds at least one
music.apple.com link while focus is outside a text field and no overlay or
dialog is open opens `#/new?url=<the links>` (on `#/new` the links join those
already in `url`). Under 640 px the tab bar holds Overview, Queue, New,
Showcase and More; More opens a bottom sheet (`MoreSheet`) listing Logs,
System and Settings with the current page marked, and the More tab is
`aria-current` while one of them is shown or while the sheet is open (the
page under the sheet then loses its mark).

Filters live in the hash query, so a filtered view is a link. KPI tiles open the
matching Queue tab; the drawer's "View logs" opens `#/logs?job_id=…`; a log
row's job chip filters to that job. The tab title reads `"{Page} · AMDL"`, and
`"{n} running · {Page} · AMDL"` while jobs run.

## Layout

The DOM frame is fixed; the skin restyles it.

```
.shell
  .sidebar (nav: .brand > .brand-mark + .brand-text > .brand-name, .brand-sub; a.sidebar-new; .nav > .nav-item)
  .main-col
    header.topbar (.topbar-right: .sim-tag in simulate mode, .conn, .search-trigger, .theme)
    main.main[data-page] (.page-head > h1.page-title + .page-sub; .banners; .page.page-<name>)
  nav.tabbar (> .tab x2, a.tab.tab-new, .tab, button.tab.tab-more, under 640 px)
```

| Width | Base frame | Notes |
| --- | --- | --- |
| ≥1024 | `--sidebar-w` sidebar, `--topbar-h` top bar | Overview is a 12-column grid, every section 12 wide |
| 640-1023 | `--rail-w` icon rail (labels kept for screen readers) | two-column cards stack |
| <640 | top bar, `--tabbar-h` bottom tab bar + safe area, 16 px gutters | single column, 44 px targets and 16 px fields (also on any coarse pointer at any width) |

Siblings are spaced by the parent's `gap` (`--page-gap` between sections,
`--grid-gap` inside grids, `--card-gap` inside cards), never by child margins.
No horizontal page scroll at 390 px in either theme.

## Skin

The skin is the console's look: colors, radii, shadows, font stacks, display
type and a few component rules. There is one, `music`; the theme (system,
light, dark) is a separate axis, and the skin supports all three.

### Mechanism

- `<html data-skin="music">` is static in `index.html`, so the first frame has
  every token before `main.js` runs. Nothing switches it; an `amdl.console.skin`
  value an earlier version stored in the browser is never read.
- The theme is stored per browser under `localStorage["amdl.console.theme"]`
  with a guarded read and write (`lib/prefs.js`: `normalizeTheme`, `readPref`,
  `writePref`); `main.js` applies it before mounting.
- The theme is `<html data-theme="light|dark">` or no attribute for "system";
  `css/tokens.css` sets `color-scheme` from it, and the skin writes every color
  as `light-dark(<light>, <dark>)`, which resolves against that. The skin never
  sets `color-scheme`.

### Files and what each may do

| File | Holds | May | May not |
| --- | --- | --- | --- |
| `static/css/tokens.css` | Skin-neutral scales: type scale `--fs-*`/`--lh-*`, weights, spacing `--s-*`, sizes, motion, `--on-art-light/dark`, system font fallbacks | change a shared scale | hold a skin's color, radius, shadow or font stack |
| `static/css/*.css` (the base: `base.css`, `shell.css`, `controls.css`, `layout.css`, one file per page area, `overlays.css`, `responsive.css`, then the newer pages' `download-form.css`, `download.css` and `settings.css`) | Structure: layout, component structure and behavior, all visual values through tokens, one file per area. `index.html` links them after `tokens.css` and before the skin, and that link order is the cascade order: `responsive.css` follows the older areas, and `download-form.css`, `download.css` and `settings.css` follow it, so each carries its own media queries (the phone rules at the end of `download.css` cover both New download files) | add structure, in the file of its area | contain a color literal, `!important` (outside reduced motion) or a skin-specific choice |
| `static/css/skins/<name>.css` | One `:root[data-skin="<name>"]` token block (light and dark via `light-dark()`), responsive token overrides, and that skin's component rules, every selector prefixed `:root[data-skin="<name>"]` | set any token, override any base rule (repeating its selector chain when it is deeper than three classes), show opt-in hooks, reorder overview sections, use CSS counters and `::before` text | leave its scope, add JS, use `font-size` other than `var(--fs-*)`, use `transition: all`, rely on `color-scheme` it sets itself |
| `skins/<name>.md` | The skin's spec: intent, token table with contrast, component notes | | restate the shared contract |

The prefix `:root[data-skin="<name>"]` adds (0,2,0) (`:root` is a
pseudo-class), so a skin selector with one class wins over any base selector
of up to three classes, attributes and pseudo-classes (a tie goes to the
skin, linked later), including those inside base media queries; a skin
therefore carries its own responsive overrides. For a deeper base selector
(the chart hover chain, the Submitted row chain
`.job-list.job-list-compact.new-results > .new-result > .job-row`), repeat
its chain under the skin prefix.

### Tokens the skin must declare

`TestSkinsDeclareEveryBaseToken` fails when the skin misses one that a base
stylesheet reads and `tokens.css` does not declare. Declare the whole table anyway (for
example every role's `-text`, though the base reads only `--danger-text` and
`--warning-text`), so the skin's rules can rely on it.

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

Stable class names and attributes the skin can style without touching JS:

| Surface | Hooks |
| --- | --- |
| Section / card | `section.card[data-section]` > `header.card-head` (`h2.card-title`, `.card-note`, `.card-actions`) + `.card-body`; `.card-flush` for edge-to-edge lists. Overview sections: `attention`, `live`, `recent`, `kpis`, `throughput`; Queue `jobs`; Logs `log` |
| Page | `main.main[data-page]`, `.page-head`, `.page-title`, `.page-sub` (Overview and Queue only: live counts), `.banners`, `.page.page-<name>` (`page-new`, `page-settings` included) |
| KPI | `a.kpi[data-key=queued\|running\|completed\|failed\|total][data-role][data-zero]` > `.kpi-label`, `.kpi-icon`, `.kpi-value`, `.kpi-meta` |
| Job row | `a.job-row[data-status][data-progress=on\|off][.has-palette]` > `.art`, `.job-main` (`.job-facts` > `.job-fact[data-key=type\|genre\|year\|started]`, `.job-title[.is-unresolved]` (a failed link reads "Link not found" for a 404), `.job-sub` (one line: artist, the type unless it is Album, the storefront unless it is the default, the track count of a finished job; a failed link's address), `.job-error` (failed jobs: the reason on its own line, at most two lines, full text in the title attribute)), `.job-progress` (empty unless the job is queued or running; `data-progress=off` sets `--job-prog` to 0 so the title takes the room), `.job-status` (a plain completed job is `.status-text.status-icon[role=img]`, the icon alone; every other status a badge), `.job-updated`, `.job-chevron`. The Queue adds `li.job-item[data-action=retry]` > `.btn.job-retry` after the row for a failed job (a button cannot sit inside the link); every Queue row reserves the button's width as `--job-trail` (the chevron sits at its end), so the status, the time and Retry line up down the list |
| Progress | `.jprog[data-mode=cells\|bar][data-working]` (a running job with nothing finished yet shows a striped remaining track): up to 60 tracks `.pcells` (`--n`) > `.pcell[data-state=completed\|skipped\|failed\|cancelled\|remaining][data-role]` in that order (the list payload has counts, not per-track order); above 60 `.pbar` > `.pbar-seg[data-role]`; both followed by `.jprog-text` ("12 / 14") and `.jprog-legend` > `.jprog-key[data-state][data-role][data-zero]` > `.jprog-key-n` + `.jprog-key-label` (saved, on disk, failed, cancelled, remaining; every state, zeros included). The list payload cannot tell a track in flight from a waiting one, so "remaining" covers both and there is no byte percentage |
| Health | `.health-row[data-role]` > `.health-icon`, `.health-name`, `.health-status` (badge), `.health-detail` (System page) |
| Facts | `dl.facts` > `.fact[data-key][data-role]` > `dt.fact-label` + `dd.fact-value` (throughput totals `.tp-totals` with `tracks`, `tracks-failed`, `bytes`, `jobs`, `jobs-failed`, the failed rows only when non-zero and `data-role=danger`, hero facts `.hero-facts`) |
| Recent strip | `a.strip-tile[.has-palette][.is-overflow]` > `.art`, `.strip-title`, `.strip-sub`; every completed album renders once (newest job of a link target), those past the whole-tile count carry `.is-overflow` |
| Showcase | `a.hero.has-palette` (a banner at most 200 px tall, hidden under 640 px) > `.hero-art`, `.hero-text` (`.hero-kicker`, `.hero-title`, `.hero-artist`, `.hero-facts`); `ul.gallery` > `li[.is-latest][.is-new]` > `a.tile` > `.tile-art` (`.tile-motion` text chip "Motion cover"), `.tile-title`, `.tile-sub`; the `li.is-latest` is the banner's release, shown only under 640 px; one entry per link target |
| Status strip, Activity, failure | Overview `p.health-strip[data-state=ok]` or `.health-strip.notice[data-state=problem][data-role=danger\|warning]` > `.health-strip-link`; drawer `details.drawer-section.drawer-details` (closed) > `.drawer-details-body` > `dl.drawer-times`, `.drawer-id` (`code` + Copy), `section.activity` > `ol.activity-list` > `li.activity-row[data-role]` (`.activity-time`, `.activity-label`, `.activity-msg`) and a Hooks `section`; failure notice with `details.notice-more` > `pre.notice-raw`; Files `section` > `.drawer-files` > `code.drawer-files-dir` + Copy, `p.drawer-files-note` (several folders only), `a.drawer-files-link`; tracks `ol.tracks` > `li.track[data-status][data-detail]` (`data-detail` on a track with stage meters and a status) > `.track-index`, `.track-main` (`.track-title`, `.track-artist` only when it differs from the album artist), `.track-codec` (only when it differs from the album's), `.track-len`, `.track-size`, then `.track-stages`, `.track-status`, `.track-error`/`.track-note` for a detail track; Logs `.log-err`, `.attr-chip-job` > `.attr-chip-name`, `.filter-chip-name`, rows `[data-seq]` with the one tab stop |
| Needs attention | `section.card[data-section=attention]` > `ul.attention` > `li.attention-row[data-role=danger]` > `.attention-title` (`.attention-label` "Failed" with a circle-x icon, `a.attention-link` to the drawer, mono `.attention-input`, `.attention-time`), `.attention-detail` (`.attention-error`, clamped to two lines), Retry `.btn`; `p.live-idle` is the empty live queue; `p.tp-sparse` the short Throughput card |
| Throughput | `.tp-totals` (Facts), `.tp-formats` > `.tp-formats-title`, `.formats-bar` > `.formats-seg[data-key]` (`--w` share, `--i` index), `ul.formats-list` > `li.formats-item[data-key]` (`--i`) > `.formats-swatch`, `.formats-label`, `.formats-count`; `.chart-xaxis` > one `span[data-day]` per day, `.is-first`, `.is-mid`, `.is-latest` on the three the base shows |
| Logs | `.log-entry[data-level]` on log page rows (`.log-row`) > `.log-time`, `.log-level[data-role]`, `.log-comp`, `.log-msg` (`.log-err`, then up to three `key=value` pairs in `.log-attrs`); toolbar `button.level-chip[aria-pressed]` (a pressed one holds a check icon, an unpressed one the hollow `.log-dot`) and `button.wrap-switch[aria-pressed]` > `.wrap-switch-track` "Wrap lines" |
| Shell | `.brand-sub`, `a.sidebar-new.btn.btn-primary[aria-current]` > `.icon` + `.sidebar-new-label` (visually hidden on the rail), `.nav-item[aria-current]`, `.nav-badge`, `.tab[data-page][aria-current]`, `a.tab.tab-new` > `.tab-new-mark` (accent pill around the plus; it rises over the icon line with a negative `margin-top`, so its label shares the other labels' line, and a skin rule that resizes the mark or the tab icons restates it) + `.tab-label` "New", `button.tab.tab-more[aria-expanded][aria-current]`, `.theme` > `button.theme-cycle` (one button cycling System, Light, Dark; its label names the next), `.sim-tag` (simulate mode, explanation in its title and an `.sr-only` span); loading shell `.boot` > `.boot-mark` + `.boot-text` (static in `index.html`, replaced on mount); `svg.sprite` at the start of `body`, the in-document icon sprite (`ui/icons.js`), never styled by the skin; `meta[name=theme-color]` set from the body background; `.conn[data-role][data-state=connecting\|live\|reconnecting\|offline\|paused]` > `.conn-dot` + `.conn-label` (the base hides the label visually unless `data-state` is `reconnecting` or `offline`, and always under 640 px) |
| Dialogs | `.overlay-host[data-placement=center\|bottom]` > `.scrim` + the panel `[role=dialog\|alertdialog][aria-modal][data-dialog=<name>]` (`ui/dialogs.js` `Dialog`). More sheet: `.sheet.sheet-more[data-dialog=more]` > `.sheet-head`, `ul.more-list` > `a.more-item[data-page][aria-current]` > `.icon`, `.more-label`, `.more-current` ("Current"), chevron. Confirm: `.sheet.dialog-confirm[role=alertdialog][data-dialog=confirm]` > `h2.dialog-title`, `.dialog-body`, `.dialog-actions[data-choices=3]` > `.btn.dialog-cancel` + `.btn.dialog-confirm-btn[aria-busy]` (`btn-danger` when destructive), in drawn order: with three choices `.dialog-confirm-btn` first and `.btn.dialog-alt-btn` last, on a phone sheet the confirm button before Cancel and `.dialog-alt-btn` first; a busy button is `.btn[aria-disabled=true]`; a bottom panel draws its grabber as `::before`. Shortcut sheet `.sheet.sheet-shortcuts[data-dialog=shortcuts]` > `.sheet-head` + `.shortcut-body` > `.shortcut-groups` > `section.shortcut-group` > `h3.shortcut-group-title` + `dl.kv` (`dt.shortcut-keys` with `kbd`s) |
| Status | `[data-role=info\|success\|danger\|warning\|neutral\|skipped]` sets `--role` and `--role-tint` for everything inside it; `.badge`, `.status-text`, `.notice` |
| Settings | `.page-settings[data-section-active][data-dirty]` > `nav.settings-nav` (`ul.settings-nav-list` > `a.settings-nav-item[data-section][aria-current=true][data-dirty][data-invalid]` > `.icon`, `.settings-nav-label`, `.settings-nav-mark` (`.settings-nav-dot` when unsaved); hidden under 640 px), `.page-settings[data-view=list\|section]` on phones, where `.settings-main` holds `nav.settings-list-card` > `ul.settings-list` > `a.settings-list-row[data-section]` (`.settings-list-icon`, `.settings-list-text` > `.settings-list-label` + `.settings-list-summary`, `.settings-nav-mark`, `.settings-list-chevron`) or `a.settings-back` before the section, `.settings-main` (a size container) > `.settings-error` + `section.card.settings-section[data-section=<id>]` > `.settings-intro`, in Advanced `section.settings-group#settings-group-<id>[data-group]` (`.settings-group-title`, `.settings-group-intro`, `.field.settings-keys-toggle`), `.field-list` > `.field[data-key][data-type][data-wide][data-changed][data-invalid]` > `.field-text` (`.field-label`, `.field-changed`, `button.field-revert`, `button.field-reset`, `.field-help` (a select's chosen option adds its `.field-option-hint` to it), `code.field-key` only while keys are shown), `.field-control` (`input.field-switch[role=switch]`, `.field-select` > `.select` + `input.field-custom` (a `custom` select's Custom…), `.field-number` > `.field-input` + `.field-unit`, `.field-input`, `.list-editor[data-ordered]` > `ol.list-items` > `li.list-item[data-item]` (`.list-rank`, `.list-label`, `.list-actions` > `.list-up`, `.list-down`, `.list-remove`; rank and arrows only with two or more items) + `.list-add` > `select`, `.check-group` > `label.check`), then `.field-default` (`code.field-default-value`), `.field-error`, `.field-warning`; panels `.settings-panel[data-panel=library-sync\|config-file]` (`.settings-panel-head`, `.settings-panel-actions`, `.sync-reset` (once a check has recorded the library), `.sync-state`, `.config-reload`, `.config-outcome`), a template's `.template-field` > `.template-row` (`.field-input` + `.template-vars-btn[aria-expanded]`), `.template-popover` > `.template-chips` > `button.template-chip[data-var]` + `.template-popover-foot` (`.template-var-desc`, `.template-vars-done`), `.template-example`, `.account` > `.account-block` (`.account-block-head`, `ul.account-rows` > `.account-row` > `.account-name` + `.account-logout`), `p.account-down` (+ `.account-down-refresh`), `form.login-form[data-step=credentials\|code]` > `.form-row` (the password in `.field-secret`), `.form-actions`; the token's `.token-field` > `.token-row[.token-set]` (`.token-state`, `.token-replace`, `.token-remove`, or the box) + `.token-help` (`.token-help-btn`, `ol.token-steps`); `.save-bar[data-state=dirty\|saving]` > `.save-bar-text` (`.save-bar-count`, `.save-bar-where`) + `.save-bar-actions` (`.save-bar-discard`, `.save-bar-save`) |
| Job selection (Queue) | Tabs `.seg-count` > `.seg-count-danger` (the Failed count while it is above zero); under 640 px the tabs are a chip row that scrolls when it must, `.seg[data-fade=start\|end\|both]` marking the edges that hide chips (`views/queue-chips.js`); toolbar `span.queue-count` ("5 results", while the filter is set) and `.queue-tools` > `.btn.queue-retry-all` (Failed and Cancelled tabs) + `.btn.queue-select[aria-pressed]` (reads "Select", "Done" while selecting); `.bulk-bar[data-state=select\|run]` (sticky at `top: var(--topbar-h)`; a skin whose top bar does not stick would override `top`) > `label.bulk-all` > `input.bulk-check` (indeterminate when some), `.bulk-count` ("3 selected", or "3 selected · 1 shown" when a filter hides some), `.bulk-actions` > `.btn.bulk-action[data-action=retry\|cancel\|delete]` ("Retry 3", "Cancel 3", "Remove 3 from list") or `.bulk-hint`, or during a run `.bulk-progress` > `.bulk-progress-text` + `.bulk-meter[role=progressbar]` > `.bulk-meter-fill` (`--f`); `ul.job-list.is-selecting` > `li.job-item[data-job-id][data-selected]` > `label.job-check` > `input.job-check-input` + `a.job-row[data-selected]`. Drawer: `.btn.drawer-delete` (finished jobs) |
| New download | `.page.page-new[style=--new-bar-h]` (one column, 760 px at most, the page head centered over it) > cards `compose`, `links`, then `.new-submit`, card `results`, `details.new-shortcut`. Compose: `.card-actions` > `.new-paste` (a coarse pointer with clipboard reading), `textarea#new-input.new-input`, `.new-hint` (the Cmd/Ctrl+Enter hint, fine pointer only). Submit bar `.new-submit` (sticky at the bottom of the viewport; under 640 px fixed above the tab bar, and the page keeps `--new-bar-h` free below) > `p.new-count[role=status]`, `.new-submit-row` > `.new-controls` (`.new-quality` > `select#new-quality.select.new-quality-select`, `label.new-overwrite` > `input.field-switch[role=switch]` + text with `.new-overwrite-more`, hidden under 640 px) + `.btn.new-submit-btn[aria-busy]`, then `.notice`s and `.new-cfg-note`. Links: `ul.new-links` > `li.new-link[data-kind][data-state=queued\|loading\|ready\|error\|idle\|invalid][data-issue=not_apple\|no_storefront\|unsupported\|music_video\|bad_storefront\|storefront_not_served\|refused\|not_found\|needs_token\|preview_failed][data-blocking][data-excluded][.has-palette]` > `input.job-check-input.new-link-pick[type=checkbox]` (the Queue selection control; disabled and hidden when blocking), `.art.art-48.new-link-art`, `.new-link-main` (`.new-link-head` > `.new-link-title[.is-url]`, `.new-link-explicit`, `.chip.new-link-adm` (Apple Digital Master, hidden under 640 px), a storefront `.chip.new-link-sf` (only when it is not the server's default or was borrowed; it never shrinks), `.new-link-sub` (one line, ellipsized, its text the tooltip; under 640 px it wraps between facts, two lines at most, and with the title (two lines at most) beside the row's buttons it runs under them) > `.new-link-fact[data-key=type\|artist\|album\|tracks\|year\|length]` and `.new-link-loading`, `.new-link-note[data-note=artist\|storefront\|overwrite]`, `span.new-link-prior[data-state=active\|done]` > `a.link.new-link-open`, `.new-link-will` (once the link's qualities were read; a `.status-text[data-role=warning]` when the first codec is missing), `.new-link-issue.status-text[data-role]`, with `a.link.new-link-fix` when a page fixes it), `.new-link-actions` > `.new-link-quality` (albums and songs; its text hidden under 640 px), `.new-link-retry`, `.new-link-remove`; the row's `title` is the link; `.new-links-all` > `.new-links-toggle[aria-expanded]`, `.new-links-more`, `p.new-links-will` ("Quality: …", what the chosen quality asks for). Results: `#new-results-summary.new-results-summary[role=status][tabindex=-1]`, `ul.job-list.job-list-compact.new-results` > `li.new-result[data-status=accepted\|invalid\|duplicate_active\|duplicate_in_request\|queue_full\|error\|deleted]` > a Job row, or `.new-result-row[data-role]` > `.new-result-icon`, `.new-result-main` (`.new-result-label`, `.new-result-title` (a deleted job), `.new-result-url`, `.new-result-msg`, `.new-result-detail`), `.new-result-actions`; head `.new-results-retry`, `.new-results-clear`. Custom quality sheet `.sheet.sheet-custom[data-dialog=custom]` > `.sheet-head` (`h2#new-custom-title.card-title[tabindex=-1]`, focused on open), `.new-custom-for`, `.new-custom-body` (scrolls) > `.new-custom-group` (`.new-custom-label`, the Settings `.list-editor`, `.new-hint`), `label.new-custom-switch`, `.new-custom-limits` > `label.new-custom-field` > `.select`; `.dialog-actions.new-custom-actions` > `.new-custom-follow`, `.new-custom-done`. Browser shortcut (hidden for a coarse pointer): `details.new-shortcut` > `summary.new-shortcut-summary`, `.new-shortcut-body` > `.new-hint`, `.new-bookmarklet` > `code#new-bookmarklet-code.new-bookmarklet-code` + `.new-bookmarklet-copy`, `ol.new-steps`. Quality sheet `.sheet.sheet-quality[data-dialog=quality]` > `.quality-for`, `.quality-wait`, `.quality-body[role=region][tabindex=0]` (the part that scrolls, so the head and its Close stay) > `ul.quality-slots` > `li.quality-slot[data-id]` (`.quality-slot-label` > `.quality-slot-codec` (only when the label does not say it), `.quality-slot-count`, `.quality-slot-best`), `p.quality-same` > `.quality-all` (Show tracks), `ol.quality-tracks` (the tracks whose formats differ, all after Show tracks) > `li.quality-track` > `.quality-track-head` (`.quality-track-no`, `.quality-track-name`, `.chip`) + `.quality-track-opts` > `.quality-opt[data-id]` (`.quality-opt-label`, `.quality-opt-spec`) |

**Artwork palette.** `.job-row`, `.strip-tile` and `.hero` carry
`--art-bg` and `--art-t1`, `--art-t2`, `--art-t3` (from `lib/format.js`
`artPalette`/`paletteStyle`) plus `.has-palette` when Apple sent one; the
drawer's `.drawer-head` carries the properties without the class. Each `--art-t*` already reaches 4.5:1 on `--art-bg` or is replaced by
`--on-art-light`/`--on-art-dark`. A background lighter than 24 % HSL
lightness also gets a dark-theme variant (`darkArtColor`: same hue, darkened
until white text reaches 4.5:1, its text light), and each property is then a
`light-dark()` pair, so a light album never paints a bright slab on the dark
page. A playing motion cover switches to the
`motion_artwork_*` palette; the two are never mixed. `.art` also sets `--art-bg`
behind a cover while it loads or when it fails.

**Opt-in hooks** are hidden by the base and shown by the skin when it wants them:
`.brand-sub`, `.job-facts`, `.health-icon`,
`.strip-tile.is-overflow`, `.jprog-legend`,
`.attention`, and the x-axis day labels other than the first, middle and
latest.

## Color and contrast rules

The skin meets these; its spec lists the measured values.

- Body and secondary text 4.5:1 on `--bg-surface` and `--bg-canvas`;
  `--border-control`, focus rings and progress fills 3:1 against neighbors.
- `--*-text` is used only on `--bg-surface`. A role may share a color with
  another as long as the icon and label differ.
- The danger and warning roles never look like the accent of a healthy state.
- Under `forced-colors`, badges (with inline padding, so the border clears the
  text), bars, cells and dots get borders (base); a progress cell with an
  outcome stays filled in `CanvasText`; a switch is a hollow track with the
  thumb at the start when off and a `Highlight` track with the thumb at the end
  when on.

## Type, spacing, shape, motion

System fonts only: no web fonts, no CDN. The skin picks stacks for `--font-sans`,
`--font-display` and `--font-mono` and falls back to `--font-system` or
`--font-system-mono`. Every `font-size` is a `--fs-*` token (a Go test
checks every CSS file); the skin adds display roles as tokens. Numbers that
update live use `tabular-nums`. Long titles truncate with the full text in
`title`.

Spacing steps (px): 2, 4, 6, 8, 12, 16, 20, 24, 32, 40, 48, 64 (`--s-0_5` …
`--s-16`). Controls are 32 px (28 in toolbars), 44 px under 640 px and on any coarse
pointer (a phone in landscape, a tablet), where the theme switch
becomes a cycle button and the icon rail scrolls. Under those conditions a
standalone link or a chip's remove button has a 44 px box; the log attribute
chips stay 20 px drawn and get an invisible 44 px hit area inside a row that is
44 px tall (so `getBoundingClientRect` reads 20, `elementFromPoint` 44). On the
phone the Queue's Select button is icon-only beside the filters, and the
selection bar keeps its actions on the select-all row. Radii and
shadows are skin tokens.

Motion: 120 ms hover color fades, 200 ms drawer/palette/toast/insert,
`--ease-out` in. Movement uses only `transform` and `opacity` (stage and meter
fills: `scaleX`, linear); never `transition: all`. The running pulse and the
skeleton pulse are the only loops the skin gets for free; any other loop (an
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
  while `total_items` is 0; a running one-track job with nothing finished reads
  "Working" instead of a "0 / 1" that never moves. A job without a title shows its link (host and path)
  muted, with a hint line instead of an artist. A failed job shows a one-line
  reason (`errorSummary` in `lib/format.js`), never raw JSON; a stored legacy
  catalog failure ("catalog request failed: 404 Not Found") reads in plain words
  (`legacyCatalogError`), and the drawer keeps the stored text under "Full error". Under 640 px it is a two-line card.
- **Stage track**: seven segments (Resolve, Download, Decrypt, Remux, Verify,
  Tag, Save). Download and decrypt fill to their fraction; download at 0 while
  downloading is striped ("size unknown"); a completed item with
  `verified=false` shows a hollow "Not verified" segment, never red.
- **Job drawer**: dialog with focus trap, Esc and Back close it, focus returns
  to the row. Close is first in the DOM, so Tab goes from it to the header's
  Back on a phone and then the body (`.drawer-bar` is `row-reverse`,
  which keeps the drawn order). Header with cover, artist link, meta, a summary
  line computed from the tracks (`lib/jobview.js` `drawerSummary`), status, live
  tag; for a failed job the notice with the trimmed reason (full error under a
  disclosure) comes first, then Cancel (inline confirm) for queued or running,
  Retry for failed or cancelled, View logs, Open in Apple Music, Remove (a
  `ConfirmDialog` beside the panel; it removes the job's records, never the
  files) for completed, failed or cancelled jobs,
  which closes the drawer. A job Apple has nothing at (a 404) makes Open in
  Apple Music the solid button and Retry secondary, and its header reads as its
  queue row does ("Link not found", the link under it, no type or region);
  on a phone the actions fill their rows, Remove included. Tracks:
  one 44 px line each (number, title, length, size); the artist shows when it
  differs from the album artist, a codec chip such as `ALAC 24/96` when it
  differs from the album's (`.track-legend` says once which codec most tracks carry and that the
  others are marked on their rows, only when the codecs differ), and stage meters, status and error only for a track that is
  working, failed, skipped or waiting; no tracks section for a finished job that
  has none. A completed job shows Files: the one folder its tracks
  went to (the common folder of the items' `output_path`, from `lib/files.js`) with a
  Copy button, or, when they went to several folders, the downloads folder and
  "Spread over N folders"; a link to Settings follows. Details is closed:
  times in one short format (`dateTime`, the exact time in the title), the job
  id with Copy, Activity (the job's persisted events, which the job stream
  replays from the start; `lib/store.js` keeps those at or before the snapshot
  cursor out of the job and items; newest first, with a "Gave up" folded into
  the "Failed" that says the same) and hooks.
- **Showcase**: a compact banner for the newest release (desktop only), then a
  grid, one tile per album (jobs of one link target collapse to the newest),
  of cover, title and artist on the page surface, with `srcset`
  300w/600w and lazy loading. Covers keep pre-signed URLs without placeholders
  verbatim. Motion covers play muted on hover or focus, one at a time, only
  where `video.canPlayType` accepts HLS and motion is allowed. Titles underline
  on hover.
- **Throughput**: under three days with data one sentence ("246 tracks, 11.1 GB" under the card's "Last 14 days"); otherwise totals as facts, the formats mix of every retained job
  (`/stats` `codecs`, the top four with the rest as "Other"; nothing before
  a track completes), then two small multiples sharing the date
  axis (tracks per day, bytes per day), each with its own 0/max axis plus a
  max/2 gridline and label once max reaches 2 (for tracks only when max/2 is
  whole), a hover readout, and a screen-reader table.
- **Log viewer**: `role="log"`, `aria-live="off"`, mono rows (time, level
  marker, component, message, up to three attributes in secondary text), job/request chips that filter (the job chip shows the job's title),
  a trimmed `attrs.error` inline, one tab stop for the list with arrow keys, Home and End moving between rows, expandable pretty
  JSON with Copy, follow-bottom within 24 px else an "N new lines" pill, at most
  2,000 rows. Warn and error rows are marked in the skin's warning and danger
  roles.
- **Health rows**: name · status · detail for backend, wrapper, library sync
  and hooks (System page); the Overview keeps only the status line above
  the sections; polled every 30 s and on focus.
- **Config summary** (System): a few `GET /config` values that no other card
  shows (codec priority, log level, whether the media user token is set) and
  "Open settings"; a warning appears only when changes are not saved to a file.
- **Settings**: one field per `RuntimeConfig` leaf (`lib/settings.js`
  `FIELDS`; `TestSettingsFieldsCoverRuntimeConfig` fails on a leaf with
  neither a field nor a `NOT_EDITED` reason), in five sections: Account,
  Quality & downloads, Files & naming, Lyrics & artwork, Advanced. A field
  is a label in plain words, one short hint about its effect, and a control
  by type (switch, select, number with unit, text, path, template, token,
  ordered list, checkbox set). Every field carries its `defaultValue`, which
  a test holds to `configs/config.example.yaml`; the dotted key and a
  "Default: …" line show only while Advanced's "Show config keys and
  defaults" is on (a per-device preference, `amdl.console.settings.keys`). A
  saved value that differs from its default offers "Reset to default", an
  edit like any other. Controls end on one edge: switches without an On/Off
  word, selects as wide as their longest option, numbers with the unit inside
  the box; cover size is a select of the documented sizes plus Custom…. The
  codec list adds a codec as soon as it is picked from "+ Add codec…", hides
  rank and arrows while it holds one, and its last entry's Remove says why it
  cannot go. Each path template shows the path it gives a sample track
  (track 1 of Blonde, `templateExample`, which follows the backend's
  `formatPattern` and `safeName`) while it is typed, and its Variables
  button opens chips that insert a variable at the caret; the panel stays
  open for the next one until Done, Escape or a press outside, and names the
  focused or last inserted variable's meaning under the chips. A select's
  chosen option may add a note to the help line; the control column holds
  only the control. Edits stay in a draft; the sticky save bar
  counts them (announced politely, debounced), Discard drops them and reads
  `GET /config` again (it asks first when more than one field is edited), Save (or
  Cmd/Ctrl+S, unless a dialog is up) sends `PUT /config` with only the
  changed leaves, lists whole (`buildPatch`); an edit made while the save
  runs stays in the draft (`draftAfterSave`). A section opens at its top. A refused save
  keeps the draft: a notice says why and links to each field at fault by its
  label, and `fieldErrors` puts the message on each field it names
  (`details.keys` for `config_key_not_mutable`, the dotted keys in the message for
  `invalid_config` and 400), with every known key in it replaced by its label
  (`keysToLabels`). A number that is not whole or lies outside the field's
  `min`/`max` is an inline error as it is typed ("Use a number from 1 to
  10", `rangeError`); Save stays enabled but sends nothing while one is
  marked and moves focus to the first, opening its section. An odd cover
  size or an unknown template variable is a warning, and a saved one keeps
  its warning. The page reads `GET /config` when it opens,
  on Retry after a failed load and on Discard, and takes the answer to a save
  or to "Reload from file" as the new saved values;
  focus, a reconnect and changes made in another tab or in the file do not
  reload it, so a save from a long-open tab sends its edited keys over
  whatever was saved since (only those keys: the rest keep the file's
  values).
  Library sync shows `GET /library-sync` (read again after a save changes the
  switch; "On, idle" without a token) and "Start over from now" (the anchor
  reset, a `ConfirmDialog`, offered only once a check has recorded the library); Account shows
  the session service's status and, when it has a login API
  (`login_supported`), Log out (a `ConfirmDialog`: per named account, else one
  that asks for the Apple ID, because no wrapper lists its accounts) and the
  login form (with a show-password toggle) with its two-step code step, which
  starts over from the password after any failed code; a session service
  without a login API gets the command-line steps in place of the form;
  while the service cannot be reached, one line ("Can't reach the session
  service at <address>.") with Refresh; the page and its errors say "session
  service" and leave wrapper-manager to the server's own text under "Details
  from the server".
  That line replaces all of these. Then the media user token: a saved token
  is never put in the page, only "Token set · ends …a1b2" with Replace and
  Remove; without one an empty box, and "Where do I find this?" opens three
  steps; Configuration file shows
  `persisted` and "Reload from file", which reports how many
  settings changed; a file that cannot be used is one warning under the button
  (`reload_error`, also after leaving and returning, but announced as an alert
  only for a reload made on the panel): the parser's reason, the server's path
  behind a "Which file" details line.
- **Primitives**: buttons (primary, secondary, ghost, danger), segmented
  control, search field, select, chips, notices, toasts (`role="status"`; 5 s,
  8 s with an action, errors 10 s, every timer paused while the pointer or
  focus is on them; `sticky` ones stay until dismissed or the page changes;
  one raised with `offline` (a request that could not reach the server)
  also goes once the feed is live again, after 3 s up and not while the pointer
  or focus is on the toasts; the toast list is
  `ui/toasts.js`;
  `.toast` > `.icon`, `.toast-msg`, `a.toast-action`, Dismiss),
  completion toasts (`app.watchJobs(ids)`: when watched jobs end, one toast
  per 3 s burst, "Album X finished" with Open to its drawer or "3 downloads
  finished" with a link to the Queue, `lib/watch.js`; such a job is not also
  sent to the milestone region), command palette (Cmd/Ctrl+K, never over a
  modal dialog; combobox + listbox; focus handling from `useDialog`, so
  focus returns to the opener; rows from `lib/palette.js`: a query holding
  Apple Music links offers "Download this link" first, Settings sections and
  fields match by label, key or panel, job rows add status and date and
  time (`dateTime`); a
  visually hidden polite status reads the result count once the job search
  answers; under `(hover: none)` a `.palette-close` "Cancel" text button
  shows, the key hints and `.palette-foot` hide, rows are 44 px with the sub
  line under the label and the Navigate group is left out until something is
  typed), shortcut sheet
  (`?`: groups Global, Lists, New download, Settings, two flowing columns
  from 640 px (CSS columns, so a group follows the one above it), keycaps only
  for real keys, the body scrolls under its head).
- **Dialogs** (`ui/dialogs.js`): `Dialog` is a scrim plus a modal panel
  (`useDialog`: focus the first `[data-autofocus]`, trap Tab, Escape closes
  without reaching the page shortcuts, focus returns to the opener).
  `ConfirmDialog` asks before a destructive or irreversible action: an
  alertdialog with the title, the consequence, Cancel (focused) and the
  confirm button; with a third, primary choice the three share one row in
  macOS order (the destructive one alone on the left, then Cancel and the
  primary). Under 640 px it is a bottom sheet with full-width buttons:
  primary, destructive, Cancel. The DOM follows the drawn order. While busy every button is disabled and Escape does nothing (the
  Settings leave dialog is busy while its Save saves, and that button reads
  "Saving…"). The
  More sheet and the shortcut sheet are `Dialog`s. Every bottom sheet
  (`placement="bottom"`: More, Qualities and confirms on phones) is full
  width on the tab bar (to the screen edge over the drawer), with rounded top
  corners, a 36 x 4 px grabber and `overscroll-behavior: contain`. A change of page or drawer
  (a link, Back, a shortcut) closes the palette and the shell's sheets; a sheet pushes
  no history entry, so Back leaves the page rather than closing it first.
- **Queue selection** (`views/queue-select.js`, rules in `lib/jobactions.js`):
  Select (or `x` on a row) adds a checkbox column; a plain click on a row
  then selects it (Shift selects a range), Cmd/Ctrl-click still opens it.
  The bar offers only what the selection allows, with counts: Retry for
  failed or cancelled jobs (the API retries nothing else), Cancel for queued or running,
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
  link's kind at once, then a `POST /lookup` preview (a 48 px cover, the
  title with E and Apple Digital Master, one meta line): the first
  100 previewable links (one submit chunk), 4 at a time, a lookup the text no
  longer holds is aborted, answers are cached per tab (an album `?i=` link's
  only under the `catalog.album_track_url_mode` it was read with); after a
  feed resync (`app.epoch`) failed lookups run again. The first 12 rows are
  listed plus every row with an issue (blocking or a warning) or an earlier job
  still queued or running, then "Show all N"; its hint counts the hidden rows
  that are only in the library. The earlier-job search uses the id a job
  would download (`lib/lookup.js` `priorQuery`: the song for an album `?i=`
  link unless `album_track_url_mode` is `album`). Legacy
  `itunes.apple.com` links and `id`-prefixed ids read as music.apple.com
  links; a link without a storefront takes the first one another link in the
  text has, else the server's `catalog.default_storefront` (`GET /system`
  `default_storefront`, `app.defaultStorefront`), else it is flagged; two
  links glued together split at the second `http`.
  Repeats are merged and counted in the card note; a row shows the storefront
  only when it is not the server's default or was borrowed, and keeps the
  link as its tooltip. Links that are not Apple Music, not a
  downloadable kind, music videos, refused by the lookup (400), in a
  storefront Apple does not have, in a storefront the wrapper's account does not
  serve (read from `GET /wrapper/status` `regions`, polled every 30 s; unknown
  while that is not loaded, failed, not ready or in simulate mode, and then
  nothing is flagged; the submit's `unsupported_storefront` still has the last
  word), or unknown to Apple (its 404, private
  playlists included: the catalog read never carries the token) are flagged
  and not sent; the count line counts them as "can't be downloaded", and warnings
  apart. Albums, songs and playlists also ask `GET /downloads?q=<id>` (2 at a
  time, cached per tab, `lib/lookup.js` `priorJob`, kept live by the feed
  through `applyFeedToJobs`) and show "In your library · Oct 29 · Open" or
  "In the queue now · Open". Every row that can be sent has a checkbox, and
  only checked rows are sent and counted on Download; a row in the library
  or the queue starts unchecked with its cover dimmed (`isPicked`), and
  checking one in the library adds that tracks on disk are skipped unless
  Overwrite is on. The count line reads "2 links · 1 new · 1 in library". An artist row says it queues the whole discography, and a
  submit with artist links asks first. The only media-user-token is
  Settings' `catalog.media_user_token`: while it is empty a station (or a
  link whose preview says `needs_media_user_token`) gets a warning with a
  "Set it in Settings" link to `#/settings?section=catalog&field=catalog.media_user_token` (Account),
  which scrolls to the field; a private playlist downloads without one
  and uses it only for the owner's cover, and is looked up again when the
  token is set or cleared.
  Available qualities (albums and songs only) opens a read-only sheet from
  `POST /quality`: each offered slot once, then only the tracks whose formats
  differ from the album's ("All 12 tracks offer the same formats" otherwise,
  Show tracks lists them all); errors show inline with the server's message.
  The answer is kept per link for the tab, and the row then says what the
  chosen quality gets there ("Will download: Lossless (ALAC), 24-bit / 44.1
  kHz"), with a warning when its first codec is missing ("Dolby Atmos not
  offered", `lib/overrides.js` `probedQuality`). The card ends with what the
  chosen quality asks for ("Quality: Dolby Atmos, then Lossless (ALAC) up to
  24-bit / 192 kHz", `qualityText`).
  The submit bar sits under the links and sticks to the bottom of the
  viewport while they run past it (under 640 px it is fixed above the tab
  bar), and toasts rise above it so they never cover Download. Its Quality menu offers Follow Settings, Lossless (ALAC), Dolby Atmos then
  lossless, AAC, and Custom… (`lib/overrides.js` `PRESETS`; the closed menu shows
  only the name, and under 640 px it has a line to itself while Overwrite
  and Download share the next); Custom… opens a 420 px sheet (a bottom
  sheet under 640 px) with the Settings codec list editor, the fallback switch
  and the ALAC limits (while ALAC is listed), Done and Use Settings; it opens
  with its heading focused and Escape returns focus to the menu. While Custom
  is active the menu names its codecs. The Overwrite switch starts at
  `download.force_overwrite`. A submit sends only what differs from
  `GET /config` `download` as `overrides` (`presetOverrides`;
  `quality_priority` order matters), so Follow Settings with Overwrite
  untouched sends none. Until the config loads only Follow Settings can be
  chosen. The choice lasts for the tab, so the next album can follow with it.
  Download names what it sends ("Download 1 album", "Download 3 items" for
  mixed kinds; "Paste a link to start" while the box holds none, "Select a
  link to download" while none is checked;
  Cmd/Ctrl+Enter, hinted only with a fine pointer) and posts `POST /downloads` in chunks of 100, hands the
  accepted job ids to `app.watchJobs`, and the per-URL
  results replace the previous ones: accepted jobs as live Job rows (feed
  batches through `applyFeedToResults`) that open the drawer over the page,
  from 640 px up in two lines so the title and artist keep the main
  column's width (the art beside both, title and artist across the row,
  progress and status under them, no relative time; the base sets it with a
  five-class chain that outranks the skin's `.job-list-compact` rules),
  each showing its link's preview (title, artist, cover) until Apple names
  the job (`seedJob`), the rest with their reason, View job for `duplicate_active`, Retry for
  `queue_full`, `decryptor_unavailable` and server errors. The handled
  links then leave the text box; an error under Download ("Cannot reach the
  server") clears when the text is edited and after a reconnect; the rest of it stays, links typed during
  the request included, and focus moves to the results summary, right under
  the submit bar. The summary and the toast use the same words ("Started 2
  downloads", then how started jobs finished, `submitSummary`); the toast
  offers View in Queue. The whole text box lasts for the
  tab when the page is left. The results stay until Clear (which moves focus to
  the text box) or the next submit, for the life of the tab (module state in
  `views/download-results.js` `useResults`, and
  `sessionStorage["amdl.console.download.results"]` over a reload, without
  the token): coming back to the page shows them, and their jobs (at most
  25) are read again since the page missed the feed meanwhile. The Browser
  shortcut disclosure (closed, absent for a coarse pointer) shows
  `javascript:location.href='<this page>#/new?url='+encodeURIComponent(location.href)`
  as copyable text, never as a link.

## States

| Surface | Loading | Empty | Error |
| --- | --- | --- | --- |
| KPI | skeleton of final size | zeros | "—" while disconnected |
| Queue | 6 skeleton rows | per tab, with Clear filters when filtered; Active and All offer "Start a download" | inline note + Retry |
| Drawer | skeleton tracks | "No tracks resolved yet" | 404 on first load "No download with this id"; 404 after it loaded "This job was deleted"; a failed first load retries when the feed reconnects; 204 on the stream means finished |
| Showcase | skeleton hero and tiles (cover plus two text lines) | "Completed albums will appear here." with a link to the Queue and "Start a download" | inline note + Retry; broken cover shows bg color + glyph (a Showcase tile: a neutral placeholder + glyph) |
| Logs | 12 skeleton lines | "No records match" + Clear filters | the API's validation message |
| System, throughput | skeleton rows | per card | per card; a 404 endpoint shows a quiet "not available" note |
| Settings | 6 skeleton lines in the section | | inline note + Retry; a refused save keeps the draft with a notice and per-field errors; wrapper errors as notices in plain words with the server's text under "Details"; a failed load retries only with Retry |
| New download | a skeleton cover and line per link being looked up; until `GET /config` answers the Quality menu offers Follow Settings only and Overwrite waits | no count line, "Paste a link to start" on Download | a failed lookup flags its row (warning, Retry) and the link is still sent, and it runs again after a feed resync; config: a quiet note + Retry under the submit bar (also reloaded after a resync), and a submit sends no overrides; a refused submit shows a notice under the bar, `invalid_overrides` with a Custom… button |
| Feed | | | top bar: the live dot alone while connected, "Reconnecting in N s" (the wait before the next attempt); banner after 10 s; Offline (and the Disconnected banner) after 60 s with Retry; the browser's `online` event retries at once |
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
theme control is a button group with `aria-pressed` and the accessible name
"Theme". Milestones ("Album X completed") go to one
polite status region, at most one announcement per 5 s. Progress carries
`role="progressbar"` with `aria-valuetext` such as "12 of 14 tracks, 1 failed",
whether it renders cells or a bar. Text the skin adds with `::before` (section
numbers, "No.") is decoration and repeats nothing a screen reader needs.
With unsaved settings, leaving the page asks "Save changes?" and names up to
three changed fields ("Don't Save" in red text, Cancel, and Save while no
field is invalid): a page sets
`setLeaveGuard` from the app context, every hash change passes it (links,
the palette, shortcuts, Back), and a refused change is undone through the
history, so no entry is added (a pushed entry is dropped with Back, a Back
or Forward is reversed; entries carry `history.state.amdlIdx`,
`lib/route.js` `refusedMove`) while the page shows its `ConfirmDialog`;
leaving after all repeats the same move (`resumeLeave`). The browser's own
prompt covers closing the tab. Discard with more than one change asks
"Discard N changes?" (Discard, Keep Editing). Moving between settings
sections never asks.

## Data flow

`GET /downloads?limit=1` seeds counts and `event_cursor`; the feed stream then
resumes from that cursor and every page applies its batches (at most 10 a
second) to its own list (`lib/store.js`). The drawer and logs page each open one
secondary stream from their snapshot cursor. Streams pause 30 s after the tab
hides and resume from the stored cursor. When the feed is live again after
any outage, or the browser fires `online`, `app.epoch` (AppContext) goes up:
every page refetches what it loads, and a page showing a load error reloads.
Settings is the exception: it reads the config only as its component notes
say, so a reconnect never replaces values on screen.

## Verification

Run the backend with `make dev` (simulate mode), open `/console/`, and check
it at 1440 and 390 px wide in light and dark: Overview, Queue with the drawer, Showcase, Logs, System, New
download, Settings, the command palette, and under 640 px the tab bar and its
More sheet. No horizontal scroll at 390 px, status icon plus label everywhere,
visible focus rings.
