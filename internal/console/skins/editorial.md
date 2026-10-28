# Skin: editorial (Swiss editorial)

Implementation: `internal/console/static/css/skins/editorial.css`. The shared
contract (structure, tokens a skin must declare, DOM hooks, what a skin file
may do) is [DESIGN.md](../DESIGN.md#skins); this page only says what is
particular to this skin.

## Intent

A record label's catalogue page. Paper white and ink black, a strict
12-column grid, heavy grotesk display type at large sizes, huge numerals,
numbered sections with a left label column ("01 Jobs"), hairline rules
instead of boxes and cards, no shadows, no radius, generous whitespace, and
one red used only for what needs attention.

## Tokens

Contrast is measured on paper (`--bg-canvas`), light / dark.

| Token | Light | Dark | Use | Contrast |
| --- | --- | --- | --- | --- |
| `--bg-canvas`, `--bg-surface`, `--bg-chrome` | `#fbfbf8` | `#0c0c0b` | paper | |
| `--bg-raised` | `#fbfbf8` | `#121211` | drawer, palette, toasts | |
| `--bg-hover` | `#f0f0eb` | `#1a1a18` | log line hover | |
| `--bg-selected` | `#e6e6e0` | `#262624` | palette selection, skeletons | |
| `--text-primary`, `--ink`, `--border-strong` | `#0b0b0b` | `#f2f1ec` | ink; section and masthead rules | 19.0 / 17.3 |
| `--text-secondary` | `#464644` | `#bdbcb6` | ink-2: subtitles, artists | 9.1 / 10.3 |
| `--text-tertiary` (skin-local), `--neutral`, `--skipped` | `#636360` | `#94938e` | ink-3: meta, numbers, kickers | 5.8 / 6.4; 4.8 / 4.9 on `--bg-selected` |
| `--border-subtle` | `#d6d6d0` | `#2c2c2a` | hairlines (decorative only) | 1.4 |
| `--border-control` | `#8e8e89` | `#6f6e6a` | outlines that carry meaning (cells, fields) | 3.2 / 3.8 |
| `--signal` (skin-local) | `#d6001c` | `#ff3b30` | the one red | 5.2 / 5.5 |
| `--on-signal` | `#ffffff` | `#0c0c0b` | text on the red band | 5.4 / 5.5 |
| `--track` | `#e6e6e0` | `#2c2c2a` | empty meters | |

- `--accent`, `--focus`, `--brand` are ink, with `--on-ink` on them: primary
  buttons and the focus ring are ink, never red. `--accent-text` (links) is
  `--text-primary`, underlined.
- Status roles: `--info` and `--success` are ink; `--danger` and `--warning`
  are `--signal`; `--neutral` and `--skipped` are ink-3. Every `-tint` is
  `transparent`, so nothing relies on a tinted fill; the components below give
  each tinted surface a rule instead.
- `--chart-bar` is ink, `--chart-grid` is the hairline, `--scrim` is ink at
  40 % (light) or black at 72 % (dark).

## Type

System grotesk only: `--font-sans` and `--font-display` are "Helvetica Neue",
Helvetica, SF Pro, -apple-system, PingFang SC, Arial; `--font-mono` is SF Mono,
ui-monospace, Menlo, Consolas. Numbers are tabular.

| Role | Token | Size / line height | Weight, tracking |
| --- | --- | --- | --- |
| Page title | `--fs-page-title` | clamp(64px, 9.4vw, 148px) / 0.86 | 800, -0.055em |
| KPI numeral | `--fs-kpi` | clamp(64px, 6.8vw, 112px) / 0.86; 72px under 640 | 800, -0.06em |
| Showcase hero title | `--fs-hero-title` | clamp(52px, 8vw, 144px) / 0.98 (a wrapped title's descenders clear the next line's capitals) | 800, -0.06em |
| Hero artist | `--fs-hero-artist` | clamp(22px, 2.6vw, 36px) / 1.15 | 500 |
| Throughput figures | `--fs-figure` | clamp(40px, 3.9vw, 56px) / 0.92 | 800, -0.05em |
| Showcase filter tabs | `--fs-tab-lg` | 30px / 36px; 22px under 900 | 700 |
| Drawer title | `--fs-drawer-title` | 32px; 26px under 640 | 800 |
| Live queue title and fraction | `--fs-row-title` | 28px; 22px under 900 | 700 |
| Health status | `--fs-status-lg` | 26px; 22px under 640 | 700 |
| Wordmark | `--fs-brand` | 26px | 800 |
| Queue row and detected link titles | `--fs-list-title` | 20px; 17px under 640 | 700 |
| Dialog and sheet titles | `--fs-24` | 24px / 32px | 800, -0.025em |
| Section and tile titles | `--fs-card-title`, `--fs-tile-title` | 19px | 700 |
| Body | `--fs-body` | 15px | 400 |
| Kickers | `--fs-kicker` | 11px, uppercase, +0.09em | 600 |
| Section numbers, log lines, catalogue numbers | `--fs-11`/`--fs-12` mono | | 400 |

## Grid, shape, elevation, motion

- Gutter `--gutter`: 48px at 1024 and up, 24px from 640, 16px under 640.
  Column gap `--grid-gap` 24px, 12px under 900. Page rhythm `--page-gap` 48px,
  40px under 1024, 32px under 640.
- Overview and System sections at 900 px and up: a 12-column grid, the label
  column (number, title, note, "→" link) spans 2, the body spans 10. Below 900
  the label is one line above the body.
- Display line heights (`--lh-page-title`, `--lh-kpi`, `--lh-hero-title`,
  `--lh-figure`, `--lh-hero-artist`) are rounded to whole pixels where
  `round()` is supported, so the 1px rules under them stay sharp.
- No radius anywhere (`--r-*` are 0); the 7px live dot is the one round mark.
- No shadows (`--shadow-art`, `--shadow-float`, `--card-shadow` are none).
  Hierarchy is type size and weight plus rules: 3px ink (masthead, drawer bar,
  palette input), 1px ink (sections, tab rows, floating layers), hairline
  (rows, facts).
- Motion: only the base fades and slides plus the live dot's 2 s opacity
  pulse, which runs only under `prefers-reduced-motion: no-preference`. Covers
  do not lift on hover; titles underline instead.

## Components

- **Masthead** (640 px and up): the sidebar becomes a band across the top,
  wordmark "AMDL" with "Apple Music Downloader" beside it (1024 and up), the
  ink New download button, the numbered nav (`01 Overview` … `06 Settings`
  via CSS counters) and "API docs"; the top-bar controls sit at the right end
  of the wordmark's line (`.main-col` is `display: contents`, so the top bar
  and the sidebar share the grid cell). The active item has a 4px ink
  underline on the 3px rule. Below 1760 px the button, nav and API docs take
  a second line under the wordmark; from 1760 (the line needs about 1700)
  everything is one line, the controls in a grid column of their own that
  carries the rule too, so a line that grows wraps in its column instead of
  running under the controls. Under 1024 the button is an ink square with
  the plus (base rail behavior), and under 900 the nav spreads full width
  without numbers. Search, Style and Theme read as text with an underline,
  not boxes. The page under it spans the same width (no `--content-max`
  cap), so the bands and the grid end under the controls at any width.
- **Mobile** (under 640): the top bar carries the wordmark (`::before` with
  empty alt text) and the 3px rule; the bottom tab bar has a 1px ink top rule
  and a 4px ink bar over the active tab. New is a 48 x 28px ink block that
  rises over the icon line, its label on the other labels' line (current: a
  bold label, no bar, which would run into the block); More turns ink while
  its sheet is open and takes the bar on Logs, System and Settings.
  The connection word ("Live", "Retrying in 8 s") stays beside the live dot,
  which is ink whether live or connecting.
- **Banners**: full-bleed bands under the masthead, ink by default and red for
  warning and danger (the simulate band). In-page notices are a 3px rule in
  the role color with no fill.
- **Page head**: the huge title left, the page summary in a right-hand column
  behind a hairline (under 640 it drops below the title over a hairline).
  Overview's New download is a 44px ink button after the summary, on its
  bottom line; under 640 it sits under the summary on the left keyline.
- **KPIs**: glyph and label over a hairline, the bare numeral, the meta line.
  Failed turns red only when it is not zero. Total keeps its big numeral on
  phones.
- **Live queue**: 80px cover, kicker (type, genre, year, start), 28px title,
  artist; a fraction ("3 / 12 tracks") over 24px per-track cells, then a
  legend in words with a square key per shape ("1 saved · 11 remaining",
  zeros left out); a "when" column behind one unbroken hairline with the
  status and "Updated …". Failures of the last 24 h follow under a 1px ink
  rule: a red "FAILED" kicker, the title, the mono link, the error and its
  age, and a Retry button.
- **Queue list**: 64px covers, kicker, 20px title, cells, status, time; the
  filter tabs sit on a 1px ink rule with a 4px underline for the active tab.
  From 640 to 1023 the cells take a full-width line under the cover and
  title, as in the live queue, so each cell keeps its shape.
- **Kicker**: one line, cut with an ellipsis; under 900, and from 1024 to
  1279 where the lists sit in narrower columns, it drops the start time.
- **Fields** (search, select, every text, number, path, template and secret
  input) are a rule to write on, text on the keyline; focus thickens the rule
  to 2px ink instead of drawing a box. An invalid field's rule is red; a
  pinned (disabled) one is dashed with its value in ink-2. The New download
  link box is ruled paper: a hairline under every 20px line of mono text and
  the field's rule under the last.
- **Checkboxes and switches** are square: a checkbox is a control outline,
  solid ink with a paper tick when checked, a paper bar when some are
  (select all); a switch is a squared track, ink when on. Under
  `forced-colors` the checkboxes are the native ones.
- **Card actions** that are buttons (Edit, Done, Reset, Clear, Retry) read as
  semibold words that underline on hover, like the "→" links.
- **Toggle buttons** (Pause, Wrap, Reveal): a control outline at rest;
  pressed, an ink outline with a 3px ink bar along the bottom. No ink fill.
- **Cells** (status by shape): waiting is an outline, completed solid ink,
  skipped half-filled ink, failed solid red, cancelled a dashed outline. The
  same half fill marks skipped tracks in the drawer strip.
- **Status** is the base icon plus a word, set without a pill: ink for
  running, completed and healthy, ink-3 for queued, off and skipped, red for
  failed, unreachable and warnings.
- **Health**: four columns of kicker, 26px status and mono detail, then the
  runtime facts under hairlines.
- **Recently completed**: exactly one row of five whole covers (four from 640,
  2×2 under 640) by `nth-child`, whatever the base's column count.
- **Recently completed** head under 640: the "→ Showcase" link stays on the
  title line and the count drops below it.
- **Throughput**: totals as large figures over their labels; ink bars on an
  ink baseline, a 1px tick for zero days, mono axes. The formats legend uses
  square swatches.
- **Logs**: mono rows under hairlines; the level is a word, red for WRN and
  bold red for ERR, no dots or fills (the level chips keep their dots).
- **Showcase**: tabs set at 30px on an ink rule, the hero as a full-bleed band
  in the album's Apple colors (cover in columns 1–5, text in 7–12, kicker with
  the catalogue number, facts under rules at the bottom), then the numbered
  cover grid: six across, four under 1280, two under 640. The tab row drops
  its count note at every width (the page summary gives it), and an empty or
  failed tab shows its note right under the tab rule, with no second rule.
  Tile facts keep each fact whole; the separator dot that would open a line
  is clipped, so no line starts or ends with one.
- **Drawer**: ink rule on its left edge, 3px rule under the bar, 120px cover,
  32px title, times as four facts, each section under a 1px ink rule. The
  action row's first ghost button sits on the left keyline and Delete (red
  icon, ink word) ends on the right one. Tracks: the head
  keeps the "16 / 16" count and the full-width strip below is the one
  per-track picture (the head's cells are hidden); a track's stage strip
  shows only while some stage is not done.
- **Palette, sheet, toasts**: 1px ink border, no shadow; the palette input sits
  on a 3px rule and the selection is a grey band with a 4px ink bar; toasts
  have a 4px left rule, red for errors.
- **Dialogs and sheets** hang from a 3px ink rule; a confirm whose button is
  destructive hangs from a red one (`:has(.btn-danger)`). Titles are 24px
  heavy, the body 15px ink-2, Cancel an ink outline and the destructive
  button a red outline.
- **More sheet**: a band across the phone that sits on the tab bar's rule
  (3px ink top, no sides), its text on the page keyline; rows under
  hairlines, the current page the palette's grey band and 4px ink bar with a
  "CURRENT" kicker.
- **Qualities sheet**: each quality slot is a fact under a 1px ink rule (20px
  bold name, mono codec, counts); each track is a mono number, its name, a
  "LYRICS" kicker and its formats as one line of words split by hairlines,
  no boxes; a wrapped line never opens with a hairline (each format carries
  its rule and the list clips the first one of a line).
- **New download**: on the 12-column grid from 1024, links in 7 columns and
  options in 5 (8 and 4 from 1280). Detected links: 64px covers (56 under
  640), the kind, storefront, explicit and ADM as a kicker instead of chips,
  a 20px title, facts that never open a line with a dot (the tile facts'
  clip), the mono link in ink-3; a flagged link's title turns ink-2 and its
  issue is a red status line. The count line is semibold ink and Download N
  a 44px ink button. Submitted rows are the live queue's; a link that was
  not queued reads like a recent failure, a kicker in its role's color
  over the mono link. Options: groups between hairlines under ink kickers,
  the codec list ruled with mono ranks, "+ AAC" and the other codecs to add
  as words with no side padding (a wrapped line starts on the keyline), and
  the changed-value mark an ink square. Text actions (Reset to defaults,
  Refresh in an account head) put their label, not their box, on the
  keyline. The bookmarklet code
  sits between two hairlines and the steps are numbered `01` … in mono.
- **Settings**: the section list is the label column (three of twelve
  columns from 900, two from 1280), numbered `01` … in mono under the same
  ink rule as the open section, items under hairlines without icons, the
  open one bold behind a 4px ink bar; unsaved sections carry a square ink
  mark, invalid ones the red icon. Fields: 15px semibold label, ink-2 help,
  mono key in ink-3, the control on a rule at the right; "UNSAVED" is an ink
  kicker beside an underlined Undo; errors and warnings carry the 3px red
  rule. Ordered lists and the template reference are ruled, not boxed;
  panel, account and login heads are kickers over a 1px ink rule. Inline
  code (an environment variable, a config key in a note) is the mono face
  with no fill. The save bar is an ink band across the page's foot (flush on
  the tab bar under 640, also at the end of the page, where the page's
  bottom padding gives way to it), Save the paper button on it and the
  focus ring paper.
- **Queue selection**: the bar is a band between two ink rules over the
  list, opaque, sticking at the very top (the top bar does not stick here);
  its lower rule stands in for the list's. Checkboxes sit 12px in from the
  keyline, 16px from the cover (14px in a 44px target under 640), with the
  select-all box in the same column; a selected row is ink, the palette's
  grey band and 4px ink bar, never red.
- **Empty states** are left-aligned display text without an icon; error notes
  carry a 3px red rule.

## Do and don't

- Do use red only for failure, unreachable services, warnings, error logs and
  the simulate band. Running and selected are ink.
- Do pair every status color with its icon and word; never a colored dot alone.
- Do add display sizes as `--fs-…` tokens in the token block.
- Don't add boxes, fills or shadows to separate content; use a rule.
- Don't round corners, except the live dot.

## Known limits

- The mobile wordmark uses `content: "AMDL" / ""`; browsers without
  alternative-text support for `content` (Firefox before 128) drop it.
- Under 900 and from 1024 to 1279 the job kicker drops the start time on
  every row (it would be cut off); the lists show "Updated …" and the drawer
  has the times.
- Helvetica Neue and SF Pro are Apple-only; elsewhere the stack falls back to
  Arial or the system UI face, which weakens the tight display tracking.
- The progress legend says "remaining" for tracks in flight and waiting alike
  (the list payload does not tell them apart), not the mockup's "11
  downloading · 9 waiting".
- On the showcase the filter tabs stay above the hero (DOM and focus order);
  the mockup placed the hero first.
- The showcase tabs carry no counts (the view knows only the current type's
  total), the tab row has no "Sorted by …" note, and the overview KPI meta
  lines all read "of N total"; each needs copy from the view.
- The overview's "Recently completed" meta line is one string, so a wrapped
  line can still start with a separator dot.
- The one-line masthead starts at a measured width (1760 px for six nav
  items, "Live" and "Queue 2"); a longer connection label or nav makes the
  line wrap inside its column (API docs first) rather than overlap.
- The destructive confirm's red rule relies on `:has()`; without it the
  dialog keeps the ink rule.
